package service

import (
	"context"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

type PreparedQuestInstanceEntry struct {
	Instance model.QuestInstance
	Members  []*model.Player
}

type QuestInstanceEntryService struct {
	lifecycle      *model.QuestInstanceLifecycle
	party          *PartyService
	nextDungeonKey func() uint32
}

func NewQuestInstanceEntryService(lifecycle *model.QuestInstanceLifecycle, party *PartyService, nextDungeonKey func() uint32) *QuestInstanceEntryService {
	return &QuestInstanceEntryService{lifecycle: lifecycle, party: party, nextDungeonKey: nextDungeonKey}
}

func (s *QuestInstanceEntryService) Prepare(ctx context.Context, player *model.Player, questID int) (*PreparedQuestInstanceEntry, error) {
	if s == nil || s.lifecycle == nil || s.party == nil || s.nextDungeonKey == nil || player == nil || questID <= 0 || player.COSState.Snapshot().TransportUniqueID != 0 {
		return nil, model.ErrQuestInstanceInvalid
	}
	party := player.GetParty()
	if party == nil || party.MasterUniqueID != player.GetUniqueID() || !party.PartySettingsFlag.IsSharingExp() || !party.PartySettingsFlag.IsSharingItem() {
		return nil, model.ErrQuestInstanceInvalid
	}
	connected := s.party.ConnectedMembers(party.Number)
	members := make([]*model.Player, 0, len(connected))
	for _, member := range connected {
		if member == nil || member.GetGameWorldID() != 0 || member.COSState.Snapshot().TransportUniqueID != 0 || !QuestInstanceEligible(member, questID) {
			continue
		}
		members = append(members, member)
	}
	if len(members) == 0 || members[0] != player {
		return nil, model.ErrQuestInstanceInvalid
	}
	dungeonKey := s.nextDungeonKey()
	if dungeonKey == 0 {
		return nil, model.ErrQuestInstanceInvalid
	}
	opened := time.Now().UTC()
	instance, err := s.lifecycle.Create(ctx, questID, dungeonKey, player, opened, true)
	if err != nil {
		return nil, err
	}
	joined := []*model.Player{player}
	for _, member := range members[1:] {
		instance, err = s.lifecycle.Join(ctx, instance.Scope, dungeonKey, member, opened, 1)
		if err != nil {
			_ = s.lifecycle.Destroy(ctx, instance.Scope, joined)
			return nil, err
		}
		joined = append(joined, member)
	}
	return &PreparedQuestInstanceEntry{Instance: instance, Members: joined}, nil
}

func (s *QuestInstanceEntryService) Abort(ctx context.Context, prepared *PreparedQuestInstanceEntry) error {
	if s == nil || s.lifecycle == nil || prepared == nil {
		return model.ErrQuestInstanceInvalid
	}
	return s.lifecycle.Destroy(ctx, prepared.Instance.Scope, prepared.Members)
}

func QuestInstanceEligible(player *model.Player, questID int) bool {
	for _, quest := range player.QuestSnapshot() {
		if quest.QuestID == questID && (quest.Status.ClientActive() || quest.Status == model.QuestClosed) {
			return true
		}
	}
	return false
}
