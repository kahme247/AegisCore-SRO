#include "compat/temporary_enhancement_tooltip.h"

#include <windows.h>

#include <array>
#include <cstdint>
#include <cstdio>
#include <cstring>

namespace aegis::compat {
namespace {

constexpr std::uintptr_t kAppendTooltipLineRva = 0x376D20;
constexpr std::uintptr_t kItemTimedStateRva = 0x4BA580;
constexpr std::uintptr_t kItemRemainingTimeRva = 0x4BA5B0;
constexpr std::uintptr_t kWideStringConstructRva = 0x07E880;
constexpr std::uintptr_t kWideStringDestroyRva = 0x06E380;
constexpr std::uintptr_t kItemUpdateKindGateRva = 0x48C6A4;
constexpr std::array<std::uint8_t, 5> kExpectedItemUpdateKindGate = {0x80, 0xFB, 0x0D, 0x72, 0x75};
constexpr std::uintptr_t kInventoryItemLookupResultRva = 0x48BFFD;
constexpr std::array<std::uint8_t, 8> kExpectedInventoryItemLookupResult = {
    0x8B, 0xF8, 0x8B, 0xCF, 0x89, 0x7C, 0x24, 0x38,
};


using AppendTooltipLineFn = char(__thiscall*)(void*, void*, int, int, char, int);
using TimedStateFn = std::uint8_t(__thiscall*)(void*);
using RemainingTimeFn = std::int64_t(__thiscall*)(void*);
using WideStringConstructFn = void(__thiscall*)(void*, const wchar_t*);
using WideStringDestroyFn = void(__thiscall*)(void*);

AppendTooltipLineFn g_appendTooltipLine = nullptr;
TimedStateFn g_timedState = nullptr;
RemainingTimeFn g_remainingTime = nullptr;
WideStringConstructFn g_constructWideString = nullptr;
WideStringDestroyFn g_destroyWideString = nullptr;
void* g_lastOwner = nullptr;
DWORD g_lastAppendTick = 0;
bool g_appendingTimer = false;

bool PatchBytes(void* address, const void* expected, const void* replacement, std::size_t length) {
    if (address == nullptr || std::memcmp(address, expected, length) != 0) {
        return false;
    }
    DWORD previousProtection = 0;
    if (!VirtualProtect(address, length, PAGE_EXECUTE_READWRITE, &previousProtection)) {
        return false;
    }
    std::memcpy(address, replacement, length);
    FlushInstructionCache(GetCurrentProcess(), address, length);
    DWORD ignoredProtection = 0;
    VirtualProtect(address, length, previousProtection, &ignoredProtection);
    return true;
}
bool PatchRelativeJump(std::uint8_t* address, const std::uint8_t* expected, std::size_t length, const void* target) {
    if (length < 5 || length > 16) {
        return false;
    }
    std::array<std::uint8_t, 16> replacement{};
    replacement.fill(0x90);
    replacement[0] = 0xE9;
    const auto displacement = static_cast<std::int32_t>(
        reinterpret_cast<const std::uint8_t*>(target) - (address + 5));
    std::memcpy(replacement.data() + 1, &displacement, sizeof(displacement));
    return PatchBytes(address, expected, replacement.data(), length);
}

__declspec(naked) void UpdateEquippedItemExpiration() {
    __asm {
        cmp bl, 0x0d
        jae inventory_item

        movzx edi, bl
        push edi
        mov ecx, ebp
        mov eax, 0x006ab6e0
        call eax
        test eax, eax
        jz missing_equipped_item

        push eax
        lea ecx, [esp + 0x40]
        mov eax, 0x00579da0
        call eax

        mov eax, [esp + 0x28]
        mov ecx, 1000
        mul ecx
        push edx
        push eax
        lea ecx, [esp + 0x44]
        mov eax, 0x008bbe10
        call eax

        lea eax, [esp + 0x3c]
        push eax
        push edi
        mov ecx, ebp
        mov eax, 0x006ae1b0
        call eax

        mov eax, 0x0088c71e
        jmp eax

    missing_equipped_item:
        mov eax, 0x0088c71e
        jmp eax

    inventory_item:
        mov eax, 0x0088c6a9
        jmp eax
    }
}

bool InstallItemUpdateKindGate(std::uint8_t* imageBase) {
    return PatchRelativeJump(
        imageBase + kItemUpdateKindGateRva,
        kExpectedItemUpdateKindGate.data(),
        kExpectedItemUpdateKindGate.size(),
        reinterpret_cast<const void*>(&UpdateEquippedItemExpiration));
}

__declspec(naked) void GuardInventoryItemLookupResult() {
    __asm {
        test eax, eax
        jz missing_item
        mov edi, eax
        mov ecx, edi
        mov [esp + 0x38], edi
        mov eax, 0x0088c005
        jmp eax

    missing_item:
        mov eax, 0x0088c71e
        jmp eax
    }
}

bool InstallInventoryItemLookupGuard(std::uint8_t* imageBase) {
    return PatchRelativeJump(
        imageBase + kInventoryItemLookupResultRva,
        kExpectedInventoryItemLookupResult.data(),
        kExpectedInventoryItemLookupResult.size(),
        reinterpret_cast<const void*>(&GuardInventoryItemLookupResult));
}



bool IsReadableRange(const void* address, std::size_t length) {
    if (address == nullptr || length == 0) {
        return false;
    }
    MEMORY_BASIC_INFORMATION memory{};
    if (VirtualQuery(address, &memory, sizeof(memory)) != sizeof(memory) ||
        memory.State != MEM_COMMIT ||
        (memory.Protect & (PAGE_GUARD | PAGE_NOACCESS)) != 0) {
        return false;
    }
    const auto start = reinterpret_cast<std::uintptr_t>(address);
    const auto regionEnd = reinterpret_cast<std::uintptr_t>(memory.BaseAddress) + memory.RegionSize;
    return start <= regionEnd && length <= regionEnd - start;
}

std::int64_t UnixTimeMilliseconds() {
    FILETIME fileTime{};
    GetSystemTimeAsFileTime(&fileTime);
    ULARGE_INTEGER ticks{};
    ticks.LowPart = fileTime.dwLowDateTime;
    ticks.HighPart = fileTime.dwHighDateTime;
    constexpr std::uint64_t kWindowsToUnixEpochTicks = 116444736000000000ULL;
    return static_cast<std::int64_t>((ticks.QuadPart - kWindowsToUnixEpochTicks) / 10000ULL);
}

void FormatRemainingTime(std::int64_t milliseconds, wchar_t (&output)[96]) {
    const auto totalMinutes = milliseconds / 60000;
    const auto days = totalMinutes / (24 * 60);
    const auto hours = (totalMinutes / 60) % 24;
    const auto minutes = totalMinutes % 60;
    if (days > 0) {
        _snwprintf_s(output, _TRUNCATE, L"Temporary enhancement: %lldd %02lldh %02lldm remaining", days, hours, minutes);
    } else {
        _snwprintf_s(output, _TRUNCATE, L"Temporary enhancement: %02lldh %02lldm remaining", hours, minutes);
    }
}

char __fastcall AppendTooltipLineHook(void* helper, void*, void* text, int line, int color, char indented, int style) {
    const char result = g_appendTooltipLine(helper, text, line, color, indented, style);
    if (g_appendingTimer || helper == nullptr) {
        return result;
    }

    auto* owner = *reinterpret_cast<void**>(static_cast<std::uint8_t*>(helper) + 0x7DC);
    const DWORD now = GetTickCount();
    if (owner == nullptr || (owner == g_lastOwner && now - g_lastAppendTick < 500)) {
        return result;
    }
    g_lastOwner = owner;
    g_lastAppendTick = now;

    if (!IsReadableRange(owner, 0x394)) {
        return result;
    }
    auto* item = *reinterpret_cast<void**>(static_cast<std::uint8_t*>(owner) + 0x390);
    if (!IsReadableRange(item, 0x68)) {
        return result;
    }
    if (g_timedState(item) != 1) {
        return result;
    }
    const std::int64_t remaining = g_remainingTime(item) - UnixTimeMilliseconds();
    if (remaining <= 0) {
        return result;
    }

    wchar_t timerText[96]{};
    FormatRemainingTime(remaining, timerText);
    alignas(4) std::uint8_t clientString[28]{};
    g_constructWideString(clientString, timerText);
    g_appendingTimer = true;
    g_appendTooltipLine(helper, clientString, 0, 0x00FFD060, 0, 1);
    g_appendingTimer = false;
    g_destroyWideString(clientString);
    return result;
}

bool InstallAppendHook(std::uint8_t* imageBase) {
    auto* target = imageBase + kAppendTooltipLineRva;
    constexpr std::size_t stolenLength = 7;
    constexpr std::array<std::uint8_t, stolenLength> expected = {0x6A, 0xFF, 0x68, 0xE8, 0x82, 0xD1, 0x00};
    if (std::memcmp(target, expected.data(), expected.size()) != 0) {
        return false;
    }

    auto* trampoline = static_cast<std::uint8_t*>(VirtualAlloc(nullptr, stolenLength + 5, MEM_COMMIT | MEM_RESERVE, PAGE_EXECUTE_READWRITE));
    if (trampoline == nullptr) {
        return false;
    }
    std::memcpy(trampoline, target, stolenLength);
    trampoline[stolenLength] = 0xE9;
    const auto returnDisplacement = static_cast<std::int32_t>((target + stolenLength) - (trampoline + stolenLength + 5));
    std::memcpy(trampoline + stolenLength + 1, &returnDisplacement, sizeof(returnDisplacement));
    g_appendTooltipLine = reinterpret_cast<AppendTooltipLineFn>(trampoline);

    std::array<std::uint8_t, stolenLength> replacement{};
    replacement.fill(0x90);
    replacement[0] = 0xE9;
    const auto hookDisplacement = static_cast<std::int32_t>(reinterpret_cast<std::uint8_t*>(&AppendTooltipLineHook) - (target + 5));
    std::memcpy(replacement.data() + 1, &hookDisplacement, sizeof(hookDisplacement));
    return PatchBytes(target, expected.data(), replacement.data(), replacement.size());
}

} // namespace

bool InstallTemporaryEnhancementTooltip() {
    auto* imageBase = reinterpret_cast<std::uint8_t*>(GetModuleHandleW(nullptr));
    if (imageBase == nullptr) {
        return false;
    }
    g_timedState = reinterpret_cast<TimedStateFn>(imageBase + kItemTimedStateRva);
    g_remainingTime = reinterpret_cast<RemainingTimeFn>(imageBase + kItemRemainingTimeRva);
    g_constructWideString = reinterpret_cast<WideStringConstructFn>(imageBase + kWideStringConstructRva);
    g_destroyWideString = reinterpret_cast<WideStringDestroyFn>(imageBase + kWideStringDestroyRva);
    return InstallInventoryItemLookupGuard(imageBase) &&
           InstallItemUpdateKindGate(imageBase) &&
           InstallAppendHook(imageBase);
}

} // namespace aegis::compat
