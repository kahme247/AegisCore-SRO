#include "compat/navigation_policy.h"

#include <windows.h>

#include <array>
#include <cstdint>
#include <cstring>

namespace aegis::compat {
namespace {

struct HidePatch {
    std::uintptr_t virtualSlotRva;
    std::uintptr_t firstArgumentRva;
    std::uintptr_t visibleArgumentRva;
    std::uint8_t expectedFirstPush;
    std::uint8_t expectedVisiblePush;
};

constexpr std::array<HidePatch, 2> kCharacterSelectionControls = {{
    {0x45EE70, 0x45EE82, 0x45EE87, 0x51, 0x51}, // GDR_BTN_BACK
    {0x45EF26, 0x45EF2D, 0x45EF2E, 0x51, 0x57}, // GDR_BTN_CANCEL
}};

constexpr std::uintptr_t kCharacterSelectCreateMoveRva = 0x45EE58;
constexpr std::array<std::uint8_t, 9> kExpectedCreateMove = {
    0x83, 0xC1, 0x1B, 0x51, 0x57, 0x8B, 0xC8, 0xFF, 0xD2,
};

constexpr std::uintptr_t kSystemMenuDynamicButtonRva = 0x2F6868;
constexpr std::array<std::uint8_t, 25> kExpectedSystemMenuShow = {
    0x0F, 0xB6, 0x4B, 0x0D, 0x6A, 0x01, 0x51, 0x8B, 0xCF,
    0xE8, 0x7A, 0xE9, 0x1B, 0x00, 0x8B, 0x10, 0x8B, 0xC8,
    0x8B, 0x42, 0x5C, 0x6A, 0x01, 0xFF, 0xD0,
};

__declspec(naked) void MoveCreateToFormerCancelPosition() {
    __asm {
        add ecx, 0x1b
        lea edx, [edi + 0x68]
        push ecx
        push edx
        mov ecx, eax
        mov edx, [eax]
        call dword ptr [edx + 0x98]
        ret
    }
}

__declspec(naked) void ApplySystemMenuButtonVisibility() {
    __asm {
        movzx ecx, byte ptr [ebx + 0x0d]
        push 1
        push ecx
        mov ecx, edi
        mov eax, 0x008b51f0
        call eax
        mov edx, [eax]
        mov ecx, eax
        mov eax, [edx + 0x5c]
        xor edx, edx
        cmp byte ptr [ebx + 0x0d], 0x0d
        setne dl
        push edx
        call eax
        ret
    }
}

bool PatchVerifiedBytes(std::uint8_t* address,
                        const std::uint8_t* expected,
                        const std::uint8_t* replacement,
                        std::size_t length) {
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

bool PatchRelativeCall(std::uint8_t* address, const std::uint8_t* expected,
                       std::size_t length, const void* target) {
    if (length < 5 || address == nullptr || target == nullptr ||
        std::memcmp(address, expected, length) != 0) {
        return false;
    }

    std::array<std::uint8_t, 32> replacement{};
    if (length > replacement.size()) {
        return false;
    }
    replacement.fill(0x90);
    replacement[0] = 0xE8;
    const auto displacement = static_cast<std::int32_t>(
        reinterpret_cast<const std::uint8_t*>(target) - (address + 5));
    std::memcpy(replacement.data() + 1, &displacement, sizeof(displacement));
    return PatchVerifiedBytes(address, expected, replacement.data(), length);
}

} // namespace

bool InstallNavigationPolicy() {
    auto* imageBase = reinterpret_cast<std::uint8_t*>(GetModuleHandleW(nullptr));
    if (imageBase == nullptr) {
        return false;
    }

    constexpr std::uint8_t kMoveGWndSlot = 0x98;
    constexpr std::uint8_t kShowGWndSlot = 0x5C;
    constexpr std::uint8_t kPushEbx = 0x53; // EBX is zero throughout OnCreate.
    constexpr std::uint8_t kNop = 0x90;

    bool installed = true;
    installed = PatchRelativeCall(imageBase + kCharacterSelectCreateMoveRva,
                                  kExpectedCreateMove.data(), kExpectedCreateMove.size(),
                                  reinterpret_cast<const void*>(&MoveCreateToFormerCancelPosition)) && installed;
    installed = PatchRelativeCall(imageBase + kSystemMenuDynamicButtonRva,
                                  kExpectedSystemMenuShow.data(), kExpectedSystemMenuShow.size(),
                                  reinterpret_cast<const void*>(&ApplySystemMenuButtonVisibility)) && installed;
    for (const HidePatch& patch : kCharacterSelectionControls) {
        installed = PatchVerifiedBytes(imageBase + patch.virtualSlotRva,
                                       &kMoveGWndSlot, &kShowGWndSlot, 1) && installed;
        installed = PatchVerifiedBytes(imageBase + patch.firstArgumentRva,
                                       &patch.expectedFirstPush, &kNop, 1) && installed;
        installed = PatchVerifiedBytes(imageBase + patch.visibleArgumentRva,
                                       &patch.expectedVisiblePush, &kPushEbx, 1) && installed;
    }
    return installed;
}

} // namespace aegis::compat
