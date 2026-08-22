package inventory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
)

type itemUseRepositoryFake struct {
	calls  int
	result model.ItemUseResult
}

type returnTeleportFake struct {
	prepared    int
	aborted     int
	completed   int
	destination model.TeleportDestination
}

func (f *returnTeleportFake) PrepareItemReturn(*server.Session, *model.Player) error {
	f.prepared++
	return nil
}

func (f *returnTeleportFake) AbortItemReturn(*server.Session) { f.aborted++ }

func (f *returnTeleportFake) CompleteItemReturn(_ *server.Session, _ *model.Player, destination model.TeleportDestination) error {
	f.completed++
	f.destination = destination
	return nil
}

func (f *itemUseRepositoryFake) UseItem(context.Context, int64, byte, uint16, model.ItemUseVitals) (model.ItemUseResult, error) {
	f.calls++
	return f.result, nil
}

func (f *itemUseRepositoryFake) UseResurrectionItem(context.Context, int64, model.ItemUseVitals) (model.ItemUseResult, error) {
	return model.ItemUseResult{}, model.ErrItemUseUnsupported
}

func TestItemUseMalformedPayloadFailsBeforeIdentityOrDatabaseLookup(t *testing.T) {
	lookup := &inventoryHandlerPlayerLookupFake{}
	repository := &itemUseRepositoryFake{}
	h := NewItemUseHandler(lookup, repository)
	var sent []network.Packet
	h.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	packet := network.EmptyClientPacket()
	packet.WriteByte(14)
	packet.WriteUInt16(0x08EC)
	packet.WriteByte(0xFF)
	if err := h.handlePacket(context.Background(), &server.Session{}, packet); err != nil {
		t.Fatalf("handlePacket() error = %v", err)
	}
	if lookup.calls != 0 || repository.calls != 0 {
		t.Fatalf("malformed request reached lookup/repository: %d/%d", lookup.calls, repository.calls)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.InventoryItemUseResponse {
		t.Fatalf("failure packets = %+v, want one 0xB04C response", sent)
	}
}

func TestItemUseResponseMatchesExactVSRO188Parser(t *testing.T) {
	success := buildItemUseSuccess(14, 0x08EC, 49)
	result, e1 := success.ReadByte()
	slot, e2 := success.ReadByte()
	amount, e3 := success.ReadUInt16()
	itemType, e4 := success.ReadUInt16()
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || result != 1 || slot != 14 || itemType != 0x08EC || amount != 49 || success.Remaining() != 0 {
		t.Fatalf("success response = result %d slot %d itemType 0x%04X amount %d remaining %d errors %v/%v/%v/%v", result, slot, itemType, amount, success.Remaining(), e1, e2, e3, e4)
	}
	failure := buildItemUseFailure()
	result, e1 = failure.ReadByte()
	errorCode, e2 := failure.ReadUInt16()
	if e1 != nil || e2 != nil || result != 2 || errorCode != 0 || failure.Remaining() != 0 {
		t.Fatalf("failure response = result %d errorCode %d remaining %d errors %v/%v", result, errorCode, failure.Remaining(), e1, e2)
	}
}

func TestDevilSpiritActivationUsesInventoryItem(t *testing.T) {
	original := model.RefItems
	model.RefItems = map[uint32]model.RefItem{24405: {
		RefObject: model.RefObject{ID: 24405, CodeName: "ITEM_MALL_AVATAR_M_NASRUN", TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 14, TypeID4: 1}},
	}}
	t.Cleanup(func() { model.RefItems = original })

	item := model.Item{ID: 148, SRObject: model.SRObject{RefObjectID: 24405}, StackSize: 1, Data: 180000}
	item.RentInfo.PeriodEndTime = uint32(time.Now().Add(3 * time.Minute).Unix())
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 7}
	var sent []network.Packet
	var published []network.Packet
	h := &ItemUseHandler{
		devilSpirit: &devilSpiritRepositoryFake{activation: model.DevilSpiritActivation{Inventory: model.NewInventory(map[byte]model.Item{35: item}), Item: item, Slot: 35, ExpiresAt: time.Now().Add(3 * time.Minute)}},
		send: func(_ *server.Session, packet network.Packet) error {
			sent = append(sent, packet)
			return nil
		},
		publish: func(_ *model.Player, packet *network.Packet) {
			published = append(published, *packet)
		},
	}
	h.schedule = func(time.Duration, func()) {}
	if err := h.activateDevilSpirit(context.Background(), &server.Session{}, player, 35); err != nil {
		t.Fatalf("activateDevilSpirit() error = %v", err)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.InventoryItemUseResponse {
		t.Fatalf("item-use responses = %+v", sent)
	}
	if len(published) != 1 || published[0].MessageID != opcode.EntityItemUsed {
		t.Fatalf("published packets = %+v", published)
	}
	uid, e1 := published[0].ReadUInt32()
	refID, e2 := published[0].ReadUInt32()
	if errors.Join(e1, e2) != nil || uid != 77 || refID != 24405 || published[0].Remaining() != 0 {
		t.Fatalf("devil animation = uid %d ref %d trailing %d errors %v", uid, refID, published[0].Remaining(), errors.Join(e1, e2))
	}
}

type devilSpiritRepositoryFake struct {
	activation model.DevilSpiritActivation
	expiration model.DevilSpiritExpiration
}

func (f *devilSpiritRepositoryFake) GetCharacterAvatarInventory(context.Context, int64) (model.Inventory, error) {
	return model.NewInventory(nil), nil
}
func (f *devilSpiritRepositoryFake) TransferAvatarItem(context.Context, int64, bool, byte, byte) (model.AvatarTransferResult, error) {
	return model.AvatarTransferResult{}, nil
}
func (f *devilSpiritRepositoryFake) ActivateDevilSpirit(context.Context, int64, byte) (model.DevilSpiritActivation, error) {
	return f.activation, nil
}
func (f *devilSpiritRepositoryFake) ExpireDevilSpirit(context.Context, int64, int, time.Time) (model.DevilSpiritExpiration, error) {
	return f.expiration, nil
}

func TestSpeedItemPublishesTimedBuffIcon(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100}, ID: 7, Level: 10,
		BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
		WalkSpeed: 15, RunSpeed: 30, Session: session,
	}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{
		SourceSlot: 33, ItemType: 0x0EEC, NewAmount: 9,
		EffectSkillID: 3977, SpeedPercent: 100, EffectDuration: 1800000, EffectExpiresAt: time.Now().Add(30 * time.Minute),
	}}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.ConfigureTimedBuffDuration(true)
	var published []network.Packet
	var expire func()
	h.send = func(_ *server.Session, packet network.Packet) error { return nil }
	h.publish = func(_ *model.Player, packet *network.Packet) { published = append(published, *packet) }
	h.schedule = func(delay time.Duration, callback func()) {
		if delay != 30*time.Minute {
			t.Fatalf("speed effect duration = %v, want 30m", delay)
		}
		expire = callback
	}
	request := network.EmptyClientPacket()
	request.WriteByte(33)
	request.WriteUInt16(0x0EEC)
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("speed item use failed: %v", err)
	}
	if len(published) != 1 || published[0].MessageID != opcode.EntityBuffAdd || expire == nil {
		t.Fatalf("published packets = %+v, expiry scheduled = %v", published, expire != nil)
	}
	for range 3 {
		if _, err := published[0].ReadUInt32(); err != nil {
			t.Fatal(err)
		}
	}
	duration, err := published[0].ReadUInt32()
	if err != nil || duration != 1800000 || published[0].Remaining() != 0 {
		t.Fatalf("published speed duration = %d remaining %d: %v", duration, published[0].Remaining(), err)
	}
	expire()
	if len(published) != 2 || published[1].MessageID != opcode.EntityBuffRemove {
		t.Fatalf("expiration packets = %+v, want buff removal", published)
	}
}

func TestReferenceSkillItemAppliesAndExpiresStatBuff(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100}, ID: 7, Level: 10, Session: session,
		BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
	}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{
		Inventory: model.Inventory{Items: map[byte]model.Item{}}, HP: 600, MP: 250,
		SourceSlot: 33, ItemType: 0x6EEC, NewAmount: 2,
		EffectSkillID: 5410, EffectDuration: 600_000,
		BuffModifiers: model.BuffModifiers{DamagePercent: 20, HP: 500, HitRate: 10},
	}}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.ConfigureTimedBuffDuration(true)
	var published []network.Packet
	var expire func()
	h.send = func(*server.Session, network.Packet) error { return nil }
	h.publish = func(_ *model.Player, packet *network.Packet) { published = append(published, *packet) }
	h.schedule = func(delay time.Duration, callback func()) {
		if delay != 10*time.Minute {
			t.Fatalf("buff duration = %v, want 10m", delay)
		}
		expire = callback
	}
	request := network.EmptyClientPacket()
	request.WriteByte(33)
	request.WriteUInt16(0x6EEC)
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("buff item use failed: %v", err)
	}
	if player.BonusAttackStats.PhyAttackPercent != 20 || player.BonusStats.HP != 500 || player.BonusAttackStats.HitRate != 10 {
		t.Fatalf("applied modifiers = attack %d hp %d hit %d", player.BonusAttackStats.PhyAttackPercent, player.BonusStats.HP, player.BonusAttackStats.HitRate)
	}
	if len(published) != 1 || published[0].MessageID != opcode.EntityBuffAdd || expire == nil {
		t.Fatalf("buff publication = %+v expiry=%v", published, expire != nil)
	}
	expire()
	if player.BonusAttackStats.PhyAttackPercent != 0 || player.BonusStats.HP != 0 || player.BonusAttackStats.HitRate != 0 {
		t.Fatalf("expired modifiers = attack %d hp %d hit %d", player.BonusAttackStats.PhyAttackPercent, player.BonusStats.HP, player.BonusAttackStats.HitRate)
	}
	if len(published) != 2 || published[1].MessageID != opcode.EntityBuffRemove {
		t.Fatalf("expiration publication = %+v", published)
	}
}

func TestReturnScrollPreparesAndCompletesAfterReferenceDelay(t *testing.T) {
	previousRefs := model.RefItems
	t.Cleanup(func() { model.RefItems = previousRefs })
	ref := model.RefItem{RefObject: model.RefObject{
		ID: 61, IsUseable: true,
		TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 1},
	}}
	ref.Param1, ref.Param3Desc = 5_000, "RESURRECT"
	model.RefItems = map[uint32]model.RefItem{61: ref}

	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100}, ID: 7, Level: 10, Session: session,
		BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
		Inventory: model.Inventory{Items: map[byte]model.Item{
			14: {SRObject: model.SRObject{RefObjectID: 61}, StackSize: 2},
		}},
	}
	destination := model.TeleportDestination{ID: 1, Region: 25000, X: 969, Z: 1369, WorldID: 1}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{
		Inventory: model.Inventory{Items: map[byte]model.Item{}}, HP: 600, MP: 250,
		SourceSlot: 14, ItemType: model.InventoryItemType(ref), NewAmount: 1,
		ReturnDestination: &destination, ReturnDelay: 5 * time.Second,
	}}
	teleport := &returnTeleportFake{}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.returnTeleport = teleport
	h.send = func(*server.Session, network.Packet) error { return nil }
	var delayed func()
	h.schedule = func(delay time.Duration, callback func()) {
		if delay != 5*time.Second {
			t.Fatalf("return delay = %v, want 5s", delay)
		}
		delayed = callback
	}
	request := network.EmptyClientPacket()
	request.WriteByte(14)
	request.WriteUInt16(model.InventoryItemType(ref))
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("return-scroll item use failed: %v", err)
	}
	if teleport.prepared != 1 || teleport.aborted != 0 || teleport.completed != 0 || delayed == nil {
		t.Fatalf("return lifecycle before delay = prepared %d aborted %d completed %d scheduled %v", teleport.prepared, teleport.aborted, teleport.completed, delayed != nil)
	}
	delayed()
	if teleport.completed != 1 || teleport.destination != destination {
		t.Fatalf("completed return = %d destination %+v, want %+v", teleport.completed, teleport.destination, destination)
	}
}
