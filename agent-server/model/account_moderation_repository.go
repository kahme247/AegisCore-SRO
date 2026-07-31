package model

import (
	"context"
	"database/sql"
	"fmt"
)

// AccountModerationRepository persists login access decisions owned by the
// account database. STATUS=0 is the blocked state enforced by Gateway.
type AccountModerationRepository interface {
	BanAccount(context.Context, uint32) error
}

type accountModerationExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

type SQLAccountModerationRepository struct {
	account accountModerationExecer
}

func NewAccountModerationRepository(account *sql.DB) (*SQLAccountModerationRepository, error) {
	if account == nil {
		return nil, fmt.Errorf("account moderation repository requires an account database")
	}
	return &SQLAccountModerationRepository{account: account}, nil
}

func (r *SQLAccountModerationRepository) BanAccount(ctx context.Context, accountID uint32) error {
	if r == nil || r.account == nil || accountID == 0 {
		return fmt.Errorf("invalid account moderation request")
	}
	result, err := r.account.ExecContext(ctx, "UPDATE `SRO_ACCOUNT`.`USER` SET `STATUS`=0,`UTIME`=CURRENT_TIMESTAMP WHERE `ID`=? AND `STATUS`<>0", accountID)
	if err != nil {
		return fmt.Errorf("ban account %d: %w", accountID, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("read account ban result: %w", err)
	}
	if affected != 1 {
		return fmt.Errorf("account %d was not active", accountID)
	}
	return nil
}
