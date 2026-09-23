package inventory

import (
	"bytes"
	"context"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

func TestAlchemyLifecycleAdvancesOnThirdGameTick(t *testing.T) {
	player := &model.Player{ID: 77, LifeState: model.Alive, BodyState: model.Normal}
	h := &AlchemyHandler{states: newAlchemyLifecycle()}
	if !h.beginDelayed(player, alchemyTypeElixir) {
		t.Fatal("begin alchemy operation failed")
	}
	h.TickAlchemy()
	h.TickAlchemy()
	select {
	case <-h.lifecycle().active[player.ID].wake:
		t.Fatal("operation woke before phase three")
	default:
	}
	h.TickAlchemy()
	if wake := h.awaitPhaseThree(context.Background(), player); !wake.execute || wake.suppressResponse {
		t.Fatalf("phase-three wake = %+v", wake)
	}
}

func TestAlchemyLifecycleInterruptsUntouchableAndSuppressesDuplicateCancel(t *testing.T) {
	player := &model.Player{ID: 78, LifeState: model.Alive, BodyState: model.Normal}
	h := &AlchemyHandler{states: newAlchemyLifecycle()}
	if !h.beginDelayed(player, alchemyTypeMagicStone) {
		t.Fatal("begin alchemy operation failed")
	}
	player.SetBodyState(model.Untouchable)
	h.TickAlchemy()
	if wake := h.awaitPhaseThree(context.Background(), player); wake.execute || wake.suppressResponse {
		t.Fatalf("body-state interruption wake = %+v", wake)
	}

	player.SetBodyState(model.Normal)
	h.lifecycle().finish(player.ID)
	if !h.beginDelayed(player, alchemyTypeMagicStone) || !h.lifecycle().cancel(player.ID) {
		t.Fatal("cancel setup failed")
	}
	if wake := h.awaitPhaseThree(context.Background(), player); wake.execute || !wake.suppressResponse {
		t.Fatalf("explicit cancel wake = %+v", wake)
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
	socket.WriteByte(2)
	request, err = parseAlchemySocketRequest(&socket)
	if err != nil || request.targetSlot != 13 || request.materialSlot != 14 || request.index != 2 {
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
	targetSlot, e4 := packet.ReadByte()
	if result != 1 || action != 2 || !success || targetSlot != 13 || e1 != nil || e2 != nil || e3 != nil || e4 != nil || packet.Remaining() == 0 {
		t.Fatalf("alchemy ACK = %d/%d/%v/%d errors %v/%v/%v/%v remaining %d", result, action, success, targetSlot, e1, e2, e3, e4, packet.Remaining())
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
	reserved, _ := removed.ReadByte()
	if removed.MessageID != opcode.ItemOperationResponse || ok != 1 || operation != 15 || rSlot != 15 || reserved != 0 || removed.Remaining() != 0 {
		t.Fatalf("remove update = opcode %#x result %d operation %d slot %d", removed.MessageID, ok, operation, rSlot)
	}
}

func TestAlchemyResultPayloadsMatchVSRO188Layouts(t *testing.T) {
	item := model.Item{OptLevel: 0, Data: 10}
	item.RefObjectID = 1234
	item.SetTypeInfo(model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 2})
	tests := []struct {
		name   string
		packet network.Packet
		opcode uint16
		prefix []byte
	}{
		{name: "elixir success", packet: buildElixirAlchemyResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), opcode: opcode.AlchemyElixirResponse, prefix: []byte{1, 2, 1, 13}},
		{name: "elixir non-destructive failure", packet: buildElixirAlchemyResult(model.AlchemyResult{TargetSlot: 13, Target: item}), opcode: opcode.AlchemyElixirResponse, prefix: []byte{1, 2, 0, 13, 0}},
		{name: "elixir destruction", packet: buildElixirAlchemyResult(model.AlchemyResult{TargetSlot: 13, Destroyed: true}), opcode: opcode.AlchemyElixirResponse, prefix: []byte{1, 2, 0, 13, 1}},
		{name: "stone success", packet: buildStoneAlchemyResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), opcode: opcode.AlchemyStoneResponse, prefix: []byte{1, 2, 1, 13}},
		{name: "stone failure", packet: buildStoneAlchemyResult(model.AlchemyResult{TargetSlot: 13, Target: item}), opcode: opcode.AlchemyStoneResponse, prefix: []byte{1, 2, 0, 13}},
		{name: "socket success", packet: buildSocketAlchemyResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), opcode: opcode.AlchemySocketResponse, prefix: []byte{1, 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			want := append([]byte(nil), tt.prefix...)
			exact := tt.name == "elixir destruction" || tt.name == "socket success"
			matches := bytes.HasPrefix(tt.packet.Payload(), want) && (!exact || bytes.Equal(tt.packet.Payload(), want))
			if tt.packet.MessageID != tt.opcode || !matches {
				t.Fatalf("packet = opcode %#x payload % X, want opcode %#x payload % X", tt.packet.MessageID, tt.packet.Payload(), tt.opcode, want)
			}
		})
	}
}

func TestAlchemyEquipmentTargetCarriesCompleteStockItemBody(t *testing.T) {
	item := model.Item{
		OptLevel: 12, Variance: 9, Data: 100,
		MagicOptions:    []model.MagicOption{{Type: 212, Value: 3}},
		SocketOptions:   []model.BindingOption{{Slot: 1, ID: 1000, Value: 2}},
		AdvancedElixirs: []model.BindingOption{{Slot: 0, ID: 25874, Value: 1}},
	}
	item.RefObjectID = 0x11223344
	item.SetTypeInfo(model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 2})
	wantTarget := []byte{
		0, 0, 0, 0, // no rent
		0x44, 0x33, 0x22, 0x11,
		12,
		9, 0, 0, 0, 0, 0, 0, 0,
		100, 0, 0, 0,
		1, 212, 0, 0, 0, 3, 0, 0, 0,
		1, 1, 1, 0xE8, 3, 0, 0, 2, 0, 0, 0,
		2, 1, 0, 0x12, 0x65, 0, 0, 1, 0, 0, 0,
	}
	tests := []struct {
		name   string
		packet network.Packet
		prefix []byte
	}{
		{name: "elixir", packet: buildElixirAlchemyResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), prefix: []byte{1, 2, 1, 13}},
		{name: "stone", packet: buildStoneAlchemyResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), prefix: []byte{1, 2, 1, 13}},
		{name: "socket", packet: buildSocketAlchemyActionResult(2, model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), prefix: []byte{1, 2, 13}},
		{name: "avatar grant", packet: buildAlchemyAvatarGrantResult(model.AlchemyResult{Success: true, TargetSlot: 13, Target: item}), prefix: []byte{1, 1, 13}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			want := append(append([]byte(nil), test.prefix...), wantTarget...)
			if got := test.packet.Payload(); !bytes.Equal(got, want) {
				t.Fatalf("payload = % X, want % X", got, want)
			}
		})
	}
}

func TestSocketActionResultsEmbedTargetSlot(t *testing.T) {
	for _, action := range []byte{2, 3, 4} {
		packet := buildSocketAlchemyActionResult(action, model.AlchemyResult{TargetSlot: 99})
		if packet.MessageID != opcode.AlchemySocketResponse || !bytes.HasPrefix(packet.Payload(), []byte{1, action, 99}) || len(packet.Payload()) == 3 {
			t.Errorf("socket action %d response = opcode %#x payload % X", action, packet.MessageID, packet.Payload())
		}
	}
}

func TestParseAlchemyAvatarRequests(t *testing.T) {
	avatar := network.EmptyPacket()
	avatar.WriteByte(alchemyActionFuse)
	avatar.WriteByte(2)
	avatar.WriteByte(13)
	avatar.WriteByte(14)
	request, err := parseAlchemyAvatarRequest(&avatar)
	if err != nil || request.cancel || !bytes.Equal(request.slots, []byte{13, 14}) {
		t.Fatalf("avatar request = %+v, %v", request, err)
	}

	cancel := network.EmptyPacket()
	cancel.WriteByte(alchemyActionCancel)
	request, err = parseAlchemyAvatarRequest(&cancel)
	if err != nil || !request.cancel || len(request.slots) != 0 {
		t.Fatalf("avatar cancel = %+v, %v", request, err)
	}

	grant := network.EmptyPacket()
	grant.WriteByte(13)
	grant.WriteString("MATTR_AVATAR_STR")
	grantRequest, err := parseAlchemyAvatarGrantRequest(&grant)
	if err != nil || grantRequest.targetSlot != 13 || grantRequest.optionCode != "MATTR_AVATAR_STR" {
		t.Fatalf("avatar grant = %+v, %v", grantRequest, err)
	}
}

func TestAlchemyAvatarResultLayouts(t *testing.T) {
	avatar := buildAlchemyAvatarResult(true)
	if avatar.MessageID != opcode.AlchemyAvatarResponse || !bytes.Equal(avatar.Payload(), []byte{1, 2, 1}) {
		t.Fatalf("avatar response = opcode %#x payload % X", avatar.MessageID, avatar.Payload())
	}
	grant := buildAlchemyAvatarGrantResult(model.AlchemyResult{
		Success:    true,
		TargetSlot: 13,
		Target: model.Item{
			SRObject: model.SRObject{RefObjectID: 0x11223344, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1}},
			OptLevel: 7,
			Variance: 0x0102030405060708,
			Data:     900,
		},
	})
	wantGrant := []byte{
		1, 1, 13,
		0, 0, 0, 0,
		0x44, 0x33, 0x22, 0x11,
		7,
		8, 7, 6, 5, 4, 3, 2, 1,
		0x84, 0x03, 0, 0,
		0,
		1, 0,
		2, 0,
	}
	if grant.MessageID != opcode.AlchemyAvatarGrantResponse || !bytes.Equal(grant.Payload(), wantGrant) {
		t.Fatalf("avatar grant response = opcode %#x payload % X", grant.MessageID, grant.Payload())
	}
}

func TestAlchemyProtocolFailuresMatchVSRO188Layouts(t *testing.T) {
	tests := []struct {
		name   string
		packet network.Packet
		opcode uint16
		want   []byte
	}{
		{name: "stone", packet: vsro188.BuildAlchemyFailure(opcode.AlchemyStoneResponse, 0), opcode: opcode.AlchemyStoneResponse, want: []byte{2, 0, 0}},
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

func TestAlchemyZeroCodeFailureOpcodesUseCanonicalWireLayout(t *testing.T) {
	for _, test := range []struct {
		name   string
		opcode uint16
	}{
		{name: "elixir", opcode: opcode.AlchemyElixirResponse},
		{name: "avatar", opcode: opcode.AlchemyAvatarResponse},
		{name: "material", opcode: opcode.AlchemyMaterialResponse},
		{name: "dismantle", opcode: opcode.AlchemyDismantleResponse},
	} {
		t.Run(test.name, func(t *testing.T) {
			packet := vsro188.BuildAlchemyFailure(test.opcode, 0)
			if packet.MessageID != test.opcode || !bytes.Equal(packet.Payload(), []byte{2, 0, 0}) {
				t.Fatalf("packet = opcode %#x payload % X, want opcode %#x payload 02 00 00", packet.MessageID, packet.Payload(), test.opcode)
			}
		})
	}
}

func TestParseMaterialAndDismantleRequestsVSRO188(t *testing.T) {
	material := network.EmptyClientPacket()
	material.WriteByte(2)
	material.WriteByte(1)
	material.WriteByte(2)
	material.WriteByte(13)
	material.WriteByte(14)
	request, err := parseAlchemyMaterialRequest(&material)
	if err != nil || request.selector != 1 || !bytes.Equal(request.slots, []byte{13, 14}) {
		t.Fatalf("material request = %+v, %v", request, err)
	}

	duplicate := network.EmptyClientPacket()
	duplicate.WriteByte(2)
	duplicate.WriteByte(1)
	duplicate.WriteByte(2)
	duplicate.WriteByte(13)
	duplicate.WriteByte(13)
	if _, err = parseAlchemyMaterialRequest(&duplicate); err == nil {
		t.Fatal("duplicate resolve slots unexpectedly accepted")
	}

	dismantle := network.EmptyClientPacket()
	dismantle.WriteByte(2)
	dismantle.WriteByte(15)
	dismantle.WriteByte(16)
	slots, err := parseAlchemyDismantleRequest(&dismantle)
	if err != nil || !bytes.Equal(slots, []byte{15, 16}) {
		t.Fatalf("dismantle slots = %v, %v", slots, err)
	}

	tooMany := network.EmptyClientPacket()
	tooMany.WriteByte(10)
	for slot := byte(13); slot < 23; slot++ {
		tooMany.WriteByte(slot)
	}
	if _, err = parseAlchemyDismantleRequest(&tooMany); err == nil {
		t.Fatal("ten dismantle slots unexpectedly accepted")
	}
}

func TestMaterialAndDismantleResponsesMatchBinaryLayouts(t *testing.T) {
	material := buildAlchemyMaterialSuccess(model.AlchemyMakeResult{ResponseByte: 21, ProducedValue: 0x11223344})
	if material.MessageID != opcode.AlchemyMaterialResponse || !bytes.Equal(material.Payload(), []byte{1, 2, 21, 0x44, 0x33, 0x22, 0x11}) {
		t.Fatalf("material response = opcode %#x payload % X", material.MessageID, material.Payload())
	}
	dismantle := buildAlchemyDismantleSuccess()
	if dismantle.MessageID != opcode.AlchemyDismantleResponse || !bytes.Equal(dismantle.Payload(), []byte{1}) {
		t.Fatalf("dismantle response = opcode %#x payload % X", dismantle.MessageID, dismantle.Payload())
	}
}
