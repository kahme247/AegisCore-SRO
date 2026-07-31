package model

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"testing"
)

type accountModerationExecerFake struct {
	query string
	args  []any
	rows  int64
	err   error
}

func (e *accountModerationExecerFake) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	e.query = query
	e.args = append([]any(nil), args...)
	return driver.RowsAffected(e.rows), e.err
}

func TestAccountModerationBanBlocksActiveAccount(t *testing.T) {
	store := &accountModerationExecerFake{rows: 1}
	repository := &SQLAccountModerationRepository{account: store}
	if err := repository.BanAccount(context.Background(), 77); err != nil {
		t.Fatal(err)
	}
	if store.query != "UPDATE `SRO_ACCOUNT`.`USER` SET `STATUS`=0,`UTIME`=CURRENT_TIMESTAMP WHERE `ID`=? AND `STATUS`<>0" {
		t.Fatalf("query = %q", store.query)
	}
	if len(store.args) != 1 || store.args[0] != uint32(77) {
		t.Fatalf("args = %#v", store.args)
	}
}

func TestAccountModerationBanRequiresOneActiveAccount(t *testing.T) {
	for _, test := range []struct {
		name  string
		store *accountModerationExecerFake
	}{
		{name: "already blocked", store: &accountModerationExecerFake{}},
		{name: "database failure", store: &accountModerationExecerFake{err: errors.New("database unavailable")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			repository := &SQLAccountModerationRepository{account: test.store}
			if err := repository.BanAccount(context.Background(), 77); err == nil {
				t.Fatal("BanAccount error = nil")
			}
		})
	}
}
