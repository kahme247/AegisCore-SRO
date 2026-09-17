package inventory

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

type itemUseRepositoryFake struct {
	calls  int
	err    error
	result model.ItemUseResult
	vitals model.ItemUseVitals
	target model.COSItemTarget
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

func (f *itemUseRepositoryFake) UseItem(_ context.Context, _ int64, _ byte, _ uint16, vitals model.ItemUseVitals) (model.ItemUseResult, error) {
	f.calls++
	f.vitals = vitals
	return f.result, f.err
}

func (f *itemUseRepositoryFake) UseTargetedItem(_ context.Context, _ int64, _ byte, _ uint16, vitals model.ItemUseVitals, target model.COSItemTarget) (model.ItemUseResult, error) {
	f.calls++
	f.vitals = vitals
	f.target = target
	return f.result, nil
}

type dimensionHoleItemUseWorldFake struct {
	nextUID uint32
	omitUID bool
	added   []model.ISRObject
	removed []model.ISRObject
}

func (w *dimensionHoleItemUseWorldFake) AddVisibleObject(object model.ISRObject) {
	if w.omitUID {
		w.added = append(w.added, object)
		return
	}
	w.nextUID++
	object.SetUniqueID(w.nextUID)
	w.added = append(w.added, object)
}

func TestDimensionHoleItemUseDoesNotCommitWhenPortalPublicationFails(t *testing.T) {
	definition, ref, session, player := dimensionHoleItemUseFixture(t)
	repository := &itemUseRepositoryFake{}
	world := &dimensionHoleItemUseWorldFake{omitUID: true}
	scopes := &dimensionHoleItemUseScopeFake{}
	h := dimensionHoleItemUseHandler(t, player, repository, world, scopes)
	if err := h.handlePacket(context.Background(), session, dimensionHoleItemUseRequest(ref)); err != nil {
		t.Fatalf("Dimension Hole publication failure error = %v", err)
	}
	if repository.calls != 0 || scopes.reserves != 1 || scopes.releases != 1 || len(world.removed) != 1 {
		t.Fatalf("publication failure = calls:%d reserves:%d releases:%d removed:%d definition:%d", repository.calls, scopes.reserves, scopes.releases, len(world.removed), definition.ItemRefID)
	}
}

func TestDimensionHoleItemUseRollsBackPublishedPortalOnKnownCommitFailure(t *testing.T) {
	_, ref, session, player := dimensionHoleItemUseFixture(t)
	repository := &itemUseRepositoryFake{err: errors.New("item use rejected")}
	world := &dimensionHoleItemUseWorldFake{}
	scopes := &dimensionHoleItemUseScopeFake{}
	h := dimensionHoleItemUseHandler(t, player, repository, world, scopes)
	if err := h.handlePacket(context.Background(), session, dimensionHoleItemUseRequest(ref)); err != nil {
		t.Fatalf("Dimension Hole commit rejection error = %v", err)
	}
	if repository.calls != 1 || len(world.added) != 1 || len(world.removed) != 1 || scopes.releases != 1 {
		t.Fatalf("commit rollback = calls:%d added:%d removed:%d releases:%d", repository.calls, len(world.added), len(world.removed), scopes.releases)
	}
}

func TestDimensionHoleItemUseKeepsPublishedPortalOnUnknownCommitOutcome(t *testing.T) {
	_, ref, session, player := dimensionHoleItemUseFixture(t)
	repository := &itemUseRepositoryFake{err: model.ErrItemUseOutcomeUnknown}
	world := &dimensionHoleItemUseWorldFake{}
	scopes := &dimensionHoleItemUseScopeFake{}
	h := dimensionHoleItemUseHandler(t, player, repository, world, scopes)
	err := h.handlePacket(context.Background(), session, dimensionHoleItemUseRequest(ref))
	if !errors.Is(err, model.ErrItemUseOutcomeUnknown) {
		t.Fatalf("unknown commit error = %v", err)
	}
	if repository.calls != 1 || len(world.added) != 1 || len(world.removed) != 0 || scopes.releases != 0 {
		t.Fatalf("unknown outcome = calls:%d added:%d removed:%d releases:%d", repository.calls, len(world.added), len(world.removed), scopes.releases)
	}
}

func dimensionHoleItemUseFixture(t *testing.T) (model.DimensionHoleDefinition, model.RefItem, *server.Session, *model.Player) {
	t.Helper()
	previousRefs := model.RefItems
	t.Cleanup(func() { model.RefItems = previousRefs })
	definition := model.DimensionHoleCatalog()[0]
	ref := dimensionHoleItemUseReference(definition)
	model.RefItems = map[uint32]model.RefItem{ref.ID: ref}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(10, 0, 20),
		}},
		ID: 7, CharName: "Owner", Level: int(definition.MinLevel), Session: session,
		BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
		Inventory: model.NewInventory(map[byte]model.Item{13: {SRObject: model.SRObject{RefObjectID: ref.ID}, StackSize: 1}}),
		Party:     &model.Party{Number: 1, MasterUniqueID: 100, Members: []uint32{100}},
	}
	return definition, ref, session, player
}

func dimensionHoleItemUseHandler(t *testing.T, player *model.Player, repository *itemUseRepositoryFake, world *dimensionHoleItemUseWorldFake, scopes *dimensionHoleItemUseScopeFake) *ItemUseHandler {
	t.Helper()
	portals, err := service.NewDimensionHolePortalService(world)
	if err != nil {
		t.Fatal(err)
	}
	lifecycle, err := service.NewDimensionHolePortalLifecycle(portals, scopes, func() uint32 { return 700 })
	if err != nil {
		t.Fatal(err)
	}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.ConfigureDimensionHolePortals(lifecycle)
	h.send = func(*server.Session, network.Packet) error { return nil }
	return h
}

func dimensionHoleItemUseRequest(ref model.RefItem) network.Packet {
	request := network.EmptyClientPacket()
	request.WriteByte(13)
	request.WriteUInt16(model.InventoryItemType(ref))
	return request
}

func (w *dimensionHoleItemUseWorldFake) RemoveVisibleObject(object model.ISRObject) {
	w.removed = append(w.removed, object)
}

type dimensionHoleItemUseScopeFake struct {
	reserves int
	releases int
}

func (f *dimensionHoleItemUseScopeFake) ReserveDimensionHoleScope(_ context.Context, definition model.DimensionHoleDefinition, key uint32) (model.DimensionHoleScopeLease, error) {
	f.reserves++
	scope, _ := model.NewGameWorldScope(definition.WorldID, 1)
	return model.DimensionHoleScopeLease{Scope: scope, ReservationKey: key, ItemRefID: definition.ItemRefID}, nil
}

func (f *dimensionHoleItemUseScopeFake) ReleaseDimensionHoleScope(context.Context, model.DimensionHoleScopeLease) error {
	f.releases++
	return nil
}

func (*dimensionHoleItemUseScopeFake) DimensionHoleLeaseForScope(model.GameWorldScope) (model.DimensionHoleScopeLease, bool) {
	return model.DimensionHoleScopeLease{}, false
}

func TestDimensionHoleItemUsePublishesPreparedPortalBeforeSuccess(t *testing.T) {
	previousRefs := model.RefItems
	t.Cleanup(func() { model.RefItems = previousRefs })
	definition := model.DimensionHoleCatalog()[0]
	ref := dimensionHoleItemUseReference(definition)
	model.RefItems = map[uint32]model.RefItem{ref.ID: ref}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100, RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(10, 0, 20),
		}},
		ID: 7, CharName: "Owner", Level: int(definition.MinLevel), Session: session,
		BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
		Inventory: model.NewInventory(map[byte]model.Item{13: {SRObject: model.SRObject{RefObjectID: ref.ID}, StackSize: 1}}),
		Party:     &model.Party{Number: 1, MasterUniqueID: 100, Members: []uint32{100}},
	}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{
		Inventory: model.NewInventory(nil), HP: 600, MP: 250, SourceSlot: 13,
		ItemType: model.InventoryItemType(ref), DimensionHolePortal: &definition,
	}}
	world := &dimensionHoleItemUseWorldFake{}
	portals, _ := service.NewDimensionHolePortalService(world)
	scopes := &dimensionHoleItemUseScopeFake{}
	lifecycle, _ := service.NewDimensionHolePortalLifecycle(portals, scopes, func() uint32 { return 700 })
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.ConfigureDimensionHolePortals(lifecycle)
	var sent network.Packet
	h.send = func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(13)
	request.WriteUInt16(model.InventoryItemType(ref))
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("Dimension Hole use error = %v", err)
	}
	if repository.calls != 1 || repository.vitals.DimensionHolePreparedItemRefID != definition.ItemRefID {
		t.Fatalf("repository calls=%d prepared ref=%d", repository.calls, repository.vitals.DimensionHolePreparedItemRefID)
	}
	if scopes.reserves != 1 || scopes.releases != 0 || len(world.added) != 1 {
		t.Fatalf("portal runtime = reserves:%d releases:%d added:%d", scopes.reserves, scopes.releases, len(world.added))
	}
	portal, ok := world.added[0].(*model.DimensionHolePortal)
	if !ok || portal.Definition != definition || portal.OwnerUniqueID != player.GetUniqueID() {
		t.Fatalf("published portal = %#v", world.added[0])
	}
	if sent.MessageID != opcode.InventoryItemUseResponse || !bytes.Equal(sent.Payload(), []byte{1, 13, 0, 0, 0x6C, 0x3E}) {
		t.Fatalf("Dimension Hole response = 0x%04X % X", sent.MessageID, sent.Payload())
	}
}

func TestDimensionHoleOpenerRequiresAuthoredLevelTownAndPartyMaster(t *testing.T) {
	previousRefs := model.RefItems
	t.Cleanup(func() { model.RefItems = previousRefs })
	definition := model.DimensionHoleCatalog()[0]
	player := &model.Player{SRObject: model.SRObject{
		UniqueID: 100,
		RtNavmeshPosition: navmeshv2.RtNavmeshPosition{
			Region: navmeshv2.NewRegionFromInt16(25000), Offset: math32.NewVector3(10, 0, 20),
		},
	}, Level: int(definition.MinLevel)}
	if dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("party-less player was eligible")
	}
	player.Party = &model.Party{MasterUniqueID: 200}
	if dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("non-master party member was eligible")
	}
	player.Party.MasterUniqueID = player.GetUniqueID()
	if !dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("in-range party master was rejected")
	}
	player.SetPVPCape(model.PvpCapeBlue)
	if dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("free-PVP player was eligible")
	}
	player.SetPVPCape(model.PvpCapeNone)
	if err := player.COSState.ActivateTransport(900, 901, true); err != nil {
		t.Fatal(err)
	}
	if dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("mounted player was eligible")
	}
	if _, err := player.COSState.SetMounted(900, false); err != nil {
		t.Fatal(err)
	}
	model.RefItems = map[uint32]model.RefItem{902: {RefObject: model.RefObject{ID: 902, CodeName: "ITEM_ETC_TRADE_THIEF_SUIT"}}}
	player.SetJob(model.PlayerJob{Type: model.PlayerJobThief, Level: 1})
	player.Inventory.Items = map[byte]model.Item{model.SlotExtra: {SRObject: model.SRObject{RefObjectID: 902}}}
	if dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("job-suited player was eligible")
	}
	player.SetJob(model.PlayerJob{})
	delete(player.Inventory.Items, model.SlotExtra)
	player.Level = int(definition.MinLevel) - 1
	if dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("under-level party master was eligible")
	}
	player.Level = int(definition.MaxLevel) + 1
	if dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("over-level party master was eligible")
	}
	player.Level = int(definition.MinLevel)
	player.RtNavmeshPosition.Region = navmeshv2.NewRegionFromInt16(25960)
	if dimensionHoleOpenerEligible(player, definition) {
		t.Fatal("party master outside a town was eligible")
	}
}

func dimensionHoleItemUseReference(definition model.DimensionHoleDefinition) model.RefItem {
	return model.RefItem{
		RefObject: model.RefObject{
			ID: definition.ItemRefID, CodeName: definition.ItemCodeName, IsUseable: true,
			TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 12, TypeID4: 7},
		},
		Param1: int(definition.Difficulty), Param2: 0,
		Param3: int(definition.MinLevel), Param4: int(definition.MaxLevel), Param5: int(definition.Capacity),
		Param6: 6, Param7: 1, Param8: -1, Param9: -1, Param10: int(definition.Cooldown.Milliseconds()),
	}
}

func TestReverseReturnSelectionPayloadReachesRepository(t *testing.T) {
	previousRefs := model.RefItems
	t.Cleanup(func() { model.RefItems = previousRefs })
	ref := model.RefItem{RefObject: model.RefObject{ID: 61, IsUseable: true, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 3, TypeID4: 3}}}
	ref.Param1 = 1_000
	model.RefItems = map[uint32]model.RefItem{61: ref}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}, ID: 7, Level: 42, Session: session, BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29}, Inventory: model.Inventory{Items: map[byte]model.Item{14: {SRObject: model.SRObject{RefObjectID: 61}, StackSize: 1}}}}
	destination := model.TeleportDestination{ID: 19, Region: 25000, WorldID: 1}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{Inventory: model.Inventory{Items: map[byte]model.Item{}}, HP: 600, MP: 250, SourceSlot: 14, ItemType: model.InventoryItemType(ref), ReturnDestination: &destination}}
	teleport := &returnTeleportFake{}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.returnTeleport = teleport
	h.send = func(*server.Session, network.Packet) error { return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(14)
	request.WriteUInt16(model.InventoryItemType(ref))
	request.WriteByte(7)
	request.WriteUInt32(19)
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("reverse-return map selection failed: %v", err)
	}
	if repository.vitals.ReverseReturnOption != 7 || repository.vitals.ReverseReturnMapID != 19 || repository.vitals.Level != 42 {
		t.Fatalf("reverse selection = option %d map %d level %d calls %d prepared %d", repository.vitals.ReverseReturnOption, repository.vitals.ReverseReturnMapID, repository.vitals.Level, repository.calls, teleport.prepared)
	}
}

func TestMagicCubeUseSelectsPersistentCubeWithoutConsumption(t *testing.T) {
	previousRefs := model.RefItems
	t.Cleanup(func() { model.RefItems = previousRefs })
	ref := model.RefItem{RefObject: model.RefObject{ID: 20200, IsUseable: true, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 3}}}
	model.RefItems = map[uint32]model.RefItem{20200: ref}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}, ID: 7, Session: session, Inventory: model.Inventory{Items: map[byte]model.Item{
		14: {ID: 444, SRObject: model.SRObject{RefObjectID: 20200}},
	}}}
	repository := &itemUseRepositoryFake{}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	var sent network.Packet
	h.send = func(_ *server.Session, packet network.Packet) error { sent = packet; return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(14)
	request.WriteUInt16(model.InventoryItemType(ref))

	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if cubeID, active := player.ActiveMagicCube(); !active || cubeID != 444 {
		t.Fatalf("active cube = %d/%t, want 444/true", cubeID, active)
	}
	if repository.calls != 0 {
		t.Fatalf("cube activation consumed through item repository: %d calls", repository.calls)
	}
	if sent.MessageID != opcode.InventoryItemUseResponse || !bytes.Equal(sent.Payload(), []byte{1, 14, 0, 0, byte(model.InventoryItemType(ref)), byte(model.InventoryItemType(ref) >> 8)}) {
		t.Fatalf("cube activation response = 0x%04X % X", sent.MessageID, sent.Payload())
	}
}

func (f *itemUseRepositoryFake) UseResurrectionItem(context.Context, int64, model.ItemUseVitals) (model.ItemUseResult, error) {
	return model.ItemUseResult{}, model.ErrItemUseUnsupported
}

func TestCOSServiceItemSelectorResolvesPersistentPetFromInventorySlot(t *testing.T) {
	previousRefs := model.RefItems
	t.Cleanup(func() { model.RefItems = previousRefs })
	ref := model.RefItem{RefObject: model.RefObject{ID: 22712, IsUseable: true, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 13, TypeID4: 15}}}
	ref.Param1 = 1800
	model.RefItems = map[uint32]model.RefItem{22712: ref}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}, ID: 7, Session: session, BaseStats: model.BaseStats{HP: 600, MP: 250}, Inventory: model.Inventory{Items: map[byte]model.Item{
		14: {SRObject: model.SRObject{RefObjectID: 22712}, StackSize: 1},
		17: {Data: 79, COS: model.COSItemInfo{Present: true, State: model.COSItemSummoned}, SRObject: model.SRObject{TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 2, TypeID3: 1}}},
	}}}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{Inventory: player.Inventory, HP: 600, MP: 250, SourceSlot: 14, ItemType: model.InventoryItemType(ref)}}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.send = func(*server.Session, network.Packet) error { return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(14)
	request.WriteUInt16(model.InventoryItemType(ref))
	request.WriteByte(17)
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("COS service selector failed: %v", err)
	}
	if repository.calls != 1 || repository.target.ObjectID != 79 {
		t.Fatalf("targeted item use = calls %d target %#v", repository.calls, repository.target)
	}
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

func TestTalismanUsePublishesCommittedQuestCompletion(t *testing.T) {
	original := model.RefItems
	t.Cleanup(func() { model.RefItems = original })
	ref := model.RefItem{RefObject: model.RefObject{ID: 33893, IsUseable: true, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 12, TypeID4: 5}}}
	model.RefItems = map[uint32]model.RefItem{ref.ID: ref}
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	quest := model.CharacterQuest{QuestID: 910, Status: model.QuestRewardReady, AchievementCount: 1, ObjectiveKey: "SN_CON_TALISMAN", TaskCount: 1}
	player := &model.Player{SRObject: model.SRObject{UniqueID: 100}, ID: 7, Session: session, BaseStats: model.BaseStats{HP: 600, MP: 250}, Inventory: model.NewInventory(map[byte]model.Item{14: {SRObject: model.SRObject{RefObjectID: ref.ID}, StackSize: 1}})}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{
		Inventory: player.Inventory, HP: 600, MP: 250, SourceSlot: 14, ItemType: model.InventoryItemType(ref),
		CollectionThemeID: 1, CollectionSlotIndex: 1, CollectionThemeComplete: true,
		CollectionQuestProgress: []model.QuestProgressResult{{Quest: quest, Changed: true}},
	}}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	var sent []network.Packet
	h.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
	request := network.EmptyClientPacket()
	request.WriteByte(14)
	request.WriteUInt16(model.InventoryItemType(ref))
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatalf("use talisman: %v", err)
	}
	if len(sent) != 2 || sent[0].MessageID != opcode.InventoryItemUseResponse || sent[1].MessageID != opcode.QuestUpdate {
		t.Fatalf("talisman packets = %+v", sent)
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
	if published[0].Remaining() != 0 {
		t.Fatalf("stock speed buff has %d trailing bytes", published[0].Remaining())
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
	if player.ActiveCombatDamagePercent() != 20 || player.BonusStats.HP != 500 || player.BonusAttackStats.HitRate != 10 {
		t.Fatalf("applied modifiers = damage %d hp %d hit %d", player.ActiveCombatDamagePercent(), player.BonusStats.HP, player.BonusAttackStats.HitRate)
	}
	if len(published) != 1 || published[0].MessageID != opcode.EntityBuffAdd || expire == nil {
		t.Fatalf("buff publication = %+v expiry=%v", published, expire != nil)
	}
	expire()
	if player.ActiveCombatDamagePercent() != 0 || player.BonusStats.HP != 0 || player.BonusAttackStats.HitRate != 0 {
		t.Fatalf("expired modifiers = damage %d hp %d hit %d", player.ActiveCombatDamagePercent(), player.BonusStats.HP, player.BonusAttackStats.HitRate)
	}
	if len(published) != 2 || published[1].MessageID != opcode.EntityBuffRemove {
		t.Fatalf("expiration publication = %+v", published)
	}
}

func TestDirectCureItemConsumesOnNoEffectAndPublishesChangedStatus(t *testing.T) {
	for _, active := range []bool{false, true} {
		t.Run(map[bool]string{false: "no eligible status", true: "eligible status"}[active], func(t *testing.T) {
			previousRefs := model.RefItems
			t.Cleanup(func() { model.RefItems = previousRefs })
			ref := model.RefItem{RefObject: model.RefObject{
				ID: 10376, IsUseable: true,
				TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 1},
			}, Param1: 25_145_280, Param2: 9, Param3: 100}
			model.RefItems = map[uint32]model.RefItem{ref.ID: ref}
			effect, ok := model.ItemCureFromReference(ref)
			if !ok {
				t.Fatal("test cure reference did not decode")
			}

			now := time.Unix(4_000, 0)
			session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
			player := &model.Player{
				SRObject: model.SRObject{UniqueID: 100}, ID: 7, LifeState: model.Alive, Level: 10, Session: session,
				BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
				Inventory: model.Inventory{Items: map[byte]model.Item{14: {
					SRObject: model.SRObject{RefObjectID: ref.ID}, StackSize: 1,
				}}},
			}
			if active && !player.ApplyCombatStatusAt(model.CombatStatus{
				Kind: model.CombatStatusDarkness, Token: 77, Level: 9, ExpiresAt: now.Add(time.Minute),
			}, now) {
				t.Fatal("apply darkness")
			}
			repository := &itemUseRepositoryFake{result: model.ItemUseResult{
				Inventory: model.Inventory{Items: map[byte]model.Item{}}, HP: 600, MP: 250,
				SourceSlot: 14, ItemType: model.InventoryItemType(ref), ItemCure: &effect,
			}}
			h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
			h.now = func() time.Time { return now }
			var sent, published []network.Packet
			h.send = func(_ *server.Session, packet network.Packet) error { sent = append(sent, packet); return nil }
			h.publish = func(_ *model.Player, packet *network.Packet) { published = append(published, *packet) }
			h.schedule = func(time.Duration, func()) {}
			request := network.EmptyClientPacket()
			request.WriteByte(14)
			request.WriteUInt16(model.InventoryItemType(ref))
			if err := h.handlePacket(context.Background(), session, request); err != nil {
				t.Fatal(err)
			}
			if repository.calls != 1 || len(sent) != 1 || sent[0].MessageID != opcode.InventoryItemUseResponse {
				t.Fatalf("committed use = calls %d sent %+v", repository.calls, sent)
			}
			if _, exists := player.GetInventory().Items[14]; exists {
				t.Fatal("committed cure did not publish consumed inventory snapshot")
			}
			if !active {
				if len(published) != 0 {
					t.Fatalf("no-effect cure published %+v", published)
				}
				return
			}
			if _, remains := player.CombatStatus(model.CombatStatusDarkness, now); remains {
				t.Fatal("eligible darkness was not cured")
			}
			if len(published) != 1 || published[0].MessageID != opcode.EntityUpdateStatus {
				t.Fatalf("cure publications = %+v", published)
			}
			uid, e1 := published[0].ReadUInt32()
			marker, e2 := published[0].ReadUInt16()
			kind, e3 := published[0].ReadByte()
			mask, e4 := published[0].ReadUInt32()
			if errors.Join(e1, e2, e3, e4) != nil || uid != 100 || marker != 0 || kind != 4 || mask != 0 || published[0].Remaining() != 0 {
				t.Fatalf("bad-effect update = uid %d marker %d kind %d mask %#x remaining %d errors %v", uid, marker, kind, mask, published[0].Remaining(), errors.Join(e1, e2, e3, e4))
			}
		})
	}
}

func TestElementalCureSchedulesShortenedStatusExpiration(t *testing.T) {
	previousRefs := model.RefItems
	t.Cleanup(func() { model.RefItems = previousRefs })
	ref := model.RefItem{RefObject: model.RefObject{
		ID: 55, IsUseable: true,
		TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 3, TypeID3: 2, TypeID4: 6},
	}, Param1: 36, Param2: 36, Param3: 36, Param4: 36, Param5: 36, Param6: 36}
	model.RefItems = map[uint32]model.RefItem{ref.ID: ref}
	effect, ok := model.ItemCureFromReference(ref)
	if !ok {
		t.Fatal("test cure reference did not decode")
	}

	current := time.Unix(5_000, 0)
	session := &server.Session{UserContext: server.UserContext{UniqueID: 100, CharacterID: 7}}
	player := &model.Player{
		SRObject: model.SRObject{UniqueID: 100}, ID: 7, LifeState: model.Alive, Level: 10, Session: session,
		BaseStats: model.BaseStats{HP: 600, MP: 250, Str: 29, Int: 29},
		Inventory: model.Inventory{Items: map[byte]model.Item{14: {SRObject: model.SRObject{RefObjectID: ref.ID}, StackSize: 1}}},
	}
	if !player.ApplyCombatStatusAt(model.CombatStatus{Kind: model.CombatStatusFreeze, Token: 88, ExpiresAt: current.Add(10 * time.Second)}, current) {
		t.Fatal("apply freeze")
	}
	repository := &itemUseRepositoryFake{result: model.ItemUseResult{
		Inventory: model.Inventory{Items: map[byte]model.Item{}}, HP: 600, MP: 250,
		SourceSlot: 14, ItemType: model.InventoryItemType(ref), ItemCure: &effect,
	}}
	h := NewItemUseHandler(&inventoryHandlerPlayerLookupFake{player: player}, repository)
	h.now = func() time.Time { return current }
	h.send = func(*server.Session, network.Packet) error { return nil }
	var published []network.Packet
	h.publish = func(_ *model.Player, packet *network.Packet) { published = append(published, *packet) }
	var delay time.Duration
	var expire func()
	h.schedule = func(got time.Duration, callback func()) { delay, expire = got, callback }
	request := network.EmptyClientPacket()
	request.WriteByte(14)
	request.WriteUInt16(model.InventoryItemType(ref))
	if err := h.handlePacket(context.Background(), session, request); err != nil {
		t.Fatal(err)
	}
	if delay != 6_508*time.Millisecond || expire == nil || len(published) != 1 {
		t.Fatalf("shortened expiry = %v scheduled=%t publications=%d", delay, expire != nil, len(published))
	}
	current = current.Add(delay)
	expire()
	if len(published) != 2 || published[1].MessageID != opcode.EntityUpdateStatus {
		t.Fatalf("expiry publications = %+v", published)
	}
	// Exact payload decoding is already covered above; the refresh must expose
	// an empty abnormal-state mask after the shortened authoritative expiry.
	packet := published[1]
	_, _ = packet.ReadUInt32()
	_, _ = packet.ReadUInt16()
	_, _ = packet.ReadByte()
	mask, err := packet.ReadUInt32()
	if err != nil || mask != 0 {
		t.Fatalf("expiry mask = %#x error %v", mask, err)
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
