#pragma once

#include <cstddef>
#include <cstdint>

namespace aegis::auth {

constexpr std::uint32_t kLaunchContextMagic = 0x314C4341; // "ACL1"
constexpr std::uint16_t kLaunchContextVersion = 1;
constexpr std::size_t kUsernameBytes = 64;
constexpr std::size_t kTokenBytes = 64;

#pragma pack(push, 1)
struct SharedLaunchContext {
    std::uint32_t magic;
    std::uint16_t version;
    std::uint16_t size;
    std::uint16_t shardId;
    std::uint8_t contentId;
    std::uint8_t reserved;
    char username[kUsernameBytes];
    char token[kTokenBytes];
};
#pragma pack(pop)

static_assert(sizeof(SharedLaunchContext) == 140, "launcher contract layout changed");

struct LaunchContext {
    std::uint16_t shardId{};
    std::uint8_t contentId{};
    char username[kUsernameBytes]{};
    char token[kTokenBytes]{};
};

// Reads and erases the current process's launcher mapping. A missing mapping is
// normal when the client was started without launcher authentication.
bool InitializeFromLauncher();
bool Available();

// Transfers the context exactly once and erases Aegis's retained copy.
bool Take(LaunchContext& destination);
void Discard();

} // namespace aegis::auth
