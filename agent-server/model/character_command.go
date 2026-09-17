package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

type characterCommandContextKey struct{}

type CharacterCommandIdentity struct {
	Key  string
	Name string
}

func WithCharacterCommand(ctx context.Context, identity CharacterCommandIdentity) context.Context {
	if ctx == nil || identity.Key == "" || len(identity.Key) > 128 || identity.Name == "" || len(identity.Name) > 128 {
		return ctx
	}
	return context.WithValue(ctx, characterCommandContextKey{}, identity)
}

type characterCommandState struct {
	identity  CharacterCommandIdentity
	version   uint64
	duplicate bool
	result    []byte
}

func beginCharacterRosterCommand(ctx context.Context, tx *sql.Tx, userID uint32, shardID uint16) (characterCommandState, error) {
	identity, ok := ctx.Value(characterCommandContextKey{}).(CharacterCommandIdentity)
	if !ok {
		return characterCommandState{}, nil
	}
	aggregateID := fmt.Sprintf("roster:%d:%d", userID, shardID)
	if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO `AEGIS_AGGREGATE_VERSION` (`AGGREGATE_KIND`,`AGGREGATE_ID`,`VERSION`) VALUES ('character',?,0)", aggregateID); err != nil {
		return characterCommandState{}, err
	}
	var version uint64
	if err := tx.QueryRowContext(ctx, "SELECT `VERSION` FROM `AEGIS_AGGREGATE_VERSION` WHERE `AGGREGATE_KIND`='character' AND `AGGREGATE_ID`=? FOR UPDATE", aggregateID).Scan(&version); err != nil {
		return characterCommandState{}, err
	}
	var name, kind, storedID, status string
	var result []byte
	err := tx.QueryRowContext(ctx, "SELECT `COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`STATUS`,`RESULT_PAYLOAD` FROM `AEGIS_COMMAND_IDEMPOTENCY` WHERE `IDEMPOTENCY_KEY`=? FOR UPDATE", identity.Key).Scan(&name, &kind, &storedID, &status, &result)
	if err == nil {
		if name != identity.Name || kind != "character" || storedID != aggregateID || status != "completed" {
			return characterCommandState{}, ErrSocialConflict
		}
		return characterCommandState{identity: identity, version: version, duplicate: true, result: append([]byte(nil), result...)}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return characterCommandState{}, err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY` (`IDEMPOTENCY_KEY`,`COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`EXPECTED_VERSION`,`STATUS`) VALUES (?,?,'character',?,?,'in_progress')", identity.Key, identity.Name, aggregateID, version)
	return characterCommandState{identity: identity, version: version}, err
}

func completeCharacterRosterCommand(ctx context.Context, tx *sql.Tx, userID uint32, shardID uint16, state characterCommandState, topic string, payload []byte) error {
	if state.identity.Key == "" {
		return nil
	}
	aggregateID := fmt.Sprintf("roster:%d:%d", userID, shardID)
	next := state.version + 1
	result, err := tx.ExecContext(ctx, "UPDATE `AEGIS_AGGREGATE_VERSION` SET `VERSION`=? WHERE `AGGREGATE_KIND`='character' AND `AGGREGATE_ID`=? AND `VERSION`=?", next, aggregateID, state.version)
	if err != nil {
		return err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return ErrSocialConflict
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `AEGIS_OUTBOX_MESSAGE` (`MESSAGE_ID`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`AGGREGATE_VERSION`,`TOPIC`,`PAYLOAD`,`OCCURRED_AT`) VALUES (?,'character',?,?,?,?,CURRENT_TIMESTAMP(6))", state.identity.Key, aggregateID, next, topic, payload); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE `AEGIS_COMMAND_IDEMPOTENCY` SET `STATUS`='completed',`RESULT_VERSION`=?,`RESULT_PAYLOAD`=?,`COMPLETED_AT`=CURRENT_TIMESTAMP(6) WHERE `IDEMPOTENCY_KEY`=? AND `STATUS`='in_progress'", next, payload, state.identity.Key)
	return err
}
