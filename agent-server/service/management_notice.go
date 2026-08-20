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
	if err := vsro188.ValidateText(message, vsro188.TextRules{
		Field: "management notice", MaxRunes: 200, RequireTrimmed: true,
	}); err != nil {
		return 0, err
	}
	packet := network.EmptyPacket()
	packet.MessageID = opcode.ChatUpdate
	packet.WriteByte(managementNoticeChatType)
	packet.WriteString(message)
	if err := packet.WriteError(); err != nil {
		return 0, fmt.Errorf("build management notice packet: %w", err)
	}
	recipientCount := 0
	for _, player := range w.GetActivePlayers() {
		if player != nil && player.GetSession() != nil && player.GetSession().Send(packet) == nil {
			recipientCount++
		}
	}
	return recipientCount, nil
}
