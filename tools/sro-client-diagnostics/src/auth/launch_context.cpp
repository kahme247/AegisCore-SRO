#include "auth/launch_context.h"

#include <windows.h>

#include <cstdio>
#include <cstring>

namespace aegis::auth {
namespace {

SRWLOCK g_contextLock = SRWLOCK_INIT;
LaunchContext g_context{};
bool g_hasContext = false;

bool IsTerminated(const char* value, std::size_t size) {
    return std::memchr(value, '\0', size) != nullptr;
}

bool IsValid(const SharedLaunchContext& shared) {
    return shared.magic == kLaunchContextMagic && shared.version == kLaunchContextVersion &&
           shared.size == sizeof(SharedLaunchContext) && shared.shardId != 0 &&
           IsTerminated(shared.username, sizeof(shared.username)) && shared.username[0] != '\0' &&
           IsTerminated(shared.token, sizeof(shared.token)) && shared.token[0] != '\0';
}

} // namespace

bool InitializeFromLauncher() {
    wchar_t mappingName[64]{};
    _snwprintf_s(mappingName, sizeof(mappingName) / sizeof(mappingName[0]), _TRUNCATE, L"Local\\AegisLaunchContext-%lu", GetCurrentProcessId());
    HANDLE mapping = OpenFileMappingW(FILE_MAP_READ | FILE_MAP_WRITE, FALSE, mappingName);
    if (mapping == nullptr) {
        return false;
    }

    auto* shared = static_cast<SharedLaunchContext*>(MapViewOfFile(mapping, FILE_MAP_READ | FILE_MAP_WRITE, 0, 0, sizeof(SharedLaunchContext)));
    if (shared == nullptr) {
        CloseHandle(mapping);
        return false;
    }

    SharedLaunchContext local{};
    std::memcpy(&local, shared, sizeof(local));
    SecureZeroMemory(shared, sizeof(*shared));
    FlushViewOfFile(shared, sizeof(*shared));
    UnmapViewOfFile(shared);
    CloseHandle(mapping);

    if (!IsValid(local)) {
        SecureZeroMemory(&local, sizeof(local));
        return false;
    }

    AcquireSRWLockExclusive(&g_contextLock);
    SecureZeroMemory(&g_context, sizeof(g_context));
    g_context.shardId = local.shardId;
    g_context.contentId = local.contentId;
    std::memcpy(g_context.username, local.username, sizeof(g_context.username));
    std::memcpy(g_context.token, local.token, sizeof(g_context.token));
    g_hasContext = true;
    ReleaseSRWLockExclusive(&g_contextLock);
    SecureZeroMemory(&local, sizeof(local));
    return true;
}

bool Available() {
    AcquireSRWLockShared(&g_contextLock);
    const bool available = g_hasContext;
    ReleaseSRWLockShared(&g_contextLock);
    return available;
}

bool Take(LaunchContext& destination) {
    AcquireSRWLockExclusive(&g_contextLock);
    if (!g_hasContext) {
        ReleaseSRWLockExclusive(&g_contextLock);
        return false;
    }
    std::memcpy(&destination, &g_context, sizeof(destination));
    SecureZeroMemory(&g_context, sizeof(g_context));
    g_hasContext = false;
    ReleaseSRWLockExclusive(&g_contextLock);
    return true;
}

void Discard() {
    AcquireSRWLockExclusive(&g_contextLock);
    SecureZeroMemory(&g_context, sizeof(g_context));
    g_hasContext = false;
    ReleaseSRWLockExclusive(&g_contextLock);
}

} // namespace aegis::auth
