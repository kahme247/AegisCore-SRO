package character

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/server"
	"github.com/Harvi8/AegisCore/protocol/vsro188"
	"github.com/sirupsen/logrus"
)

type questEventDispatcherFunc func(context.Context, int, int, model.QuestAuthoredEvent) error

func (f questEventDispatcherFunc) DispatchQuestEvent(ctx context.Context, characterID, questID int, event model.QuestAuthoredEvent) error {
	return f(ctx, characterID, questID, event)
}

// QuestCountdownService owns the two script-authored positive countdowns.
// GameServer keeps its remaining byte in transient runtime state. AegisCore
// checkpoints that byte explicitly so reconnect hydration resumes instead of
// granting a fresh authored duration.
type QuestCountdownService struct {
	repository model.CharacterQuestRepository
	schedule   func(time.Duration, func())
	send       func(*server.Session, network.Packet) error
}

type questCountdownCheckpointRepository interface {
	SaveCountdown(context.Context, int, int, uint32) error
}

func NewQuestCountdownService(repository model.CharacterQuestRepository) *QuestCountdownService {
	return &QuestCountdownService{repository: repository, schedule: func(delay time.Duration, callback func()) {
		time.AfterFunc(delay, callback)
	}}
}

func timedQuestExpiryKey(codeName string) (string, bool) {
	switch codeName {
	case "QNO_KT_ACCESSORY_1":
		return "SN_TALK_QNO_KT_ACCESSORY_1_09", true
	case "QNO_WC_SMITH_1":
		return "SN_TALK_QNO_WC_SMITH_1_07", true
	default:
		return "", false
	}
}

func (s *QuestCountdownService) Start(ctx context.Context, session *server.Session, player *model.Player, quest model.CharacterQuest) error {
	if s == nil || s.repository == nil || session == nil || player == nil || quest.TimerDuration == 0 {
		return nil
	}
	checkpointRepository, ok := s.repository.(questCountdownCheckpointRepository)
	if !ok {
		return fmt.Errorf("quest countdown repository cannot persist remaining time")
	}
	expiryKey, supported := timedQuestExpiryKey(quest.QuestCodeName)
	if !supported || quest.TimerDuration > 255 || !quest.Status.ClientActive() {
		return fmt.Errorf("quest %d has unsupported countdown metadata", quest.QuestID)
	}
	send := func(packet network.Packet) error {
		if s.send != nil {
			return s.send(session, packet)
		}
		return session.Send(packet)
	}
	dispatcher := questEventDispatcherFunc(func(eventCtx context.Context, characterID, questID int, event model.QuestAuthoredEvent) error {
		if event != model.QuestAuthoredCountdown {
			return model.ErrQuestStateConflict
		}
		err := s.repository.Abandon(eventCtx, characterID, questID)
		if errors.Is(err, model.ErrCharacterQuestNotActive) {
			return nil
		}
		if err != nil {
			return err
		}
		player.MarkQuestAborted(questID)
		if err = send(buildQuestRemovePacket(questID)); err != nil {
			return err
		}
		notice, err := vsro188.BuildQuestNotification(expiryKey)
		if err != nil {
			return err
		}
		return send(notice)
	})
	publisher := questTimerPublisher{
		dispatcher: dispatcher, send: send, schedule: s.schedule, tickCadence: time.Second,
		onError: func(err error) {
			logrus.WithError(err).WithFields(logrus.Fields{"character_id": player.ID, "quest_id": quest.QuestID}).Error("quest countdown tick failed")
		},
		isActive: func(candidate *model.Player, questID int) bool {
			current, exists := playerQuest(candidate, questID)
			return exists && current.Status.ClientActive()
		},
		checkpoint: func(checkpointCtx context.Context, timer model.QuestTimer) error {
			return checkpointRepository.SaveCountdown(checkpointCtx, player.ID, timer.QuestID, timer.Remaining)
		},
	}
	duration := quest.TimerDuration
	if quest.TimerRemaining != 0 {
		duration = quest.TimerRemaining
	}
	_, _, err := publisher.start(ctx, player, quest.QuestID, duration, 0, questTimerWireMetadata{
		PackedQuestByte: byte(quest.AchievementCount),
	})
	return err
}

func (s *QuestCountdownService) Rehydrate(ctx context.Context, session *server.Session, player *model.Player) error {
	if s == nil || player == nil {
		return nil
	}
	for _, quest := range player.QuestSnapshot() {
		if quest.TimerDuration == 0 || !quest.Status.ClientActive() {
			continue
		}
		if err := s.Start(ctx, session, player, quest); err != nil {
			return err
		}
	}
	return nil
}
