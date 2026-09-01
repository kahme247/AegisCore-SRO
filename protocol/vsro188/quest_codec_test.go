package vsro188

import (
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestDecodeQuestRequestsRejectsShapeMismatches(t *testing.T) {
	questID := network.EmptyClientPacket()
	questID.WriteUInt32(0x11223344)
	decodedID, err := DecodeQuestIDRequest(&questID)
	if err != nil || decodedID.QuestID != 0x11223344 {
		t.Fatalf("DecodeQuestIDRequest = %#v, %v", decodedID, err)
	}

	selection := network.EmptyClientPacket()
	selection.WriteByte(7)
	decodedSelection, err := DecodeQuestTalkSelection(&selection)
	if err != nil || decodedSelection.Selection != 7 {
		t.Fatalf("DecodeQuestTalkSelection = %#v, %v", decodedSelection, err)
	}

	trailing := network.EmptyClientPacket()
	trailing.WriteUInt32(1)
	trailing.WriteByte(0)
	if _, err = DecodeQuestIDRequest(&trailing); err == nil {
		t.Fatal("DecodeQuestIDRequest accepted trailing data")
	}
}

func TestBuildQuestTalkEncodesCompleteTypeFiveGrammar(t *testing.T) {
	packet, err := BuildQuestTalk(QuestTalkMessage{
		Type: 5, Text: "T", Selections: []string{"A", "BC"}, ExtraText: "E", Argument: 0x11223344,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{5, 1, 0, 'T', 2, 1, 0, 'A', 2, 0, 'B', 'C', 1, 0, 'E', 0x44, 0x33, 0x22, 0x11}
	if packet.MessageID != opcode.QuestTalk || !reflect.DeepEqual(packet.Payload(), want) {
		t.Fatalf("quest talk = opcode 0x%04X payload % X, want % X", packet.MessageID, packet.Payload(), want)
	}
}

func TestBuildQuestTalkEnforcesRecoveredSelectionLimit(t *testing.T) {
	selections := make([]string, QuestDialogueSelectionLimit+1)
	if _, err := BuildQuestTalk(QuestTalkMessage{Type: 4, Selections: selections}); err == nil {
		t.Fatal("BuildQuestTalk accepted more than 64 selections")
	}
	if _, err := BuildQuestTalk(QuestTalkMessage{Type: 4, ExtraText: "type-five-only"}); err == nil {
		t.Fatal("BuildQuestTalk accepted type-5 fields for type 4")
	}
}

func TestBuildQuestUpdateEncodesEveryRecoveredConditionalSection(t *testing.T) {
	packet, err := BuildQuestUpdate(QuestUpdateMessage{
		Subtype:             QuestUpdateDelta,
		QuestID:             0x11223344,
		PackedQuestByte:     0x21,
		QuestDefinitionByte: 3,
		Flags:               QuestDeltaTimerFlag | QuestDeltaStatusFlag | QuestDeltaMissionsFlag | QuestDeltaValuesFlag,
		Timer:               0xaabbccdd,
		Status:              8,
		Missions: []QuestMissionDelta{{
			EntryKey: 2, TransitionCode: 2, Text: "X", Values: []uint32{5, 6},
		}},
		PendingValues: []uint32{9},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		2, 0x44, 0x33, 0x22, 0x11, 0x21, 3, 0x5c,
		0xdd, 0xcc, 0xbb, 0xaa, 8,
		1, 2, 2, 1, 0, 'X', 2,
		5, 0, 0, 0, 6, 0, 0, 0,
		1, 9, 0, 0, 0,
	}
	if packet.MessageID != opcode.QuestUpdate || !reflect.DeepEqual(packet.Payload(), want) {
		t.Fatalf("quest update = opcode 0x%04X payload % X, want % X", packet.MessageID, packet.Payload(), want)
	}
}

func TestBuildQuestUpdateRetireHasNoTail(t *testing.T) {
	packet, err := BuildQuestUpdate(QuestUpdateMessage{Subtype: QuestUpdateRetire, QuestID: 77})
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{3, 77, 0, 0, 0}; !reflect.DeepEqual(packet.Payload(), want) {
		t.Fatalf("retire payload = % X, want % X", packet.Payload(), want)
	}
	if _, err = BuildQuestUpdate(QuestUpdateMessage{Subtype: QuestUpdateRetire, QuestID: 77, Flags: QuestDeltaStatusFlag}); err == nil {
		t.Fatal("retire accepted a conditional tail")
	}
}

func TestBuildQuestMarksMatchesClientReadOrder(t *testing.T) {
	add, err := BuildQuestMarkAdd(QuestMark{
		RuntimeID: 0x11223344, Flags: 2, Kind: 3, AuthoredValue: 0x5566,
		X: -1, Y: 2, Z: -3, TargetID: 0x778899aa,
	})
	if err != nil {
		t.Fatal(err)
	}
	wantAdd := []byte{
		0x44, 0x33, 0x22, 0x11, 2, 3, 0x66, 0x55,
		0xff, 0xff, 0xff, 0xff, 2, 0, 0, 0, 0xfd, 0xff, 0xff, 0xff,
		0xaa, 0x99, 0x88, 0x77,
	}
	if add.MessageID != opcode.QuestMarkAdd || !reflect.DeepEqual(add.Payload(), wantAdd) {
		t.Fatalf("mark add = opcode 0x%04X payload % X", add.MessageID, add.Payload())
	}
	remove, err := BuildQuestMarkRemove(0x11223344)
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{0x44, 0x33, 0x22, 0x11}; remove.MessageID != opcode.QuestMarkRemove || !reflect.DeepEqual(remove.Payload(), want) {
		t.Fatalf("mark remove = opcode 0x%04X payload % X", remove.MessageID, remove.Payload())
	}
}

func TestBuildQuestAuxiliaryPacketsMatchesClientReadOrder(t *testing.T) {
	for _, packetOpcode := range []uint16{opcode.QuestGatherResult, opcode.QuestCatchResult} {
		packet, err := BuildQuestActionResult(packetOpcode, 0x11223344, 7)
		if err != nil {
			t.Fatal(err)
		}
		if want := []byte{0x44, 0x33, 0x22, 0x11, 7}; packet.MessageID != packetOpcode || !reflect.DeepEqual(packet.Payload(), want) {
			t.Fatalf("action 0x%04X = % X", packetOpcode, packet.Payload())
		}
	}

	notification, err := BuildQuestNotification("TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if want := []byte{5, 0, 'T', 'O', 'K', 'E', 'N'}; notification.MessageID != opcode.QuestNotification || !reflect.DeepEqual(notification.Payload(), want) {
		t.Fatalf("notification = opcode 0x%04X payload % X", notification.MessageID, notification.Payload())
	}

	prompt, err := BuildQuestRewardPrompt(0x11223344, 4, "PAY")
	if err != nil {
		t.Fatal(err)
	}
	wantPrompt := []byte{0x44, 0x33, 0x22, 0x11, 4, 3, 0, 'P', 'A', 'Y'}
	if prompt.MessageID != opcode.QuestRewardPrompt || !reflect.DeepEqual(prompt.Payload(), wantPrompt) {
		t.Fatalf("prompt = opcode 0x%04X payload % X", prompt.MessageID, prompt.Payload())
	}
}

func TestDecodeQuestRewardSelectionConsumesExactCount(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteUInt32(91)
	packet.WriteByte(2)
	packet.WriteUInt32(7)
	packet.WriteUInt32(8)
	request, err := DecodeQuestRewardSelectionRequest(&packet)
	if err != nil {
		t.Fatal(err)
	}
	if request.RewardID != 91 || !reflect.DeepEqual(request.EntryIDs, []uint32{7, 8}) {
		t.Fatalf("request = %#v", request)
	}

	truncated := network.EmptyClientPacket()
	truncated.WriteUInt32(91)
	truncated.WriteByte(2)
	truncated.WriteUInt32(7)
	if _, err = DecodeQuestRewardSelectionRequest(&truncated); err == nil {
		t.Fatal("reward selection accepted a truncated entry vector")
	}
}

func TestBuildQuestResponsesPreservesOneByteFailureReason(t *testing.T) {
	for _, responseOpcode := range []uint16{opcode.QuestInteractionResponse, opcode.QuestAbandonResponse, opcode.QuestLocalActionResponse} {
		success, err := BuildQuestResponse(responseOpcode, true, 0x11223344, 0)
		if err != nil {
			t.Fatal(err)
		}
		if want := []byte{1, 0x44, 0x33, 0x22, 0x11}; !reflect.DeepEqual(success.Payload(), want) {
			t.Fatalf("success 0x%04X = % X", responseOpcode, success.Payload())
		}
		failure, err := BuildQuestResponse(responseOpcode, false, 0, 5)
		if err != nil {
			t.Fatal(err)
		}
		if want := []byte{2, 5}; !reflect.DeepEqual(failure.Payload(), want) {
			t.Fatalf("failure 0x%04X = % X", responseOpcode, failure.Payload())
		}
	}

	rewardFailure := BuildQuestRewardSelectionResponse(false, 2)
	if want := []byte{2, 2}; rewardFailure.MessageID != opcode.QuestRewardSelectionResponse || !reflect.DeepEqual(rewardFailure.Payload(), want) {
		t.Fatalf("reward failure = opcode 0x%04X payload % X", rewardFailure.MessageID, rewardFailure.Payload())
	}
}
