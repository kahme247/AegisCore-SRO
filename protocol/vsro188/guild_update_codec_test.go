package vsro188

import (
	"encoding/hex"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestGuildUpdateLayoutsMatchVSRO188(t *testing.T) {
	tests := []struct {
		name string
		got  func() (uint16, string)
		want string
	}{
		{"disband", func() (uint16, string) {
			p := BuildGuildDisbandUpdate()
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "01"},
		{"member join", func() (uint16, string) {
			p := BuildGuildMemberJoinUpdate(GuildMemberUpdate{CharacterID: 7, Name: "A", Class: 10, Level: 80, Donation: 9, Permissions: 16, Contribution: 11, Nickname: "B", RefObjectID: 1907, SiegeAuthority: 2, Online: true})
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "02070000000100410a5009000000100000000b0000000000000000000000010042730700000200"},
		{"member leave", func() (uint16, string) {
			p := BuildGuildMemberLeaveUpdate(7, GuildMemberLeft)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "030700000001"},
		{"member kick", func() (uint16, string) {
			p := BuildGuildMemberLeaveUpdate(7, GuildMemberKicked)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "030700000002"},
		{"permissions", func() (uint16, string) {
			p := BuildGuildPermissionUpdate([]GuildPermissionUpdate{{CharacterID: 7, Permissions: 16}, {CharacterID: 9, Permissions: 31}})
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "14020700000010000000090000001f000000"},
		{"guild points", func() (uint16, string) {
			p := BuildGuildGPUpdate(0x11223344)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "050844332211"},
		{"promote", func() (uint16, string) {
			p := BuildGuildPromoteUpdate(4, 0x11223344)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "050c0444332211"},
		{"notice", func() (uint16, string) {
			p := BuildGuildNoticeUpdate("A", "BC")
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "051001004102004243"},
		{"member points", func() (uint16, string) {
			p := BuildGuildMemberGPUpdate(7, 9)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "06070000000809000000"},
		{"member level", func() (uint16, string) {
			p := BuildGuildMemberLevelUpdate(7, 80)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "06070000000150"},
		{"member position", func() (uint16, string) {
			p := BuildGuildMemberPositionUpdate(7, 10)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "0607000000400a"},
		{"member online", func() (uint16, string) {
			p := BuildGuildMemberStateUpdate(7, true)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "06070000000200"},
		{"member offline", func() (uint16, string) {
			p := BuildGuildMemberStateUpdate(7, false)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "06070000000201"},
		{"leader transfer", func() (uint16, string) {
			p := BuildGuildLeaderTransferUpdate(7, 9)
			return p.MessageID, hex.EncodeToString(p.Payload())
		}, "1602540700000000ffffffff01090000000a0000000000"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messageID, payload := test.got()
			if messageID != opcode.GuildUpdate {
				t.Fatalf("message id = %#04x, want %#04x", messageID, opcode.GuildUpdate)
			}
			if payload != test.want {
				t.Fatalf("payload = %s, want %s", payload, test.want)
			}
		})
	}
}

func TestGuildEntityNicknameLayoutMatchesVSRO188(t *testing.T) {
	packet := BuildGuildEntityNicknameUpdate(7, "A", "B")
	if packet.MessageID != opcode.EntityUpdateGuildNickname {
		t.Fatalf("message id = %#04x, want %#04x", packet.MessageID, opcode.EntityUpdateGuildNickname)
	}
	if got, want := hex.EncodeToString(packet.Payload()), "07000000010041010042"; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestGuildEntityUpdateLayoutMatchesVSRO188(t *testing.T) {
	packet := BuildGuildEntityUpdate(0x11223344, 7, "A", "B")
	if packet.MessageID != opcode.GuildEntityUpdate {
		t.Fatalf("message id = %#04x, want %#04x", packet.MessageID, opcode.GuildEntityUpdate)
	}
	if got, want := hex.EncodeToString(packet.Payload()), "44332211070000000100410100420000000000000000000000000001"; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}

func TestGuildWarScoreUpdateMatchesGameServerLayout(t *testing.T) {
	packet := BuildGuildWarScoreUpdate(1, 0x11223344, 80, 7, "Khaled")
	if packet.MessageID != opcode.GuildUpdate {
		t.Fatalf("message id = %#04x", packet.MessageID)
	}
	if got, want := hex.EncodeToString(packet.Payload()), "1d0144332211500000000700000006004b68616c6564"; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}
