package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/aggregate"
)

type competitiveRewardCoordinatorFake struct{ command aggregate.Command }

func (coordinator *competitiveRewardCoordinatorFake) ExecuteLatest(_ context.Context, command aggregate.Command, _ aggregate.Mutation) (aggregate.Result, error) {
	coordinator.command = command
	return aggregate.Result{Payload: json.RawMessage(`{"gold_balance":1250}`), Duplicate: true}, nil
}

type competitivePlayerLookupFake struct{ player *model.Player }

func (lookup competitivePlayerLookupFake) GetPlayerByCharacterID(id int) (*model.Player, error) {
	if lookup.player == nil || lookup.player.ID != id {
		return nil, errors.New("not found")
	}
	return lookup.player, nil
}

type competitiveRewardSinkFake struct{ grants []CompetitiveReward }

func (sink *competitiveRewardSinkFake) GrantCompetitiveReward(_ context.Context, reward CompetitiveReward) error {
	sink.grants = append(sink.grants, reward)
	return nil
}

func TestCompetitiveRuntimeQueuesBalancesScoresAndRewards(t *testing.T) {
	sink := &competitiveRewardSinkFake{}
	runtime, err := NewCompetitiveRuntime(CompetitiveConfig{MinimumPlayers: 2, MaximumPlayers: 4, MatchDuration: time.Minute, WinnerGold: 100, LoserGold: 25}, sink)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	runtime.now = func() time.Time { return now }
	if err = runtime.Open(CompetitiveBattleArena); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 4; id++ {
		entry := CompetitiveEntry{CharacterID: id, UniqueID: uint32(100 + id), Name: string(rune('A' + id - 1)), GameType: 2, MatchType: 3}
		if err = runtime.Register(CompetitiveBattleArena, entry); err != nil {
			t.Fatalf("register %d: %v", id, err)
		}
		now = now.Add(time.Second)
	}
	matches, err := runtime.Start(CompetitiveBattleArena)
	if err != nil || len(matches) != 1 {
		t.Fatalf("start = %#v, %v", matches, err)
	}
	match := matches[0]
	if len(match.Teams[0]) != 2 || len(match.Teams[1]) != 2 || match.Teams[0][0].CharacterID != 1 || match.Teams[1][0].CharacterID != 2 {
		t.Fatalf("teams = %#v", match.Teams)
	}
	if _, err = runtime.AddScore(match.ID, 1, 10); err != nil {
		t.Fatal(err)
	}
	if _, err = runtime.AddScore(match.ID, 2, 5); err != nil {
		t.Fatal(err)
	}
	result, err := runtime.Finish(context.Background(), match.ID)
	if err != nil || result.Scores != [2]uint32{10, 5} || len(sink.grants) != 4 {
		t.Fatalf("finish = %#v grants=%#v err=%v", result, sink.grants, err)
	}
	if !sink.grants[0].Winner || sink.grants[0].Gold != 100 || sink.grants[2].Winner || sink.grants[2].Gold != 25 {
		t.Fatalf("rewards = %#v", sink.grants)
	}
}

func TestCompetitiveRewardReplaySynchronizesOnlinePlayerBalance(t *testing.T) {
	coordinator := &competitiveRewardCoordinatorFake{}
	repository, err := NewCompetitiveRewardRepository(coordinator)
	if err != nil {
		t.Fatal(err)
	}
	player := &model.Player{ID: 7, Gold: 1000}
	repository.ConfigureOnlinePlayers(competitivePlayerLookupFake{player: player})
	reward := CompetitiveReward{CommandKey: "competitive:9:7", MatchID: 9, Event: CompetitiveBattleArena, CharacterID: 7, Winner: true, Score: 3, Gold: 250}
	if err = repository.GrantCompetitiveReward(context.Background(), reward); err != nil {
		t.Fatal(err)
	}
	if player.GoldSnapshot() != 1250 || coordinator.command.Name != "competitive.reward" || string(coordinator.command.IdempotencyKey) != reward.CommandKey {
		t.Fatalf("player gold=%d command=%+v", player.GoldSnapshot(), coordinator.command)
	}
}

func TestCompetitiveRuntimeCancelAndRejectDuplicate(t *testing.T) {
	runtime, err := NewCompetitiveRuntime(CompetitiveConfig{MinimumPlayers: 2, MaximumPlayers: 2, MatchDuration: time.Minute, WinnerGold: 2, LoserGold: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	entry := CompetitiveEntry{CharacterID: 1, UniqueID: 11, Name: "A"}
	if err = runtime.Register(CompetitiveFlagWar, entry); err != ErrCompetitiveClosed {
		t.Fatalf("closed register = %v", err)
	}
	_ = runtime.Open(CompetitiveFlagWar)
	if err = runtime.Register(CompetitiveFlagWar, entry); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Register(CompetitiveFlagWar, entry); err != ErrCompetitiveAlreadyQueued {
		t.Fatalf("duplicate = %v", err)
	}
	if err = runtime.Open(CompetitiveBattleArena); err != nil {
		t.Fatal(err)
	}
	if err = runtime.Register(CompetitiveBattleArena, entry); err != ErrCompetitiveAlreadyQueued {
		t.Fatalf("cross-event duplicate = %v", err)
	}
	if err = runtime.Cancel(CompetitiveFlagWar, entry.CharacterID); err != nil || len(runtime.Queue(CompetitiveFlagWar)) != 0 {
		t.Fatalf("cancel = %v queue=%#v", err, runtime.Queue(CompetitiveFlagWar))
	}
}

func TestCompetitiveRuntimeRejectsActiveParticipantReregistration(t *testing.T) {
	runtime, err := NewCompetitiveRuntime(CompetitiveConfig{MinimumPlayers: 2, MaximumPlayers: 2, MatchDuration: time.Minute, WinnerGold: 2, LoserGold: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = runtime.Open(CompetitiveBattleArena)
	for id := 1; id <= 2; id++ {
		if err = runtime.Register(CompetitiveBattleArena, CompetitiveEntry{CharacterID: id, UniqueID: uint32(id), Name: "P", GameType: 1, MatchType: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = runtime.Start(CompetitiveBattleArena); err != nil {
		t.Fatal(err)
	}
	_ = runtime.Open(CompetitiveFlagWar)
	if err = runtime.Register(CompetitiveFlagWar, CompetitiveEntry{CharacterID: 1, UniqueID: 1, Name: "P"}); err != ErrCompetitiveParticipating {
		t.Fatalf("active participant registration = %v", err)
	}
}

func TestCompetitiveCombatAdmissionIsolatesOpposingTeams(t *testing.T) {
	runtime, err := NewCompetitiveRuntime(CompetitiveConfig{MinimumPlayers: 2, MaximumPlayers: 2, MatchDuration: time.Minute, WinnerGold: 2, LoserGold: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	_ = runtime.Open(CompetitiveBattleArena)
	for id := 1; id <= 2; id++ {
		if err = runtime.Register(CompetitiveBattleArena, CompetitiveEntry{CharacterID: id, UniqueID: uint32(id), Name: "P", GameType: 1, MatchType: 1}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = runtime.Start(CompetitiveBattleArena); err != nil {
		t.Fatal(err)
	}
	if participating, allowed := runtime.CombatAdmission(1, 2); !participating || !allowed {
		t.Fatalf("opponents admission = participating %t allowed %t", participating, allowed)
	}
	if participating, allowed := runtime.CombatAdmission(1, 99); !participating || allowed {
		t.Fatalf("outsider admission = participating %t allowed %t", participating, allowed)
	}
	if participating, _ := runtime.CombatAdmission(98, 99); participating {
		t.Fatal("ordinary world players treated as match participants")
	}
}

func TestCompetitiveRuntimeRejectsImpossiblePolicy(t *testing.T) {
	tests := []CompetitiveConfig{
		{MinimumPlayers: 3, MaximumPlayers: 4, MatchDuration: time.Minute, WinnerGold: 2, LoserGold: 1},
		{MinimumPlayers: 2, MaximumPlayers: 3, MatchDuration: time.Minute, WinnerGold: 2, LoserGold: 1},
		{MinimumPlayers: 2, MaximumPlayers: 4, MatchDuration: time.Millisecond, WinnerGold: 2, LoserGold: 1},
		{MinimumPlayers: 2, MaximumPlayers: 4, MatchDuration: 25 * time.Hour, WinnerGold: 2, LoserGold: 1},
		{MinimumPlayers: 2, MaximumPlayers: 4, MatchDuration: time.Minute, WinnerGold: 1, LoserGold: 2},
	}
	for _, config := range tests {
		if _, err := NewCompetitiveRuntime(config, nil); err == nil {
			t.Fatalf("accepted invalid config %+v", config)
		}
	}
}

func TestCompetitiveRuntimeExpiresMatchesAndRemovesDisconnectedQueueEntries(t *testing.T) {
	sink := &competitiveRewardSinkFake{}
	runtime, err := NewCompetitiveRuntime(CompetitiveConfig{MinimumPlayers: 2, MaximumPlayers: 2, MatchDuration: time.Minute, WinnerGold: 2, LoserGold: 1}, sink)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	runtime.now = func() time.Time { return now }
	_ = runtime.Open(CompetitiveFlagWar)
	for id := 1; id <= 3; id++ {
		if err = runtime.Register(CompetitiveFlagWar, CompetitiveEntry{CharacterID: id, UniqueID: uint32(id), Name: "P"}); err != nil {
			t.Fatal(err)
		}
	}
	matches, err := runtime.Start(CompetitiveFlagWar)
	if err != nil || len(matches) != 1 || len(runtime.Queue(CompetitiveFlagWar)) != 1 {
		t.Fatalf("start = %#v queue=%#v err=%v", matches, runtime.Queue(CompetitiveFlagWar), err)
	}
	runtime.RemoveCharacter(3)
	if queue := runtime.Queue(CompetitiveFlagWar); len(queue) != 0 {
		t.Fatalf("disconnect queue = %#v", queue)
	}
	now = now.Add(time.Minute)
	if err = runtime.Expire(context.Background()); err != nil {
		t.Fatal(err)
	}
	finished := runtime.Matches(CompetitiveFlagWar, CompetitiveFinished)
	if len(finished) != 1 || len(sink.grants) != 2 {
		t.Fatalf("finished = %#v grants=%#v", finished, sink.grants)
	}
}

func TestBattleArenaMatchmakingPartitionsModesAndPreservesFIFOLeftovers(t *testing.T) {
	runtime, err := NewCompetitiveRuntime(CompetitiveConfig{MinimumPlayers: 2, MaximumPlayers: 4, MatchDuration: time.Minute, WinnerGold: 2, LoserGold: 1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 7, 25, 12, 0, 0, 0, time.UTC)
	runtime.now = func() time.Time { return now }
	_ = runtime.Open(CompetitiveBattleArena)
	entries := []CompetitiveEntry{
		{CharacterID: 1, UniqueID: 1, Name: "A", GameType: 1, MatchType: 1},
		{CharacterID: 2, UniqueID: 2, Name: "B", GameType: 2, MatchType: 1},
		{CharacterID: 3, UniqueID: 3, Name: "C", GameType: 1, MatchType: 1},
		{CharacterID: 4, UniqueID: 4, Name: "D", GameType: 2, MatchType: 1},
		{CharacterID: 5, UniqueID: 5, Name: "E", GameType: 3, MatchType: 2},
	}
	for _, entry := range entries {
		if err = runtime.Register(CompetitiveBattleArena, entry); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Second)
	}
	matches, err := runtime.Start(CompetitiveBattleArena)
	if err != nil || len(matches) != 2 {
		t.Fatalf("matches = %#v, err=%v", matches, err)
	}
	if matches[0].GameType != 1 || matches[1].GameType != 2 {
		t.Fatalf("mixed or unstable modes = %#v", matches)
	}
	for _, match := range matches {
		for _, team := range match.Teams {
			for _, entry := range team {
				if entry.GameType != match.GameType || entry.MatchType != match.MatchType {
					t.Fatalf("entry %#v mixed into match %#v", entry, match)
				}
			}
		}
	}
	queue := runtime.Queue(CompetitiveBattleArena)
	if len(queue) != 1 || queue[0].CharacterID != 5 {
		t.Fatalf("leftovers = %#v", queue)
	}
}
