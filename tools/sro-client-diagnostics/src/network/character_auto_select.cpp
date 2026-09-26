#include "network/character_auto_select.h"

#include <windows.h>

#include <cstddef>
#include <cstdint>
#include <cstring>

namespace aegis::network::character_auto_select {
namespace {

constexpr std::uintptr_t kGameObject = 0x00EECE80;
constexpr std::size_t kCurrentProcessOffset = 0x24;
constexpr std::uintptr_t kCharacterSelectVtable = 0x00DD811C;
constexpr std::uintptr_t kMessageBufferConstructor = 0x00508F90;
constexpr std::uintptr_t kMessageBufferWrite = 0x00508FE0;
constexpr std::uintptr_t kMessageBufferDestructor = 0x005097A0;
constexpr std::uintptr_t kSendMessage = 0x008418D0;
constexpr unsigned kPollMilliseconds = 100;
constexpr unsigned kMaximumPolls = 1200;
constexpr std::uint16_t kJoinCharacterOpcode = 0x7001;

struct MessageBuffer {
    std::uintptr_t vtable;
    std::uintptr_t readNode;
    std::uint32_t bytes;
    std::uint8_t mode;
    std::uint8_t padding[3];
    std::uintptr_t writeNode;
    std::uintptr_t savedNode;
    std::uint16_t opcode;
    std::uint16_t opcodePadding;
};

static_assert(sizeof(MessageBuffer) == 28, "unexpected CMsgStreamBuffer layout");

using MessageBufferConstructorFn = MessageBuffer*(__thiscall*)(MessageBuffer*);
using MessageBufferWriteFn = void*(__thiscall*)(MessageBuffer*, const void*, std::size_t);
using MessageBufferDestructorFn = void(__thiscall*)(MessageBuffer*);
using SendMessageFn = void(__cdecl*)(MessageBuffer&);

bool IsReadable(const void* address, std::size_t bytes) {
    MEMORY_BASIC_INFORMATION memory{};
    if (address == nullptr || VirtualQuery(address, &memory, sizeof(memory)) != sizeof(memory) ||
        memory.State != MEM_COMMIT || (memory.Protect & (PAGE_NOACCESS | PAGE_GUARD)) != 0) {
        return false;
    }
    const auto start = reinterpret_cast<std::uintptr_t>(address);
    const auto end = start + bytes;
    const auto regionEnd = reinterpret_cast<std::uintptr_t>(memory.BaseAddress) + memory.RegionSize;
    return end >= start && end <= regionEnd;
}

bool CharacterSelectIsActive() {
    const auto* process = reinterpret_cast<const std::uintptr_t*>(kGameObject + kCurrentProcessOffset);
    if (!IsReadable(process, sizeof(*process))) {
        return false;
    }
    const auto currentProcess = *process;
    if (currentProcess == 0 || !IsReadable(reinterpret_cast<const void*>(currentProcess), sizeof(std::uintptr_t))) {
        return false;
    }
    return *reinterpret_cast<const std::uintptr_t*>(currentProcess) == kCharacterSelectVtable;
}

void SendCharacterJoin(const char* characterName) {
    const auto length = std::strlen(characterName);
    if (length == 0 || length >= 64 || length > UINT16_MAX) {
        return;
    }

    MessageBuffer buffer{};
    const auto construct = reinterpret_cast<MessageBufferConstructorFn>(kMessageBufferConstructor);
    const auto write = reinterpret_cast<MessageBufferWriteFn>(kMessageBufferWrite);
    const auto destroy = reinterpret_cast<MessageBufferDestructorFn>(kMessageBufferDestructor);
    const auto send = reinterpret_cast<SendMessageFn>(kSendMessage);
    construct(&buffer);
    buffer.opcode = kJoinCharacterOpcode;
    buffer.mode = 1;
    buffer.savedNode = buffer.writeNode;
    buffer.bytes = 0;

    const auto stringLength = static_cast<std::uint16_t>(length);
    write(&buffer, &stringLength, sizeof(stringLength));
    write(&buffer, characterName, length);

    buffer.mode = 0;
    buffer.savedNode = buffer.writeNode;
    buffer.readNode = 0;
    send(buffer);
    destroy(&buffer);
}

DWORD WINAPI WaitForCharacterSelect(void* parameter) {
    auto* characterName = static_cast<char*>(parameter);
    for (unsigned poll = 0; poll < kMaximumPolls; ++poll) {
        if (CharacterSelectIsActive()) {
            SendCharacterJoin(characterName);
            break;
        }
        Sleep(kPollMilliseconds);
    }
    SecureZeroMemory(characterName, 64);
    delete[] characterName;
    return 0;
}

} // namespace

void Schedule(const char* characterName) {
    if (characterName == nullptr || characterName[0] == '\0' || strnlen_s(characterName, 64) >= 64) {
        return;
    }
    auto* copy = new char[64]{};
    std::memcpy(copy, characterName, std::strlen(characterName));
    HANDLE thread = CreateThread(nullptr, 0, WaitForCharacterSelect, copy, 0, nullptr);
    if (thread == nullptr) {
        SecureZeroMemory(copy, 64);
        delete[] copy;
        return;
    }
    CloseHandle(thread);
}

} // namespace aegis::network::character_auto_select
