package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const CommunityFriendLimit = 20

type CommunityFriend struct {
	CharacterID uint32
	Name        string
	RefObjectID uint32
}

type CommunityFriendRequestState struct {
	Requester CommunityFriend
	Target    CommunityFriend
}

type CommunityFriendRepository interface {
	CommunityFriends(context.Context, int) ([]CommunityFriend, error)
	PendingCommunityFriendRequests(context.Context, int) ([]CommunityFriendRequestState, error)
	RequestCommunityFriend(context.Context, int, string, time.Time) (CommunityFriendRequestState, error)
	RespondCommunityFriend(context.Context, int, int, int, bool, time.Time) (CommunityFriendRequestState, error)
	DeleteCommunityFriend(context.Context, int, int) error
}

func (r *SQLSocialRepository) PendingCommunityFriendRequests(ctx context.Context, target int) ([]CommunityFriendRequestState, error) {
	if r == nil || r.shard == nil {
		return nil, ErrSocialRepositoryUnavailable
	}
	if target <= 0 {
		return nil, ErrSocialInvalidRequest
	}
	rows, err := r.shard.QueryContext(socialContext(ctx), "SELECT requester.id,requester.CHAR_NAME,requester.REF_OBJ_ID,target.id,target.CHAR_NAME,target.REF_OBJ_ID FROM COMMUNITY_FRIEND_REQUEST request JOIN `CHAR` requester ON requester.id=request.REQUESTER_CHAR JOIN `CHAR` target ON target.id=request.TARGET_CHAR WHERE request.TARGET_CHAR=? ORDER BY request.REQUESTED_AT,request.REQUESTER_CHAR", target)
	if err != nil {
		return nil, fmt.Errorf("load pending community friend requests: %w", err)
	}
	defer rows.Close()
	requests := make([]CommunityFriendRequestState, 0)
	for rows.Next() {
		var state CommunityFriendRequestState
		if err = rows.Scan(&state.Requester.CharacterID, &state.Requester.Name, &state.Requester.RefObjectID, &state.Target.CharacterID, &state.Target.Name, &state.Target.RefObjectID); err != nil {
			return nil, fmt.Errorf("scan pending community friend request: %w", err)
		}
		requests = append(requests, state)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pending community friend requests: %w", err)
	}
	return requests, nil
}

func communityPair(left, right int) (int, int, bool) {
	if left <= 0 || right <= 0 || left == right {
		return 0, 0, false
	}
	if left < right {
		return left, right, true
	}
	return right, left, true
}

func scanCommunityFriend(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, characterID int) (friend CommunityFriend, err error) {
	err = q.QueryRowContext(ctx, "SELECT id,CHAR_NAME,REF_OBJ_ID FROM `CHAR` WHERE id=?", characterID).Scan(&friend.CharacterID, &friend.Name, &friend.RefObjectID)
	if errors.Is(err, sql.ErrNoRows) {
		return CommunityFriend{}, ErrSocialNotFound
	}
	return friend, err
}

func communityFriendCount(ctx context.Context, tx *sql.Tx, characterID int) (int, error) {
	var count int
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM COMMUNITY_FRIEND WHERE CHAR_LOW=? OR CHAR_HIGH=?", characterID, characterID).Scan(&count)
	return count, err
}

func (r *SQLSocialRepository) CommunityFriends(ctx context.Context, characterID int) ([]CommunityFriend, error) {
	if r == nil || r.shard == nil {
		return nil, ErrSocialRepositoryUnavailable
	}
	if characterID <= 0 {
		return nil, ErrSocialInvalidRequest
	}
	ctx = socialContext(ctx)
	rows, err := r.shard.QueryContext(ctx, "SELECT c.id,c.CHAR_NAME,c.REF_OBJ_ID FROM COMMUNITY_FRIEND f JOIN `CHAR` c ON c.id=IF(f.CHAR_LOW=?,f.CHAR_HIGH,f.CHAR_LOW) WHERE f.CHAR_LOW=? OR f.CHAR_HIGH=? ORDER BY c.CHAR_NAME,c.id", characterID, characterID, characterID)
	if err != nil {
		return nil, fmt.Errorf("load community friends: %w", err)
	}
	defer rows.Close()
	friends := make([]CommunityFriend, 0, RuntimeFriendLimit())
	for rows.Next() {
		var friend CommunityFriend
		if err = rows.Scan(&friend.CharacterID, &friend.Name, &friend.RefObjectID); err != nil {
			return nil, fmt.Errorf("scan community friend: %w", err)
		}
		friends = append(friends, friend)
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate community friends: %w", err)
	}
	if len(friends) > RuntimeFriendLimit() {
		return nil, ErrSocialCapacity
	}
	return friends, nil
}

func (r *SQLSocialRepository) RequestCommunityFriend(ctx context.Context, actor int, targetName string, now time.Time) (state CommunityFriendRequestState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	if actor <= 0 || now.IsZero() || validateSocialText("friend name", targetName, 64, false) != nil {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var target int
		if scanErr := tx.QueryRowContext(ctx, "SELECT id FROM `CHAR` WHERE CHAR_NAME=? FOR UPDATE", targetName).Scan(&target); errors.Is(scanErr, sql.ErrNoRows) {
			return ErrSocialNotFound
		} else if scanErr != nil {
			return scanErr
		}
		low, high, valid := communityPair(actor, target)
		if !valid {
			return ErrSocialConflict
		}
		var blocked int
		if scanErr := tx.QueryRowContext(ctx, "SELECT 1 FROM COMMUNITY_BLOCK WHERE (BLOCKER_CHAR=? AND BLOCKED_CHAR=?) OR (BLOCKER_CHAR=? AND BLOCKED_CHAR=?) LIMIT 1", actor, target, target, actor).Scan(&blocked); scanErr == nil {
			return ErrSocialAccessDenied
		} else if !errors.Is(scanErr, sql.ErrNoRows) {
			return scanErr
		}
		rows, queryErr := tx.QueryContext(ctx, "SELECT id FROM `CHAR` WHERE id IN (?,?) ORDER BY id FOR UPDATE", low, high)
		if queryErr != nil {
			return queryErr
		}
		locked := 0
		for rows.Next() {
			locked++
		}
		if closeErr := rows.Close(); closeErr != nil {
			return closeErr
		}
		if locked != 2 {
			return ErrSocialNotFound
		}
		for _, characterID := range []int{actor, target} {
			count, countErr := communityFriendCount(ctx, tx, characterID)
			if countErr != nil {
				return countErr
			}
			if count >= RuntimeFriendLimit() {
				return ErrSocialCapacity
			}
		}
		var existing int
		if scanErr := tx.QueryRowContext(ctx, "SELECT 1 FROM COMMUNITY_FRIEND WHERE CHAR_LOW=? AND CHAR_HIGH=?", low, high).Scan(&existing); scanErr == nil {
			return ErrSocialConflict
		} else if !errors.Is(scanErr, sql.ErrNoRows) {
			return scanErr
		}
		if scanErr := tx.QueryRowContext(ctx, "SELECT 1 FROM COMMUNITY_FRIEND_REQUEST WHERE CHAR_LOW=? AND CHAR_HIGH=?", low, high).Scan(&existing); scanErr == nil {
			return ErrSocialConflict
		} else if !errors.Is(scanErr, sql.ErrNoRows) {
			return scanErr
		}
		if _, execErr := tx.ExecContext(ctx, "INSERT INTO COMMUNITY_FRIEND_REQUEST (CHAR_LOW,CHAR_HIGH,REQUESTER_CHAR,TARGET_CHAR,REQUESTED_AT) VALUES (?,?,?,?,?)", low, high, actor, target, now.UTC()); execErr != nil {
			return execErr
		}
		state.Requester, queryErr = scanCommunityFriend(ctx, tx, actor)
		if queryErr == nil {
			state.Target, queryErr = scanCommunityFriend(ctx, tx, target)
		}
		return queryErr
	})
	return state, err
}

func (r *SQLSocialRepository) RespondCommunityFriend(ctx context.Context, actor, requester, target int, accepted bool, now time.Time) (state CommunityFriendRequestState, err error) {
	if r == nil || r.shard == nil {
		return state, ErrSocialRepositoryUnavailable
	}
	low, high, valid := communityPair(requester, target)
	if actor != target || !valid || now.IsZero() {
		return state, ErrSocialInvalidRequest
	}
	err = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		rows, queryErr := tx.QueryContext(ctx, "SELECT id FROM `CHAR` WHERE id IN (?,?) ORDER BY id FOR UPDATE", low, high)
		if queryErr != nil {
			return queryErr
		}
		for rows.Next() {
		}
		if closeErr := rows.Close(); closeErr != nil {
			return closeErr
		}
		result, execErr := tx.ExecContext(ctx, "DELETE FROM COMMUNITY_FRIEND_REQUEST WHERE CHAR_LOW=? AND CHAR_HIGH=? AND REQUESTER_CHAR=? AND TARGET_CHAR=?", low, high, requester, target)
		if execErr != nil {
			return execErr
		}
		if execErr = requireSocialRow(result); execErr != nil {
			return ErrSocialNotFound
		}
		if accepted {
			for _, characterID := range []int{requester, target} {
				count, countErr := communityFriendCount(ctx, tx, characterID)
				if countErr != nil {
					return countErr
				}
				if count >= RuntimeFriendLimit() {
					return ErrSocialCapacity
				}
			}
			if _, execErr = tx.ExecContext(ctx, "INSERT INTO COMMUNITY_FRIEND (CHAR_LOW,CHAR_HIGH,ACCEPTED_AT) VALUES (?,?,?)", low, high, now.UTC()); execErr != nil {
				return ErrSocialConflict
			}
		}
		state.Requester, queryErr = scanCommunityFriend(ctx, tx, requester)
		if queryErr == nil {
			state.Target, queryErr = scanCommunityFriend(ctx, tx, target)
		}
		return queryErr
	})
	return state, err
}

func (r *SQLSocialRepository) DeleteCommunityFriend(ctx context.Context, actor, friend int) error {
	if r == nil || r.shard == nil {
		return ErrSocialRepositoryUnavailable
	}
	low, high, valid := communityPair(actor, friend)
	if !valid {
		return ErrSocialInvalidRequest
	}
	result, err := r.shard.ExecContext(socialContext(ctx), "DELETE FROM COMMUNITY_FRIEND WHERE CHAR_LOW=? AND CHAR_HIGH=?", low, high)
	if err != nil {
		return fmt.Errorf("delete community friend: %w", err)
	}
	if err = requireSocialRow(result); err != nil {
		return ErrSocialNotFound
	}
	return nil
}

var _ CommunityFriendRepository = (*SQLSocialRepository)(nil)
