#include "compat/debug_render_commands.h"

#include <windows.h>

#include <array>
#include <cstdint>
#include <cstring>
#include <cwchar>

namespace aegis::compat {
namespace {

constexpr std::uintptr_t kChatDisplayAddress = 0x00876810;
constexpr std::uintptr_t kMapRenderAddress = 0x00A26F60;
constexpr std::uintptr_t kCharacterListAddress = 0x00EC4CEC;
constexpr std::uintptr_t kRenderContextAddress = 0x0110F7F8;
constexpr std::uintptr_t kCharacterFadeAddress = 0x00A5AC80;
constexpr std::size_t kGroundVisibleOffset = 11932;
constexpr std::size_t kSkyVisibleOffset = 11936;
constexpr std::size_t kMapObjectsVisibleOffset = 11944;
constexpr wchar_t kDirectivePrefix[] = L"@AegisRender:";

constexpr std::array<std::uint8_t, 7> kExpectedChatDisplay = {
    0x6A, 0xFF, 0x68, 0xE3, 0x82, 0xD2, 0x00,
};
constexpr std::array<std::uint8_t, 6> kExpectedMapRender = {
    0x83, 0xEC, 0x5C, 0x53, 0x6A, 0x00,
};

using ChatDisplayFn = void(__stdcall*)(std::uint8_t, void*, int, void*, std::uint8_t);
using CharacterFadeFn = void(__thiscall*)(void*, int, float);

ChatDisplayFn g_chatDisplay = nullptr;
std::uintptr_t g_mapRenderContinue = kMapRenderAddress + kExpectedMapRender.size();
void* volatile g_map = nullptr;

struct WideStringView {
    const wchar_t* data;
    std::size_t size;
};

WideStringView ReadWideString(const void* value) {
    if (value == nullptr) {
        return {};
    }
    const auto* bytes = static_cast<const std::uint8_t*>(value);
    const auto capacity = *reinterpret_cast<const std::uint32_t*>(bytes + 24);
    const auto size = *reinterpret_cast<const std::uint32_t*>(bytes + 20);
    const auto* data = capacity < 8
        ? reinterpret_cast<const wchar_t*>(bytes + 4)
        : *reinterpret_cast<const wchar_t* const*>(bytes + 4);
    if (data == nullptr || size > 256) {
        return {};
    }
    return {data, size};
}

bool ToggleMapFlag(std::size_t offset) {
    auto* map = static_cast<std::uint8_t*>(InterlockedCompareExchangePointer(&g_map, nullptr, nullptr));
    if (map == nullptr) {
        return false;
    }
    auto* flag = reinterpret_cast<std::uint32_t*>(map + offset);
    *flag = *flag == 0 ? 1u : 0u;
    return true;
}

void ToggleCharacters() {
    static bool visible = true;
    visible = !visible;
    const float targetAlpha = visible ? 1.0f : 0.0f;
    const int renderContext = *reinterpret_cast<const int*>(kRenderContextAddress);
    const auto fade = reinterpret_cast<CharacterFadeFn>(kCharacterFadeAddress);

    std::uintptr_t node = *reinterpret_cast<const std::uintptr_t*>(kCharacterListAddress);
    while (node != 0) {
        const std::uintptr_t next = *reinterpret_cast<const std::uintptr_t*>(node + 12);
        auto* character = reinterpret_cast<std::uint8_t*>(node - 628);
        auto* visual = *reinterpret_cast<void**>(character + 156);
        if (visual != nullptr) {
            fade(visual, renderContext, targetAlpha);
        }
        node = next;
    }
}

bool HandleDirective(void* message) {
    const WideStringView text = ReadWideString(message);
    constexpr std::size_t prefixLength = (sizeof(kDirectivePrefix) / sizeof(kDirectivePrefix[0])) - 1;
    if (text.size <= prefixLength || std::wmemcmp(text.data, kDirectivePrefix, prefixLength) != 0) {
        return false;
    }
    const wchar_t* command = text.data + prefixLength;
    const std::size_t commandLength = text.size - prefixLength;
    if (commandLength == 3 && std::wmemcmp(command, L"sky", 3) == 0) {
        return ToggleMapFlag(kSkyVisibleOffset);
    }
    if (commandLength == 6 && std::wmemcmp(command, L"ground", 6) == 0) {
        return ToggleMapFlag(kGroundVisibleOffset);
    }
    if (commandLength == 6 && std::wmemcmp(command, L"mapobj", 6) == 0) {
        return ToggleMapFlag(kMapObjectsVisibleOffset);
    }
    if (commandLength == 4 && std::wmemcmp(command, L"char", 4) == 0) {
        ToggleCharacters();
        return true;
    }
    return false;
}

void __stdcall ChatDisplayHook(std::uint8_t type, void* sender, int senderId, void* message, std::uint8_t local) {
    if (!HandleDirective(message)) {
        g_chatDisplay(type, sender, senderId, message, local);
    }
}

void __cdecl CaptureMap(void* map) {
    InterlockedExchangePointer(&g_map, map);
}

__declspec(naked) void MapRenderHook() {
    __asm {
        pushfd
        pushad
        push ecx
        call CaptureMap
        add esp, 4
        popad
        popfd
        sub esp, 5Ch
        push ebx
        push 0
        jmp dword ptr [g_mapRenderContinue]
    }
}

bool WriteJump(void* source, const void* destination, std::size_t length) {
    if (source == nullptr || destination == nullptr || length < 5) {
        return false;
    }
    DWORD oldProtect = 0;
    if (!VirtualProtect(source, length, PAGE_EXECUTE_READWRITE, &oldProtect)) {
        return false;
    }
    auto* bytes = static_cast<std::uint8_t*>(source);
    bytes[0] = 0xE9;
    *reinterpret_cast<std::int32_t*>(bytes + 1) = static_cast<std::int32_t>(
        reinterpret_cast<const std::uint8_t*>(destination) - bytes - 5);
    std::memset(bytes + 5, 0x90, length - 5);
    FlushInstructionCache(GetCurrentProcess(), source, length);
    DWORD ignored = 0;
    VirtualProtect(source, length, oldProtect, &ignored);
    return true;
}

template <std::size_t N>
void* InstallTrampoline(std::uintptr_t address, const std::array<std::uint8_t, N>& expected, const void* hook) {
    auto* target = reinterpret_cast<std::uint8_t*>(address);
    if (std::memcmp(target, expected.data(), expected.size()) != 0) {
        return nullptr;
    }
    auto* trampoline = static_cast<std::uint8_t*>(VirtualAlloc(
        nullptr, expected.size() + 5, MEM_COMMIT | MEM_RESERVE, PAGE_EXECUTE_READWRITE));
    if (trampoline == nullptr) {
        return nullptr;
    }
    std::memcpy(trampoline, expected.data(), expected.size());
    trampoline[expected.size()] = 0xE9;
    *reinterpret_cast<std::int32_t*>(trampoline + expected.size() + 1) = static_cast<std::int32_t>(
        target + expected.size() - (trampoline + expected.size() + 5));
    if (!WriteJump(target, hook, expected.size())) {
        VirtualFree(trampoline, 0, MEM_RELEASE);
        return nullptr;
    }
    return trampoline;
}

} // namespace

bool InstallDebugRenderCommands() {
#if !defined(_M_IX86)
    return false;
#else
    g_chatDisplay = reinterpret_cast<ChatDisplayFn>(
        InstallTrampoline(kChatDisplayAddress, kExpectedChatDisplay, &ChatDisplayHook));
    if (g_chatDisplay == nullptr) {
        return false;
    }
    auto* mapTarget = reinterpret_cast<void*>(kMapRenderAddress);
    if (std::memcmp(mapTarget, kExpectedMapRender.data(), kExpectedMapRender.size()) != 0 ||
        !WriteJump(mapTarget, &MapRenderHook, kExpectedMapRender.size())) {
        return false;
    }
    return true;
#endif
}

} // namespace aegis::compat
