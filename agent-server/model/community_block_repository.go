package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const CommunityBlockLimit = 20

type CommunityBlockRepository interface {
	UpdateCommunityBlock(context.Context, int, string, byte, time.Time) (CommunityFriend, error)
	CommunityBlocked(context.Context, int, int) (bool, error)
}

func (r *SQLSocialRepository) UpdateCommunityBlock(ctx context.Context, actor int, targetName string, action byte, now time.Time) (blocked CommunityFriend, err error) {
	if r == nil || r.shard == nil {
		return blocked, ErrSocialRepositoryUnavailable
	}
	if actor <= 0 || now.IsZero() || (action != 1 && action != 2) || validateSocialText("blocked character", targetName, 64, false) != nil {
		return blocked, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if scanErr := tx.QueryRowContext(ctx, "SELECT id,CHAR_NAME,REF_OBJ_ID FROM `CHAR` WHERE CHAR_NAME=? FOR UPDATE", targetName).Scan(&blocked.CharacterID, &blocked.Name, &blocked.RefObjectID); errors.Is(scanErr, sql.ErrNoRows) {
			return ErrSocialNotFound
		} else if scanErr != nil {
			return scanErr
		}
		if int(blocked.CharacterID) == actor {
			return ErrSocialConflict
		}
		if action == 1 {
			var count int
			if countErr := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM COMMUNITY_BLOCK WHERE BLOCKER_CHAR=?", actor).Scan(&count); countErr != nil {
				return countErr
			}
			if count >= RuntimeBlockLimit() {
				return ErrSocialCapacity
			}
			var exists int
			if scanErr := tx.QueryRowContext(ctx, "SELECT 1 FROM COMMUNITY_BLOCK WHERE BLOCKER_CHAR=? AND BLOCKED_CHAR=?", actor, blocked.CharacterID).Scan(&exists); scanErr == nil {
				return ErrSocialConflict
			} else if !errors.Is(scanErr, sql.ErrNoRows) {
				return scanErr
			}
			if _, execErr := tx.ExecContext(ctx, "INSERT INTO COMMUNITY_BLOCK (BLOCKER_CHAR,BLOCKED_CHAR,CREATED_AT) VALUES (?,?,?)", actor, blocked.CharacterID, now.UTC()); execErr != nil {
				return execErr
			}
			_, execErr := tx.ExecContext(ctx, "DELETE FROM COMMUNITY_FRIEND_REQUEST WHERE CHAR_LOW=LEAST(?,?) AND CHAR_HIGH=GREATEST(?,?)", actor, blocked.CharacterID, actor, blocked.CharacterID)
			return execErr
		}
		result, execErr := tx.ExecContext(ctx, "DELETE FROM COMMUNITY_BLOCK WHERE BLOCKER_CHAR=? AND BLOCKED_CHAR=?", actor, blocked.CharacterID)
		if execErr != nil {
			return execErr
		}
		if execErr = requireSocialRow(result); execErr != nil {
			return ErrSocialNotFound
		}
		return nil
	})
	return blocked, err
}

func (r *SQLSocialRepository) CommunityBlocked(ctx context.Context, left, right int) (bool, error) {
	if r == nil || r.shard == nil {
		return false, ErrSocialRepositoryUnavailable
	}
	if left <= 0 || right <= 0 || left == right {
		return false, ErrSocialInvalidRequest
	}
	var exists int
	err := r.shard.QueryRowContext(socialContext(ctx), "SELECT 1 FROM COMMUNITY_BLOCK WHERE (BLOCKER_CHAR=? AND BLOCKED_CHAR=?) OR (BLOCKER_CHAR=? AND BLOCKED_CHAR=?) LIMIT 1", left, right, right, left).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check community block: %w", err)
	}
	return true, nil
}

var _ CommunityBlockRepository = (*SQLSocialRepository)(nil)
