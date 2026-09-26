#include "auth/launch_context.h"

#include <windows.h>

#include <cstdio>
#include <cstring>

int main() {
    wchar_t mappingName[64]{};
    _snwprintf_s(mappingName, sizeof(mappingName) / sizeof(mappingName[0]), _TRUNCATE, L"Local\\AegisLaunchContext-%lu", GetCurrentProcessId());
    HANDLE mapping = CreateFileMappingW(INVALID_HANDLE_VALUE, nullptr, PAGE_READWRITE, 0, sizeof(aegis::auth::SharedLaunchContext), mappingName);
    if (mapping == nullptr) {
        return 1;
    }
    auto* shared = static_cast<aegis::auth::SharedLaunchContext*>(MapViewOfFile(
        mapping, FILE_MAP_ALL_ACCESS, 0, 0, sizeof(aegis::auth::SharedLaunchContext)));
    if (shared == nullptr) {
        CloseHandle(mapping);
        return 2;
    }
    *shared = {};
    shared->magic = aegis::auth::kLaunchContextMagic;
    shared->version = aegis::auth::kLaunchContextVersion;
    shared->size = sizeof(*shared);
    shared->shardId = 1;
    shared->contentId = 22;
    std::memcpy(shared->username, "alice", sizeof("alice"));
    std::memcpy(shared->token, "test-launch-token", sizeof("test-launch-token"));
    std::memcpy(shared->characterName, "CherEU", sizeof("CherEU"));

    const bool initialized = aegis::auth::InitializeFromLauncher();
    const bool erased = shared->magic == 0 && shared->token[0] == '\0';
    const bool available = aegis::auth::Available();
    aegis::auth::LaunchContext context{};
    const bool taken = aegis::auth::Take(context);
    const bool valid = context.shardId == 1 && context.contentId == 22 &&
                       std::strcmp(context.username, "alice") == 0 &&
                       std::strcmp(context.token, "test-launch-token") == 0 &&
                       std::strcmp(context.characterName, "CherEU") == 0;
    SecureZeroMemory(&context, sizeof(context));
    const bool singleUse = !aegis::auth::Take(context);

    UnmapViewOfFile(shared);
    CloseHandle(mapping);
    return initialized && erased && available && taken && valid && singleUse ? 0 : 3;
}
