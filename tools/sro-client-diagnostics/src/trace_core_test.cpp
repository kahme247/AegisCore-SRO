#include "client_trace.h"

#include <windows.h>

#include <array>
#include <cstdio>

int main() {
    const std::array<std::uint8_t, 4> worldPayload{0x01, 0x02, 0x03, 0x04};
    const std::array<std::uint8_t, 4> loginPayload{0xAA, 0xBB, 0xCC, 0xDD};
    const wchar_t* path = L"AegisClientDiagnosticsTest.actrace";

    aegis::trace::RecordPacket(aegis::trace::Direction::Inbound, aegis::trace::Phase::World, 0x3017, worldPayload.data(), worldPayload.size());
    aegis::trace::RecordPacket(aegis::trace::Direction::Outbound, aegis::trace::Phase::Gateway, 0x6100, loginPayload.data(), loginPayload.size());
    if (!aegis::trace::Flush(path)) {
        return 1;
    }

    FILE* file = nullptr;
    if (_wfopen_s(&file, path, L"rb") != 0 || file == nullptr) {
        return 2;
    }
    aegis::trace::FileHeader header{};
    const bool valid = std::fread(&header, sizeof(header), 1, file) == 1 && header.magic == aegis::trace::kMagic &&
                       header.recordSize == sizeof(aegis::trace::Record) && header.recordCount == aegis::trace::kRecordCount;
    std::array<aegis::trace::Record, aegis::trace::kRecordCount> records{};
    const bool readRecords = std::fread(records.data(), sizeof(records), 1, file) == 1;
    std::fclose(file);
    DeleteFileW(path);

    const auto& world = records[1];
    const auto& login = records[2];
    const bool worldCaptured = world.opcode == 0x3017 && (world.flags & aegis::trace::PayloadOmitted) == 0 &&
                               std::memcmp(world.payloadPrefix, worldPayload.data(), worldPayload.size()) == 0;
    const bool loginRedacted = login.opcode == 0x6100 && (login.flags & aegis::trace::PayloadOmitted) != 0 &&
                               login.payloadPrefix[0] == 0;
    return valid && readRecords && worldCaptured && loginRedacted ? 0 : 3;
}
