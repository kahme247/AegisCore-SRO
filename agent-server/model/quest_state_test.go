package model

import (
	"errors"
	"reflect"
	"testing"
)

func TestTransitionQuestConfirmedMatrix(t *testing.T) {
	tests := []struct {
		name       string
		status     QuestStatus
		transition QuestTransition
		want       QuestStatus
	}{
		{"activate inactive", QuestInactive, QuestTransitionActivate, QuestActive},
		{"reactivate aborted", QuestAborted, QuestTransitionActivate, QuestActive},
		{"first progress", QuestActive, QuestTransitionProgress, QuestProgressed},
		{"later progress", QuestProgressed, QuestTransitionProgress, QuestProgressed},
		{"active reward ready", QuestActive, QuestTransitionRewardReady, QuestRewardReady},
		{"progressed reward ready", QuestProgressed, QuestTransitionRewardReady, QuestRewardReady},
		{"close", QuestRewardReady, QuestTransitionClose, QuestClosed},
		{"abort active", QuestActive, QuestTransitionAbort, QuestAborted},
		{"abort progressed", QuestProgressed, QuestTransitionAbort, QuestAborted},
		{"abort reward ready", QuestRewardReady, QuestTransitionAbort, QuestAborted},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := TransitionQuest(test.status, test.transition)
			if err != nil || got != test.want {
				t.Fatalf("TransitionQuest(%d, %d) = %d, %v; want %d", test.status, test.transition, got, err, test.want)
			}
		})
	}
}

func TestTransitionQuestRejectsEveryUnconfirmedEdge(t *testing.T) {
	allowed := map[[2]byte]struct{}{
		{byte(QuestInactive), byte(QuestTransitionActivate)}:      {},
		{byte(QuestAborted), byte(QuestTransitionActivate)}:       {},
		{byte(QuestActive), byte(QuestTransitionProgress)}:        {},
		{byte(QuestProgressed), byte(QuestTransitionProgress)}:    {},
		{byte(QuestActive), byte(QuestTransitionRewardReady)}:     {},
		{byte(QuestProgressed), byte(QuestTransitionRewardReady)}: {},
		{byte(QuestRewardReady), byte(QuestTransitionClose)}:      {},
		{byte(QuestActive), byte(QuestTransitionAbort)}:           {},
		{byte(QuestProgressed), byte(QuestTransitionAbort)}:       {},
		{byte(QuestRewardReady), byte(QuestTransitionAbort)}:      {},
	}
	for status := QuestActive; status <= QuestRewardReady; status++ {
		for transition := QuestTransitionActivate; transition <= QuestTransitionAbort; transition++ {
			if _, ok := allowed[[2]byte{byte(status), byte(transition)}]; ok {
				continue
			}
			if _, err := TransitionQuest(status, transition); !errors.Is(err, ErrQuestStateConflict) {
				t.Fatalf("TransitionQuest(%d, %d) error = %v, want ErrQuestStateConflict", status, transition, err)
			}
		}
	}
}

func TestQuestTransitionSourcesAreExact(t *testing.T) {
	want := map[QuestTransition][]QuestStatus{
		QuestTransitionActivate:    {QuestInactive, QuestAborted},
		QuestTransitionProgress:    {QuestActive, QuestProgressed},
		QuestTransitionRewardReady: {QuestActive, QuestProgressed},
		QuestTransitionClose:       {QuestRewardReady},
		QuestTransitionAbort:       {QuestActive, QuestProgressed, QuestRewardReady},
	}
	for transition, statuses := range want {
		if got := QuestTransitionSources(transition); !reflect.DeepEqual(got, statuses) {
			t.Fatalf("QuestTransitionSources(%d) = %v, want %v", transition, got, statuses)
		}
	}
}
