package inventory

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/g3n/engine/math32"
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

type petInventoryWorldFake struct {
	player *model.Player
	pet    *model.COS
}

type groundPickupWorldFake struct {
	player  *model.Player
	item    *model.Item
	removed model.ISRObject
}

type groundPickupLifecycleFake struct{ canceled *model.Item }

func (f *groundPickupLifecycleFake) Cancel(item *model.Item) { f.canceled = item }

func (f *groundPickupWorldFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return f.player, nil
}

func (f *groundPickupWorldFake) GetObjectByUniqueId(uint32) (model.ISRObject, error) {
	return f.item, nil
}

func (f *groundPickupWorldFake) RemoveVisibleObject(object model.ISRObject) {
	f.removed = object
}

type groundPickupRepositoryFake struct {
	characterID int64
	itemID      int64
	slot        byte
	result      model.GroundItemPickupResult
	err         error
	calls       int
}

type groundGoldPickupRepositoryFake struct {
	characterID int64
	itemID      int64
	result      model.GroundGoldPickupResult
	err         error
	calls       int
}

type groundDropRepositoryFake struct {
	result      model.GroundItemDropResult
	err         error
	restoreErr  error
	characterID int64
	slot        byte
	restoredID  int64
	events      *[]string
	called      int
}

func (f *groundDropRepositoryFake) Drop(_ context.Context, characterID int64, slot byte) (model.GroundItemDropResult, error) {
	f.called++
	f.characterID, f.slot = characterID, slot
	if f.events != nil {
		*f.events = append(*f.events, "drop")
	}
	return f.result, f.err
}

func (f *groundDropRepositoryFake) Restore(_ context.Context, characterID int64, slot byte, itemID int64) error {
	f.characterID, f.slot, f.restoredID = characterID, slot, itemID
	if f.events != nil {
		*f.events = append(*f.events, "restore")
	}
	return f.restoreErr
}

type groundDropSpawnerFake struct {
	item   *model.Item
	err    error
	events *[]string
}

func (f *groundDropSpawnerFake) Spawn(item *model.Item, partyNumber uint32) error {
	f.item = item
	if partyNumber != 0 {
		return errors.New("ground-drop test expects no party ownership")
	}
	if f.events != nil {
		*f.events = append(*f.events, "spawn")
	}
	return f.err
}

func (f *groundGoldPickupRepositoryFake) PickupGold(_ context.Context, characterID, itemID int64) (model.GroundGoldPickupResult, error) {
	f.calls++
	f.characterID, f.itemID = characterID, itemID
	return f.result, f.err
}

type collectionProgressRepositoryFake struct {
	characterID int
	itemRefID   uint32
	quantity    uint32
	result      []model.QuestProgressResult
	err         error
	calls       int
}

func (f *collectionProgressRepositoryFake) RecordCollectedItem(_ context.Context, characterID int, itemRefID, quantity uint32) ([]model.QuestProgressResult, error) {
	f.calls++
	f.characterID, f.itemRefID, f.quantity = characterID, itemRefID, quantity
	return f.result, f.err
}

func (f *groundPickupRepositoryFake) Pickup(_ context.Context, characterID, itemID int64, slot byte) (model.GroundItemPickupResult, error) {
	f.calls++
	f.characterID, f.itemID, f.slot = characterID, itemID, slot
	return f.result, f.err
}

func (f *petInventoryWorldFake) GetPlayerByUniqueId(uint32) (*model.Player, error) {
	return f.player, nil
}
func (f *petInventoryWorldFake) GetObjectByUniqueId(uint32) (model.ISRObject, error) {
	return f.pet, nil
}

type petInventoryTransferFake struct {
	owner, itemSource, itemDestination int
	cosID                              uint64
	toCOS                              bool
	playerInventory, cosInventory      model.Inventory
}

func (f *petInventoryTransferFake) TransferItem(_ context.Context, owner int, cosID uint64, source, destination byte, toCOS bool) (model.Inventory, model.Inventory, error) {
	f.owner, f.cosID, f.itemSource, f.itemDestination, f.toCOS = owner, cosID, int(source), int(destination), toCOS
	return f.playerInventory, f.cosInventory, nil
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

func TestGroundItemPickupPersistsBeforeRemovingWorldObject(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}},
		ID:       44, Session: session,
	}
	item := &model.Item{
		SRObject: model.SRObject{UniqueID: 900, RefObjectID: 700, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 1}, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(3, 0, 0)}},
		ID:       91,
	}
	world := &groundPickupWorldFake{player: player, item: item}
	repository := &groundPickupRepositoryFake{result: model.GroundItemPickupResult{Slot: 13, Inventory: model.NewInventory(map[byte]model.Item{13: *item})}}
	lifecycle := &groundPickupLifecycleFake{}
	handler := NewInventoryHandler(world, nil)
	handler.ConfigureGroundItemPickupRepository(repository)
	handler.ConfigureGroundItemLifecycle(lifecycle)
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		packets = append(packets, packet)
		return nil
	}
	request := network.EmptyClientPacket()
	request.WriteByte(byte(PickupItemOperation))
	request.WriteUInt32(item.GetUniqueID())

	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("pickup: %v", err)
	}
	if repository.calls != 1 || repository.characterID != 44 || repository.itemID != 91 || repository.slot != 13 {
		t.Fatalf("repository call = %+v, want character 44 item 91 slot 13", repository)
	}
	if world.removed != item {
		t.Fatalf("removed object = %v, want picked item", world.removed)
	}
	if lifecycle.canceled != item {
		t.Fatalf("lifecycle canceled = %v, want picked item", lifecycle.canceled)
	}
	if got := player.Inventory.Items[13].ID; got != 91 {
		t.Fatalf("runtime inventory item = %d, want 91", got)
	}
	if len(packets) != 2 || packets[0].MessageID != 0x3036 || packets[1].MessageID != opcode.ItemOperationResponse {
		t.Fatalf("pickup packets = %#v, want 0x3036 followed by 0xB034", packets)
	}
	if got, want := packets[1].Payload(), []byte{1, 0x06, 0x0D}; !bytes.Equal(got, want) {
		t.Fatalf("pickup response payload = % X, want % X", got, want)
	}
	if got := packets[0].Payload(); len(got) != 5 || got[0] != 77 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Fatalf("pickup animation payload = % X, want collector UID 77 and one heading byte", got)
	}
}

func TestGroundItemDropPersistsBeforeWorldAdmission(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(10, 0, 20)}
	item := &model.Item{ID: 91, SRObject: model.SRObject{RefObjectID: 700}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: position}, ID: 44, Session: session, Inventory: model.NewInventory(map[byte]model.Item{14: *item})}
	world := &groundPickupWorldFake{player: player}
	events := make([]string, 0, 2)
	drop := &groundDropRepositoryFake{events: &events, result: model.GroundItemDropResult{Item: item, Slot: 14, Inventory: model.NewInventory(nil)}}
	spawner := &groundDropSpawnerFake{events: &events}
	handler := NewInventoryHandler(world, nil)
	handler.ConfigureGroundItemDropRepository(drop)
	handler.ConfigureGroundItemService(spawner)
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(DropItemOperation))
	request.WriteByte(14)

	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("drop: %v", err)
	}
	if drop.called != 1 || drop.characterID != 44 || drop.slot != 14 {
		t.Fatalf("drop repository call = %+v", drop)
	}
	if got, want := events, []string{"drop", "spawn"}; !bytes.Equal([]byte(joinEventNames(got)), []byte(joinEventNames(want))) {
		t.Fatalf("lifecycle events = %v, want %v", got, want)
	}
	if spawner.item != item || item.GetNavmeshPosition().Offset.X != 10 || item.GetNavmeshPosition().Offset.Z != 20 {
		t.Fatalf("spawned item/position = %v/%v", spawner.item, item.GetNavmeshPosition())
	}
	if _, exists := player.Inventory.Items[14]; exists {
		t.Fatal("runtime inventory retained the dropped item")
	}
	if response.MessageID != opcode.ItemOperationResponse {
		t.Fatalf("response opcode = 0x%04X, want 0xB034", response.MessageID)
	}
	if got, want := response.Payload(), []byte{1, 0x07, 14}; !bytes.Equal(got, want) {
		t.Fatalf("drop response = % X, want % X", got, want)
	}
}

func TestGroundItemDropRestoresLinkWhenWorldAdmissionFails(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	item := &model.Item{ID: 91, SRObject: model.SRObject{RefObjectID: 700}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}}, ID: 44, Session: session, Inventory: model.NewInventory(map[byte]model.Item{14: *item})}
	world := &groundPickupWorldFake{player: player}
	events := make([]string, 0, 3)
	drop := &groundDropRepositoryFake{events: &events, result: model.GroundItemDropResult{Item: item, Slot: 14, Inventory: model.NewInventory(nil)}}
	spawner := &groundDropSpawnerFake{events: &events, err: errors.New("world unavailable")}
	handler := NewInventoryHandler(world, nil)
	handler.ConfigureGroundItemDropRepository(drop)
	handler.ConfigureGroundItemService(spawner)
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(DropItemOperation))
	request.WriteByte(14)

	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("drop admission failure: %v", err)
	}
	if got, want := events, []string{"drop", "spawn", "restore"}; !bytes.Equal([]byte(joinEventNames(got)), []byte(joinEventNames(want))) {
		t.Fatalf("lifecycle events = %v, want %v", got, want)
	}
	if drop.restoredID != 91 {
		t.Fatalf("restored item ID = %d, want 91", drop.restoredID)
	}
	if _, exists := player.Inventory.Items[14]; !exists {
		t.Fatal("runtime inventory changed after compensated drop failure")
	}
	if response.MessageID != opcode.ItemOperationResponse {
		t.Fatalf("failure response opcode = 0x%04X, want 0xB034", response.MessageID)
	}
}

func joinEventNames(events []string) string {
	result := ""
	for _, event := range events {
		result += event + ","
	}
	return result
}

func TestGroundItemPickupPublishesCommittedCollectionProgress(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}},
		ID:       44, Session: session,
	}
	item := &model.Item{
		SRObject: model.SRObject{UniqueID: 900, RefObjectID: 700, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 1, TypeID4: 1}, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(3, 0, 0)}},
		ID:       91, StackSize: 3,
	}
	world := &groundPickupWorldFake{player: player, item: item}
	pickup := &groundPickupRepositoryFake{result: model.GroundItemPickupResult{Slot: 13, Inventory: model.NewInventory(map[byte]model.Item{13: *item})}}
	quest := model.CharacterQuest{QuestID: 58, Status: model.QuestProgressed, AchievementCount: 1, Data1: 3, ObjectiveKey: "SN_CON_QNO_CH_SMITH_2", TaskCount: 1}
	progress := &collectionProgressRepositoryFake{result: []model.QuestProgressResult{{Quest: quest, Changed: true}}}
	handler := NewInventoryHandler(world, nil)
	handler.ConfigureGroundItemPickupRepository(pickup)
	handler.ConfigureQuestProgressRepository(progress)
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { packets = append(packets, packet); return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(PickupItemOperation))
	request.WriteUInt32(item.GetUniqueID())

	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if progress.calls != 1 || progress.characterID != 44 || progress.itemRefID != 700 || progress.quantity != 3 {
		t.Fatalf("collection progress call = %+v", progress)
	}
	if len(packets) != 3 || packets[2].MessageID != opcode.QuestUpdate {
		t.Fatalf("packets = %#v", packets)
	}
	if len(player.Quests) != 1 || player.Quests[0] != quest {
		t.Fatalf("runtime quests = %#v", player.Quests)
	}
}

func TestGroundGoldPickupCreditsBalanceInsteadOfInventory(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}},
		ID:       44, Session: session, Gold: 100,
	}
	item := &model.Item{
		SRObject: model.SRObject{UniqueID: 901, RefObjectID: 1, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 5, TypeID4: 0}, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(3, 0, 0)}},
		ID:       92, Data: 250,
	}
	world := &groundPickupWorldFake{player: player, item: item}
	repository := &groundGoldPickupRepositoryFake{result: model.GroundGoldPickupResult{Gold: 350, Amount: 250}}
	lifecycle := &groundPickupLifecycleFake{}
	handler := NewInventoryHandler(world, nil)
	handler.ConfigureGroundGoldPickupRepository(repository)
	handler.ConfigureGroundItemLifecycle(lifecycle)
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { packets = append(packets, packet); return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(PickupItemOperation))
	request.WriteUInt32(item.GetUniqueID())

	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("gold pickup: %v", err)
	}
	if repository.calls != 1 || repository.characterID != 44 || repository.itemID != 92 {
		t.Fatalf("gold repository call = %+v", repository)
	}
	if player.Gold != 350 || len(player.Inventory.Items) != 0 {
		t.Fatalf("player state gold=%d inventory=%v", player.Gold, player.Inventory.Items)
	}
	if lifecycle.canceled != item || world.removed != item {
		t.Fatalf("lifecycle/world state canceled=%v removed=%v", lifecycle.canceled, world.removed)
	}
	if len(packets) != 2 || packets[0].MessageID != opcode.EntityAnimationPickup || packets[1].MessageID != opcode.EntityUpdatePoints {
		t.Fatalf("gold pickup packets = %#v", packets)
	}
}

func TestGroundItemPickupRejectsForeignOwnerBeforePersistence(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}}, ID: 44, Session: session}
	item := &model.Item{SRObject: model.SRObject{UniqueID: 900, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(1, 0, 0)}}, ID: 91, OwnerUniqueID: 88}
	world := &groundPickupWorldFake{player: player, item: item}
	repository := &groundPickupRepositoryFake{result: model.GroundItemPickupResult{Slot: 13, Inventory: model.NewInventory(nil)}}
	handler := NewInventoryHandler(world, nil)
	handler.ConfigureGroundItemPickupRepository(repository)
	handler.send = func(_ *server.Session, _ network.Packet) error { return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(PickupItemOperation))
	request.WriteUInt32(item.GetUniqueID())
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("foreign-owner pickup returned error: %v", err)
	}
	if repository.calls != 0 || world.removed != nil {
		t.Fatalf("foreign-owner pickup mutated state: repository=%d removed=%v", repository.calls, world.removed)
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

func TestPlayerItemMovesIntoAbilityPetInventory(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Inventory: model.NewInventory(map[byte]model.Item{14: {ID: 100}})}
	pet := &model.COS{NPC: model.NPC{SRObject: model.SRObject{UniqueID: 88}}, PersistentID: 9, OwnerCharID: 44, OwnerUniqueID: 77, Kind: model.COSAbilityPet, InventorySize: 28, Inventory: model.NewInventory(nil)}
	nextPlayer := model.NewInventory(nil)
	nextCOS := model.NewInventory(map[byte]model.Item{3: {ID: 100}})
	repository := &petInventoryTransferFake{playerInventory: nextPlayer, cosInventory: nextCOS}
	handler := NewInventoryHandler(&petInventoryWorldFake{player: player, pet: pet}, nil)
	handler.ConfigureCOSRepository(repository)
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(PutItemIntoPetInventoryOperation))
	request.WriteUInt32(88)
	request.WriteByte(14)
	request.WriteByte(3)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if repository.owner != 44 || repository.cosID != 9 || repository.itemSource != 14 || repository.itemDestination != 3 || !repository.toCOS {
		t.Fatalf("transfer = %+v", repository)
	}
	if len(player.GetInventory().Items) != 0 || pet.Snapshot().Inventory.Items[3].ID != 100 {
		t.Fatalf("runtime inventories player=%+v pet=%+v", player.GetInventory().Items, pet.Snapshot().Inventory.Items)
	}
	want := []byte{1, vsro188.ItemOperationCOSTransfer, 88, 0, 0, 0, 3}
	if response.MessageID != 0xB034 || !bytes.Equal(response.Payload(), want) {
		t.Fatalf("response = 0x%04X/% X, want 0xB034/% X", response.MessageID, response.Payload(), want)
	}
}

func TestPlayerItemMovesFromAbilityPetInventory(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	pet := &model.COS{NPC: model.NPC{SRObject: model.SRObject{UniqueID: 88}}, PersistentID: 9, OwnerCharID: 44, OwnerUniqueID: 77, Kind: model.COSAbilityPet, InventorySize: 28, Inventory: model.NewInventory(map[byte]model.Item{3: {ID: 100}})}
	nextPlayer := model.NewInventory(map[byte]model.Item{14: {ID: 100}})
	nextCOS := model.NewInventory(nil)
	repository := &petInventoryTransferFake{playerInventory: nextPlayer, cosInventory: nextCOS}
	handler := NewInventoryHandler(&petInventoryWorldFake{player: player, pet: pet}, nil)
	handler.ConfigureCOSRepository(repository)
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(TakeItemFromPetInventoryOperation))
	request.WriteUInt32(88)
	request.WriteByte(3)
	request.WriteByte(14)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if repository.owner != 44 || repository.cosID != 9 || repository.itemSource != 3 || repository.itemDestination != 14 || repository.toCOS {
		t.Fatalf("transfer = %+v", repository)
	}
	if player.GetInventory().Items[14].ID != 100 || len(pet.Snapshot().Inventory.Items) != 0 {
		t.Fatalf("runtime inventories player=%+v pet=%+v", player.GetInventory().Items, pet.Snapshot().Inventory.Items)
	}
	want := []byte{1, vsro188.ItemOperationCOSTransfer, 88, 0, 0, 0, 14}
	if response.MessageID != 0xB034 || !bytes.Equal(response.Payload(), want) {
		t.Fatalf("response = 0x%04X/% X, want 0xB034/% X", response.MessageID, response.Payload(), want)
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

	resolved, plan, _, err := handler.relocate(context.Background(), session, moveItemRequest{sourceSlot: 14, targetSlot: 15})
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

func TestInventoryEquipRecalculatesRequiredItemPassive(t *testing.T) {
	const passiveID = uint32(39)
	const fireShieldID = uint32(40)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	shield := model.Item{
		SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 4, TypeID4: 1}},
		ID:       101, LevelInfo: model.LevelInfo{RequiredLevelType1: 1, RequiredLevel1: 1},
	}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 77, RefObjectID: 1907}, ID: 44, Level: 1, Session: session,
		Skills: map[uint32]byte{passiveID: 1}, Inventory: model.NewInventory(map[byte]model.Item{14: shield}), AvatarInventory: model.NewInventory(nil),
	}
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{
		passiveID:    {ID: passiveID, BasicActivity: 0, BuffModifiers: model.BuffModifiers{BlockRate: 2}, RequiredItems: []model.SkillRequiredItem{{TypeID3: 4, TypeID4: 1}}},
		fireShieldID: {ID: fireShieldID, CodeName: "SKILL_CH_FIRE_SHIELD_A_01", BasicActivity: 2, RequiredItems: []model.SkillRequiredItem{{TypeID3: 4, TypeID4: 1}}},
	}})
	handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{player: player}, &inventoryHandlerRelocatorFake{})
	handler.ConfigureReferenceData(catalog)
	if _, _, _, err := handler.relocate(context.Background(), session, moveItemRequest{sourceSlot: 14, targetSlot: model.SlotSecondaryWeapon}); err != nil {
		t.Fatalf("equip shield: %v", err)
	}
	if player.BonusDefenseStats.BlockRate != 2 {
		t.Fatalf("equipped shield passive = %d, want 2", player.BonusDefenseStats.BlockRate)
	}
	if _, ok := player.CommitActiveBuff(fireShieldID, 900, time.Minute, model.BuffModifiers{MagicalDefense: 5}); !ok {
		t.Fatal("arrange active Fire Shield")
	}
	_, _, removed, err := handler.relocate(context.Background(), session, moveItemRequest{sourceSlot: model.SlotSecondaryWeapon, targetSlot: 15})
	if err != nil {
		t.Fatalf("unequip shield: %v", err)
	}
	if player.BonusDefenseStats.BlockRate != 0 {
		t.Fatalf("unequipped shield passive = %d, want 0", player.BonusDefenseStats.BlockRate)
	}
	if len(removed) != 1 || removed[0] != 900 || len(player.ActiveBuffSnapshot()) != 0 || player.BonusDefenseStats.MagDef != 0 {
		t.Fatalf("Fire Shield teardown = tokens:%v buffs:%v magical defense:%d", removed, player.ActiveBuffSnapshot(), player.BonusDefenseStats.MagDef)
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

	_, _, _, err := NewInventoryHandler(lookup, relocator).relocate(context.Background(), session, moveItemRequest{sourceSlot: 14, targetSlot: 15})
	if !errors.Is(err, ErrInventoryHandlerSessionPlayerMismatch) {
		t.Fatalf("relocate() error = %v, want session/player mismatch", err)
	}
	if relocator.calls != 0 {
		t.Fatalf("mismatched player reached relocation repository %d times", relocator.calls)
	}
}

func Test7034Operation22FailsClosedBecauseBuybackUsesDedicatedOpcode(t *testing.T) {
	handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{}, nil)
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		response = packet
		return nil
	}
	packet := network.EmptyClientPacket()
	packet.WriteByte(byte(BuyBackItemOperation))
	packet.WriteUInt32(0x11223344)
	packet.WriteByte(0)

	if err := handler.handlePacket(context.Background(), &server.Session{}, packet); err != nil {
		t.Fatalf("operation 0x22 error = %v", err)
	}
	if response.MessageID != opcode.ItemOperationResponse || !bytes.Equal(response.Payload(), []byte{2, 0, 0}) {
		t.Fatalf("operation 0x22 response = opcode 0x%04X payload %x, want failure envelope", response.MessageID, response.Payload())
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
