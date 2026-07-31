package service

import (
	"fmt"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const managementNoticeChatType byte = 7

// BroadcastNotice sends a server-authored notice to the current in-world
// player snapshot. The returned count is the number of sessions that accepted
// the packet for delivery, not a client acknowledgement count.
func (w *WorldService) BroadcastNotice(message string) (int, error) {
	packet, err := buildServerNoticePacket(message)
	if err != nil {
		return 0, err
	}
	recipientCount := 0
	for _, player := range w.GetActivePlayers() {
		if player != nil && player.GetSession() != nil && player.GetSession().Send(packet) == nil {
			recipientCount++
		}
	}
	return recipientCount, nil
}

// buildServerNoticePacket uses the type-7 ChatUpdate variant consumed by the
// stock GM debug console's "~ <text>" command. Unlike ordinary player chat,
// this variant contains no sender UID or name.
func buildServerNoticePacket(message string) (network.Packet, error) {
	if err := vsro188.ValidateText(message, vsro188.TextRules{
		Field: "management notice", MaxRunes: 200, RequireTrimmed: true,
	}); err != nil {
		return network.Packet{}, err
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ChatUpdate
	packet.WriteByte(managementNoticeChatType)
	packet.WriteString(message)
	if err := packet.WriteError(); err != nil {
		return network.Packet{}, fmt.Errorf("build management notice packet: %w", err)
	}
	return packet, nil
}
