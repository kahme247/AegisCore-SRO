package inventory

import (
	"bytes"
	"context"
	"errors"
	"fmt"
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

type magicCubeRepositoryFake struct {
	pushes, pops int
	characterID  int
	userID       uint32
	cubeID       int
	source       byte
	destination  byte
	amount       uint16
	result       model.MagicCubeMoveResult
	err          error
}

func (f *magicCubeRepositoryFake) Push(_ context.Context, characterID int, userID uint32, cubeID int, source, destination byte, amount uint16) (model.MagicCubeMoveResult, error) {
	f.pushes++
	f.characterID, f.userID, f.cubeID, f.source, f.destination, f.amount = characterID, userID, cubeID, source, destination, amount
	return f.result, f.err
}

func (f *magicCubeRepositoryFake) Pop(_ context.Context, characterID int, userID uint32, cubeID int, source, destination byte, amount uint16) (model.MagicCubeMoveResult, error) {
	f.pops++
	f.characterID, f.userID, f.cubeID, f.source, f.destination, f.amount = characterID, userID, cubeID, source, destination, amount
	return f.result, f.err
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

type oldWomanGroundActionFake struct {
	characterID int
	calls       int
	result      model.OldWomanSearchStartResult
	advance     model.OldWomanSearchActionResult
	advances    int
	advanceRoll byte
	err         error
}

type nativeCatchActionFake struct {
	characterID int
	questID     int
	roll        byte
	starts      int
	advances    int
	start       model.NativeCatchStartResult
	advance     model.NativeCatchActionResult
	err         error
}

func (f *nativeCatchActionFake) StartNativeCatchAction(_ context.Context, characterID, questID int) (model.NativeCatchStartResult, error) {
	f.characterID, f.questID = characterID, questID
	f.starts++
	return f.start, f.err
}

func (f *nativeCatchActionFake) AdvanceNativeCatchAction(_ context.Context, characterID, questID int, roll byte) (model.NativeCatchActionResult, error) {
	f.characterID, f.questID, f.roll = characterID, questID, roll
	f.advances++
	return f.advance, f.err
}

func (f *oldWomanGroundActionFake) AdvanceOldWomanSearchAction(_ context.Context, characterID int, roll byte) (model.OldWomanSearchActionResult, error) {
	f.characterID = characterID
	f.advances++
	f.advanceRoll = roll
	return f.advance, f.err
}

func (f *oldWomanGroundActionFake) StartOldWomanSearchAction(_ context.Context, characterID int) (model.OldWomanSearchStartResult, error) {
	f.calls++
	f.characterID = characterID
	return f.result, f.err
}

type transientJobCOSPickupRepositoryFake struct {
	groundPickupRepositoryFake
	cosID    uint64
	item     *model.Item
	result   model.GroundItemCOSPickupResult
	err      error
	cosCalls int
}

func (f *transientJobCOSPickupRepositoryFake) PickupTransient(_ context.Context, characterID int64, item *model.Item, slot byte) (model.GroundItemPickupResult, error) {
	f.calls++
	f.characterID, f.item, f.slot = characterID, item, slot
	return f.groundPickupRepositoryFake.result, f.groundPickupRepositoryFake.err
}

func (f *transientJobCOSPickupRepositoryFake) PickupTransientToJobCOS(_ context.Context, characterID int64, cosID uint64, item *model.Item, slot, _ byte) (model.GroundItemCOSPickupResult, error) {
	f.cosCalls++
	f.characterID, f.cosID, f.item, f.slot = characterID, cosID, item, slot
	return f.result, f.err
}

type specialtyPickupCOSResolverFake struct{ transport *model.COS }

func (f specialtyPickupCOSResolverFake) ResolveActiveOwned(*model.Player, model.COSKind) (*model.COS, bool) {
	return f.transport, f.transport != nil
}

type specialtyPickupDistanceFake struct{ distance float32 }

func (f specialtyPickupDistanceFake) DistanceTo(navmeshv2.RtNavmeshPosition, navmeshv2.RtNavmeshPosition) float32 {
	return f.distance
}

type packageItemCatalogFake struct {
	npcRefID uint32
	tab      byte
	slot     byte
	result   model.PackageItemGood
	err      error
	calls    int
}

func (f *packageItemCatalogFake) ResolvePackageItem(_ context.Context, npcRefID uint32, tab, slot byte) (model.PackageItemGood, error) {
	f.calls++
	f.npcRefID, f.tab, f.slot = npcRefID, tab, slot
	return f.result, f.err
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
	reordered                          bool
	playerInventory, cosInventory      model.Inventory
}

func (f *petInventoryTransferFake) TransferItem(_ context.Context, owner int, cosID uint64, source, destination byte, toCOS bool) (model.Inventory, model.Inventory, error) {
	f.owner, f.cosID, f.itemSource, f.itemDestination, f.toCOS = owner, cosID, int(source), int(destination), toCOS
	return f.playerInventory, f.cosInventory, nil
}

func (f *petInventoryTransferFake) ReorderItem(_ context.Context, owner int, cosID uint64, source, destination byte) (model.Inventory, error) {
	f.owner, f.cosID, f.itemSource, f.itemDestination, f.reordered = owner, cosID, int(source), int(destination), true
	return f.cosInventory, nil
}

func (f *inventoryHandlerRelocatorFake) Relocate(_ context.Context, characterID int64, plan model.InventoryRelocationPlan) error {
	f.calls++
	f.characterID = characterID
	f.plan = plan
	return f.err
}

func TestJobSuitEquipWaitsForStockWearingPeriod(t *testing.T) {
	previous := model.RefItems
	t.Cleanup(func() { model.RefItems = previous })
	model.RefItems = map[uint32]model.RefItem{
		2001: {RefObject: model.RefObject{ID: 2001, CodeName: "ITEM_CH_M_TRADE_THIEF_02"}},
	}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}}}, ID: 44, Session: session,
		Inventory: model.NewInventory(map[byte]model.Item{40: {ID: 91, SRObject: model.SRObject{RefObjectID: 2001}}}),
	}
	player.SetJob(model.PlayerJob{Type: model.PlayerJobThief, Level: 1})
	relocator := &inventoryHandlerRelocatorFake{}
	handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{player: player}, relocator)
	handler.send = func(_ *server.Session, _ network.Packet) error { return nil }
	var delay time.Duration
	var complete func()
	handler.jobSuitSchedule = func(got time.Duration, callback func()) {
		delay, complete = got, callback
	}

	request := network.EmptyClientPacket()
	request.WriteByte(byte(MoveItemOperation))
	request.WriteByte(40)
	request.WriteByte(model.SlotExtra)
	request.WriteUInt16(0)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("begin job-suit equip: %v", err)
	}
	if delay != 30*time.Second || complete == nil {
		t.Fatalf("scheduled delay/callback = %v/%v, want 30s/non-nil", delay, complete != nil)
	}
	if relocator.calls != 0 {
		t.Fatalf("relocator called %d times before wearing period", relocator.calls)
	}
	if _, equipped := player.Inventory.Items[model.SlotExtra]; equipped {
		t.Fatal("job suit equipped before wearing period completed")
	}

	complete()
	if relocator.calls != 1 {
		t.Fatalf("relocator calls after wearing period = %d, want 1", relocator.calls)
	}
	if _, equipped := player.Inventory.Items[model.SlotExtra]; !equipped {
		t.Fatal("job suit was not equipped after wearing period completed")
	}
}

func TestUnconfiguredTokenAndInvalidOperationRemainFailClosed(t *testing.T) {
	for _, operation := range []ItemInventoryOperation{BuyTokenPackageItemOperation, RejectedItemOperation2C} {
		t.Run(fmt.Sprintf("0x%02X", operation), func(t *testing.T) {
			handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{}, nil)
			var packets []network.Packet
			handler.send = func(_ *server.Session, packet network.Packet) error {
				packets = append(packets, packet)
				return nil
			}
			request := network.EmptyClientPacket()
			request.WriteByte(byte(operation))
			request.WriteBytes(make([]byte, 16))

			if err := handler.handlePacket(context.Background(), &server.Session{}, request); err != nil {
				t.Fatalf("operation 0x%02X: %v", operation, err)
			}
			if len(packets) != 1 || packets[0].MessageID != opcode.ItemOperationResponse {
				t.Fatalf("operation 0x%02X packets = %#v", operation, packets)
			}
			if got, want := packets[0].Payload(), []byte{2, 0, 0}; !bytes.Equal(got, want) {
				t.Fatalf("operation 0x%02X failure payload = %X, want %X", operation, got, want)
			}
		})
	}
}

func TestPackageItemsResolveSelectedNPCAuthorityBeforeMutation(t *testing.T) {
	for _, operation := range []byte{vsro188.ItemOperationPackageItem, vsro188.ItemOperationPackageItemExtended} {
		catalog := &packageItemCatalogFake{result: model.PackageItemGood{ID: 74222, CodeName: "PACKAGE_ITEM_TEST", PaymentDevice: 1, Cost: 383, Members: []model.PackageItemMember{{Index: 1, RefItemID: 2147, Data: 20, MaxStack: 40}}}}
		handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{}, nil)
		handler.ConfigurePackageItems(catalog)
		var response network.Packet
		handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
		session := &server.Session{UserContext: server.UserContext{SelectedNPCRefObjID: 1234}}
		request := network.EmptyClientPacket()
		request.WriteByte(operation)
		request.WriteByte(2)
		request.WriteByte(7)
		request.WriteUInt16(3)
		if err := handler.handlePacket(context.Background(), session, request); err != nil {
			t.Fatalf("package 0x%02X: %v", operation, err)
		}
		if catalog.calls != 1 || catalog.npcRefID != 1234 || catalog.tab != 2 || catalog.slot != 7 {
			t.Fatalf("catalog call = %+v", catalog)
		}
		if response.MessageID != opcode.ItemOperationResponse || len(response.Payload()) == 0 || response.Payload()[0] != 2 {
			t.Fatalf("fail-closed response = %#v", response)
		}
	}
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

func TestParseMoveItemRequestReadsExactPayload(t *testing.T) {
	packet := network.EmptyClientPacket()
	if err := packet.WriteByte(14); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteByte(15); err != nil {
		t.Fatal(err)
	}
	if err := packet.WriteUInt16(49351); err != nil {
		t.Fatal(err)
	}
	request, err := parseMoveItemRequest(&packet)
	if err != nil {
		t.Fatalf("parseMoveItemRequest() error = %v", err)
	}
	if request.sourceSlot != 14 || request.targetSlot != 15 || request.amount != 49351 || packet.Remaining() != 0 {
		t.Fatalf("request = %+v, trailing=%d", request, packet.Remaining())
	}
}

func TestStockActionZeroGroundItemPickupPersistsBeforeRemovingWorldObject(t *testing.T) {
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
	handler.oldWomanSchedule = func(time.Duration, func()) {}
	handler.ConfigureGroundItemPickupRepository(repository)
	handler.ConfigureGroundItemLifecycle(lifecycle)
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		packets = append(packets, packet)
		return nil
	}
	request := network.EmptyClientPacket()
	request.WriteByte(byte(MoveItemOperation))
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
	if got := packets[0].Payload(); len(got) != 5 || got[0] != 77 || got[1] != 0 || got[2] != 0 || got[3] != 0 {
		t.Fatalf("pickup animation payload = % X, want collector UID 77 and one heading byte", got)
	}
	if got, want := packets[1].Payload(), []byte{1, 0x06, 0x0D, 0, 0, 0, 0, 0xBC, 0x02, 0, 0, 1, 0}; !bytes.Equal(got, want) {
		t.Fatalf("pickup inventory payload = % X, want hydrated action-6 body % X", got, want)
	}
}

func TestOldWomanSearchGroundTriggerStartsActionWithoutNormalPickup(t *testing.T) {
	stage := model.OldWomanSearchStages[0]
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(stage.Region), Offset: math32.NewVector3(stage.X, stage.Y, stage.Z)}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: position}, ID: 44, Session: session}
	item := &model.Item{SRObject: model.SRObject{UniqueID: 900, RefObjectID: model.OldWomanSearchTriggerItemRefID, RtNavmeshPosition: position}, ID: 91}
	world := &groundPickupWorldFake{player: player, item: item}
	pickup := &groundPickupRepositoryFake{}
	quest := model.CharacterQuest{QuestID: model.OldWomanSearchQuestID, Status: model.QuestProgressed, Data1: 1 | uint64(stage.Countdown)<<32}
	action := &oldWomanGroundActionFake{result: model.OldWomanSearchStartResult{Quest: quest, Countdown: stage.Countdown}}
	handler := NewInventoryHandler(world, nil)
	handler.ConfigureGroundItemPickupRepository(pickup)
	handler.ConfigureOldWomanSearch(action)
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { packets = append(packets, packet); return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(PickupItemOperation))
	request.WriteUInt32(item.GetUniqueID())
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if action.calls != 1 || action.characterID != 44 || pickup.calls != 0 || world.removed != nil {
		t.Fatalf("action=%+v pickupCalls=%d removed=%v", action, pickup.calls, world.removed)
	}
	if len(packets) != 1 || packets[0].MessageID != opcode.QuestCatchResult {
		t.Fatalf("packets = %+v", packets)
	}
	questID, _ := packets[0].ReadUInt32()
	countdown, _ := packets[0].ReadByte()
	if questID != model.OldWomanSearchQuestID || countdown != stage.Countdown {
		t.Fatalf("capture = quest %d countdown %d", questID, countdown)
	}
	if got := player.QuestSnapshot(); len(got) != 1 || got[0].Data1 != quest.Data1 {
		t.Fatalf("live quests = %+v", got)
	}
}

func TestNativeCatchGroundTriggerStartsPeriodicActionWithoutNormalPickup(t *testing.T) {
	definition, _ := model.NativeCatchDefinitionForQuest(163)
	sphere := definition.Spheres[0]
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(sphere.Region), Offset: math32.NewVector3(sphere.X, sphere.Y, sphere.Z)}
	quest := model.CharacterQuest{QuestID: definition.QuestID, QuestCodeName: definition.CodeName, Status: model.QuestProgressed, Data1: uint64(definition.Countdown) << 32}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: position}, ID: 44, Session: session, Quests: []model.CharacterQuest{quest}}
	item := &model.Item{SRObject: model.SRObject{UniqueID: 901, RefObjectID: definition.TriggerRefID, RtNavmeshPosition: position}, ID: 92}
	world := &groundPickupWorldFake{player: player, item: item}
	pickup := &groundPickupRepositoryFake{}
	action := &nativeCatchActionFake{start: model.NativeCatchStartResult{Quest: quest, Definition: definition, Countdown: definition.Countdown}}
	handler := NewInventoryHandler(world, nil)
	handler.ConfigureGroundItemPickupRepository(pickup)
	handler.ConfigureNativeCatchActions(action)
	var scheduled func()
	handler.nativeCatchSchedule = func(delay time.Duration, callback func()) {
		if delay != time.Second {
			t.Fatalf("schedule delay = %s", delay)
		}
		scheduled = callback
	}
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { packets = append(packets, packet); return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(byte(PickupItemOperation))
	request.WriteUInt32(item.GetUniqueID())
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if action.starts != 1 || action.characterID != 44 || action.questID != definition.QuestID || pickup.calls != 0 || world.removed != nil {
		t.Fatalf("action=%+v pickupCalls=%d removed=%v", action, pickup.calls, world.removed)
	}
	if scheduled == nil || len(packets) != 1 || packets[0].MessageID != opcode.QuestCatchResult {
		t.Fatalf("scheduled=%t packets=%+v", scheduled != nil, packets)
	}
}

func TestNativeCatchPeriodicEventPublishesRecoveredFailureTalk(t *testing.T) {
	definition, _ := model.NativeCatchDefinitionForQuest(163)
	quest := model.CharacterQuest{QuestID: definition.QuestID, Status: model.QuestProgressed}
	action := &nativeCatchActionFake{advance: model.NativeCatchActionResult{
		Quest: quest, Definition: definition, Resolved: true,
		FailureTalkKey: definition.FailureTalkKey,
	}}
	player := &model.Player{ID: 44, Quests: []model.CharacterQuest{quest}}
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44}}
	handler := NewInventoryHandler(nil, nil)
	handler.ConfigureNativeCatchActions(action)
	handler.nativeCatchRoll = func() byte { return 51 }
	var callback func()
	handler.nativeCatchSchedule = func(delay time.Duration, next func()) {
		if delay != time.Second {
			t.Fatalf("delay = %s", delay)
		}
		callback = next
	}
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { packets = append(packets, packet); return nil }
	handler.beginNativeCatchEvents(session, player, definition.QuestID)
	callback()
	if action.advances != 1 || action.roll != 51 || len(packets) != 1 || packets[0].MessageID != opcode.QuestNotification {
		t.Fatalf("action=%+v packets=%+v", action, packets)
	}
}

func TestNativeCatchPeriodicReplacementRejectsStaleGeneration(t *testing.T) {
	definition, _ := model.NativeCatchDefinitionForQuest(163)
	quest := model.CharacterQuest{QuestID: definition.QuestID, Status: model.QuestProgressed}
	action := &nativeCatchActionFake{advance: model.NativeCatchActionResult{Quest: quest, Definition: definition, Resolved: true}}
	player := &model.Player{ID: 44, Quests: []model.CharacterQuest{quest}}
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44}}
	handler := NewInventoryHandler(nil, nil)
	handler.ConfigureNativeCatchActions(action)
	var callbacks []func()
	handler.nativeCatchSchedule = func(_ time.Duration, callback func()) { callbacks = append(callbacks, callback) }
	handler.beginNativeCatchEvents(session, player, definition.QuestID)
	handler.beginNativeCatchEvents(session, player, definition.QuestID)
	if len(callbacks) != 2 {
		t.Fatalf("callbacks = %d", len(callbacks))
	}
	callbacks[0]()
	if action.advances != 0 {
		t.Fatalf("stale callback advanced %d times", action.advances)
	}
	callbacks[1]()
	if action.advances != 1 {
		t.Fatalf("current callback advanced %d times", action.advances)
	}
}

func TestOldWomanSearchPeriodicEventConsumesNativeEvent18(t *testing.T) {
	quest := model.CharacterQuest{QuestID: model.OldWomanSearchQuestID, Status: model.QuestProgressed, Data1: 1}
	action := &oldWomanGroundActionFake{advance: model.OldWomanSearchActionResult{
		Quest: quest, Resolved: true, FailureTalkKey: model.OldWomanSearchFailureTalkKey,
	}}
	player := &model.Player{ID: 44, Quests: []model.CharacterQuest{quest}}
	session := &server.Session{UserContext: server.UserContext{CharacterID: 44}}
	handler := NewInventoryHandler(nil, nil)
	handler.ConfigureOldWomanSearch(action)
	handler.oldWomanRoll = func() byte { return 26 }
	var callback func()
	handler.oldWomanSchedule = func(delay time.Duration, next func()) {
		if delay != time.Second {
			t.Fatalf("delay = %s", delay)
		}
		callback = next
	}
	var packets []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { packets = append(packets, packet); return nil }
	handler.beginOldWomanSearchEvents(session, player)
	if callback == nil {
		t.Fatal("periodic event was not scheduled")
	}
	callback()
	if action.advances != 1 || action.advanceRoll != 26 || len(packets) != 1 || packets[0].MessageID != opcode.QuestNotification {
		t.Fatalf("action=%+v packets=%+v", action, packets)
	}
}

func TestSpecialtyGroundPickupJobAdmissionMatrix(t *testing.T) {
	tests := []struct {
		name    string
		jobType byte
		alias   string
		creator string
		allowed bool
	}{
		{name: "trader recovers own cargo", jobType: model.PlayerJobTrader, alias: "TraderAlias", creator: "traderalias", allowed: true},
		{name: "trader cannot take foreign cargo", jobType: model.PlayerJobTrader, alias: "TraderAlias", creator: "OtherTrader"},
		{name: "thief takes foreign cargo", jobType: model.PlayerJobThief, alias: "ThiefAlias", creator: "TraderAlias", allowed: true},
		{name: "thief cannot take own cargo", jobType: model.PlayerJobThief, alias: "ThiefAlias", creator: "thiefalias"},
		{name: "hunter rejected", jobType: model.PlayerJobHunter, alias: "HunterAlias", creator: "TraderAlias"},
		{name: "no job rejected", jobType: model.PlayerJobNone, creator: "TraderAlias"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
			position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.Region{ID: 25000}, Offset: math32.NewVector3(0, 0, 0)}
			player := &model.Player{SRObject: model.SRObject{UniqueID: 77, RtNavmeshPosition: position}, ID: 44, Session: session}
			player.SetJob(model.PlayerJob{Type: test.jobType, Alias: test.alias, Level: 1})
			item := &model.Item{SRObject: model.SRObject{UniqueID: 900, RefObjectID: 700, RtNavmeshPosition: position}, StackSize: 2, CreatorName: test.creator, TransientGroundItem: true}
			transport := &model.COS{NPC: model.NPC{SRObject: model.SRObject{UniqueID: 88, RtNavmeshPosition: position}}, PersistentID: 99, OwnerCharID: 44, OwnerUniqueID: 77, Kind: model.COSJobTransport, InventorySize: 40, Inventory: model.NewInventory(nil)}
			world := &groundPickupWorldFake{player: player, item: item}
			repository := &transientJobCOSPickupRepositoryFake{result: model.GroundItemCOSPickupResult{Item: model.Item{ID: 123, SRObject: model.SRObject{RefObjectID: 700}, StackSize: 2, CreatorName: test.creator}, Slot: 0, Scale: 2, SafeTradeReset: true}}
			handler := NewInventoryHandler(world, nil)
			handler.ConfigureGroundItemPickupRepository(repository)
			handler.ConfigureSafeTrade(nil, specialtyPickupCOSResolverFake{transport: transport}, specialtyPickupDistanceFake{distance: 10})
			var packets []network.Packet
			handler.send = func(_ *server.Session, packet network.Packet) error { packets = append(packets, packet); return nil }
			request := network.EmptyClientPacket()
			request.WriteByte(byte(PickupItemOperation))
			request.WriteUInt32(item.GetUniqueID())
			if err := handler.handlePacket(context.Background(), session, request); err != nil {
				t.Fatalf("pickup: %v", err)
			}
			if test.allowed {
				if repository.cosCalls != 1 || repository.characterID != 44 || repository.cosID != 99 || repository.item != item || repository.slot != 0 {
					t.Fatalf("COS repository call = %+v", repository)
				}
				if world.removed != item || transport.Inventory.Items[0].ID != 123 {
					t.Fatalf("committed world/runtime state removed=%v inventory=%+v", world.removed, transport.Inventory.Items)
				}
				if len(packets) != 4 {
					t.Fatalf("success packets = %#v", packets)
				}
				pickupPayload := packets[1].Payload()
				if packets[1].MessageID != opcode.ItemOperationResponse || len(pickupPayload) <= 7 || !bytes.Equal(pickupPayload[:7], []byte{1, 0x11, 88, 0, 0, 0, 0}) || packets[2].MessageID != opcode.JobUpdateTradeScale || packets[3].MessageID != opcode.JobUpdateSafeTrade || !bytes.Equal(packets[3].Payload(), []byte{0, 0x40}) {
					t.Fatalf("success packets = %#v", packets)
				}
			} else if repository.cosCalls != 0 || world.removed != nil {
				t.Fatalf("rejected pickup mutated state: calls=%d removed=%v", repository.cosCalls, world.removed)
			}
		})
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

func TestBuildGoldPickupUpdateMarksPickupVariant(t *testing.T) {
	packet := buildGoldPickupUpdate(350)
	update, err := vsro188.DecodeGoldPointUpdate(&packet)
	if err != nil {
		t.Fatalf("DecodeGoldPointUpdate() error = %v", err)
	}
	if packet.MessageID != opcode.EntityUpdatePoints || update.Value != 350 || update.TrailingFlag != 1 || packet.Remaining() != 0 {
		t.Fatalf("gold pickup update = opcode 0x%04X value=%d flag=%d trailing=%d", packet.MessageID, update.Value, update.TrailingFlag, packet.Remaining())
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

func TestActiveCOSItemsCannotEnterStorage(t *testing.T) {
	for _, state := range []model.COSItemState{model.COSItemSummoned, model.COSItemActive} {
		item := model.Item{COS: model.COSItemInfo{Present: true, State: state}}
		if !isActiveCOSItem(item) {
			t.Fatalf("COS state %d was not protected", state)
		}
	}
	for _, state := range []model.COSItemState{model.COSItemInactive, model.COSItemDead} {
		item := model.Item{COS: model.COSItemInfo{Present: true, State: state}}
		if isActiveCOSItem(item) {
			t.Fatalf("COS state %d was incorrectly protected", state)
		}
	}
}

func TestNPCBuyRejectsZeroQuantityWithStockFailureCode(t *testing.T) {
	handler := &InventoryHandler{economy: &storageResyncEconomyFake{}}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}
	request := network.EmptyClientPacket()
	request.WriteByte(2)
	request.WriteByte(3)
	request.WriteUInt16(0)
	request.WriteUInt32(0x11223344)

	if err := handler.handleBuy(context.Background(), &server.Session{}, &request); err != nil {
		t.Fatalf("zero-quantity NPC buy returned error: %v", err)
	}
	if len(sent) != 1 || sent[0].MessageID != opcode.ItemOperationResponse || !bytes.Equal(sent[0].Payload(), []byte{2, 0x29, 0x18}) {
		t.Fatalf("zero-quantity NPC buy response = %#v", sent)
	}
}

type storageResyncEconomyFake struct {
	model.EconomyRepository
	snapshot model.StorageSnapshot
	userID   uint32
}

func (f *storageResyncEconomyFake) LoadStorage(_ context.Context, userID uint32) (model.StorageSnapshot, error) {
	f.userID = userID
	return f.snapshot, nil
}

func TestRejectedStorageMutationResendsAuthoritativeSnapshot(t *testing.T) {
	repository := &storageResyncEconomyFake{snapshot: model.StorageSnapshot{Capacity: 30, Items: map[byte]model.Item{}}}
	handler := &InventoryHandler{economy: repository}
	var sent []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		sent = append(sent, packet)
		return nil
	}
	session := &server.Session{UserContext: server.UserContext{UserID: 99}}
	if err := handler.rejectStorageMutation(context.Background(), session, model.ErrEconomyConflict); err != nil {
		t.Fatal(err)
	}
	if repository.userID != 99 {
		t.Fatalf("storage reload user = %d, want 99", repository.userID)
	}
	wantOpcodes := []uint16{opcode.ItemOperationResponse, opcode.StorageDataBegin, opcode.StorageData, opcode.StorageDataEnd}
	if len(sent) != len(wantOpcodes) {
		t.Fatalf("sent %d packets, want %d", len(sent), len(wantOpcodes))
	}
	for index, want := range wantOpcodes {
		if sent[index].MessageID != want {
			t.Fatalf("packet %d opcode = 0x%04X, want 0x%04X", index, sent[index].MessageID, want)
		}
	}
	if got := sent[2].Payload(); !bytes.Equal(got, []byte{30, 0}) {
		t.Fatalf("storage snapshot payload = % X, want 1E 00", got)
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
	want := []byte{1, byte(PutItemIntoPetInventoryOperation), 88, 0, 0, 0, 14, 3}
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
	want := []byte{1, byte(TakeItemFromPetInventoryOperation), 88, 0, 0, 0, 3, 14}
	if response.MessageID != 0xB034 || !bytes.Equal(response.Payload(), want) {
		t.Fatalf("response = 0x%04X/% X, want 0xB034/% X", response.MessageID, response.Payload(), want)
	}
}

func TestAbilityPetInventoryReorderUsesStockOperation10Layout(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session}
	pet := &model.COS{NPC: model.NPC{SRObject: model.SRObject{UniqueID: 88}}, PersistentID: 9, OwnerCharID: 44, OwnerUniqueID: 77, Kind: model.COSAbilityPet, InventorySize: 28, Inventory: model.NewInventory(map[byte]model.Item{3: {ID: 100, StackSize: 1}})}
	nextCOS := model.NewInventory(map[byte]model.Item{4: {ID: 100, StackSize: 1}})
	repository := &petInventoryTransferFake{cosInventory: nextCOS}
	handler := NewInventoryHandler(&petInventoryWorldFake{player: player, pet: pet}, nil)
	handler.ConfigureCOSRepository(repository)
	var responses []network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error {
		responses = append(responses, packet)
		return nil
	}
	request := network.EmptyClientPacket()
	request.WriteByte(byte(MoveItemInPetInventoryOperation))
	request.WriteUInt32(88)
	request.WriteByte(3)
	request.WriteByte(4)
	request.WriteUInt16(1)
	if err := handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if !repository.reordered || repository.owner != 44 || repository.cosID != 9 || repository.itemSource != 3 || repository.itemDestination != 4 {
		t.Fatalf("reorder = %+v", repository)
	}
	if _, sourceHas := pet.Snapshot().Inventory.Items[3]; sourceHas || pet.Snapshot().Inventory.Items[4].ID != 100 {
		t.Fatalf("runtime COS inventory = %+v", pet.Snapshot().Inventory.Items)
	}
	if len(responses) != 2 {
		t.Fatalf("reorder responses = %#v, want B034 then 30C9", responses)
	}
	want := []byte{1, 0x10, 88, 0, 0, 0, 3, 4, 1, 0}
	if responses[0].MessageID != opcode.ItemOperationResponse || !bytes.Equal(responses[0].Payload(), want) || responses[1].MessageID != opcode.COSUpdateResponse {
		t.Fatalf("reorder responses = %#v, want B034/% X then 30C9", responses, want)
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

func TestMagicCubePushCommitsRepositorySnapshotAndStockSuccess(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UserID: 9, UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	if !player.ActivateMagicCube(555) {
		t.Fatal("activate cube")
	}
	repository := &magicCubeRepositoryFake{result: model.MagicCubeMoveResult{Inventory: model.NewInventory(map[byte]model.Item{14: {ID: 700}}), ItemID: 700}}
	handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{player: player}, nil)
	handler.ConfigureMagicCube(repository)
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
	request, err := vsro188.BuildMagicCubeMoveRequest(vsro188.ItemOperationMagicCubePush, vsro188.MagicCubeMoveRequest{Source: 14, Destination: 2, Amount: 3})
	if err != nil {
		t.Fatal(err)
	}
	if err = handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if repository.pushes != 1 || repository.pops != 0 || repository.characterID != 44 || repository.userID != 9 || repository.cubeID != 555 || repository.source != 14 || repository.destination != 2 || repository.amount != 3 {
		t.Fatalf("repository call = %+v", repository)
	}
	if player.Inventory.Items[14].ID != 700 {
		t.Fatalf("runtime inventory = %+v", player.Inventory.Items)
	}
	if got, want := response.Payload(), []byte{1, 0x27, 14, 2, 3, 0}; !bytes.Equal(got, want) {
		t.Fatalf("response = % X, want % X", got, want)
	}
}

func TestMagicCubePopRejectsMismatchedLinkedContext(t *testing.T) {
	session := &server.Session{UserContext: server.UserContext{UserID: 9, UniqueID: 77, CharacterID: 44}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 77}, ID: 44, Session: session, Inventory: model.NewInventory(nil)}
	player.ActivateMagicCube(555)
	repository := &magicCubeRepositoryFake{}
	handler := NewInventoryHandler(&inventoryHandlerPlayerLookupFake{player: player}, nil)
	handler.ConfigureMagicCube(repository)
	var response network.Packet
	handler.send = func(_ *server.Session, packet network.Packet) error { response = packet; return nil }
	request, err := vsro188.BuildMagicCubeMoveRequest(vsro188.ItemOperationMagicCubePop, vsro188.MagicCubeMoveRequest{Context: 556, Source: 2, Destination: 14, Amount: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err = handler.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if repository.pops != 0 || !bytes.Equal(response.Payload(), []byte{2, 0, 0}) {
		t.Fatalf("mismatched context reached repository or wrong response: calls=%d payload=% X", repository.pops, response.Payload())
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

func TestWholeStackMoveSuccessPreservesQuantity(t *testing.T) {
	request := moveItemRequest{sourceSlot: 64, targetSlot: 63, amount: 49351}
	packet := vsro188.BuildItemOperationRelocationSuccess(request.sourceSlot, request.targetSlot, request.amount)
	if got, want := packet.Payload(), []byte{1, 0, 64, 63, 0xC7, 0xC0, 0}; !bytes.Equal(got, want) {
		t.Fatalf("whole-stack response = % X, want % X", got, want)
	}
}

func TestInventoryUnknownRequestFailsWithoutMutation(t *testing.T) {
	var response network.Packet
	handler := &InventoryHandler{send: func(_ *server.Session, packet network.Packet) error {
		response = packet
		return nil
	}}
	request := network.EmptyClientPacket()
	request.WriteUInt32(0x11223344)
	request.WriteByte(7)

	if err := handler.handleUnknownPacket(context.Background(), &server.Session{}, request); err != nil {
		t.Fatalf("handleUnknownPacket() error = %v", err)
	}
	if response.MessageID != opcode.InventoryUnknownResponse || !bytes.Equal(response.Payload(), []byte{2, 5, 0}) {
		t.Fatalf("response = %04x/%x, want B03F native failure", response.MessageID, response.Payload())
	}
}
