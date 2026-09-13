package character

import (
	"errors"
	"testing"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestQuestDayLimitPeriodAndSharedQuota(t *testing.T) {
	now := time.Date(2026, 7, 23, 21, 0, 0, 0, time.UTC)
	service := NewQuestDayLimitService(func() time.Time { return now })
	offer := model.QuestOffer{QuestID: 1, DayLimit: 2, DayOrNight: 1}
	if !service.Available(offer) {
		t.Fatal("night quest unavailable at 21:00")
	}
	for attempt := 0; attempt < 2; attempt++ {
		if _, err := service.WithActivation(offer, func() (model.CharacterQuest, error) {
			return model.CharacterQuest{QuestID: 1}, nil
		}); err != nil {
			t.Fatalf("activation %d: %v", attempt, err)
		}
	}
	if service.Available(offer) {
		t.Fatal("shared quota remained available after two starts")
	}
	if _, err := service.WithActivation(offer, func() (model.CharacterQuest, error) {
		return model.CharacterQuest{}, nil
	}); !errors.Is(err, model.ErrCharacterQuestNotAvailable) {
		t.Fatalf("exhausted activation error = %v", err)
	}

	now = now.Add(23 * time.Hour) // next 20:00 boundary has passed
	if !service.Available(offer) {
		t.Fatal("quota did not replenish at the next night boundary")
	}
}

func TestQuestDayLimitFailedActivationDoesNotConsumeQuota(t *testing.T) {
	now := time.Date(2026, 7, 23, 10, 0, 0, 0, time.UTC)
	service := NewQuestDayLimitService(func() time.Time { return now })
	offer := model.QuestOffer{QuestID: 2, DayLimit: 1, DayOrNight: 2}
	wantErr := errors.New("database unavailable")
	if _, err := service.WithActivation(offer, func() (model.CharacterQuest, error) {
		return model.CharacterQuest{}, wantErr
	}); !errors.Is(err, wantErr) {
		t.Fatalf("failed activation error = %v", err)
	}
	if !service.Available(offer) {
		t.Fatal("failed activation consumed quota")
	}
	now = time.Date(2026, 7, 23, 3, 59, 0, 0, time.UTC)
	if service.Available(offer) {
		t.Fatal("day quest available before 04:00")
	}
}
