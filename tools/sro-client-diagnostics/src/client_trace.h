#pragma once

#include <cstddef>
#include <cstdint>

namespace aegis::trace {

constexpr std::uint32_t kMagic = 0x54534341; // "ACST"
constexpr std::size_t kRecordCount = 64;
constexpr std::size_t kPayloadPrefixBytes = 128;

enum class Direction : std::uint8_t {
    Inbound = 1,
    Outbound = 2,
};

enum class Phase : std::uint8_t {
    Unknown = 0,
    Gateway = 1,
    Agent = 2,
    World = 3,
};

enum RecordFlags : std::uint16_t {
    PayloadOmitted = 1 << 0,
    PayloadTruncated = 1 << 1,
};

#pragma pack(push, 1)
struct Record {
    std::uint32_t sequence;
    std::uint32_t tickMilliseconds;
    Direction direction;
    Phase phase;
    std::uint16_t flags;
    std::uint16_t opcode;
    std::uint16_t payloadLength;
    std::uint8_t payloadHash[32];
    std::uint8_t payloadPrefix[kPayloadPrefixBytes];
};

struct FileHeader {
    std::uint32_t magic;
    std::uint16_t version;
    std::uint16_t recordSize;
    std::uint32_t writeSequence;
    std::uint32_t recordCount;
};
#pragma pack(pop)

static_assert(sizeof(Record) == 176, "trace record layout changed");

void RecordPacket(Direction direction, Phase phase, std::uint16_t opcode, const void* payload, std::size_t payloadLength);
bool Flush(const wchar_t* outputPath);

} // namespace aegis::trace
