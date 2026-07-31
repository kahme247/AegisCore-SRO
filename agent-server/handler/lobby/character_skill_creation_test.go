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
	originalReferences := model.RefChars
	model.RefChars = map[uint32]model.RefChar{
		1907: {RefObject: model.RefObject{Country: 0}, DefaultSkill1: 1, DefaultSkill2: 2, DefaultSkill3: 40, DefaultSkill4: 70},
	}
	t.Cleanup(func() { model.RefChars = originalReferences })

	creator := &characterSkillCreatorFake{}
	handler := NewCharacterSelectionHandler(&fakeCharacterStore{})
	handler.creator = creator
	session, clientConn := newCharacterSelectionTestSession(t)
	defer session.Close()
	defer clientConn.Close()
	session.MarkAuthenticated()
	session.UserContext.UserID = 42

	request := buildCharacterSelectionRequest(t,
		func(packet *network.Packet) error { return packet.WriteByte(CharActionCreate) },
		func(packet *network.Packet) error { return packet.WriteString("skill-owner") },
		func(packet *network.Packet) error { return packet.WriteUInt32(1907) },
		func(packet *network.Packet) error { return packet.WriteByte(34) },
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
