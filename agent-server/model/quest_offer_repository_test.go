package model

import (
	"context"
	"database/sql/driver"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestQuestOfferRepositoryListsOnlyQueryQualifiedRows(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(selectAvailableQuestOffers, catalogRepositoryTestResponse{
		columns: []string{"ID", "CODE_NAME", "NAME_STRING", "PAY_STRING", "PAY_CONTENTS", "OBJECTIVE_KEY", "REQUIRED_COUNT", "TASK_COUNT", "SELECTION_COUNT"},
		rows: [][]driver.Value{
			{int64(3), "QNO_CH_SMITH_1", "SN_QNO_CH_SMITH_1", "SN_PAY_QNO_CH_SMITH_1", "SN_PAYCON_QNO_CH_SMITH_1", "SN_CON_QNO_CH_SMITH_1", int64(0), int64(0), int64(0)},
			{int64(58), "QNO_CH_SMITH_2", "SN_QNO_CH_SMITH_2", "SN_PAY_QNO_CH_SMITH_2", "SN_PAYCON_QNO_CH_SMITH_2", "SN_CON_QNO_CH_SMITH_2", int64(50), int64(1), int64(0)},
		},
	})
	repository, err := NewQuestOfferRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	offers, err := repository.ListAvailable(context.Background(), 42, 2003)
	if err != nil {
		t.Fatal(err)
	}
	want := []QuestOffer{
		{QuestID: 3, CodeName: "QNO_CH_SMITH_1", NameKey: "SN_QNO_CH_SMITH_1", PayKey: "SN_PAY_QNO_CH_SMITH_1", PayContentsKey: "SN_PAYCON_QNO_CH_SMITH_1", ObjectiveKey: "SN_CON_QNO_CH_SMITH_1"},
		{QuestID: 58, CodeName: "QNO_CH_SMITH_2", NameKey: "SN_QNO_CH_SMITH_2", PayKey: "SN_PAY_QNO_CH_SMITH_2", PayContentsKey: "SN_PAYCON_QNO_CH_SMITH_2", ObjectiveKey: "SN_CON_QNO_CH_SMITH_2", RequiredCount: 50, TaskCount: 1},
	}
	if !reflect.DeepEqual(offers, want) {
		t.Fatalf("offers = %#v, want %#v", offers, want)
	}
	catalogRepositoryTestState.Lock()
	defer catalogRepositoryTestState.Unlock()
	if len(catalogRepositoryTestState.queries) != 1 || len(catalogRepositoryTestState.queries[0].args) != 2 ||
		catalogRepositoryTestState.queries[0].query != selectAvailableQuestOffers ||
		catalogRepositoryTestState.queries[0].args[0].Value != int64(42) || catalogRepositoryTestState.queries[0].args[1].Value != int64(2003) {
		t.Fatalf("queries = %#v", catalogRepositoryTestState.queries)
	}
}

func TestQuestOfferQueryLetsAbandonedAndActiveQuestsBypassInitialLevelWindow(t *testing.T) {
	for _, required := range []string{
		"current_quest.STATUS IN (1,6,7,8)",
		"current_quest.STATUS IN (1,4,6,7,8)",
		"character_state.CURRENT_LEVEL BETWEEN start_condition.LEVEL_LOWER AND start_condition.LEVEL_UPPER",
		"current_quest.STATUS IN (1,6,7,8) OR policy.ACHIEVEMENT_LIMIT IS NULL",
		"dependency_state.STATUS = 5",
	} {
		if !strings.Contains(selectAvailableQuestOffers, required) {
			t.Fatalf("offer query is missing %q", required)
		}
	}
}

func TestQuestOfferRepositoryFailsClosedOnInvalidProtocolMetadata(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(selectAvailableQuestOffers, catalogRepositoryTestResponse{
		columns: []string{"ID", "CODE_NAME", "NAME_STRING", "PAY_STRING", "PAY_CONTENTS", "OBJECTIVE_KEY", "REQUIRED_COUNT", "TASK_COUNT", "SELECTION_COUNT"},
		rows:    [][]driver.Value{{int64(99), "QUEST", "NAME", "PAY", "CONTENTS", "", int64(1), int64(2)}},
	})
	repository, err := NewQuestOfferRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.ListAvailable(context.Background(), 1, 2); err == nil {
		t.Fatal("invalid protocol metadata was exposed")
	}
}

func TestQuestOfferRepositoryValidatesDependencies(t *testing.T) {
	if _, err := NewQuestOfferRepository(nil); !errors.Is(err, ErrQuestOfferRepositoryUnavailable) {
		t.Fatalf("constructor error = %v", err)
	}
	repository, err := NewQuestOfferRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.ListAvailable(context.Background(), 0, 1); err == nil {
		t.Fatal("zero character ID was accepted")
	}
}
