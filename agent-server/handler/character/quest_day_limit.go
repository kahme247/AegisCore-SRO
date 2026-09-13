package character

import (
	"sync"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

// QuestDayLimitService owns the shared start budget authored by
// LuaStartDayLimit. The pinned GameServer stores this counter on the singleton
// quest descriptor, so it is deliberately shared by every character.
type QuestDayLimitService struct {
	mu     sync.Mutex
	now    func() time.Time
	states map[int]questDayLimitState
}

type questDayLimitState struct {
	window    int64
	remaining uint32
}

func NewQuestDayLimitService(now func() time.Time) *QuestDayLimitService {
	if now == nil {
		now = time.Now
	}
	return &QuestDayLimitService{now: now, states: make(map[int]questDayLimitState)}
}

func (s *QuestDayLimitService) Available(offer model.QuestOffer) bool {
	if offer.DayLimit == 0 {
		return true
	}
	if s == nil || !questDayPeriodAllows(offer.DayOrNight, s.now().Hour()) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stateLocked(offer).remaining != 0
}

// WithActivation serializes the quota check with the durable character
// activation. The budget is consumed only after that activation succeeds.
func (s *QuestDayLimitService) WithActivation(offer model.QuestOffer, activate func() (model.CharacterQuest, error)) (model.CharacterQuest, error) {
	if offer.DayLimit == 0 {
		return activate()
	}
	if s == nil {
		return model.CharacterQuest{}, model.ErrCharacterQuestNotAvailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if !questDayPeriodAllows(offer.DayOrNight, now.Hour()) {
		return model.CharacterQuest{}, model.ErrCharacterQuestNotAvailable
	}
	state := s.stateAtLocked(offer, now)
	if state.remaining == 0 {
		return model.CharacterQuest{}, model.ErrCharacterQuestNotAvailable
	}
	quest, err := activate()
	if err != nil {
		return model.CharacterQuest{}, err
	}
	state.remaining--
	s.states[offer.QuestID] = state
	return quest, nil
}

func (s *QuestDayLimitService) stateLocked(offer model.QuestOffer) questDayLimitState {
	return s.stateAtLocked(offer, s.now())
}

func (s *QuestDayLimitService) stateAtLocked(offer model.QuestOffer, now time.Time) questDayLimitState {
	window := questDayWindow(offer.DayOrNight, now)
	state, ok := s.states[offer.QuestID]
	if !ok || state.window != window {
		state = questDayLimitState{window: window, remaining: offer.DayLimit}
		s.states[offer.QuestID] = state
	}
	return state
}

func questDayPeriodAllows(selector uint32, hour int) bool {
	switch selector {
	case 1:
		return hour < 4 || hour >= 20
	case 2:
		return hour >= 4 && hour < 20
	default:
		return false
	}
}

func questDayWindow(selector uint32, now time.Time) int64 {
	boundaryHour := 4
	if selector == 1 {
		boundaryHour = 20
	}
	boundary := time.Date(now.Year(), now.Month(), now.Day(), boundaryHour, 0, 0, 0, now.Location())
	if now.Before(boundary) {
		boundary = boundary.AddDate(0, 0, -1)
	}
	return boundary.Unix()
}
