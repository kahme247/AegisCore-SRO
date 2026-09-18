#include "client_trace.h"
#include "core/runtime.h"
#include "diagnostics/crash_integration.h"

#include <windows.h>

#include <cstdio>
#include <cstdint>
#include <iterator>
#include <string>

namespace {

std::string JsonEscape(const std::string& value) {
    std::string escaped;
    escaped.reserve(value.size());
    for (const unsigned char character : value) {
        switch (character) {
        case '"': escaped += "\\\""; break;
        case '\\': escaped += "\\\\"; break;
        case '\b': escaped += "\\b"; break;
        case '\f': escaped += "\\f"; break;
        case '\n': escaped += "\\n"; break;
        case '\r': escaped += "\\r"; break;
        case '\t': escaped += "\\t"; break;
        default:
            if (character < 0x20) {
                char encoded[7]{};
                std::snprintf(encoded, sizeof(encoded), "\\u%04x", character);
                escaped += encoded;
            } else {
                escaped += static_cast<char>(character);
            }
        }
    }
    return escaped;
}

std::string Utf8FromWide(const wchar_t* value) {
    if (value == nullptr || *value == L'\0') {
        return {};
    }
    const int bytes = WideCharToMultiByte(CP_UTF8, 0, value, -1, nullptr, 0, nullptr, nullptr);
    if (bytes <= 1) {
        return {};
    }
    std::string result(static_cast<std::size_t>(bytes), '\0');
    WideCharToMultiByte(CP_UTF8, 0, value, -1, result.data(), bytes, nullptr, nullptr);
    result.pop_back();
    return result;
}

void AppendHex(std::string& output, std::uintptr_t value) {
    char text[2 + sizeof(value) * 2 + 1]{};
    std::snprintf(text, sizeof(text), "0x%0*llX", static_cast<int>(sizeof(value) * 2), static_cast<unsigned long long>(value));
    output += text;
}

void AppendLocation(std::string& output, std::uintptr_t address) {
    MEMORY_BASIC_INFORMATION memory{};
    const SIZE_T queried = VirtualQuery(reinterpret_cast<void*>(address), &memory, sizeof(memory));
    wchar_t modulePath[MAX_PATH]{};
    const auto module = queried == sizeof(memory) ? static_cast<HMODULE>(memory.AllocationBase) : nullptr;
    const DWORD moduleLength = module != nullptr ? GetModuleFileNameW(module, modulePath, MAX_PATH) : 0;

    output += "{\"address\":\"";
    AppendHex(output, address);
    output += "\",\"module\":";
    if (moduleLength == 0 || moduleLength >= MAX_PATH) {
        output += "null";
    } else {
        output += "\"" + JsonEscape(Utf8FromWide(modulePath)) + "\"";
    }
    output += ",\"module_offset\":";
    if (module == nullptr || address < reinterpret_cast<std::uintptr_t>(module)) {
        output += "null";
    } else {
        output += "\"";
        AppendHex(output, address - reinterpret_cast<std::uintptr_t>(module));
        output += "\"";
    }
    output += "}";
}

bool BuildDumpDirectory(wchar_t (&dumpDirectory)[MAX_PATH]) {
    wchar_t directory[MAX_PATH]{};
    const DWORD directoryLength = GetCurrentDirectoryW(MAX_PATH, directory);
    if (directoryLength == 0 || directoryLength >= MAX_PATH) {
        return false;
    }
    _snwprintf_s(dumpDirectory, MAX_PATH, _TRUNCATE, L"%s\\Dump", directory);
    CreateDirectoryW(dumpDirectory, nullptr);
    return true;
}

void WriteCrashReport(EXCEPTION_POINTERS* exceptionPointers) {
    if (exceptionPointers == nullptr || exceptionPointers->ExceptionRecord == nullptr || exceptionPointers->ContextRecord == nullptr) {
        return;
    }

    wchar_t dumpDirectory[MAX_PATH]{};
    if (!BuildDumpDirectory(dumpDirectory)) {
        return;
    }

    SYSTEMTIME now{};
    GetLocalTime(&now);
    wchar_t reportPath[MAX_PATH]{};
    _snwprintf_s(reportPath, MAX_PATH, _TRUNCATE,
        L"%s\\AegisCrash-%04u%02u%02u-%02u%02u%02u-%03u-p%lu.json",
        dumpDirectory, now.wYear, now.wMonth, now.wDay, now.wHour, now.wMinute, now.wSecond,
        now.wMilliseconds, GetCurrentProcessId());

    const auto* exception = exceptionPointers->ExceptionRecord;
    const auto* context = exceptionPointers->ContextRecord;
    std::string json = "{\n  \"schema_version\": 1,\n  \"process_id\": " + std::to_string(GetCurrentProcessId()) +
        ",\n  \"exception_code\": \"";
    AppendHex(json, exception->ExceptionCode);
    json += "\",\n  \"exception_flags\": \"";
    AppendHex(json, exception->ExceptionFlags);
    json += "\",\n  \"exception_address\": ";
    AppendLocation(json, reinterpret_cast<std::uintptr_t>(exception->ExceptionAddress));
#if defined(_M_IX86)
    json += ",\n  \"registers\": {\"eax\": \"";
    AppendHex(json, context->Eax);
    json += "\", \"ebx\": \""; AppendHex(json, context->Ebx);
    json += "\", \"ecx\": \""; AppendHex(json, context->Ecx);
    json += "\", \"edx\": \""; AppendHex(json, context->Edx);
    json += "\", \"esi\": \""; AppendHex(json, context->Esi);
    json += "\", \"edi\": \""; AppendHex(json, context->Edi);
    json += "\", \"ebp\": \""; AppendHex(json, context->Ebp);
    json += "\", \"esp\": \""; AppendHex(json, context->Esp);
    json += "\", \"eip\": \""; AppendHex(json, context->Eip);
    json += "\"}";
#endif

    void* frames[32]{};
    const USHORT frameCount = CaptureStackBackTrace(0, static_cast<DWORD>(std::size(frames)), frames, nullptr);
    json += ",\n  \"filter_stack\": [";
    for (USHORT index = 0; index < frameCount; ++index) {
        if (index != 0) {
            json += ',';
        }
        json += "\n    ";
        AppendLocation(json, reinterpret_cast<std::uintptr_t>(frames[index]));
    }
    if (frameCount != 0) {
        json += '\n';
    }
    json += "  ]\n}\n";

    HANDLE report = CreateFileW(reportPath, GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (report == INVALID_HANDLE_VALUE) {
        return;
    }
    DWORD written = 0;
    WriteFile(report, json.data(), static_cast<DWORD>(json.size()), &written, nullptr);
    CloseHandle(report);
}

LPTOP_LEVEL_EXCEPTION_FILTER g_clientExceptionFilter = nullptr;
using SetUnhandledExceptionFilterFn = LPTOP_LEVEL_EXCEPTION_FILTER(WINAPI*)(LPTOP_LEVEL_EXCEPTION_FILTER);
SetUnhandledExceptionFilterFn g_realSetUnhandledExceptionFilter = nullptr;
volatile LONG g_flushingCrashTrace = 0;

void FlushCrashTrace() {
    if (InterlockedCompareExchange(&g_flushingCrashTrace, 1, 0) != 0) {
        return;
    }

    wchar_t directory[MAX_PATH]{};
    const DWORD directoryLength = GetCurrentDirectoryW(MAX_PATH, directory);
    if (directoryLength == 0 || directoryLength >= MAX_PATH) {
        return;
    }

    wchar_t dumpDirectory[MAX_PATH]{};
    _snwprintf_s(dumpDirectory, sizeof(dumpDirectory) / sizeof(dumpDirectory[0]), _TRUNCATE, L"%s\\Dump", directory);
    CreateDirectoryW(dumpDirectory, nullptr);

    SYSTEMTIME now{};
    GetLocalTime(&now);
    wchar_t tracePath[MAX_PATH]{};
    _snwprintf_s(
        tracePath,
        sizeof(tracePath) / sizeof(tracePath[0]),
        _TRUNCATE,
        L"%s\\AegisTrace-%04u%02u%02u-%02u%02u%02u.actrace",
        dumpDirectory,
        now.wYear,
        now.wMonth,
        now.wDay,
        now.wHour,
        now.wMinute,
        now.wSecond);
    aegis::trace::Flush(tracePath);
}

void WriteLoadMarker() {
    wchar_t directory[MAX_PATH]{};
    const DWORD directoryLength = GetCurrentDirectoryW(MAX_PATH, directory);
    if (directoryLength == 0 || directoryLength >= MAX_PATH) {
        return;
    }
    wchar_t dumpDirectory[MAX_PATH]{};
    _snwprintf_s(dumpDirectory, sizeof(dumpDirectory) / sizeof(dumpDirectory[0]), _TRUNCATE, L"%s\\Dump", directory);
    CreateDirectoryW(dumpDirectory, nullptr);
    wchar_t markerPath[MAX_PATH]{};
    _snwprintf_s(markerPath, sizeof(markerPath) / sizeof(markerPath[0]), _TRUNCATE, L"%s\\Aegis.loaded", dumpDirectory);
    HANDLE marker = CreateFileW(markerPath, GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (marker != INVALID_HANDLE_VALUE) {
        static constexpr char message[] = "Aegis client runtime loaded\r\n";
        DWORD written = 0;
        WriteFile(marker, message, sizeof(message) - 1, &written, nullptr);
        CloseHandle(marker);
    }
}

LONG WINAPI DiagnosticExceptionFilter(EXCEPTION_POINTERS* exceptionPointers) {
    WriteCrashReport(exceptionPointers);
    FlushCrashTrace();
    return g_clientExceptionFilter != nullptr ? g_clientExceptionFilter(exceptionPointers) : EXCEPTION_EXECUTE_HANDLER;
}

LPTOP_LEVEL_EXCEPTION_FILTER WINAPI RegisterClientExceptionFilter(LPTOP_LEVEL_EXCEPTION_FILTER clientFilter) {
    g_clientExceptionFilter = clientFilter;
    return g_realSetUnhandledExceptionFilter(DiagnosticExceptionFilter);
}

void InstallCrashIntegrationImpl() {
    // The pinned client calls this import slot at 0x008328F0 during startup.
    // The launcher injects this DLL before resume, so patch the IAT now and
    // retain the client filter when it registers a moment later.
    auto* importSlot = reinterpret_cast<void**>(0x00D801F8);
    g_realSetUnhandledExceptionFilter = reinterpret_cast<SetUnhandledExceptionFilterFn>(*importSlot);
    if (g_realSetUnhandledExceptionFilter == nullptr) {
        return;
    }

    DWORD previousProtection = 0;
    if (VirtualProtect(importSlot, sizeof(*importSlot), PAGE_READWRITE, &previousProtection)) {
        *importSlot = reinterpret_cast<void*>(&RegisterClientExceptionFilter);
        DWORD ignoredProtection = 0;
        VirtualProtect(importSlot, sizeof(*importSlot), previousProtection, &ignoredProtection);
        FlushInstructionCache(GetCurrentProcess(), importSlot, sizeof(*importSlot));
    }

    // This also supports manually arming an already-running client. On an
    // early launcher injection the later intercepted client registration
    // replaces this fallback with the client's original filter.
    g_clientExceptionFilter = g_realSetUnhandledExceptionFilter(DiagnosticExceptionFilter);
    WriteLoadMarker();
}

} // namespace

namespace aegis::diagnostics {

void InstallCrashIntegration() {
    InstallCrashIntegrationImpl();
}

} // namespace aegis::diagnostics

extern "C" __declspec(dllexport) void __stdcall AegisTraceRecordPacket(
    std::uint8_t direction,
    std::uint8_t phase,
    std::uint16_t opcode,
    const void* payload,
    std::uint32_t payloadLength) {
    aegis::trace::RecordPacket(
        static_cast<aegis::trace::Direction>(direction),
        static_cast<aegis::trace::Phase>(phase),
        opcode,
        payload,
        payloadLength);
}

extern "C" __declspec(dllexport) BOOL __stdcall AegisTraceFlush(const wchar_t* outputPath) {
    return aegis::trace::Flush(outputPath) ? TRUE : FALSE;
}

BOOL WINAPI DllMain(HINSTANCE module, DWORD reason, LPVOID) {
    if (reason == DLL_PROCESS_ATTACH) {
        return aegis::core::Initialize(module) ? TRUE : FALSE;
    }
    return TRUE;
}
