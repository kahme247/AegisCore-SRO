package chat

import (
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

func TestParseAdminCommandSupportsCheatConsoleGrammar(t *testing.T) {
	for _, test := range []struct {
		message   string
		allowBare bool
		command   string
		arguments string
		ok        bool
	}{
		{message: ".spawn MOB_RM_ROC", command: "spawn", arguments: "MOB_RM_ROC", ok: true},
		{message: "/ LOADMONSTER MOB_RM_ROC", allowBare: true, command: "loadmonster", arguments: "MOB_RM_ROC", ok: true},
		{message: "/DAY", allowBare: true, command: "day", ok: true},
		{message: "spawn MOB_RM_ROC", allowBare: true, command: "spawn", arguments: "MOB_RM_ROC", ok: true},
		{message: ".spawn MOB_RM_ROC", allowBare: true, command: "spawn", arguments: "MOB_RM_ROC", ok: true},
		{message: ".warp 24999 435 0 1745", command: "warp", arguments: "24999 435 0 1745", ok: true},
		{message: "/warp 24999 435 0 1745", allowBare: true, command: "warp", arguments: "24999 435 0 1745", ok: true},
		{message: "warp 24999 435 0 1745", allowBare: true, command: "warp", arguments: "24999 435 0 1745", ok: true},
		{message: "spawn MOB_RM_ROC", ok: false},
	} {
		command, arguments, ok := parseAdminCommand(test.message, test.allowBare)
		if command != test.command || arguments != test.arguments || ok != test.ok {
			t.Fatalf("parseAdminCommand(%q, %t) = (%q, %q, %t), want (%q, %q, %t)", test.message, test.allowBare, command, arguments, ok, test.command, test.arguments, test.ok)
		}
	}
}

func TestParseGMMakeItemArgs(t *testing.T) {
	for _, test := range []struct {
		arguments string
		codeName  string
		count     int
		ok        bool
	}{
		{arguments: "2147", codeName: "2147", count: 1, ok: true},
		{arguments: "ITEM_ETC_HP_POTION_01", codeName: "ITEM_ETC_HP_POTION_01", count: 1, ok: true},
		{arguments: "ITEM_ETC_HP_POTION_01 20", codeName: "ITEM_ETC_HP_POTION_01", count: 20, ok: true},
		{arguments: "MOB_CH_TIGER", ok: false},
		{arguments: "ITEM_ETC_HP_POTION_01 0", ok: false},
	} {
		codeName, count, err := parseGMMakeItemArgs(test.arguments)
		if (err == nil) != test.ok || codeName != test.codeName || count != test.count {
			t.Fatalf("parseGMMakeItemArgs(%q) = (%q, %d, %v)", test.arguments, codeName, count, err)
		}
	}
}

func TestParseGMNoticeMatchesCheatConsoleGrammar(t *testing.T) {
	for _, test := range []struct {
		message string
		want    string
		ok      bool
	}{
		{message: "~hello", want: "hello", ok: true},
		{message: "~ server restart soon ", want: "server restart soon", ok: true},
		{message: "hello", ok: false},
		{message: ".day", ok: false},
	} {
		got, ok := parseGMNotice(test.message)
		if got != test.want || ok != test.ok {
			t.Fatalf("parseGMNotice(%q) = (%q, %t), want (%q, %t)", test.message, got, ok, test.want, test.ok)
		}
	}
}

func TestGMHandlerRejectsAllGMChatCommand(t *testing.T) {
	request := network.EmptyClientPacket()
	request.WriteByte(AllGM)
	request.WriteByte(0)
	request.WriteString(".speed 500")
	chatType, err := request.ReadByte()
	if err != nil {
		t.Fatal(err)
	}
	if _, err = request.ReadByte(); err != nil {
		t.Fatal(err)
	}
	message, err := request.ReadString()
	if err != nil {
		t.Fatal(err)
	}
	if err = (&GmMessageHandler{}).HandleAdminMessage(MessageRequest{ChatType: chatType, Message: message}, &server.Session{}); err != nil {
		t.Fatalf("AllGM command returned error: %v", err)
	}
}

func TestParseGMSpawnArgs(t *testing.T) {
	for _, test := range []struct {
		arguments string
		codeName  string
		count     int
	}{
		{arguments: "MOB_RM_ROC", codeName: "MOB_RM_ROC", count: 1},
		{arguments: "  MOB_TQ_WHITESNAKE   5 ", codeName: "MOB_TQ_WHITESNAKE", count: 5},
		{arguments: "MOB_TK_BONELORD 20", codeName: "MOB_TK_BONELORD", count: 20},
	} {
		codeName, count, err := parseGMSpawnArgs(test.arguments)
		if err != nil {
			t.Fatalf("parse %q: %v", test.arguments, err)
		}
		if codeName != test.codeName || count != test.count {
			t.Fatalf("parse %q = (%q, %d), want (%q, %d)", test.arguments, codeName, count, test.codeName, test.count)
		}
	}
}

func TestParseGMSpawnArgsRejectsUnsafeInput(t *testing.T) {
	for _, arguments := range []string{"", "NPC_CH_SMITH", "MOB_RM_ROC 0", "MOB_RM_ROC 21", "MOB_RM_ROC many", "MOB_RM_ROC 1 extra"} {
		if _, _, err := parseGMSpawnArgs(arguments); err == nil {
			t.Fatalf("parse %q unexpectedly succeeded", arguments)
		}
	}
}

func TestParseGMWeatherArgs(t *testing.T) {
	for _, test := range []struct {
		arguments string
		wantType  byte
		wantLevel byte
	}{
		{arguments: "clear", wantType: 1},
		{arguments: "rain", wantType: 2, wantLevel: 50},
		{arguments: "snow 100", wantType: 3, wantLevel: 100},
	} {
		weatherType, level, err := parseGMWeatherArgs(test.arguments)
		if err != nil || byte(weatherType) != test.wantType || level != test.wantLevel {
			t.Fatalf("parseGMWeatherArgs(%q) = (%d, %d, %v)", test.arguments, weatherType, level, err)
		}
	}
}

func TestParseGMWeatherArgsRejectsUnsafeInput(t *testing.T) {
	for _, arguments := range []string{"", "storm", "clear 1", "rain 0", "snow 101", "rain much", "rain 1 extra"} {
		if _, _, err := parseGMWeatherArgs(arguments); err == nil {
			t.Fatalf("parseGMWeatherArgs(%q) unexpectedly succeeded", arguments)
		}
	}
}

func TestParseGMWarpArgs(t *testing.T) {
	for _, test := range []struct {
		arguments  string
		wantRegion int16
		wantX      float32
		wantY      float32
		wantZ      float32
	}{
		{arguments: "24999 435 0 1745", wantRegion: 24999, wantX: 435, wantY: 0, wantZ: 1745},
		{arguments: " 25991   1374.843750  -28.878524   937.109375 ", wantRegion: 25991, wantX: 1374.843750, wantY: -28.878524, wantZ: 937.109375},
		{arguments: "-32768 -100.5 0.0 50.25", wantRegion: -32768, wantX: -100.5, wantY: 0.0, wantZ: 50.25},
	} {
		regionID, x, y, z, err := parseGMWarpArgs(test.arguments)
		if err != nil {
			t.Fatalf("parseGMWarpArgs(%q) unexpected error: %v", test.arguments, err)
		}
		if regionID != test.wantRegion || x != test.wantX || y != test.wantY || z != test.wantZ {
			t.Fatalf("parseGMWarpArgs(%q) = (%d, %f, %f, %f), want (%d, %f, %f, %f)", test.arguments, regionID, x, y, z, test.wantRegion, test.wantX, test.wantY, test.wantZ)
		}
	}
}

func TestParseGMWarpArgsRejectsUnsafeInput(t *testing.T) {
	for _, arguments := range []string{
		"",
		"24999 435 0",
		"24999 435 0 1745 100",
		"invalid 435 0 1745",
		"70000 435 0 1745",
		"24999 invalid 0 1745",
		"24999 435 invalid 1745",
		"24999 435 0 invalid",
	} {
		if _, _, _, _, err := parseGMWarpArgs(arguments); err == nil {
			t.Fatalf("parseGMWarpArgs(%q) unexpectedly succeeded", arguments)
		}
	}
}

func TestGMMuteTogglesPlayerState(t *testing.T) {
	player := &model.Player{}
	if player.IsGMMuted() {
		t.Fatal("new player is muted")
	}
	if !player.ToggleGMMute() || !player.IsGMMuted() {
		t.Fatal("first toggle did not mute player")
	}
	if player.ToggleGMMute() || player.IsGMMuted() {
		t.Fatal("second toggle did not unmute player")
	}
}

func TestGMMuteRestrictsOnlyPublicChatTypes(t *testing.T) {
	for _, chatType := range []byte{All, Global, Stall} {
		if !isPublicChatType(chatType) {
			t.Fatalf("chat type %d is not public", chatType)
		}
	}
	for _, chatType := range []byte{PM, Party, Guild, Union, Academy, Notice} {
		if isPublicChatType(chatType) {
			t.Fatalf("chat type %d unexpectedly public", chatType)
		}
	}
}
