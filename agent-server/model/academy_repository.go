package model

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"time"
)

type AcademyMember struct {
	CharacterID           int
	Name                  string
	Class                 byte
	Level                 byte
	RefObjectID           uint32
	Online                bool
	MentorID              int
	MentorName            string
	AccumulatedExperience uint64
	JoinedAt              time.Time
}

type AcademyBuffState struct {
	AcademyID   int
	CharacterID int
	Type        byte
	Value       uint64
	StartsAt    time.Time
	EndsAt      time.Time
}

type AcademyGraduation struct {
	AcademyID                int
	ApprenticeCharacter      int
	LeaderCharacter          int
	AccumulatedExperience    uint64
	LeaderExperienceAward    uint64
	AssistantExperienceAward uint64
	HonorAward               uint32
	GraduatedAt              time.Time
}

type AcademySnapshot struct {
	ID              int
	LeaderCharacter int
	CommentTitle    string
	CommentBody     string
	GraduatedCount  uint32
	HonorPoints     uint32
	HonorRank       byte
	Members         []AcademyMember
}

type AcademyMatch struct {
	AcademyID       int
	LeaderCharacter int
	CountryType     byte
	Title           string
	DisplayLevel    byte
	LeaderLevel     byte
	LeaderName      string
	GraduatedCount  uint32
	HonorRank       byte
}

type AcademyRepository interface {
	Academy(context.Context, int) (AcademySnapshot, error)
	CreateAcademy(context.Context, int) (AcademySnapshot, uint64, error)
	DisbandAcademy(context.Context, int) error
	JoinAcademy(context.Context, int, int) (AcademySnapshot, error)
	LeaveAcademy(context.Context, int) error
	KickAcademyMember(context.Context, int, int) error
	GraduateAcademyMember(context.Context, int, int, string) (AcademyGraduation, error)
	AccumulateAcademyExperience(context.Context, int, uint64) ([]AcademyBuffState, error)
	AcademyBuffs(context.Context, int, time.Time) ([]AcademyBuffState, error)
	UpdateAcademyComment(context.Context, int, string, string) error
	RegisterAcademyMatch(context.Context, int, string, byte, byte) error
	DeleteAcademyMatch(context.Context, int) error
	ListAcademyMatches(context.Context, byte, int) ([]AcademyMatch, int, error)
	AcademyHonorRanking(context.Context, int) ([]AcademyMatch, error)
}

func scanAcademy(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, characterID int) (AcademySnapshot, error) {
	var a AcademySnapshot
	err := q.QueryRowContext(ctx, "SELECT a.ID,a.LEADER_CHAR,a.COMMENT_TITLE,a.COMMENT_BODY,a.GRADUATED_COUNT,a.HONOR_POINTS,a.HONOR_RANK FROM ACADEMY a JOIN ACADEMY_MEMBER am ON am.FK_ACADEMY=a.ID WHERE am.FK_CHAR=?", characterID).Scan(&a.ID, &a.LeaderCharacter, &a.CommentTitle, &a.CommentBody, &a.GraduatedCount, &a.HonorPoints, &a.HonorRank)
	if errors.Is(err, sql.ErrNoRows) {
		return AcademySnapshot{}, ErrSocialNotFound
	}
	if err != nil {
		return AcademySnapshot{}, err
	}
	rows, err := q.QueryContext(ctx, "SELECT c.id,c.CHAR_NAME,am.MEMBER_CLASS,c.CURRENT_LEVEL,c.REF_OBJ_ID,1,COALESCE(am.MENTOR_CHAR,0),COALESCE(mentor.CHAR_NAME,''),am.ACCUMULATED_EXP,am.JOINED_AT FROM ACADEMY_MEMBER am JOIN `CHAR` c ON c.id=am.FK_CHAR LEFT JOIN `CHAR` mentor ON mentor.id=am.MENTOR_CHAR WHERE am.FK_ACADEMY=? ORDER BY am.MEMBER_CLASS,c.CHAR_NAME", a.ID)
	if err != nil {
		return AcademySnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var m AcademyMember
		if err = rows.Scan(&m.CharacterID, &m.Name, &m.Class, &m.Level, &m.RefObjectID, &m.Online, &m.MentorID, &m.MentorName, &m.AccumulatedExperience, &m.JoinedAt); err != nil {
			return AcademySnapshot{}, err
		}
		a.Members = append(a.Members, m)
	}
	return a, rows.Err()
}

func (r *SQLSocialRepository) Academy(ctx context.Context, characterID int) (AcademySnapshot, error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return AcademySnapshot{}, ErrSocialInvalidRequest
	}
	return scanAcademy(socialContext(ctx), r.shard, characterID)
}

func (r *SQLSocialRepository) CreateAcademy(ctx context.Context, characterID int) (AcademySnapshot, uint64, error) {
	cost := RuntimeAcademyCreationCost()
	var snapshot AcademySnapshot
	var gold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var level int
		if err := tx.QueryRowContext(ctx, "SELECT CURRENT_LEVEL,GOLD FROM `CHAR` WHERE id=? FOR UPDATE", characterID).Scan(&level, &gold); err != nil {
			return ErrSocialNotFound
		}
		if level < RuntimeAcademyMinimumLevel() {
			return ErrSocialInsufficientLevel
		}
		if gold < cost {
			return ErrSocialInsufficientGold
		}
		created, err := tx.ExecContext(ctx, "INSERT INTO ACADEMY (LEADER_CHAR) VALUES (?)", characterID)
		if err != nil {
			return errors.Join(ErrSocialConflict, err)
		}
		academyID, err := created.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO ACADEMY_MEMBER (FK_ACADEMY,FK_CHAR,MEMBER_CLASS) VALUES (?,?,0)", academyID, characterID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE id=?", cost, characterID); err != nil {
			return err
		}
		gold -= cost
		snapshot, err = scanAcademy(ctx, tx, characterID)
		return err
	})
	return snapshot, gold, err
}

func (r *SQLSocialRepository) DisbandAcademy(ctx context.Context, actor int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE FROM ACADEMY WHERE LEADER_CHAR=?", actor)
		if err != nil {
			return err
		}
		return requireSocialRow(result)
	})
}

func (r *SQLSocialRepository) JoinAcademy(ctx context.Context, characterID, academyID int) (AcademySnapshot, error) {
	var snapshot AcademySnapshot
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var leader int
		if err := tx.QueryRowContext(ctx, "SELECT LEADER_CHAR FROM ACADEMY WHERE ID=? FOR UPDATE", academyID).Scan(&leader); err != nil {
			return ErrSocialNotFound
		}
		var count, level int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM ACADEMY_MEMBER WHERE FK_ACADEMY=?", academyID).Scan(&count); err != nil {
			return err
		}
		if count >= 8 {
			return ErrSocialCapacity
		}
		if err := tx.QueryRowContext(ctx, "SELECT CURRENT_LEVEL FROM `CHAR` WHERE id=? FOR UPDATE", characterID).Scan(&level); err != nil {
			return ErrSocialNotFound
		}
		if level >= 40 {
			return ErrSocialInsufficientLevel
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO ACADEMY_MEMBER (FK_ACADEMY,FK_CHAR,MEMBER_CLASS,MENTOR_CHAR) VALUES (?,?,2,?)", academyID, characterID, leader); err != nil {
			return errors.Join(ErrSocialConflict, err)
		}
		var err error
		snapshot, err = scanAcademy(ctx, tx, characterID)
		return err
	})
	return snapshot, err
}

func (r *SQLSocialRepository) LeaveAcademy(ctx context.Context, characterID int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE am FROM ACADEMY_MEMBER am JOIN ACADEMY a ON a.ID=am.FK_ACADEMY WHERE am.FK_CHAR=? AND a.LEADER_CHAR<>?", characterID, characterID)
		if err != nil {
			return err
		}
		return requireSocialRow(result)
	})
}

func (r *SQLSocialRepository) KickAcademyMember(ctx context.Context, actor, target int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "DELETE am FROM ACADEMY_MEMBER am JOIN ACADEMY a ON a.ID=am.FK_ACADEMY WHERE a.LEADER_CHAR=? AND am.FK_CHAR=? AND am.FK_CHAR<>a.LEADER_CHAR", actor, target)
		if err != nil {
			return err
		}
		return requireSocialRow(result)
	})
}

func (r *SQLSocialRepository) GraduateAcademyMember(ctx context.Context, actor, target int, commandKey string) (graduation AcademyGraduation, resultErr error) {
	if actor <= 0 || target <= 0 || actor == target || !validSocialCommandKey(commandKey) {
		return graduation, ErrSocialInvalidRequest
	}
	resultErr = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if existing, found := academyGraduationByCommand(ctx, tx, commandKey); found {
			graduation = existing
			return nil
		}
		var level int
		var class byte
		if err := tx.QueryRowContext(ctx, "SELECT a.ID,a.LEADER_CHAR,c.CURRENT_LEVEL,am.MEMBER_CLASS,am.ACCUMULATED_EXP FROM ACADEMY a JOIN ACADEMY_MEMBER am ON am.FK_ACADEMY=a.ID JOIN `CHAR` c ON c.id=am.FK_CHAR WHERE a.LEADER_CHAR=? AND am.FK_CHAR=? FOR UPDATE", actor, target).Scan(&graduation.AcademyID, &graduation.LeaderCharacter, &level, &class, &graduation.AccumulatedExperience); err != nil {
			return ErrSocialAccessDenied
		}
		if level < 40 {
			return ErrSocialInsufficientLevel
		}
		if class != 2 {
			return ErrSocialAccessDenied
		}
		graduation.ApprenticeCharacter = target
		graduation.LeaderExperienceAward = graduation.AccumulatedExperience
		graduation.AssistantExperienceAward = graduation.AccumulatedExperience / 10
		graduation.HonorAward = 1
		graduation.GraduatedAt = time.Now().UTC()
		if _, err := tx.ExecContext(ctx, "UPDATE ACADEMY_MEMBER SET ACCUMULATED_EXP=ACCUMULATED_EXP+? WHERE FK_ACADEMY=? AND FK_CHAR=? AND MEMBER_CLASS=0", graduation.LeaderExperienceAward, graduation.AcademyID, actor); err != nil {
			return err
		}
		if graduation.AssistantExperienceAward > 0 {
			if _, err := tx.ExecContext(ctx, "UPDATE ACADEMY_MEMBER SET ACCUMULATED_EXP=ACCUMULATED_EXP+? WHERE FK_ACADEMY=? AND MEMBER_CLASS=1", graduation.AssistantExperienceAward, graduation.AcademyID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM ACADEMY_MENTOR_BUFF WHERE ACADEMY_ID=? AND CHAR_ID=?", graduation.AcademyID, target); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM ACADEMY_MEMBER WHERE FK_ACADEMY=? AND FK_CHAR=?", graduation.AcademyID, target)
		if err != nil || requireSocialRow(result) != nil {
			return ErrSocialConflict
		}
		if _, err = tx.ExecContext(ctx, "UPDATE ACADEMY SET GRADUATED_COUNT=GRADUATED_COUNT+1,HONOR_POINTS=HONOR_POINTS+? WHERE ID=?", graduation.HonorAward, graduation.AcademyID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO ACADEMY_GRADUATION (ACADEMY_ID,APPRENTICE_CHAR,LEADER_CHAR,ACCUMULATED_EXP,LEADER_EXP_AWARD,ASSISTANT_EXP_AWARD,HONOR_AWARD,COMMAND_KEY,GRADUATED_AT) VALUES (?,?,?,?,?,?,?,?,?)", graduation.AcademyID, target, actor, graduation.AccumulatedExperience, graduation.LeaderExperienceAward, graduation.AssistantExperienceAward, graduation.HonorAward, commandKey, graduation.GraduatedAt)
		return err
	})
	return graduation, resultErr
}

func academyGraduationByCommand(ctx context.Context, tx *sql.Tx, commandKey string) (AcademyGraduation, bool) {
	var graduation AcademyGraduation
	err := tx.QueryRowContext(ctx, "SELECT ACADEMY_ID,APPRENTICE_CHAR,LEADER_CHAR,ACCUMULATED_EXP,LEADER_EXP_AWARD,ASSISTANT_EXP_AWARD,HONOR_AWARD,GRADUATED_AT FROM ACADEMY_GRADUATION WHERE COMMAND_KEY=? FOR UPDATE", commandKey).Scan(&graduation.AcademyID, &graduation.ApprenticeCharacter, &graduation.LeaderCharacter, &graduation.AccumulatedExperience, &graduation.LeaderExperienceAward, &graduation.AssistantExperienceAward, &graduation.HonorAward, &graduation.GraduatedAt)
	return graduation, err == nil
}

func (r *SQLSocialRepository) AccumulateAcademyExperience(ctx context.Context, characterID int, gained uint64) (states []AcademyBuffState, resultErr error) {
	if characterID <= 0 || gained == 0 {
		return nil, ErrSocialInvalidRequest
	}
	resultErr = r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var academyID, leader, mentor, level int
		var class byte
		var accumulated uint64
		if err := tx.QueryRowContext(ctx, "SELECT am.FK_ACADEMY,a.LEADER_CHAR,COALESCE(am.MENTOR_CHAR,a.LEADER_CHAR),am.MEMBER_CLASS,c.CURRENT_LEVEL,am.ACCUMULATED_EXP FROM ACADEMY_MEMBER am JOIN ACADEMY a ON a.ID=am.FK_ACADEMY JOIN `CHAR` c ON c.id=am.FK_CHAR WHERE am.FK_CHAR=? FOR UPDATE", characterID).Scan(&academyID, &leader, &mentor, &class, &level, &accumulated); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return err
		}
		if class != 2 || level >= 40 {
			return nil
		}
		award := gained / 10
		if award == 0 {
			award = 1
		}
		if accumulated > math.MaxUint64-award {
			return ErrSocialConflict
		}
		accumulated += award
		if _, err := tx.ExecContext(ctx, "UPDATE ACADEMY_MEMBER SET ACCUMULATED_EXP=? WHERE FK_ACADEMY=? AND FK_CHAR=?", accumulated, academyID, characterID); err != nil {
			return err
		}
		now := time.Now().UTC()
		ends := now.Add(5 * time.Minute)
		for _, state := range []AcademyBuffState{{AcademyID: academyID, CharacterID: characterID, Type: 0, Value: accumulated, StartsAt: now, EndsAt: ends}, {AcademyID: academyID, CharacterID: mentor, Type: 1, Value: accumulated, StartsAt: now, EndsAt: ends}} {
			if _, err := tx.ExecContext(ctx, "INSERT INTO ACADEMY_MENTOR_BUFF (ACADEMY_ID,CHAR_ID,BUFF_TYPE,VALUE,STARTS_AT,ENDS_AT) VALUES (?,?,?,?,?,?) ON DUPLICATE KEY UPDATE VALUE=VALUES(VALUE),STARTS_AT=VALUES(STARTS_AT),ENDS_AT=VALUES(ENDS_AT)", state.AcademyID, state.CharacterID, state.Type, state.Value, state.StartsAt, state.EndsAt); err != nil {
				return err
			}
			states = append(states, state)
		}
		_ = leader
		return nil
	})
	return states, resultErr
}

func (r *SQLSocialRepository) AcademyBuffs(ctx context.Context, characterID int, now time.Time) ([]AcademyBuffState, error) {
	if r == nil || r.shard == nil || characterID <= 0 || now.IsZero() {
		return nil, ErrSocialInvalidRequest
	}
	rows, err := r.shard.QueryContext(socialContext(ctx), "SELECT ACADEMY_ID,CHAR_ID,BUFF_TYPE,VALUE,STARTS_AT,ENDS_AT FROM ACADEMY_MENTOR_BUFF WHERE CHAR_ID=? AND ENDS_AT>? ORDER BY BUFF_TYPE", characterID, now.UTC())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var states []AcademyBuffState
	for rows.Next() {
		var state AcademyBuffState
		if err = rows.Scan(&state.AcademyID, &state.CharacterID, &state.Type, &state.Value, &state.StartsAt, &state.EndsAt); err != nil {
			return nil, err
		}
		states = append(states, state)
	}
	return states, rows.Err()
}

func (r *SQLSocialRepository) UpdateAcademyComment(ctx context.Context, actor int, title, body string) error {
	if err := validateSocialText("academy title", title, 131, true); err != nil {
		return err
	}
	if err := validateSocialText("academy comment", body, 1024, true); err != nil {
		return err
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		result, err := tx.ExecContext(ctx, "UPDATE ACADEMY SET COMMENT_TITLE=?,COMMENT_BODY=? WHERE LEADER_CHAR=?", title, body, actor)
		if err != nil {
			return err
		}
		return requireSocialRow(result)
	})
}

func (r *SQLSocialRepository) RegisterAcademyMatch(ctx context.Context, actor int, title string, country, display byte) error {
	if err := validateSocialText("academy matching title", title, 80, false); err != nil {
		return err
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var academyID int
		if err := tx.QueryRowContext(ctx, "SELECT ID FROM ACADEMY WHERE LEADER_CHAR=? FOR UPDATE", actor).Scan(&academyID); err != nil {
			return ErrSocialAccessDenied
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO ACADEMY_MATCHING (FK_ACADEMY,TITLE,COUNTRY_TYPE,DISPLAY_LEVEL) VALUES (?,?,?,?) ON DUPLICATE KEY UPDATE TITLE=VALUES(TITLE),COUNTRY_TYPE=VALUES(COUNTRY_TYPE),DISPLAY_LEVEL=VALUES(DISPLAY_LEVEL)", academyID, title, country, display)
		if err != nil {
			return err
		}
		return nil
	})
}

func (r *SQLSocialRepository) DeleteAcademyMatch(ctx context.Context, actor int) error {
	result, err := r.shard.ExecContext(socialContext(ctx), "DELETE m FROM ACADEMY_MATCHING m JOIN ACADEMY a ON a.ID=m.FK_ACADEMY WHERE a.LEADER_CHAR=?", actor)
	if err != nil {
		return err
	}
	return requireSocialRow(result)
}

func (r *SQLSocialRepository) ListAcademyMatches(ctx context.Context, page byte, pageSize int) ([]AcademyMatch, int, error) {
	if pageSize <= 0 || pageSize > 20 {
		pageSize = 10
	}
	ctx = socialContext(ctx)
	var total int
	if err := r.shard.QueryRowContext(ctx, "SELECT COUNT(*) FROM ACADEMY_MATCHING").Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.shard.QueryContext(ctx, "SELECT a.ID,a.LEADER_CHAR,m.COUNTRY_TYPE,m.TITLE,m.DISPLAY_LEVEL,c.CURRENT_LEVEL,c.CHAR_NAME,a.GRADUATED_COUNT,a.HONOR_RANK FROM ACADEMY_MATCHING m JOIN ACADEMY a ON a.ID=m.FK_ACADEMY JOIN `CHAR` c ON c.id=a.LEADER_CHAR ORDER BY a.HONOR_RANK,a.GRADUATED_COUNT DESC,a.ID LIMIT ? OFFSET ?", pageSize, int(page)*pageSize)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	list := make([]AcademyMatch, 0, pageSize)
	for rows.Next() {
		var m AcademyMatch
		if err = rows.Scan(&m.AcademyID, &m.LeaderCharacter, &m.CountryType, &m.Title, &m.DisplayLevel, &m.LeaderLevel, &m.LeaderName, &m.GraduatedCount, &m.HonorRank); err != nil {
			return nil, 0, err
		}
		list = append(list, m)
	}
	pages := (total + pageSize - 1) / pageSize
	return list, pages, rows.Err()
}

func (r *SQLSocialRepository) AcademyHonorRanking(ctx context.Context, limit int) ([]AcademyMatch, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := r.shard.QueryContext(socialContext(ctx), "SELECT a.ID,a.LEADER_CHAR,0,'',0,c.CURRENT_LEVEL,c.CHAR_NAME,a.GRADUATED_COUNT,a.HONOR_RANK FROM ACADEMY a JOIN `CHAR` c ON c.id=a.LEADER_CHAR ORDER BY a.HONOR_POINTS DESC,a.GRADUATED_COUNT DESC,a.ID LIMIT ?", limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []AcademyMatch
	for rows.Next() {
		var m AcademyMatch
		if err = rows.Scan(&m.AcademyID, &m.LeaderCharacter, &m.CountryType, &m.Title, &m.DisplayLevel, &m.LeaderLevel, &m.LeaderName, &m.GraduatedCount, &m.HonorRank); err != nil {
			return nil, err
		}
		list = append(list, m)
	}
	return list, rows.Err()
}
