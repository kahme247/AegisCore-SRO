package model

import (
	"context"
	"errors"
	"fmt"
)

var ErrQuestEventNotFound = errors.New("quest event target is not found")

type QuestAuthoredEvent byte

const (
	QuestAuthoredInteraction QuestAuthoredEvent = 0x07
	QuestAuthoredAbort       QuestAuthoredEvent = 0x08
	QuestAuthoredCountdown   QuestAuthoredEvent = 0x0e
	QuestAuthoredLocalAction QuestAuthoredEvent = 0x17
	QuestAuthoredMode2Timer  QuestAuthoredEvent = 0x18
)

// QuestEventDispatcher is the authored behavior boundary used after packet
// state validation. A dispatcher must return only after its event mutation is
// durable; packet handlers never acknowledge an event they did not execute.
type QuestEventDispatcher interface {
	DispatchQuestEvent(context.Context, int, int, QuestAuthoredEvent) error
}

// QuestTransition names durable state changes confirmed in the pinned
// SR_GameServer lifecycle. Authored quest event IDs are a separate dispatch
// layer and must not be conflated with these persistence transitions.
type QuestTransition byte

const (
	QuestTransitionActivate QuestTransition = iota + 1
	QuestTransitionProgress
	QuestTransitionRewardReady
	QuestTransitionClose
	QuestTransitionAbort
)

func (status QuestStatus) Valid() bool {
	return status >= QuestActive && status <= QuestRewardReady
}

func (status QuestStatus) ClientActive() bool {
	return status == QuestActive || status == QuestProgressed || status == QuestRewardReady
}

// TransitionQuest applies only transitions whose source and destination are
// binary-confirmed. States 2 and 3 deliberately have no authored transition
// here because their exact producers remain unresolved.
func TransitionQuest(status QuestStatus, transition QuestTransition) (QuestStatus, error) {
	switch transition {
	case QuestTransitionActivate:
		if status == QuestInactive || status == QuestAborted {
			return QuestActive, nil
		}
	case QuestTransitionProgress:
		if status == QuestActive || status == QuestProgressed {
			return QuestProgressed, nil
		}
	case QuestTransitionRewardReady:
		if status == QuestActive || status == QuestProgressed {
			return QuestRewardReady, nil
		}
	case QuestTransitionClose:
		if status == QuestRewardReady {
			return QuestClosed, nil
		}
	case QuestTransitionAbort:
		if status.ClientActive() {
			return QuestAborted, nil
		}
	default:
		return 0, fmt.Errorf("unknown quest transition %d", transition)
	}
	return 0, fmt.Errorf("quest status %d cannot apply transition %d: %w", status, transition, ErrQuestStateConflict)
}

func QuestTransitionSources(transition QuestTransition) []QuestStatus {
	switch transition {
	case QuestTransitionActivate:
		return []QuestStatus{QuestInactive, QuestAborted}
	case QuestTransitionProgress, QuestTransitionRewardReady:
		return []QuestStatus{QuestActive, QuestProgressed}
	case QuestTransitionClose:
		return []QuestStatus{QuestRewardReady}
	case QuestTransitionAbort:
		return []QuestStatus{QuestActive, QuestProgressed, QuestRewardReady}
	default:
		return nil
	}
}
