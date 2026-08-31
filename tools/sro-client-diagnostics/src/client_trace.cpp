#include "client_trace.h"

#include <windows.h>

// bcrypt.h expects NTSTATUS from the NT headers; WIN32_LEAN_AND_MEAN omits it.
using NTSTATUS = LONG;
#include <bcrypt.h>

#include <algorithm>
#include <array>
#include <cstring>

#pragma comment(lib, "bcrypt.lib")

namespace aegis::trace {
namespace {

std::array<Record, kRecordCount> g_records{};
volatile LONG g_sequence = 0;

bool IsSensitive(Direction direction, Phase phase, std::uint16_t opcode) {
    if (direction == Direction::Outbound || phase == Phase::Gateway) {
        return true;
    }

    // Security negotiation and account/login messages are metadata-only even
    // if a future hook cannot determine the current connection phase.
    return opcode == 0x5000 || opcode == 0x9000 || opcode == 0x6100 || opcode == 0x6101 || opcode == 0x6102 || opcode == 0x6103;
}

void HashPayload(const void* payload, std::size_t payloadLength, std::uint8_t (&hash)[32]) {
    BCRYPT_ALG_HANDLE algorithm = nullptr;
    if (BCryptOpenAlgorithmProvider(&algorithm, BCRYPT_SHA256_ALGORITHM, nullptr, 0) != 0) {
        return;
    }
    const auto status = BCryptHash(algorithm, nullptr, 0, static_cast<PUCHAR>(const_cast<void*>(payload)), static_cast<ULONG>(payloadLength), hash, sizeof(hash));
    if (status != 0) {
        std::memset(hash, 0, sizeof(hash));
    }
    BCryptCloseAlgorithmProvider(algorithm, 0);
}

} // namespace

void RecordPacket(Direction direction, Phase phase, std::uint16_t opcode, const void* payload, std::size_t payloadLength) {
    const auto sequence = static_cast<std::uint32_t>(InterlockedIncrement(&g_sequence));
    Record& record = g_records[sequence % kRecordCount];
    std::memset(&record, 0, sizeof(record));
    record.sequence = sequence;
    record.tickMilliseconds = GetTickCount();
    record.direction = direction;
    record.phase = phase;
    record.opcode = opcode;
    record.payloadLength = static_cast<std::uint16_t>(std::min<std::size_t>(payloadLength, UINT16_MAX));

    if (payload != nullptr && payloadLength != 0) {
        HashPayload(payload, payloadLength, record.payloadHash);
    }

    if (payload == nullptr || payloadLength == 0 || IsSensitive(direction, phase, opcode)) {
        record.flags |= PayloadOmitted;
        return;
    }

    const auto prefixLength = std::min(payloadLength, kPayloadPrefixBytes);
    std::memcpy(record.payloadPrefix, payload, prefixLength);
    if (prefixLength != payloadLength) {
        record.flags |= PayloadTruncated;
    }
}

bool Flush(const wchar_t* outputPath) {
    if (outputPath == nullptr || *outputPath == L'\0') {
        return false;
    }

    HANDLE file = CreateFileW(outputPath, GENERIC_WRITE, 0, nullptr, CREATE_ALWAYS, FILE_ATTRIBUTE_NORMAL, nullptr);
    if (file == INVALID_HANDLE_VALUE) {
        return false;
    }

    const FileHeader header{kMagic, 1, sizeof(Record), static_cast<std::uint32_t>(g_sequence), kRecordCount};
    DWORD written = 0;
    const bool ok = WriteFile(file, &header, sizeof(header), &written, nullptr) != FALSE && written == sizeof(header) &&
                    WriteFile(file, g_records.data(), static_cast<DWORD>(sizeof(g_records)), &written, nullptr) != FALSE && written == sizeof(g_records);
    CloseHandle(file);
    return ok;
}

} // namespace aegis::trace
