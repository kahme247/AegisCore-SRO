package inventory

import (
	"context"
	"errors"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
)

type inventoryHandlerPlayerLookupFake struct {
	player *model.Player
	err    error
	uid    uint32
	calls  int
}

func (f *inventoryHandlerPlayerLookupFake) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	f.calls++
	f.uid = uid
	return f.player, f.err
}

type inventoryHandlerRelocatorFake struct {
	calls       int
	characterID int64
	plan        model.InventoryRelocationPlan
	err         error
}

func (f *inventoryHandlerRelocatorFake) Relocate(_ context.Context, characterID int64, plan model.InventoryRelocationPlan) error {
	f.calls++
	f.characterID = characterID
	f.plan = plan
	return f.err
}

func TestInventoryMoveWithTrailingPayloadIsRejectedBeforeLookupOrDatabase(t *testing.T) {
	lookup := &inventoryHandlerPlayerLookupFake{}
	relocator := &inventoryHandlerRelocatorFake{}
	handler := NewInventoryHandler(lookup, relocator)
	packet := inventoryHandlerMovePacket(t, 14, 15, 0, []byte{0xFF})

	err := handler.handlePacket(context.Background(), &server.Session{}, packet)
	if err == nil {
		t.Fatal("handlePacket() accepted a move packet with trailing bytes")
	}
	if lookup.calls != 0 {
		t.Fatalf("trailing packet looked up a player %d times", lookup.calls)
	}
	if relocator.calls != 0 {
		t.Fatalf("trailing packet reached relocation repository %d times", relocator.calls)
	}
}

func TestParseAvatarTransferRequestUsesTwoSlotPayload(t *testing.T) {
	packet := network.EmptyClientPacket()
	mustWriteAvatarTest(t, packet.WriteByte(32))
	mustWriteAvatarTest(t, packet.WriteByte(4))

	request, err := parseAvatarTransferRequest(&packet)
	if err != nil {
		t.Fatalf("parseAvatarTransferRequest() error = %v", err)
	}
	if request.sourceSlot != 32 || request.targetSlot != 4 || request.amount != 0 {
		t.Fatalf("request = %+v, want source 32 target 4", request)
	}
}

func TestParseAvatarTransferRequestRejectsTrailingData(t *testing.T) {
	packet := network.EmptyClientPacket()
	mustWriteAvatarTest(t, packet.WriteByte(32))
	mustWriteAvatarTest(t, packet.WriteByte(4))
	mustWriteAvatarTest(t, packet.WriteByte(0xff))

	if _, err := parseAvatarTransferRequest(&packet); err == nil {
		t.Fatal("parseAvatarTransferRequest() accepted trailing data")
	}
}

func TestAvatarTransferSuccessEchoesAvatarOperation(t *testing.T) {
	packet := buildAvatarTransferSuccess(EquipAvatarItemOperation, moveItemRequest{sourceSlot: 35, targetSlot: 4})
	success, e1 := packet.ReadByte()
	operation, e2 := packet.ReadByte()
	source, e3 := packet.ReadByte()
	target, e4 := packet.ReadByte()
	quantity, e5 := packet.ReadUInt16()
	reserved, e6 := packet.ReadByte()
	if errors.Join(e1, e2, e3, e4, e5, e6) != nil {
		t.Fatal(errors.Join(e1, e2, e3, e4, e5, e6))
	}
	if packet.MessageID != 0xB034 || success != 1 || operation != byte(EquipAvatarItemOperation) || source != 35 || target != 4 || quantity != 0 || reserved != 0 || packet.Remaining() != 0 {
		t.Fatalf("avatar response = opcode 0x%04X payload %d/%d/%d/%d/%d/%d trailing %d", packet.MessageID, success, operation, source, target, quantity, reserved, packet.Remaining())
	}
}

func mustWriteAvatarTest(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestInventoryRelocationUsesSessionUniqueAndCharacterIDsNotCharacterName(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{
		UniqueID:    77,
		CharacterID: 44,
		CharName:    "forged-display-name",
	}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 77},
		ID:       44,
		CharName: "authoritative-display-name",
		Session:  session,
		Inventory: model.Inventory{Items: map[byte]model.Item{
			14: inventoryHandlerTestItem(101),
		}},
	}
	lookup := &inventoryHandlerPlayerLookupFake{player: player}
	relocator := &inventoryHandlerRelocatorFake{}
	handler := NewInventoryHandler(lookup, relocator)

	resolved, plan, err := handler.relocate(context.Background(), session, moveItemRequest{sourceSlot: 14, targetSlot: 15})
	if err != nil {
		t.Fatalf("relocate() error = %v", err)
	}
	if resolved != player {
		t.Fatalf("relocate() player = %p, want %p", resolved, player)
	}
	if lookup.calls != 1 || lookup.uid != 77 {
		t.Fatalf("player lookup = (%d calls, uid %d), want one lookup for 77", lookup.calls, lookup.uid)
	}
	if relocator.calls != 1 || relocator.characterID != 44 {
		t.Fatalf("repository = (%d calls, char %d), want one call for character 44", relocator.calls, relocator.characterID)
	}
	if plan.SourceItemID != 101 || relocator.plan.SourceItemID != 101 {
		t.Fatalf("relocation plan = %+v / %+v, want source item 101", plan, relocator.plan)
	}
	if _, exists := player.Inventory.Items[14]; exists {
		t.Fatal("source still exists after successful database-first relocation")
	}
	if got := player.Inventory.Items[15].ID; got != 101 {
		t.Fatalf("target item ID = %d, want 101", got)
	}
}

func TestInventoryMovePropagatesUnknownCommitOutcomeToCloseSession(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{
		SRObject:  model.SRObject{UniqueID: 77},
		ID:        44,
		Session:   session,
		Inventory: model.Inventory{Items: map[byte]model.Item{14: inventoryHandlerTestItem(101)}},
	}
	lookup := &inventoryHandlerPlayerLookupFake{player: player}
	relocator := &inventoryHandlerRelocatorFake{err: model.ErrInventoryRelocationOutcomeUnknown}
	handler := NewInventoryHandler(lookup, relocator)

	err := handler.handlePacket(context.Background(), session, inventoryHandlerMovePacket(t, 14, 15, 0, nil))
	if !errors.Is(err, model.ErrInventoryRelocationOutcomeUnknown) {
		t.Fatalf("handlePacket() error = %v, want unknown commit outcome", err)
	}
	if relocator.calls != 1 {
		t.Fatalf("unknown commit outcome reached relocation repository %d times, want 1", relocator.calls)
	}
	if _, exists := player.Inventory.Items[14]; !exists {
		t.Fatal("unknown commit outcome mutated the runtime source slot before session teardown")
	}
	if _, exists := player.Inventory.Items[15]; exists {
		t.Fatal("unknown commit outcome mutated the runtime target slot before session teardown")
	}
}

func TestInventoryRelocationRejectsResolvedPlayerWithMismatchedCharacterID(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	lookup := &inventoryHandlerPlayerLookupFake{player: &model.Player{
		SRObject: model.SRObject{UniqueID: 77},
		ID:       45,
		Session:  session,
	}}
	relocator := &inventoryHandlerRelocatorFake{}

	_, _, err := NewInventoryHandler(lookup, relocator).relocate(context.Background(), session, moveItemRequest{sourceSlot: 14, targetSlot: 15})
	if !errors.Is(err, ErrInventoryHandlerSessionPlayerMismatch) {
		t.Fatalf("relocate() error = %v, want session/player mismatch", err)
	}
	if relocator.calls != 0 {
		t.Fatalf("mismatched player reached relocation repository %d times", relocator.calls)
	}
}

func inventoryHandlerMovePacket(t *testing.T, sourceSlot, targetSlot byte, amount uint16, trailing []byte) network.Packet {
	t.Helper()
	packet := network.EmptyClientPacket()
	if err := packet.WriteByte(byte(MoveItemOperation)); err != nil {
		t.Fatalf("write operation: %v", err)
	}
	if err := packet.WriteByte(sourceSlot); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := packet.WriteByte(targetSlot); err != nil {
		t.Fatalf("write target: %v", err)
	}
	if err := packet.WriteUInt16(amount); err != nil {
		t.Fatalf("write amount: %v", err)
	}
	if err := packet.WriteBytes(trailing); err != nil {
		t.Fatalf("write trailing payload: %v", err)
	}
	return packet
}

func inventoryHandlerTestItem(id int) model.Item {
	return model.Item{ID: id}
}
