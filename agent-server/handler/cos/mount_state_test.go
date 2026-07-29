package cos

import (
	"context"
	"encoding/binary"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
	"github.com/ferdoran/go-sro/framework/network/opcode"
	"github.com/ferdoran/go-sro/framework/server"
)

type playerLookupFake struct {
	player *model.Player
	err    error
}

func (f *playerLookupFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return f.player, f.err
}

func mountRequest(action byte, uniqueID uint32) network.Packet {
	packet := network.EmptyClientPacket()
	packet.WriteByte(action)
	packet.WriteUInt32(uniqueID)
	return packet
}

func activeMountHandler(t *testing.T) (*MountStateHandler, *model.Player, *server.Session, *[]network.Packet) {
	t.Helper()
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session}
	if err := player.COSState.ActivateTransport(88, 99, false); err != nil {
		t.Fatal(err)
	}
	handler := NewMountStateHandler(&playerLookupFake{player: player})
	sent := &[]network.Packet{}
	handler.send = func(_ *server.Session, packet network.Packet) error {
		*sent = append(*sent, packet)
		return nil
	}
	handler.broadcast = func(_ *model.Player, packet network.Packet) error {
		*sent = append(*sent, packet)
		return nil
	}
	return handler, player, session, sent
}

func TestMountStateSuccessPacketMatchesRSBotVSRO188Parser(t *testing.T) {
	packet := buildMountStateSuccess(0x11223344, true, 0x55667788)
	want := []byte{1, 0x44, 0x33, 0x22, 0x11, 1, 0x88, 0x77, 0x66, 0x55}
	if packet.MessageID != opcode.COSUpdateMountStateResponse || !reflect.DeepEqual(packet.Payload(), want) {
		t.Fatalf("B0CB = opcode 0x%04X payload %x", packet.MessageID, packet.Payload())
	}
	if got := binary.LittleEndian.Uint32(packet.Payload()[1:5]); got != 0x11223344 {
		t.Fatalf("owner unique ID = %08x", got)
	}
}

func TestParseMountStateRequestIsExact(t *testing.T) {
	valid := mountRequest(1, 88)
	request, err := parseMountStateRequest(&valid)
	if err != nil || !request.mounted || request.transportUniqueID != 88 {
		t.Fatalf("valid request = %#v, %v", request, err)
	}
	for _, test := range []struct {
		name   string
		packet network.Packet
	}{
		{name: "short", packet: func() network.Packet { p := network.EmptyClientPacket(); p.WriteByte(1); return p }()},
		{name: "trailing", packet: func() network.Packet { p := mountRequest(1, 88); p.WriteByte(0); return p }()},
		{name: "invalid action", packet: mountRequest(2, 88)},
		{name: "zero uid", packet: mountRequest(1, 0)},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseMountStateRequest(&test.packet); err == nil {
				t.Fatal("request unexpectedly accepted")
			}
		})
	}
}

func TestMountStateHandlerMountsAndIdempotentlyDismounts(t *testing.T) {
	handler, player, session, sent := activeMountHandler(t)
	if err := handler.handlePacket(context.Background(), session, mountRequest(1, 88)); err != nil {
		t.Fatalf("mount: %v", err)
	}
	if err := handler.handlePacket(context.Background(), session, mountRequest(0, 88)); err != nil {
		t.Fatalf("dismount: %v", err)
	}
	if err := handler.handlePacket(context.Background(), session, mountRequest(0, 88)); err != nil {
		t.Fatalf("duplicate dismount: %v", err)
	}
	if snapshot := player.COSState.Snapshot(); snapshot.Mounted {
		t.Fatalf("state = %#v", snapshot)
	}
	if len(*sent) != 6 {
		t.Fatalf("sent %d responses", len(*sent))
	}
	wantLast := []byte{1, 77, 0, 0, 0, 0, 88, 0, 0, 0}
	if !reflect.DeepEqual((*sent)[5].Payload(), wantLast) {
		t.Fatalf("last response = %x", (*sent)[5].Payload())
	}
}

func TestMountStateHandlerRejectsForeignTransportAndSession(t *testing.T) {
	handler, player, session, sent := activeMountHandler(t)
	if err := handler.handlePacket(context.Background(), session, mountRequest(1, 999)); err != nil {
		t.Fatalf("foreign transport should receive protocol failure: %v", err)
	}
	if len(*sent) != 1 || !reflect.DeepEqual((*sent)[0].Payload(), []byte{2}) {
		t.Fatalf("failure response = %#v", *sent)
	}
	if player.COSState.Snapshot().Mounted {
		t.Fatal("foreign transport changed mount state")
	}

	foreignSession := &server.Session{UserContext: session.UserContext}
	if err := handler.handlePacket(context.Background(), foreignSession, mountRequest(1, 88)); err == nil {
		t.Fatal("foreign session unexpectedly accepted")
	}
}

func TestMountStateHandlerConcurrentDuplicateMount(t *testing.T) {
	handler, player, session, _ := activeMountHandler(t)
	var sentMu sync.Mutex
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sentMu.Lock()
		defer sentMu.Unlock()
		if !reflect.DeepEqual(packet.Payload(), []byte{1, 77, 0, 0, 0, 1, 88, 0, 0, 0}) {
			return errors.New("unexpected response")
		}
		return nil
	}
	handler.broadcast = func(_ *model.Player, packet network.Packet) error {
		if !reflect.DeepEqual(packet.Payload(), []byte{1, 77, 0, 0, 0, 1, 88, 0, 0, 0}) {
			return errors.New("unexpected broadcast response")
		}
		return nil
	}
	const workers = 32
	var wg sync.WaitGroup
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results <- handler.handlePacket(context.Background(), session, mountRequest(1, 88))
		}()
	}
	wg.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent mount: %v", err)
		}
	}
	if snapshot := player.COSState.Snapshot(); !snapshot.Mounted {
		t.Fatalf("final state = %#v", snapshot)
	}
}
