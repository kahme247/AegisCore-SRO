#include "network/gateway_login.h"

#include "network/character_auto_select.h"
#include "auth/launch_context.h"

#include <windows.h>

#include <cstdint>
#include <cstdio>
#include <cstring>

namespace aegis::network {
namespace {

constexpr std::uintptr_t kGatewayService = 0x0115E4A8;
constexpr std::uintptr_t kGatewayStatePointer = 0x0115B0BC;
constexpr std::uintptr_t kGatewayVtable = 0x00E1B24C;
constexpr std::uintptr_t kGatewayLoginMethod = 0x004C9290;
constexpr UINT_PTR kLoginTimerId = 0xA36102;
constexpr UINT kLoginTimerMilliseconds = 100;
constexpr unsigned kMaximumTimerTicks = 250;
constexpr unsigned char kLoginSignature[] = {
    0xA1, 0xBC, 0xB0, 0x15, 0x01, 0x83, 0xB8, 0xFC,
    0x00, 0x00, 0x00, 0x00, 0x53, 0x8B, 0xD9, 0x75,
};

using GatewayLoginFn = bool(__thiscall*)(void*, const char*, const char*, std::uint16_t, std::uint32_t);
unsigned g_timerTicks = 0;

bool MatchesPinnedClient() {
    MEMORY_BASIC_INFORMATION memory{};
    if (VirtualQuery(reinterpret_cast<const void*>(kGatewayLoginMethod), &memory, sizeof(memory)) != sizeof(memory) ||
        memory.State != MEM_COMMIT || (memory.Protect & (PAGE_EXECUTE | PAGE_EXECUTE_READ | PAGE_EXECUTE_READWRITE | PAGE_EXECUTE_WRITECOPY)) == 0) {
        return false;
    }
    return std::memcmp(reinterpret_cast<const void*>(kGatewayLoginMethod), kLoginSignature, sizeof(kLoginSignature)) == 0;
}

bool GatewayReady() {
    const auto serviceVtable = *reinterpret_cast<const std::uintptr_t*>(kGatewayService);
    if (serviceVtable != kGatewayVtable) {
        return false;
    }
    const auto* state = *reinterpret_cast<const std::uint8_t* const*>(kGatewayStatePointer);
    if (state == nullptr || *reinterpret_cast<const std::uint32_t*>(state + 0xFC) == 0) {
        return false;
    }
    const auto phase = *reinterpret_cast<const std::uint32_t*>(state + 0x28);
    return phase == 2 || phase == 3;
}

void CALLBACK LoginTimer(HWND window, UINT, UINT_PTR timerId, DWORD) {
    if (++g_timerTicks >= kMaximumTimerTicks) {
        KillTimer(window, timerId);
        auth::Discard();
        return;
    }
    if (!GatewayReady()) {
        return;
    }
    KillTimer(window, timerId);

    auth::LaunchContext context{};
    if (!auth::Take(context)) {
        return;
    }
    character_auto_select::Schedule(context.characterName);
    char tokenPassword[auth::kTokenBytes + 5]{};
    const int written = _snprintf_s(tokenPassword, sizeof(tokenPassword), _TRUNCATE, "@A1:%s", context.token);
    if (written > 4) {
        const auto login = reinterpret_cast<GatewayLoginFn>(kGatewayLoginMethod);
        login(reinterpret_cast<void*>(kGatewayService), context.username, tokenPassword, context.shardId, 0);
    }
    SecureZeroMemory(tokenPassword, sizeof(tokenPassword));
    SecureZeroMemory(&context, sizeof(context));
}

BOOL CALLBACK FindProcessWindow(HWND window, LPARAM parameter) {
    DWORD processId = 0;
    GetWindowThreadProcessId(window, &processId);
    if (processId != GetCurrentProcessId() || GetWindow(window, GW_OWNER) != nullptr) {
        return TRUE;
    }
    *reinterpret_cast<HWND*>(parameter) = window;
    return FALSE;
}

DWORD WINAPI ScheduleLogin(void*) {
    if (!MatchesPinnedClient()) {
        auth::Discard();
        return 0;
    }
    HWND window = nullptr;
    for (unsigned attempt = 0; attempt < 100 && window == nullptr; ++attempt) {
        EnumWindows(FindProcessWindow, reinterpret_cast<LPARAM>(&window));
        if (window == nullptr) {
            Sleep(100);
        }
    }
    if (window == nullptr || SetTimer(window, kLoginTimerId, kLoginTimerMilliseconds, LoginTimer) == 0) {
        auth::Discard();
    }
    return 0;
}

} // namespace

void InstallGatewayAutoLogin() {
    if (!auth::Available()) {
        return;
    }
    HANDLE thread = CreateThread(nullptr, 0, ScheduleLogin, nullptr, 0, nullptr);
    if (thread == nullptr) {
        auth::Discard();
        return;
    }
    CloseHandle(thread);
}

} // namespace aegis::network
