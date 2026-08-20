package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type AccountRecord struct {
	ID         int        `json:"id"`
	Username   string     `json:"username"`
	Email      string     `json:"email"`
	Status     int        `json:"status"`
	GameMaster bool       `json:"gameMaster"`
	Silk       uint64     `json:"silk"`
	GiftSilk   uint64     `json:"giftSilk"`
	SilkPoint  uint64     `json:"silkPoint"`
	CreatedAt  *time.Time `json:"createdAt,omitempty"`
	UpdatedAt  *time.Time `json:"updatedAt,omitempty"`
}

type CharacterRecord struct {
	ID          int       `json:"id"`
	AccountID   int       `json:"accountId"`
	Username    string    `json:"username"`
	Name        string    `json:"name"`
	ShardID     int       `json:"shardId"`
	Level       int       `json:"level"`
	Gold        uint64    `json:"gold"`
	RegionID    int       `json:"regionId"`
	PositionX   float64   `json:"positionX"`
	PositionY   float64   `json:"positionY"`
	PositionZ   float64   `json:"positionZ"`
	HasPosition bool      `json:"hasPosition"`
	Deleting    bool      `json:"deleting"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type DirectorySource interface {
	SearchAccounts(context.Context, string, int) ([]AccountRecord, error)
	SearchCharacters(context.Context, string, int) ([]CharacterRecord, error)
}

type DirectoryRepository struct {
	account *sql.DB
	shard   *sql.DB
}

func NewDirectoryRepository(account, shard *sql.DB) (*DirectoryRepository, error) {
	if account == nil || shard == nil {
		return nil, fmt.Errorf("SMC directory requires account and shard database pools")
	}
	return &DirectoryRepository{account: account, shard: shard}, nil
}

func (r *DirectoryRepository) SearchAccounts(ctx context.Context, query string, limit int) ([]AccountRecord, error) {
	const searchAccountsQuery = `
		SELECT id, USERNAME, COALESCE(MAIL, ''), COALESCE(STATUS, 0), COALESCE(IS_GM, 0),
		       SILK, GIFT_SILK, SILK_POINT, CTIME, UTIME
		FROM ` + "`USER`" + `
		WHERE LEFT(USERNAME_NORMALIZED, CHAR_LENGTH(?)) = LOWER(TRIM(?))
		ORDER BY USERNAME_NORMALIZED, id
		LIMIT ?`
	rows, err := r.account.QueryContext(ctx, searchAccountsQuery, query, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search SMC accounts: %w", err)
	}
	defer rows.Close()
	result := make([]AccountRecord, 0)
	for rows.Next() {
		var item AccountRecord
		var gameMaster int
		var createdAt, updatedAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.Username, &item.Email, &item.Status, &gameMaster,
			&item.Silk, &item.GiftSilk, &item.SilkPoint, &createdAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan SMC account: %w", err)
		}
		item.GameMaster = gameMaster == 1
		if createdAt.Valid {
			value := createdAt.Time.UTC()
			item.CreatedAt = &value
		}
		if updatedAt.Valid {
			value := updatedAt.Time.UTC()
			item.UpdatedAt = &value
		}
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SMC accounts: %w", err)
	}
	return result, nil
}

func (r *DirectoryRepository) SearchCharacters(ctx context.Context, query string, limit int) ([]CharacterRecord, error) {
	const searchCharactersQuery = `
		SELECT c.id, c.FK_USER, u.USERNAME, c.CHAR_NAME, c.FK_SHARD,
		       c.CURRENT_LEVEL, c.GOLD, c.FK_REGION,
		       COALESCE(c.POS_X, 0), COALESCE(c.POS_Y, 0), COALESCE(c.POS_Z, 0),
		       CASE WHEN c.POS_X IS NULL OR c.POS_Y IS NULL OR c.POS_Z IS NULL THEN 0 ELSE 1 END,
		       c.DELETING, c.UTIME
		FROM ` + "`CHAR`" + ` c
		JOIN SRO_ACCOUNT.` + "`USER`" + ` u ON u.id = c.FK_USER
		WHERE LEFT(c.CHAR_NAME_NORMALIZED, CHAR_LENGTH(?)) = LOWER(TRIM(?))
		   OR LEFT(u.USERNAME_NORMALIZED, CHAR_LENGTH(?)) = LOWER(TRIM(?))
		ORDER BY c.CHAR_NAME_NORMALIZED, c.id
		LIMIT ?`
	rows, err := r.shard.QueryContext(ctx, searchCharactersQuery, query, query, query, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search SMC characters: %w", err)
	}
	defer rows.Close()
	result := make([]CharacterRecord, 0)
	for rows.Next() {
		var item CharacterRecord
		var hasPosition, deleting int
		if err := rows.Scan(
			&item.ID, &item.AccountID, &item.Username, &item.Name, &item.ShardID,
			&item.Level, &item.Gold, &item.RegionID,
			&item.PositionX, &item.PositionY, &item.PositionZ,
			&hasPosition, &deleting, &item.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan SMC character: %w", err)
		}
		item.HasPosition = hasPosition == 1
		item.Deleting = deleting == 1
		item.UpdatedAt = item.UpdatedAt.UTC()
		result = append(result, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate SMC characters: %w", err)
	}
	return result, nil
}
