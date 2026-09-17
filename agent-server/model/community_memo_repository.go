package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const CommunityMemoLimit = 20

type CommunityMemo struct {
	RecipientID uint32
	SenderName  string
	RefObjectID uint32
	Message     string
	SentAt      time.Time
	Status      byte
}

type CommunityMemoRepository interface {
	CommunityMemos(context.Context, int) ([]CommunityMemo, error)
	OpenCommunityMemo(context.Context, int, byte) (CommunityMemo, error)
	SendCommunityMemo(context.Context, int, string, string, time.Time) (CommunityMemo, error)
	DeleteCommunityMemo(context.Context, int, byte) error
}

func (r *SQLSocialRepository) CommunityMemos(ctx context.Context, recipient int) ([]CommunityMemo, error) {
	if r == nil || r.shard == nil {
		return nil, ErrSocialRepositoryUnavailable
	}
	if recipient <= 0 {
		return nil, ErrSocialInvalidRequest
	}
	limit := RuntimeMemoLimit()
	rows, err := r.shard.QueryContext(socialContext(ctx), "SELECT RECIPIENT_CHAR,SENDER_NAME,SENDER_REF_OBJECT_ID,MESSAGE,SENT_AT,STATUS FROM COMMUNITY_MEMO WHERE RECIPIENT_CHAR=? ORDER BY SENT_AT,ID LIMIT ?", recipient, limit+1)
	if err != nil {
		return nil, fmt.Errorf("load community memos: %w", err)
	}
	defer rows.Close()
	memos := make([]CommunityMemo, 0, limit)
	for rows.Next() {
		var memo CommunityMemo
		if err = rows.Scan(&memo.RecipientID, &memo.SenderName, &memo.RefObjectID, &memo.Message, &memo.SentAt, &memo.Status); err != nil {
			return nil, fmt.Errorf("scan community memo: %w", err)
		}
		memo.SentAt = memo.SentAt.UTC()
		memos = append(memos, memo)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate community memos: %w", err)
	}
	if len(memos) > limit {
		return nil, ErrSocialCapacity
	}
	return memos, nil
}

func (r *SQLSocialRepository) OpenCommunityMemo(ctx context.Context, recipient int, slot byte) (memo CommunityMemo, err error) {
	if r == nil || r.shard == nil {
		return memo, ErrSocialRepositoryUnavailable
	}
	if recipient <= 0 || int(slot) >= RuntimeMemoLimit() {
		return memo, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var id uint64
		scanErr := tx.QueryRowContext(ctx, "SELECT ID,RECIPIENT_CHAR,SENDER_NAME,SENDER_REF_OBJECT_ID,MESSAGE,SENT_AT,STATUS FROM COMMUNITY_MEMO WHERE RECIPIENT_CHAR=? ORDER BY SENT_AT,ID LIMIT 1 OFFSET ? FOR UPDATE", recipient, slot).Scan(&id, &memo.RecipientID, &memo.SenderName, &memo.RefObjectID, &memo.Message, &memo.SentAt, &memo.Status)
		if errors.Is(scanErr, sql.ErrNoRows) {
			return ErrSocialNotFound
		}
		if scanErr != nil {
			return scanErr
		}
		memo.SentAt = memo.SentAt.UTC()
		if memo.Status == 0 {
			if _, execErr := tx.ExecContext(ctx, "UPDATE COMMUNITY_MEMO SET STATUS=1 WHERE ID=? AND RECIPIENT_CHAR=?", id, recipient); execErr != nil {
				return execErr
			}
			memo.Status = 1
		}
		return nil
	})
	return memo, err
}

func (r *SQLSocialRepository) SendCommunityMemo(ctx context.Context, sender int, targetName, message string, now time.Time) (memo CommunityMemo, err error) {
	if r == nil || r.shard == nil {
		return memo, ErrSocialRepositoryUnavailable
	}
	if sender <= 0 || now.IsZero() || validateSocialText("memo recipient", targetName, 64, false) != nil || validateSocialText("memo message", message, 1000, false) != nil {
		return memo, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var senderName string
		var senderRef uint32
		if scanErr := tx.QueryRowContext(ctx, "SELECT CHAR_NAME,REF_OBJ_ID FROM `CHAR` WHERE id=? FOR UPDATE", sender).Scan(&senderName, &senderRef); errors.Is(scanErr, sql.ErrNoRows) {
			return ErrSocialNotFound
		} else if scanErr != nil {
			return scanErr
		}
		var recipient int
		if scanErr := tx.QueryRowContext(ctx, "SELECT id FROM `CHAR` WHERE CHAR_NAME=? FOR UPDATE", targetName).Scan(&recipient); errors.Is(scanErr, sql.ErrNoRows) {
			return ErrSocialNotFound
		} else if scanErr != nil {
			return scanErr
		}
		if recipient == sender {
			return ErrSocialConflict
		}
		var blocked int
		if scanErr := tx.QueryRowContext(ctx, "SELECT 1 FROM COMMUNITY_BLOCK WHERE (BLOCKER_CHAR=? AND BLOCKED_CHAR=?) OR (BLOCKER_CHAR=? AND BLOCKED_CHAR=?) LIMIT 1", sender, recipient, recipient, sender).Scan(&blocked); scanErr == nil {
			return ErrSocialAccessDenied
		} else if !errors.Is(scanErr, sql.ErrNoRows) {
			return scanErr
		}
		var count int
		if countErr := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM COMMUNITY_MEMO WHERE RECIPIENT_CHAR=?", recipient).Scan(&count); countErr != nil {
			return countErr
		}
		if count >= RuntimeMemoLimit() {
			return ErrSocialCapacity
		}
		if _, execErr := tx.ExecContext(ctx, "INSERT INTO COMMUNITY_MEMO (RECIPIENT_CHAR,SENDER_CHAR,SENDER_NAME,SENDER_REF_OBJECT_ID,MESSAGE,SENT_AT,STATUS) VALUES (?,?,?,?,?,?,0)", recipient, sender, senderName, senderRef, message, now.UTC()); execErr != nil {
			return execErr
		}
		memo = CommunityMemo{RecipientID: uint32(recipient), SenderName: senderName, RefObjectID: senderRef, Message: message, SentAt: now.UTC(), Status: 0}
		return nil
	})
	return memo, err
}

func (r *SQLSocialRepository) DeleteCommunityMemo(ctx context.Context, recipient int, slot byte) (err error) {
	if r == nil || r.shard == nil {
		return ErrSocialRepositoryUnavailable
	}
	if recipient <= 0 || int(slot) >= RuntimeMemoLimit() {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var id uint64
		if scanErr := tx.QueryRowContext(ctx, "SELECT ID FROM COMMUNITY_MEMO WHERE RECIPIENT_CHAR=? ORDER BY SENT_AT,ID LIMIT 1 OFFSET ? FOR UPDATE", recipient, slot).Scan(&id); errors.Is(scanErr, sql.ErrNoRows) {
			return ErrSocialNotFound
		} else if scanErr != nil {
			return scanErr
		}
		result, execErr := tx.ExecContext(ctx, "DELETE FROM COMMUNITY_MEMO WHERE ID=? AND RECIPIENT_CHAR=?", id, recipient)
		if execErr != nil {
			return execErr
		}
		if execErr = requireSocialRow(result); execErr != nil {
			return ErrSocialNotFound
		}
		return nil
	})
}

var _ CommunityMemoRepository = (*SQLSocialRepository)(nil)
