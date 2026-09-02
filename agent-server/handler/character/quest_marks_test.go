package character

import (
	"errors"
	"reflect"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestQuestMarkPublisherAddsAndRemovesInWireOrder(t *testing.T) {
	player := &model.Player{}
	var sent []network.Packet
	publisher := questMarkPublisher{send: func(packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}}
	first, err := publisher.add(player, model.QuestMark{QuestID: 10, Flags: 2, Kind: 3, AuthoredValue: 4, X: -1, Y: 2, Z: 3, TargetID: 99})
	if err != nil {
		t.Fatal(err)
	}
	second, err := publisher.add(player, model.QuestMark{QuestID: 20})
	if err != nil {
		t.Fatal(err)
	}
	third, err := publisher.add(player, model.QuestMark{QuestID: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 3 || sent[0].MessageID != opcode.QuestMarkAdd {
		t.Fatalf("add packets = %#v", sent)
	}
	if got := sent[0].Payload()[:4]; !reflect.DeepEqual(got, []byte{byte(first.RuntimeID), 0, 0, 0}) {
		t.Fatalf("first runtime handle = % X", got)
	}
	if err = publisher.removeByQuest(player, 10); err != nil {
		t.Fatal(err)
	}
	if len(sent) != 5 || sent[3].MessageID != opcode.QuestMarkRemove || sent[4].MessageID != opcode.QuestMarkRemove {
		t.Fatalf("remove packets = %#v", sent)
	}
	if got, want := sent[3].Payload(), []byte{byte(first.RuntimeID), 0, 0, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("first removal = % X, want % X", got, want)
	}
	if got, want := sent[4].Payload(), []byte{byte(third.RuntimeID), 0, 0, 0}; !reflect.DeepEqual(got, want) {
		t.Fatalf("second removal = % X, want % X", got, want)
	}
	if got := player.QuestMarksSnapshot(); !reflect.DeepEqual(got, []model.QuestMark{second}) {
		t.Fatalf("remaining marks = %#v", got)
	}
}

func TestQuestMarkPublisherRollsBackAddOnSendFailure(t *testing.T) {
	player := &model.Player{}
	want := errors.New("send failed")
	publisher := questMarkPublisher{send: func(network.Packet) error { return want }}
	if _, err := publisher.add(player, model.QuestMark{QuestID: 10}); !errors.Is(err, want) {
		t.Fatalf("add error = %v", err)
	}
	if got := player.QuestMarksSnapshot(); len(got) != 0 {
		t.Fatalf("marks after failed add = %#v", got)
	}
}

func TestQuestMarkPublisherKeepsMarkOnRemoveSendFailure(t *testing.T) {
	player := &model.Player{}
	mark, err := player.AddQuestMark(model.QuestMark{QuestID: 10})
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("send failed")
	publisher := questMarkPublisher{send: func(network.Packet) error { return want }}
	if err = publisher.remove(player, mark.RuntimeID); !errors.Is(err, want) {
		t.Fatalf("remove error = %v", err)
	}
	if got, exists := player.QuestMarkByRuntimeID(mark.RuntimeID); !exists || got != mark {
		t.Fatalf("mark = %#v, exists=%t", got, exists)
	}
}
