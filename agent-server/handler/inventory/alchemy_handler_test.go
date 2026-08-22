package inventory

import (
	"bytes"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

func TestAlchemyWaitUsesAnimationDuration(t *testing.T) {
	var waited time.Duration
	h := &AlchemyHandler{wait: func(duration time.Duration) { waited = duration }}
	h.waitFor(3 * time.Second)
	if waited != 3*time.Second {
		t.Fatalf("elixir wait = %v", waited)
	}
	h.waitFor(4500 * time.Millisecond)
	if waited != 4500*time.Millisecond {
		t.Fatalf("stone wait = %v", waited)
	}
}

func TestParseAlchemyElixirRequestVSRO188(t *testing.T) {
	packet := network.EmptyClientPacket()
	packet.WriteByte(alchemyActionFuse)
	packet.WriteByte(alchemyTypeElixir)
	packet.WriteByte(3)
	packet.WriteByte(13)
	packet.WriteByte(14)
	packet.WriteByte(15)
	request, err := parseAlchemyElixirRequest(&packet)
	if err != nil || request.cancel || request.targetSlot != 13 || request.elixirSlot != 14 || request.powderSlot == nil || *request.powderSlot != 15 {
		t.Fatalf("parseAlchemyElixirRequest() = %+v, %v", request, err)
	}

	cancel := network.EmptyClientPacket()
	cancel.WriteByte(alchemyActionCancel)
	request, err = parseAlchemyElixirRequest(&cancel)
	if err != nil || !request.cancel {
		t.Fatalf("cancel parse = %+v, %v", request, err)
	}

	duplicateLength := network.EmptyClientPacket()
	duplicateLength.WriteByte(alchemyActionFuse)
	duplicateLength.WriteByte(alchemyTypeElixir)
	duplicateLength.WriteByte(2)
	duplicateLength.WriteByte(13)
	duplicateLength.WriteByte(14)
	duplicateLength.WriteByte(99)
	if _, err = parseAlchemyElixirRequest(&duplicateLength); err == nil {
		t.Fatal("request with trailing byte unexpectedly parsed")
	}
}

func TestParseExtendedAlchemyRequestsVSRO188(t *testing.T) {
	stone := network.EmptyClientPacket()
	stone.WriteByte(alchemyActionFuse)
	stone.WriteByte(alchemyTypeMagicStone)
	stone.WriteByte(2)
	stone.WriteByte(13)
	stone.WriteByte(14)
	request, err := parseAlchemyPairRequest(&stone)
	if err != nil || request.alchemyType != alchemyTypeMagicStone || request.targetSlot != 13 || request.materialSlot != 14 {
		t.Fatalf("stone request = %+v, %v", request, err)
	}

	socket := network.EmptyClientPacket()
	socket.WriteByte(alchemyActionFuse)
	socket.WriteByte(alchemyTypeSocketInsert)
	socket.WriteByte(13)
	socket.WriteByte(14)
	request, err = parseAlchemySocketRequest(&socket)
	if err != nil || request.targetSlot != 13 || request.materialSlot != 14 {
		t.Fatalf("socket request = %+v, %v", request, err)
	}

	advanced := network.EmptyClientPacket()
	advanced.WriteByte(alchemyActionFuse)
	advanced.WriteByte(alchemyTypeAdvancedElixir)
	advanced.WriteByte(2)
	advanced.WriteByte(13)
	advanced.WriteByte(14)
	elixir, err := parseAlchemyElixirRequest(&advanced)
	if err != nil || elixir.alchemyType != alchemyTypeAdvancedElixir || elixir.powderSlot != nil {
		t.Fatalf("advanced request = %+v, %v", elixir, err)
	}
}

func TestAlchemyResponsesMatchVSRO188Layout(t *testing.T) {
	item := model.Item{OptLevel: 3, Variance: 4, Data: 120}
	item.RefObjectID = 1234
	item.SetTypeInfo(model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 2})
	packet := buildAlchemySuccess(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item})
	if packet.MessageID != opcode.AlchemyElixirResponse {
		t.Fatalf("message ID = %#x", packet.MessageID)
	}
	result, e1 := packet.ReadByte()
	action, e2 := packet.ReadByte()
	success, e3 := packet.ReadBool()
	slot, e4 := packet.ReadByte()
	rent, e5 := packet.ReadUInt32()
	refID, e6 := packet.ReadUInt32()
	opt, e7 := packet.ReadByte()
	if result != 1 || action != 2 || !success || slot != 13 || rent != 0 || refID != 1234 || opt != 3 || e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || e7 != nil {
		t.Fatalf("alchemy ACK prefix = %d/%d/%v/%d/%d/%d/%d errors %v/%v/%v/%v/%v/%v/%v", result, action, success, slot, rent, refID, opt, e1, e2, e3, e4, e5, e6, e7)
	}

	quantity := buildAlchemyMaterialQuantity(14, 9)
	qSlot, _ := quantity.ReadByte()
	flag, _ := quantity.ReadByte()
	amount, _ := quantity.ReadUInt16()
	if quantity.MessageID != opcode.InventoryItemUpdate || qSlot != 14 || flag != 8 || amount != 9 || quantity.Remaining() != 0 {
		t.Fatalf("quantity update = opcode %#x slot %d flag %d amount %d remaining %d", quantity.MessageID, qSlot, flag, amount, quantity.Remaining())
	}

	removed := buildAlchemyMaterialRemoved(15)
	ok, _ := removed.ReadByte()
	operation, _ := removed.ReadByte()
	rSlot, _ := removed.ReadByte()
	if removed.MessageID != opcode.ItemOperationResponse || ok != 1 || operation != 15 || rSlot != 15 || removed.Remaining() != 0 {
		t.Fatalf("remove update = opcode %#x result %d operation %d slot %d", removed.MessageID, ok, operation, rSlot)
	}
}

func TestAlchemyResultPayloadsMatchVSRO188Layouts(t *testing.T) {
	item := model.Item{OptLevel: 0, Data: 10}
	item.RefObjectID = 1234
	item.SetTypeInfo(model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 2})
	target := network.EmptyPacket()
	writeAlchemyTarget(&target, item)
	targetPayload := append([]byte(nil), target.Payload()...)

	tests := []struct {
		name   string
		packet network.Packet
		opcode uint16
		prefix []byte
		target bool
	}{
		{name: "elixir success", packet: buildElixirAlchemyResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), opcode: opcode.AlchemyElixirResponse, prefix: []byte{1, 2, 1, 13}, target: true},
		{name: "elixir non-destructive failure", packet: buildElixirAlchemyResult(model.AlchemyResult{TargetSlot: 13, Target: item}), opcode: opcode.AlchemyElixirResponse, prefix: []byte{1, 2, 0, 13, 0}, target: true},
		{name: "elixir destruction", packet: buildElixirAlchemyResult(model.AlchemyResult{TargetSlot: 13, Destroyed: true}), opcode: opcode.AlchemyElixirResponse, prefix: []byte{1, 2, 0, 13, 1}},
		{name: "stone success", packet: buildStoneAlchemyResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), opcode: opcode.AlchemyStoneResponse, prefix: []byte{1, 2, 1, 13}, target: true},
		{name: "stone failure", packet: buildStoneAlchemyResult(model.AlchemyResult{TargetSlot: 13, Target: item}), opcode: opcode.AlchemyStoneResponse, prefix: []byte{1, 2, 0, 13}, target: true},
		{name: "socket success", packet: buildSocketAlchemyResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), opcode: opcode.AlchemySocketResponse, prefix: []byte{1, 2, 13}, target: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := append([]byte(nil), tt.prefix...)
			if tt.target {
				want = append(want, targetPayload...)
			}
			if tt.packet.MessageID != tt.opcode || !bytes.Equal(tt.packet.Payload(), want) {
				t.Fatalf("packet = opcode %#x payload % X, want opcode %#x payload % X", tt.packet.MessageID, tt.packet.Payload(), tt.opcode, want)
			}
		})
	}
}

func TestAlchemyProtocolFailuresMatchVSRO188Layouts(t *testing.T) {
	tests := []struct {
		name   string
		packet network.Packet
		opcode uint16
		want   []byte
	}{
		{name: "stone", packet: buildStoneAlchemyFailure(), opcode: opcode.AlchemyStoneResponse, want: []byte{2, 0, 0}},
		{name: "socket", packet: buildSocketAlchemyFailure(), opcode: opcode.AlchemySocketResponse, want: []byte{2, 2, 0, 0}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.packet.MessageID != tt.opcode || !bytes.Equal(tt.packet.Payload(), tt.want) {
				t.Fatalf("packet = opcode %#x payload % X, want opcode %#x payload % X", tt.packet.MessageID, tt.packet.Payload(), tt.opcode, tt.want)
			}
		})
	}
}
