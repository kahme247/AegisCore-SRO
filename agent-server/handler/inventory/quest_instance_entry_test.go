package inventory

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/navmeshv2"
	"github.com/Harvi8/AegisCore/agent-server/service"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/g3n/engine/math32"
)

type questEntryPlayerLookup map[uint32]*model.Player

func (l questEntryPlayerLookup) GetPlayerByUniqueId(uid uint32) (*model.Player, error) {
	if player := l[uid]; player != nil {
		return player, nil
	}
	return nil, fmt.Errorf("missing player")
}

type questEntryMemberRepository struct {
	saved []model.QuestInstanceMemberState
}

func (r *questEntryMemberRepository) SaveQuestInstanceMember(_ context.Context, state model.QuestInstanceMemberState) error {
	r.saved = append(r.saved, state)
	return nil
}
func (*questEntryMemberRepository) LoadQuestInstanceMember(context.Context, int, uint32) (model.QuestInstanceMemberState, error) {
	return model.QuestInstanceMemberState{}, model.ErrQuestInstanceMissing
}
func (*questEntryMemberRepository) DeleteQuestInstanceMember(context.Context, int, uint16) error {
	return nil
}

type questEntryAllocator struct{ scope model.GameWorldScope }

func (a *questEntryAllocator) AllocateQuestInstanceScope(context.Context, int, uint32) (model.GameWorldScope, error) {
	return a.scope, nil
}
func (*questEntryAllocator) ReleaseQuestInstanceScope(context.Context, model.GameWorldScope) error {
	return nil
}

type questEntryTeleport struct{}

func (*questEntryTeleport) SubmitQuestInstanceEntry(model.QuestInstance, []*model.Player) error {
	return nil
}

func questEntryPlayer(uid uint32, id int, status model.QuestStatus) *model.Player {
	position := navmeshv2.RtNavmeshPosition{Region: navmeshv2.NewRegionFromInt16(27000), Offset: math32.NewVector3(1, 2, 3)}
	player := &model.Player{ID: id, SRObject: model.SRObject{UniqueID: uid, RtNavmeshPosition: position}, Quests: []model.CharacterQuest{{QuestID: 251, Status: status}}}
	player.Session = &server.Session{UserContext: server.UserContext{UniqueID: uid, CharacterID: id}}
	player.KnownObjectList = model.NewKnownObjectList(player)
	return player
}

func TestGoriaQuestInstanceEligibilityAcceptsActiveAndCompletedQuest(t *testing.T) {
	for _, status := range []model.QuestStatus{model.QuestActive, model.QuestProgressed, model.QuestRewardReady, model.QuestClosed} {
		player := &model.Player{Quests: []model.CharacterQuest{{QuestID: 251, Status: status}}}
		if !questInstanceEligible(player, 251) {
			t.Fatalf("status %d rejected", status)
		}
	}
	for _, status := range []model.QuestStatus{model.QuestInactive, model.QuestAborted} {
		player := &model.Player{Quests: []model.CharacterQuest{{QuestID: 251, Status: status}}}
		if questInstanceEligible(player, 251) {
			t.Fatalf("status %d accepted", status)
		}
	}
}

func TestPrepareGoriaQuestInstanceRequiresAuthoredSharingPartyAndJoinsEligibleMembers(t *testing.T) {
	leader := questEntryPlayer(100, 10, model.QuestActive)
	member := questEntryPlayer(200, 20, model.QuestClosed)
	party := &model.Party{Number: 7, MasterUniqueID: 100, PartySettingsFlag: model.ExpShareItemShare, Members: []uint32{100, 200}}
	if err := leader.AddToParty(party); err != nil {
		t.Fatal(err)
	}
	if err := member.AddToParty(party); err != nil {
		t.Fatal(err)
	}
	partyService := service.NewPartyService(questEntryPlayerLookup{100: leader, 200: member})
	partyService.Parties[7] = party
	if connected := partyService.ConnectedMembers(7); len(connected) != 2 || connected[0] != leader {
		t.Fatalf("connected=%#v", connected)
	}
	scope, _ := model.NewGameWorldScope(13, 1)
	repository := &questEntryMemberRepository{}
	lifecycle, err := model.NewQuestInstanceLifecycle(model.NewQuestInstanceRegistry(), repository, &questEntryAllocator{scope: scope})
	if err != nil {
		t.Fatal(err)
	}
	handler := &ItemUseHandler{questInstances: lifecycle, instanceTeleport: &questEntryTeleport{}, party: partyService, nextDungeonKey: func() uint32 { return 9001 }}
	prepared, err := handler.prepareQuestInstance(context.Background(), leader, 251)
	if err != nil {
		t.Fatal(err)
	}
	if prepared.instance.QuestID != 251 || prepared.instance.Scope != scope || len(prepared.members) != 2 || len(repository.saved) != 2 || leader.GetGameWorldID() != uint32(scope) || member.GetGameWorldID() != uint32(scope) {
		t.Fatalf("prepared=%+v saves=%#v scopes=%X/%X", prepared, repository.saved, leader.GetGameWorldID(), member.GetGameWorldID())
	}
	if repository.saved[0].OpenedAt.After(time.Now()) || repository.saved[0].DungeonKey != 9001 {
		t.Fatalf("owner state=%+v", repository.saved[0])
	}
}
