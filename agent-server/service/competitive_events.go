package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/aggregate"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

type CompetitiveEvent byte

const (
	CompetitiveFlagWar CompetitiveEvent = iota + 1
	CompetitiveBattleArena
)

type CompetitiveState byte

const (
	CompetitiveClosed CompetitiveState = iota
	CompetitiveRegistration
	CompetitiveActive
	CompetitiveFinished
)

var (
	ErrCompetitiveClosed        = errors.New("competitive registration is closed")
	ErrCompetitiveAlreadyQueued = errors.New("character is already queued")
	ErrCompetitiveParticipating = errors.New("character is already participating")
	ErrCompetitiveNotQueued     = errors.New("character is not queued")
	ErrCompetitiveMatchNotFound = errors.New("competitive match is not active")
)

type CompetitiveEntry struct {
	CharacterID int
	UniqueID    uint32
	Name        string
	GameType    byte
	MatchType   byte
	QueuedAt    time.Time
}

type CompetitiveReward struct {
	CommandKey  string
	MatchID     uint64
	Event       CompetitiveEvent
	CharacterID int
	Winner      bool
	Score       uint32
	Gold        uint64
}

type CompetitiveRewardSink interface {
	GrantCompetitiveReward(context.Context, CompetitiveReward) error
}

type CompetitiveObserver interface {
	CompetitiveMatchStarted(CompetitiveMatch)
	CompetitiveScoreUpdated(CompetitiveMatch)
	CompetitiveMatchFinished(CompetitiveMatch)
}

type CompetitiveRewardRepository struct {
	coordinator aggregate.LatestCoordinator
	players     interface {
		GetPlayerByCharacterID(int) (*model.Player, error)
	}
}

func NewCompetitiveRewardRepository(coordinator aggregate.LatestCoordinator) (*CompetitiveRewardRepository, error) {
	if coordinator == nil {
		return nil, errors.New("competitive reward coordinator is required")
	}
	return &CompetitiveRewardRepository{coordinator: coordinator}, nil
}

func (repository *CompetitiveRewardRepository) ConfigureOnlinePlayers(players interface {
	GetPlayerByCharacterID(int) (*model.Player, error)
}) {
	if repository != nil {
		repository.players = players
	}
}

func (repository *CompetitiveRewardRepository) GrantCompetitiveReward(ctx context.Context, reward CompetitiveReward) error {
	if repository == nil || repository.coordinator == nil || reward.CommandKey == "" || reward.CharacterID <= 0 || reward.MatchID == 0 || reward.Gold == 0 {
		return errors.New("competitive reward is invalid")
	}
	aggregateID := aggregate.ID{Kind: aggregate.Character, Value: fmt.Sprintf("%d", reward.CharacterID)}
	command := aggregate.Command{Aggregate: aggregateID, IdempotencyKey: aggregate.IdempotencyKey(reward.CommandKey), Name: "competitive.reward"}
	result, err := repository.coordinator.ExecuteLatest(ctx, command, func(ctx context.Context, transaction aggregate.Transaction) (json.RawMessage, error) {
		result, updateErr := transaction.DB().ExecContext(ctx, "UPDATE `CHAR` SET `GOLD`=`GOLD`+? WHERE `ID`=?", reward.Gold, reward.CharacterID)
		if updateErr != nil {
			return nil, updateErr
		}
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
			return nil, errors.New("competitive reward character is unavailable")
		}
		var balance uint64
		if queryErr := transaction.DB().QueryRowContext(ctx, "SELECT `GOLD` FROM `CHAR` WHERE `ID`=?", reward.CharacterID).Scan(&balance); queryErr != nil {
			return nil, queryErr
		}
		payload := []byte(fmt.Sprintf(`{"match_id":%d,"character_id":%d,"winner":%t,"score":%d,"gold":%d,"gold_balance":%d}`, reward.MatchID, reward.CharacterID, reward.Winner, reward.Score, reward.Gold, balance))
		if enqueueErr := transaction.Enqueue(aggregate.OutboxMessage{ID: reward.CommandKey, Aggregate: aggregateID, Topic: "competitive.reward-granted", Payload: payload, OccurredAt: time.Now().UTC()}); enqueueErr != nil {
			return nil, enqueueErr
		}
		return payload, nil
	})
	if err != nil {
		return err
	}
	var committed struct {
		GoldBalance uint64 `json:"gold_balance"`
	}
	if err = json.Unmarshal(result.Payload, &committed); err != nil || committed.GoldBalance == 0 {
		return errors.New("competitive reward result is invalid")
	}
	if repository.players != nil {
		player, lookupErr := repository.players.GetPlayerByCharacterID(reward.CharacterID)
		if lookupErr == nil && player != nil {
			player.SetGold(committed.GoldBalance)
			if session := player.GetSession(); session != nil {
				_ = session.TrySend(vsro188.BuildGoldPointUpdate(vsro188.GoldPointUpdate{Value: committed.GoldBalance}))
			}
		}
	}
	return nil
}

type CompetitiveMatch struct {
	ID        uint64
	Event     CompetitiveEvent
	GameType  byte
	MatchType byte
	State     CompetitiveState
	StartedAt time.Time
	EndsAt    time.Time
	EndedAt   time.Time
	Teams     [2][]CompetitiveEntry
	Scores    [2]uint32
}

type CompetitiveConfig struct {
	MinimumPlayers int
	MaximumPlayers int
	MatchDuration  time.Duration
	WinnerGold     uint64
	LoserGold      uint64
}

type CompetitiveStatus struct {
	FlagWarQueued     int `json:"flagWarQueued"`
	FlagWarActive     int `json:"flagWarActive"`
	BattleArenaQueued int `json:"battleArenaQueued"`
	BattleArenaActive int `json:"battleArenaActive"`
}

type CompetitiveRuntime struct {
	mu       sync.Mutex
	now      func() time.Time
	config   CompetitiveConfig
	rewards  CompetitiveRewardSink
	observer CompetitiveObserver
	state    map[CompetitiveEvent]CompetitiveState
	queues   map[CompetitiveEvent][]CompetitiveEntry
	matches  map[uint64]*CompetitiveMatch
	nextID   uint64
	started  bool
	done     chan struct{}
}

func (r *CompetitiveRuntime) ConfigureObserver(observer CompetitiveObserver) {
	if r == nil {
		return
	}
	r.mu.Lock()
	r.observer = observer
	r.mu.Unlock()
}

func NewCompetitiveRuntime(config CompetitiveConfig, rewards CompetitiveRewardSink) (*CompetitiveRuntime, error) {
	if config.MinimumPlayers < 2 || config.MinimumPlayers%2 != 0 || config.MaximumPlayers < config.MinimumPlayers || config.MaximumPlayers%2 != 0 || config.MatchDuration < time.Second || config.MatchDuration > 24*time.Hour || config.WinnerGold < config.LoserGold {
		return nil, errors.New("competitive runtime configuration is invalid")
	}
	return &CompetitiveRuntime{now: time.Now, config: config, rewards: rewards, state: map[CompetitiveEvent]CompetitiveState{}, queues: map[CompetitiveEvent][]CompetitiveEntry{}, matches: map[uint64]*CompetitiveMatch{}}, nil
}

func (r *CompetitiveRuntime) Run(ctx context.Context) error {
	if r == nil {
		return errors.New("competitive runtime is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	if r.started {
		r.mu.Unlock()
		return errors.New("competitive runtime already started")
	}
	r.started = true
	r.done = make(chan struct{})
	done := r.done
	r.mu.Unlock()
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = r.Expire(ctx)
			}
		}
	}()
	return nil
}

func (r *CompetitiveRuntime) Wait(ctx context.Context) error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	done := r.done
	r.mu.Unlock()
	if done == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *CompetitiveRuntime) Expire(ctx context.Context) error {
	if r == nil {
		return nil
	}
	now := r.now().UTC()
	r.mu.Lock()
	var matchIDs []uint64
	for id, match := range r.matches {
		if match.State == CompetitiveActive && !now.Before(match.EndsAt) {
			matchIDs = append(matchIDs, id)
		}
	}
	r.mu.Unlock()
	sort.Slice(matchIDs, func(i, j int) bool { return matchIDs[i] < matchIDs[j] })
	for _, id := range matchIDs {
		if _, err := r.Finish(ctx, id); err != nil && !errors.Is(err, ErrCompetitiveMatchNotFound) {
			return err
		}
	}
	return nil
}

func (r *CompetitiveRuntime) Open(event CompetitiveEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !validCompetitiveEvent(event) {
		return errors.New("competitive event is invalid")
	}
	r.state[event] = CompetitiveRegistration
	return nil
}

func (r *CompetitiveRuntime) Register(event CompetitiveEvent, entry CompetitiveEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state[event] != CompetitiveRegistration {
		return ErrCompetitiveClosed
	}
	if entry.CharacterID <= 0 || entry.UniqueID == 0 || entry.Name == "" {
		return errors.New("competitive participant is invalid")
	}
	for _, queue := range r.queues {
		for _, queued := range queue {
			if queued.CharacterID == entry.CharacterID {
				return ErrCompetitiveAlreadyQueued
			}
		}
	}
	for _, match := range r.matches {
		if match.State != CompetitiveActive {
			continue
		}
		for _, team := range match.Teams {
			for _, participant := range team {
				if participant.CharacterID == entry.CharacterID {
					return ErrCompetitiveParticipating
				}
			}
		}
	}
	entry.QueuedAt = r.now().UTC()
	r.queues[event] = append(r.queues[event], entry)
	return nil
}

func (r *CompetitiveRuntime) Cancel(event CompetitiveEvent, characterID int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	queue := r.queues[event]
	for index, entry := range queue {
		if entry.CharacterID == characterID {
			r.queues[event] = append(queue[:index], queue[index+1:]...)
			return nil
		}
	}
	return ErrCompetitiveNotQueued
}

func (r *CompetitiveRuntime) Start(event CompetitiveEvent) ([]CompetitiveMatch, error) {
	r.mu.Lock()
	if r.state[event] != CompetitiveRegistration {
		r.mu.Unlock()
		return nil, ErrCompetitiveClosed
	}
	queue := append([]CompetitiveEntry(nil), r.queues[event]...)
	sort.SliceStable(queue, func(i, j int) bool { return queue[i].QueuedAt.Before(queue[j].QueuedAt) })
	type queueKey struct{ gameType, matchType byte }
	groups := make(map[queueKey][]CompetitiveEntry)
	var groupOrder []queueKey
	for _, entry := range queue {
		key := queueKey{}
		if event == CompetitiveBattleArena {
			key = queueKey{entry.GameType, entry.MatchType}
		}
		if _, exists := groups[key]; !exists {
			groupOrder = append(groupOrder, key)
		}
		groups[key] = append(groups[key], entry)
	}
	var started []CompetitiveMatch
	var remaining []CompetitiveEntry
	for _, key := range groupOrder {
		group := groups[key]
		for len(group) >= r.config.MinimumPlayers {
			count := min(len(group), r.config.MaximumPlayers)
			if count%2 != 0 {
				count--
			}
			if count < r.config.MinimumPlayers {
				break
			}
			r.nextID++
			now := r.now().UTC()
			match := &CompetitiveMatch{ID: r.nextID, Event: event, GameType: key.gameType, MatchType: key.matchType, State: CompetitiveActive, StartedAt: now, EndsAt: now.Add(r.config.MatchDuration)}
			for index, entry := range group[:count] {
				team := index % 2
				match.Teams[team] = append(match.Teams[team], entry)
			}
			r.matches[match.ID] = match
			started = append(started, cloneCompetitiveMatch(match))
			group = group[count:]
		}
		remaining = append(remaining, group...)
	}
	sort.SliceStable(remaining, func(i, j int) bool { return remaining[i].QueuedAt.Before(remaining[j].QueuedAt) })
	r.queues[event] = remaining
	if len(started) > 0 {
		r.state[event] = CompetitiveActive
	}
	observer := r.observer
	r.mu.Unlock()
	if observer != nil {
		for _, match := range started {
			observer.CompetitiveMatchStarted(match)
		}
	}
	return started, nil
}

func (r *CompetitiveRuntime) AddScore(matchID uint64, characterID int, points uint32) (CompetitiveMatch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	match := r.matches[matchID]
	if match == nil || match.State != CompetitiveActive || points == 0 {
		return CompetitiveMatch{}, ErrCompetitiveMatchNotFound
	}
	for team := range match.Teams {
		for _, entry := range match.Teams[team] {
			if entry.CharacterID == characterID {
				if match.Scores[team] > math.MaxUint32-points {
					return CompetitiveMatch{}, errors.New("competitive score overflow")
				}
				match.Scores[team] += points
				return cloneCompetitiveMatch(match), nil
			}
		}
	}
	return CompetitiveMatch{}, ErrCompetitiveMatchNotFound
}

func (r *CompetitiveRuntime) RecordPlayerKill(killerID, victimID int) error {
	r.mu.Lock()
	for _, match := range r.matches {
		if match.State != CompetitiveActive {
			continue
		}
		killerTeam, victimTeam := -1, -1
		for team, entries := range match.Teams {
			for _, entry := range entries {
				if entry.CharacterID == killerID {
					killerTeam = team
				}
				if entry.CharacterID == victimID {
					victimTeam = team
				}
			}
		}
		if killerTeam >= 0 && victimTeam >= 0 {
			if killerTeam == victimTeam {
				r.mu.Unlock()
				return errors.New("competitive friendly kill cannot score")
			}
			if match.Scores[killerTeam] == math.MaxUint32 {
				r.mu.Unlock()
				return errors.New("competitive score overflow")
			}
			match.Scores[killerTeam]++
			updated, observer := cloneCompetitiveMatch(match), r.observer
			r.mu.Unlock()
			if observer != nil {
				observer.CompetitiveScoreUpdated(updated)
			}
			return nil
		}
	}
	r.mu.Unlock()
	return ErrCompetitiveMatchNotFound
}

// CombatAdmission isolates active matches from the surrounding world. The
// first result reports whether either character belongs to an active match;
// callers retain their ordinary PvP policy when it is false.
func (r *CompetitiveRuntime) CombatAdmission(sourceID, targetID int) (participating, allowed bool) {
	if r == nil || sourceID <= 0 || targetID <= 0 || sourceID == targetID {
		return false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, match := range r.matches {
		if match.State != CompetitiveActive {
			continue
		}
		sourceTeam, targetTeam := -1, -1
		for team, entries := range match.Teams {
			for _, entry := range entries {
				if entry.CharacterID == sourceID {
					sourceTeam = team
				}
				if entry.CharacterID == targetID {
					targetTeam = team
				}
			}
		}
		if sourceTeam >= 0 || targetTeam >= 0 {
			return true, sourceTeam >= 0 && targetTeam >= 0 && sourceTeam != targetTeam
		}
	}
	return false, false
}

func (r *CompetitiveRuntime) Finish(ctx context.Context, matchID uint64) (CompetitiveMatch, error) {
	r.mu.Lock()
	match := r.matches[matchID]
	if match == nil || match.State != CompetitiveActive {
		r.mu.Unlock()
		return CompetitiveMatch{}, ErrCompetitiveMatchNotFound
	}
	result := cloneCompetitiveMatch(match)

	for team, entries := range result.Teams {
		winner := result.Scores[team] > result.Scores[1-team]
		gold := r.config.LoserGold
		if winner {
			gold = r.config.WinnerGold
		}
		for _, entry := range entries {
			if r.rewards == nil {
				continue
			}
			reward := CompetitiveReward{CommandKey: fmt.Sprintf("competitive:%d:%d", result.ID, entry.CharacterID), MatchID: result.ID, Event: result.Event, CharacterID: entry.CharacterID, Winner: winner, Score: result.Scores[team], Gold: gold}
			if err := r.rewards.GrantCompetitiveReward(ctx, reward); err != nil {
				r.mu.Unlock()
				return result, fmt.Errorf("grant competitive reward: %w", err)
			}
		}
	}
	match.State, match.EndedAt = CompetitiveFinished, r.now().UTC()
	result = cloneCompetitiveMatch(match)
	observer := r.observer
	r.mu.Unlock()
	if observer != nil {
		observer.CompetitiveMatchFinished(result)
	}
	return result, nil
}

func (r *CompetitiveRuntime) Queue(event CompetitiveEvent) []CompetitiveEntry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]CompetitiveEntry(nil), r.queues[event]...)
}

func (r *CompetitiveRuntime) Matches(event CompetitiveEvent, state CompetitiveState) []CompetitiveMatch {
	r.mu.Lock()
	defer r.mu.Unlock()
	var matches []CompetitiveMatch
	for _, match := range r.matches {
		if match.Event == event && match.State == state {
			matches = append(matches, cloneCompetitiveMatch(match))
		}
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].ID < matches[j].ID })
	return matches
}

func (r *CompetitiveRuntime) Status() CompetitiveStatus {
	if r == nil {
		return CompetitiveStatus{}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	status := CompetitiveStatus{FlagWarQueued: len(r.queues[CompetitiveFlagWar]), BattleArenaQueued: len(r.queues[CompetitiveBattleArena])}
	for _, match := range r.matches {
		if match.State != CompetitiveActive {
			continue
		}
		if match.Event == CompetitiveFlagWar {
			status.FlagWarActive++
		} else if match.Event == CompetitiveBattleArena {
			status.BattleArenaActive++
		}
	}
	return status
}

func (r *CompetitiveRuntime) RemoveCharacter(characterID int) {
	if r == nil || characterID <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for event, queue := range r.queues {
		filtered := queue[:0]
		for _, entry := range queue {
			if entry.CharacterID != characterID {
				filtered = append(filtered, entry)
			}
		}
		r.queues[event] = filtered
	}
}

func cloneCompetitiveMatch(match *CompetitiveMatch) CompetitiveMatch {
	clone := *match
	clone.Teams[0] = append([]CompetitiveEntry(nil), match.Teams[0]...)
	clone.Teams[1] = append([]CompetitiveEntry(nil), match.Teams[1]...)
	return clone
}

func validCompetitiveEvent(event CompetitiveEvent) bool {
	return event == CompetitiveFlagWar || event == CompetitiveBattleArena
}
