package lobby

import (
	"context"
	"reflect"
	"testing"

	"github.com/ferdoran/go-sro/agent-server/model"
	"github.com/ferdoran/go-sro/framework/network"
)

type characterSkillCreatorFake struct {
	character model.Char
	items     []model.CharacterStarterItem
	skills    []model.CharacterSkill
}

type characterCreationPolicyFake struct{}

func (characterCreationPolicyFake) Resolve(_ context.Context, selection model.CharacterCreationSelection) (model.CharacterCreationDefaults, []model.CharacterStarterItem, error) {
	if selection.ModelID != 1907 || selection.Items[model.SlotPrimaryWeapon] != 3632 {
		return model.CharacterCreationDefaults{}, nil, model.ErrCharacterCreationSelectionInvalid
	}
	items := make([]model.CharacterStarterItem, 0, 4)
	for _, slot := range []byte{model.SlotPrimaryWeapon, model.SlotChest, model.SlotBoots, model.SlotPants} {
		items = append(items, model.CharacterStarterItem{ReferenceItemID: selection.Items[slot], Slot: slot})
	}
	return model.CharacterCreationDefaults{
		Scale: 34, Level: 1, Strength: 20, Intellect: 20, HP: 200, MP: 200,
		PosX: 950, PosY: 40, PosZ: 1091, Region: 0x62A8, ReturnTeleportID: 1, InventorySize: model.BaseInventorySize,
	}, items, nil
}

func (f *characterSkillCreatorFake) CreateCharacter(_ context.Context, character model.Char, items []model.CharacterStarterItem) (int64, error) {
	return f.CreateCharacterWithSkills(context.Background(), character, items, nil)
}

func (f *characterSkillCreatorFake) CreateCharacterWithSkills(_ context.Context, character model.Char, items []model.CharacterStarterItem, skills []model.CharacterSkill) (int64, error) {
	return f.CreateCharacterWithProgress(context.Background(), character, items, skills, nil)
}

func (f *characterSkillCreatorFake) CreateCharacterWithProgress(_ context.Context, character model.Char, items []model.CharacterStarterItem, skills []model.CharacterSkill, masteries []model.MasteryProgress) (int64, error) {
	f.character = character
	f.items = append([]model.CharacterStarterItem(nil), items...)
	f.skills = append([]model.CharacterSkill(nil), skills...)
	return 77, nil
}

type characterSkillRepositoryFake struct {
	skills []model.CharacterSkill
	err    error
	ctx    context.Context
	id     int
}

type characterMasteryRepositoryFake struct {
	masteries map[uint32]byte
	ctx       context.Context
	id        int
}

func (f *characterMasteryRepositoryFake) LoadMasteries(ctx context.Context, characterID int) (map[uint32]byte, error) {
	f.ctx = ctx
	f.id = characterID
	result := make(map[uint32]byte, len(f.masteries))
	for id, level := range f.masteries {
		result[id] = level
	}
	return result, nil
}

func (*characterMasteryRepositoryFake) IncreaseMastery(context.Context, int, uint32, byte, byte, int, uint64) (model.MasteryIncreaseResult, error) {
	return model.MasteryIncreaseResult{}, nil
}

func (f *characterSkillRepositoryFake) LoadSkills(ctx context.Context, characterID int) ([]model.CharacterSkill, error) {
	f.ctx = ctx
	f.id = characterID
	return append([]model.CharacterSkill(nil), f.skills...), f.err
}

func TestCharacterCreationDelegatesReferenceDefaultsToRepository(t *testing.T) {
	creator := &characterSkillCreatorFake{}
	handler := NewCharacterSelectionHandler(&fakeCharacterStore{})
	handler.creator = creator
	handler.creationPolicy = characterCreationPolicyFake{}
	handler.referenceData = characterCreationTestCatalog()
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 42

	request := buildCharacterSelectionRequest(t,
		func(packet *network.Packet) error { return packet.WriteByte(CharActionCreate) },
		func(packet *network.Packet) error { return packet.WriteString("skill-owner") },
		func(packet *network.Packet) error { return packet.WriteUInt32(1907) },
		func(packet *network.Packet) error { return packet.WriteByte(255) },
		func(packet *network.Packet) error { return packet.WriteUInt32(3640) },
		func(packet *network.Packet) error { return packet.WriteUInt32(3641) },
		func(packet *network.Packet) error { return packet.WriteUInt32(3642) },
		func(packet *network.Packet) error { return packet.WriteUInt32(3632) },
	)
	response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
		return handler.handleAction(context.Background(), session, request)
	})
	if err != nil {
		t.Fatalf("handleAction(create): %v", err)
	}
	action := byte(CharActionCreate)
	assertCharacterSelectionResult(t, &response, OpcodeCharSelectionActionResponse, &action, ResultTrue)
	if len(creator.skills) != 0 {
		t.Fatalf("starter skills = %#v, want database reference defaults", creator.skills)
	}
	if creator.character.RefObjID != 1907 || creator.character.User != 42 || creator.character.Shard != 1 {
		t.Fatalf("created character ownership/reference = %+v", creator.character)
	}
	if creator.character.Scale != 34 {
		t.Fatalf("created character scale = %d, want policy default 34", creator.character.Scale)
	}
}

func TestCharacterCreationRejectsNonPlayableModelAndUnapprovedItem(t *testing.T) {
	tests := []struct {
		name    string
		modelID uint32
		weapon  uint32
	}{
		{name: "npc model", modelID: 5000, weapon: 3632},
		{name: "high level weapon", modelID: 1907, weapon: 9000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			creator := &characterSkillCreatorFake{}
			handler := NewCharacterSelectionHandler(&fakeCharacterStore{})
			handler.creator = creator
			handler.creationPolicy = characterCreationPolicyFake{}
			handler.referenceData = characterCreationTestCatalog()
			session, clientConn := newCharacterSelectionTestSession(t)
			defer session.Close()
			defer clientConn.Close()
			session.MarkAuthenticated()
			session.UserContext.UserID = 42

			request := buildCharacterSelectionRequest(t,
				func(packet *network.Packet) error { return packet.WriteByte(CharActionCreate) },
				func(packet *network.Packet) error { return packet.WriteString("invalid-owner") },
				func(packet *network.Packet) error { return packet.WriteUInt32(test.modelID) },
				func(packet *network.Packet) error { return packet.WriteByte(255) },
				func(packet *network.Packet) error { return packet.WriteUInt32(3640) },
				func(packet *network.Packet) error { return packet.WriteUInt32(3641) },
				func(packet *network.Packet) error { return packet.WriteUInt32(3642) },
				func(packet *network.Packet) error { return packet.WriteUInt32(test.weapon) },
			)
			response, err := dispatchCharacterSelectionHandler(t, clientConn, func() error {
				return handler.handleAction(context.Background(), session, request)
			})
			if err != nil {
				t.Fatalf("handleAction(create): %v", err)
			}
			action := byte(CharActionCreate)
			assertCharacterSelectionResult(t, &response, OpcodeCharSelectionActionResponse, &action, ResultFalse)
			code, readErr := response.ReadUInt16()
			if readErr != nil || code != ACTION_SMERR_INVALID_CHARGEN_INFO {
				t.Fatalf("creation error = 0x%04X, %v; want 0x%04X", code, readErr, ACTION_SMERR_INVALID_CHARGEN_INFO)
			}
			if creator.character.RefObjID != 0 {
				t.Fatalf("creator received rejected character %+v", creator.character)
			}
		})
	}
}

func characterCreationTestCatalog() *model.ReferenceDataCatalog {
	starter := func(id uint32, typeInfo model.TypeInfo) model.RefItem {
		return model.RefItem{RefObject: model.RefObject{ID: id, TypeInfo: typeInfo, ReqLevelType1: 1, ReqLevel1: 1}, ItemClass: 1}
	}
	return model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{
		Characters: map[uint32]model.RefChar{
			1907: {RefObject: model.RefObject{ID: 1907, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 1}}, CharGender: 1},
			5000: {RefObject: model.RefObject{ID: 5000, TypeInfo: model.TypeInfo{TypeID1: 1, TypeID2: 2}}},
		},
		Items: map[uint32]model.RefItem{
			3632: starter(3632, model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 2}),
			3640: starter(3640, model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 2, TypeID4: 3}),
			3641: starter(3641, model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 2, TypeID4: 4}),
			3642: starter(3642, model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 2, TypeID4: 6}),
			9000: {RefObject: model.RefObject{ID: 9000, TypeInfo: model.TypeInfo{TypeID1: 3, TypeID2: 1, TypeID3: 6, TypeID4: 2}, ReqLevelType1: 1, ReqLevel1: 100}, ItemClass: 10},
		},
	})
}

func TestCharacterSkillLoadPopulatesPlayerSnapshot(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "load-skills")
	repository := &characterSkillRepositoryFake{skills: []model.CharacterSkill{{ID: 69, Enabled: 1}, {ID: 1, Enabled: 1}}}
	catalog := model.NewReferenceDataCatalog(model.ReferenceDataSnapshot{Skills: map[uint32]model.RefSkill{69: {ID: 69, BasicActivity: 0, BuffModifiers: model.BuffModifiers{HP: 102}}}})
	handler := &CharacterSelectionHandler{skills: repository, referenceData: catalog}
	player := &model.Player{ID: 77}
	if err := handler.loadPlayerSkills(ctx, player); err != nil {
		t.Fatalf("loadPlayerSkills: %v", err)
	}
	if repository.id != 77 || repository.ctx.Value(contextKey{}) != "load-skills" {
		t.Fatalf("repository load id/context = %d/%v", repository.id, repository.ctx)
	}
	want := []model.CharacterSkill{{ID: 1, Enabled: 1}, {ID: 69, Enabled: 1}}
	if got := player.SkillSnapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("player skills = %#v, want %#v", got, want)
	}
	if player.BonusStats.HP != 102 {
		t.Fatalf("bootstrap passive HP = %d, want 102", player.BonusStats.HP)
	}
}

func TestCharacterMasteryLoadAcceptsPersistedRaceDefaultsAtLevelZero(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "load-masteries")
	repository := &characterMasteryRepositoryFake{masteries: map[uint32]byte{
		257: 0, 258: 0, 259: 0, 273: 0, 274: 0, 275: 0, 276: 0,
	}}
	handler := &CharacterSelectionHandler{masteries: repository}
	player := &model.Player{ID: 77, SRObject: model.SRObject{RefObjectID: 1907}, Level: 1}
	if err := handler.loadPlayerMasteries(ctx, player); err != nil {
		t.Fatalf("loadPlayerMasteries: %v", err)
	}
	if repository.id != 77 || repository.ctx.Value(contextKey{}) != "load-masteries" {
		t.Fatalf("repository load id/context = %d/%v", repository.id, repository.ctx)
	}
	want := map[uint32]byte{257: 0, 258: 0, 259: 0, 273: 0, 274: 0, 275: 0, 276: 0}
	if !reflect.DeepEqual(player.Masteries, want) {
		t.Fatalf("player masteries = %#v, want %#v", player.Masteries, want)
	}
}
