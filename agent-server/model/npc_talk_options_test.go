package model

import (
	"reflect"
	"testing"
)

func TestNPCTalkOptionsUsesOriginalNPCServiceMasks(t *testing.T) {
	tests := []struct {
		name     string
		codeName string
		want     []byte
	}{
		{name: "quest NPC", codeName: "NPC_CH_PRIEST", want: []byte{0, 0, 1, 0, 0}},
		{name: "shop", codeName: "NPC_CH_SMITH", want: []byte{0, 1, 1, 0, 0}},
		{name: "warehouse", codeName: "NPC_CH_WAREHOUSE_M", want: []byte{0, 0x20, 0, 0, 0}},
		{name: "ferry", codeName: "NPC_CH_FERRY", want: []byte{0, 2, 8, 0, 0}},
		{name: "teleporter", codeName: "NPC_INS_EXIT_TELEPORT", want: []byte{0, 2, 8, 0, 0}},
		{name: "city gate", codeName: "STORE_CH_GATE", want: []byte{0, 2, 8, 0, 0}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := NPCTalkOptions(test.codeName)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("NPCTalkOptions(%q) = %x, want %x", test.codeName, got, test.want)
			}
			got[0] = 0xFF
			if again := NPCTalkOptions(test.codeName); again[0] == 0xFF {
				t.Fatal("NPCTalkOptions returned shared mutable storage")
			}
		})
	}
}

func TestNPCSelectionTalkDataUsesCountedActionsAndInfoSuffix(t *testing.T) {
	tests := []struct {
		codeName string
		want     []byte
	}{
		{codeName: "NPC_CH_SMITH", want: []byte{0, 4, 1, 2, 4, 0x20, 0, 0, 0}},
		{codeName: "NPC_CH_ARMOR", want: []byte{0, 2, 1, 4, 0, 0, 0}},
		{codeName: "NPC_CH_POTION", want: []byte{0, 1, 1, 0, 0, 0}},
		{codeName: "NPC_CH_ACCESSORY", want: []byte{0, 1, 1, 0, 0x14, 0}},
		{codeName: "NPC_CH_SPECIAL", want: []byte{0, 1, 1, 0, 0, 0}},
		{codeName: "NPC_CH_SPECIAL2", want: []byte{0, 1, 1, 0, 0, 0}},
		{codeName: "NPC_KT_SPECIAL", want: []byte{0, 1, 1, 0, 0, 0}},
		{codeName: "NPC_WC_SPECIAL", want: []byte{0, 1, 1, 0}},
		{codeName: "NPC_EU_SPECIAL", want: []byte{0, 1, 1, 0}},
		{codeName: "NPC_CH_WAREHOUSE_M", want: []byte{0, 2, 1, 3, 0}},
		{codeName: "NPC_CH_FERRY", want: []byte{0, 1, 8, 0, 0, 0}},
		{codeName: "STORE_CH_GATE", want: []byte{0, 1, 8, 0, 0, 0}},
		{codeName: "NPC_TEST", want: []byte{0, 0, 0}},
	}

	for _, test := range tests {
		t.Run(test.codeName, func(t *testing.T) {
			if got := NPCSelectionTalkData(test.codeName); !reflect.DeepEqual(got, test.want) {
				t.Fatalf("NPCSelectionTalkData(%q) = %x, want %x", test.codeName, got, test.want)
			}
		})
	}
}
