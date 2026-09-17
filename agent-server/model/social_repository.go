package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
	"time"

	"github.com/Harvi8/AegisCore/protocol/vsro188"
)

const (
	GuildPermissionInvite   uint32 = 1
	GuildPermissionWithdraw uint32 = 2
	GuildPermissionUnion    uint32 = 4
	GuildPermissionStorage  uint32 = 8
	GuildPermissionNotice   uint32 = 16
	GuildPermissionAll             = GuildPermissionInvite | GuildPermissionWithdraw | GuildPermissionUnion | GuildPermissionStorage | GuildPermissionNotice

	GuildMemberMaster byte = 0
	GuildMemberVice   byte = 5
	GuildMemberNormal byte = 10

	JobTrader byte = 1
	JobThief  byte = 2
	JobHunter byte = 3
	JobNone   byte = 4
)

var (
	ErrSocialRepositoryUnavailable = errors.New("social repository is unavailable")
	ErrSocialInvalidRequest        = errors.New("invalid social-system request")
	ErrSocialAccessDenied          = errors.New("social-system access denied")
	ErrSocialConflict              = errors.New("social-system state conflicts with request")
	ErrSocialNotFound              = errors.New("social-system record not found")
	ErrSocialCapacity              = errors.New("social-system capacity reached")
	ErrSocialInsufficientLevel     = errors.New("character level is insufficient")
	ErrSocialInsufficientGold      = errors.New("character gold is insufficient")
)

type GuildMember struct {
	CharacterID    int
	Name           string
	Class          byte
	Level          byte
	Permissions    uint32
	Donation       uint32
	Contribution   uint32
	Nickname       string
	RefObjectID    uint32
	SiegeAuthority byte
	Online         bool
}

type GuildSnapshot struct {
	ID            int
	Name          string
	Level         byte
	GatheredSP    uint32
	NoticeTitle   string
	NoticeBody    string
	CrestRevision uint32
	MercenaryAttr byte
	Members       []GuildMember
}

type GuildUnionMember struct {
	GuildID    int
	Name       string
	Level      byte
	MasterName string
	Members    byte
}

type GuildUnionSnapshot struct {
	ID          int
	LeaderGuild int
	Guilds      []GuildUnionMember
}

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

type JobSnapshot struct {
	CharacterID          int
	Type                 byte
	Alias                string
	Level                byte
	Experience           uint64
	Contribution         uint64
	WeeklyContribution   uint64
	PendingRewardGold    uint64
	EnrollmentFee        uint32
	LeaveAvailableAt     sql.NullTime
	PreviousType         byte
	PreviousExperience   uint64
	PreviousContribution uint64
}

// SocialRepository is the ShardManager-equivalent durable boundary. Every
// membership or economy transition is atomic and derives authority from the
// authenticated character ID rather than trusting a guild/academy ID sent by
// the client.
type SocialRepository interface {
	Guild(context.Context, int) (GuildSnapshot, error)
	CreateGuild(context.Context, int, string) (GuildSnapshot, uint64, error)
	DisbandGuild(context.Context, int) error
	InviteGuild(context.Context, int, int) error
	AnswerGuildInvite(context.Context, int, bool) (GuildSnapshot, error)
	LeaveGuild(context.Context, int) error
	KickGuildMember(context.Context, int, int) error
	UpdateGuildNotice(context.Context, int, string, string) error
	UpdateGuildPermissions(context.Context, int, map[int]uint32) error
	DonateGuildSP(context.Context, int, uint32) (uint32, error)
	PromoteGuild(context.Context, int) (GuildSnapshot, uint64, error)
	TransferGuild(context.Context, int, int) (GuildSnapshot, error)
	UpdateGuildNickname(context.Context, int, int, string) error
	InviteGuildUnion(context.Context, int, int) error
	AnswerGuildUnionInvite(context.Context, int, bool) (GuildUnionSnapshot, error)
	LeaveGuildUnion(context.Context, int) error
	KickGuildUnion(context.Context, int, int) error
	GuildUnion(context.Context, int) (GuildUnionSnapshot, error)

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

	Job(context.Context, int) (JobSnapshot, error)
	JobPreviousInfo(context.Context, int) (JobSnapshot, error)
	JoinJob(context.Context, int, byte, uint32) (JobSnapshot, uint64, error)
	LeaveJob(context.Context, int, uint32) (uint64, error)
	AdvanceJob(context.Context, int, uint64, uint64) (JobSnapshot, error)
	JobAliasAvailable(context.Context, string) (bool, error)
	SetJobAlias(context.Context, int, string) error
	JobRanking(context.Context, byte, bool, int) ([]JobSnapshot, error)
	ClaimJobReward(context.Context, int) (uint64, uint64, error)
	DistributeWeeklyJobRewards(context.Context) error
	SocialRecipients(context.Context, int, byte) ([]int, error)
}

type SQLSocialRepository struct{ shard *sql.DB }

type socialCommandContextKey struct{}

type SocialCommandIdentity struct {
	Key  string
	Name string
}

func WithSocialCommand(ctx context.Context, identity SocialCommandIdentity) context.Context {
	if ctx == nil || !validSocialCommandKey(identity.Key) || strings.TrimSpace(identity.Name) == "" || len(identity.Name) > 128 {
		return ctx
	}
	return context.WithValue(ctx, socialCommandContextKey{}, identity)
}

type guildCommandState struct {
	identity  SocialCommandIdentity
	guildID   int
	version   uint64
	topic     string
	disabled  bool
	duplicate bool
}

func replayedGuildCommand(ctx context.Context, tx *sql.Tx) (guildCommandState, bool, error) {
	identity, ok := ctx.Value(socialCommandContextKey{}).(SocialCommandIdentity)
	if !ok {
		return guildCommandState{}, false, nil
	}
	var name, aggregateID, status, topic string
	var version uint64
	err := tx.QueryRowContext(ctx, "SELECT c.`COMMAND_NAME`,c.`AGGREGATE_ID`,c.`STATUS`,v.`VERSION`,o.`TOPIC` FROM `AEGIS_COMMAND_IDEMPOTENCY` c JOIN `AEGIS_AGGREGATE_VERSION` v ON v.`AGGREGATE_KIND`=c.`AGGREGATE_KIND` AND v.`AGGREGATE_ID`=c.`AGGREGATE_ID` JOIN `AEGIS_OUTBOX_MESSAGE` o ON o.`MESSAGE_ID`=c.`IDEMPOTENCY_KEY` WHERE c.`IDEMPOTENCY_KEY`=? AND c.`AGGREGATE_KIND`='guild' FOR UPDATE", identity.Key).Scan(&name, &aggregateID, &status, &version, &topic)
	if errors.Is(err, sql.ErrNoRows) {
		return guildCommandState{}, false, nil
	}
	if err != nil {
		return guildCommandState{}, false, err
	}
	if name != identity.Name || status != "completed" {
		return guildCommandState{}, false, ErrSocialConflict
	}
	var guildID int
	if _, err = fmt.Sscan(aggregateID, &guildID); err != nil || guildID <= 0 {
		return guildCommandState{}, false, ErrSocialConflict
	}
	return guildCommandState{identity: identity, guildID: guildID, version: version, topic: topic, duplicate: true}, true, nil
}

func beginGuildCommand(ctx context.Context, tx *sql.Tx, guildID int) (guildCommandState, error) {
	identity, ok := ctx.Value(socialCommandContextKey{}).(SocialCommandIdentity)
	if !ok {
		return guildCommandState{disabled: true}, nil
	}
	aggregateID := fmt.Sprintf("%d", guildID)
	if _, err := tx.ExecContext(ctx, "INSERT IGNORE INTO `AEGIS_AGGREGATE_VERSION` (`AGGREGATE_KIND`,`AGGREGATE_ID`,`VERSION`) VALUES ('guild',?,0)", aggregateID); err != nil {
		return guildCommandState{}, err
	}
	var version uint64
	if err := tx.QueryRowContext(ctx, "SELECT `VERSION` FROM `AEGIS_AGGREGATE_VERSION` WHERE `AGGREGATE_KIND`='guild' AND `AGGREGATE_ID`=? FOR UPDATE", aggregateID).Scan(&version); err != nil {
		return guildCommandState{}, err
	}
	var name, kind, id, status string
	err := tx.QueryRowContext(ctx, "SELECT `COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`STATUS` FROM `AEGIS_COMMAND_IDEMPOTENCY` WHERE `IDEMPOTENCY_KEY`=? FOR UPDATE", identity.Key).Scan(&name, &kind, &id, &status)
	if err == nil {
		if name != identity.Name || kind != "guild" || id != aggregateID || status != "completed" {
			return guildCommandState{}, ErrSocialConflict
		}
		return guildCommandState{identity: identity, guildID: guildID, version: version, duplicate: true}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return guildCommandState{}, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO `AEGIS_COMMAND_IDEMPOTENCY` (`IDEMPOTENCY_KEY`,`COMMAND_NAME`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`EXPECTED_VERSION`,`STATUS`) VALUES (?,?,'guild',?,?,'in_progress')", identity.Key, identity.Name, aggregateID, version); err != nil {
		return guildCommandState{}, err
	}
	return guildCommandState{identity: identity, guildID: guildID, version: version}, nil
}

func completeGuildCommand(ctx context.Context, tx *sql.Tx, state guildCommandState, topic string) error {
	if state.disabled || state.duplicate {
		return nil
	}
	aggregateID := fmt.Sprintf("%d", state.guildID)
	next := state.version + 1
	result, err := tx.ExecContext(ctx, "UPDATE `AEGIS_AGGREGATE_VERSION` SET `VERSION`=? WHERE `AGGREGATE_KIND`='guild' AND `AGGREGATE_ID`=? AND `VERSION`=?", next, aggregateID, state.version)
	if err != nil {
		return err
	}
	if affected, affectedErr := result.RowsAffected(); affectedErr != nil || affected != 1 {
		return ErrSocialConflict
	}
	payload := fmt.Sprintf(`{"guild_id":%d,"command":"%s"}`, state.guildID, state.identity.Name)
	if _, err = tx.ExecContext(ctx, "INSERT INTO `AEGIS_OUTBOX_MESSAGE` (`MESSAGE_ID`,`AGGREGATE_KIND`,`AGGREGATE_ID`,`AGGREGATE_VERSION`,`TOPIC`,`PAYLOAD`,`OCCURRED_AT`) VALUES (?,'guild',?,?,?,?,CURRENT_TIMESTAMP(6))", state.identity.Key, aggregateID, next, topic, []byte(payload)); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "UPDATE `AEGIS_COMMAND_IDEMPOTENCY` SET `STATUS`='completed',`RESULT_VERSION`=?,`RESULT_PAYLOAD`=?,`COMPLETED_AT`=CURRENT_TIMESTAMP(6) WHERE `IDEMPOTENCY_KEY`=? AND `STATUS`='in_progress'", next, []byte(`{"ok":true}`), state.identity.Key)
	return err
}

var _ SocialRepository = (*SQLSocialRepository)(nil)

func NewSocialRepository(shard *sql.DB) (*SQLSocialRepository, error) {
	if shard == nil {
		return nil, ErrSocialRepositoryUnavailable
	}
	return &SQLSocialRepository{shard: shard}, nil
}

func socialContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func validateSocialText(field, value string, max int, empty bool) error {
	if err := vsro188.ValidateText(value, vsro188.TextRules{Field: field, MaxRunes: max, RequireTrimmed: true}); err != nil {
		return errors.Join(ErrSocialInvalidRequest, err)
	}
	if !empty && value == "" {
		return ErrSocialInvalidRequest
	}
	return nil
}

func guildCapacity(level byte) int {
	switch level {
	case 1:
		return 15
	case 2:
		return 20
	case 3:
		return 25
	case 4:
		return 35
	default:
		return 50
	}
}

func (r *SQLSocialRepository) tx(ctx context.Context, fn func(context.Context, *sql.Tx) error) error {
	if r == nil || r.shard == nil {
		return ErrSocialRepositoryUnavailable
	}
	ctx = socialContext(ctx)
	tx, err := r.shard.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err = fn(ctx, tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func scanGuild(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, characterID int) (GuildSnapshot, error) {
	var g GuildSnapshot
	err := q.QueryRowContext(ctx, "SELECT g.ID,g.NAME,g.LEVEL,g.GATHERED_SP,g.NOTICE_TITLE,g.NOTICE_BODY,g.CREST_REVISION,g.MERCENARY_ATTR FROM GUILD g JOIN GUILD_MEMBER gm ON gm.FK_GUILD=g.ID WHERE gm.FK_CHAR=?", characterID).Scan(&g.ID, &g.Name, &g.Level, &g.GatheredSP, &g.NoticeTitle, &g.NoticeBody, &g.CrestRevision, &g.MercenaryAttr)
	if errors.Is(err, sql.ErrNoRows) {
		return GuildSnapshot{}, ErrSocialNotFound
	}
	if err != nil {
		return GuildSnapshot{}, err
	}
	rows, err := q.QueryContext(ctx, "SELECT c.id,c.CHAR_NAME,gm.MEMBER_CLASS,c.CURRENT_LEVEL,gm.PERMISSIONS,gm.GP_DONATION,gm.CONTRIBUTION,gm.NICKNAME,c.REF_OBJ_ID,gm.SIEGE_AUTHORITY,gm.LAST_ONLINE_AT IS NULL FROM GUILD_MEMBER gm JOIN `CHAR` c ON c.id=gm.FK_CHAR WHERE gm.FK_GUILD=? ORDER BY gm.MEMBER_CLASS,c.CHAR_NAME", g.ID)
	if err != nil {
		return GuildSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var member GuildMember
		if err = rows.Scan(&member.CharacterID, &member.Name, &member.Class, &member.Level, &member.Permissions, &member.Donation, &member.Contribution, &member.Nickname, &member.RefObjectID, &member.SiegeAuthority, &member.Online); err != nil {
			return GuildSnapshot{}, err
		}
		g.Members = append(g.Members, member)
	}
	return g, rows.Err()
}

func (r *SQLSocialRepository) Guild(ctx context.Context, characterID int) (GuildSnapshot, error) {
	if r == nil || r.shard == nil || characterID <= 0 {
		return GuildSnapshot{}, ErrSocialInvalidRequest
	}
	return scanGuild(socialContext(ctx), r.shard, characterID)
}

func (r *SQLSocialRepository) CreateGuild(ctx context.Context, characterID int, name string) (GuildSnapshot, uint64, error) {
	if characterID <= 0 || validateSocialText("guild name", name, 20, false) != nil {
		return GuildSnapshot{}, 0, ErrSocialInvalidRequest
	}
	cost := RuntimeGuildCreationCost()
	var result GuildSnapshot
	var gold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if replay, duplicate, err := replayedGuildCommand(ctx, tx); err != nil {
			return err
		} else if duplicate {
			if err = tx.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE id=?", characterID).Scan(&gold); err != nil {
				return err
			}
			result, err = scanGuild(ctx, tx, characterID)
			_ = replay
			return err
		}
		var level int
		if err := tx.QueryRowContext(ctx, "SELECT CURRENT_LEVEL,GOLD FROM `CHAR` WHERE id=? FOR UPDATE", characterID).Scan(&level, &gold); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrSocialNotFound
			}
			return err
		}
		if level < 20 {
			return ErrSocialInsufficientLevel
		}
		if gold < cost {
			return ErrSocialInsufficientGold
		}
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_MEMBER WHERE FK_CHAR=?", characterID).Scan(&exists); err != nil {
			return err
		}
		if exists != 0 {
			return ErrSocialConflict
		}
		created, err := tx.ExecContext(ctx, "INSERT INTO GUILD (NAME) VALUES (?)", name)
		if err != nil {
			return errors.Join(ErrSocialConflict, err)
		}
		guildID, err := created.LastInsertId()
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO GUILD_MEMBER (FK_GUILD,FK_CHAR,MEMBER_CLASS,PERMISSIONS,STORAGE_RIGHTS) VALUES (?,?,0,?,1)", guildID, characterID, GuildPermissionAll); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE id=?", cost, characterID); err != nil {
			return err
		}
		command, err := beginGuildCommand(ctx, tx, int(guildID))
		if err != nil || command.duplicate {
			return err
		}
		gold -= cost
		result, err = scanGuild(ctx, tx, characterID)
		if err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.created")
	})
	return result, gold, err
}

func (r *SQLSocialRepository) DisbandGuild(ctx context.Context, actor int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, duplicate, err := replayedGuildCommand(ctx, tx); err != nil || duplicate {
			return err
		}
		var guildID int
		if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD FROM GUILD_MEMBER WHERE FK_CHAR=? AND MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&guildID); err != nil {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil || command.duplicate {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM GUILD WHERE ID=?", guildID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.disbanded")
	})
}

func (r *SQLSocialRepository) InviteGuild(ctx context.Context, inviter, invitee int) error {
	if inviter <= 0 || invitee <= 0 || inviter == invitee {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var level byte
		var permissions uint32
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,g.LEVEL,gm.PERMISSIONS FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD WHERE gm.FK_CHAR=? FOR UPDATE", inviter).Scan(&guildID, &level, &permissions); err != nil {
			return ErrSocialAccessDenied
		}
		if permissions&GuildPermissionInvite == 0 {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_MEMBER WHERE FK_GUILD=?", guildID).Scan(&count); err != nil {
			return err
		}
		if count >= guildCapacity(level) {
			return ErrSocialCapacity
		}
		var already int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_MEMBER WHERE FK_CHAR=?", invitee).Scan(&already); err != nil {
			return err
		}
		if already != 0 {
			return ErrSocialConflict
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO GUILD_INVITATION (FK_GUILD,INVITER_CHAR,INVITEE_CHAR,EXPIRES_AT) VALUES (?,?,?,DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 60 SECOND)) ON DUPLICATE KEY UPDATE FK_GUILD=VALUES(FK_GUILD),INVITER_CHAR=VALUES(INVITER_CHAR),EXPIRES_AT=VALUES(EXPIRES_AT)", guildID, inviter, invitee); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.invitation.created")
	})
}

func (r *SQLSocialRepository) AnswerGuildInvite(ctx context.Context, characterID int, accept bool) (GuildSnapshot, error) {
	var result GuildSnapshot
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if replay, duplicate, err := replayedGuildCommand(ctx, tx); err != nil {
			return err
		} else if duplicate {
			if strings.HasPrefix(replay.topic, "guild.union.") {
				return ErrSocialNotFound
			}
			if !accept {
				return nil
			}
			result, err = scanGuild(ctx, tx, characterID)
			return err
		}
		var guildID int
		var level byte
		if err := tx.QueryRowContext(ctx, "SELECT gi.FK_GUILD,g.LEVEL FROM GUILD_INVITATION gi JOIN GUILD g ON g.ID=gi.FK_GUILD WHERE gi.INVITEE_CHAR=? AND gi.EXPIRES_AT>CURRENT_TIMESTAMP(6) FOR UPDATE", characterID).Scan(&guildID, &level); err != nil {
			return ErrSocialNotFound
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil || command.duplicate {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM GUILD_INVITATION WHERE INVITEE_CHAR=?", characterID); err != nil {
			return err
		}
		if !accept {
			return completeGuildCommand(ctx, tx, command, "guild.invitation.rejected")
		}
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_MEMBER WHERE FK_GUILD=?", guildID).Scan(&count); err != nil {
			return err
		}
		if count >= guildCapacity(level) {
			return ErrSocialCapacity
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO GUILD_MEMBER (FK_GUILD,FK_CHAR) VALUES (?,?)", guildID, characterID); err != nil {
			return errors.Join(ErrSocialConflict, err)
		}
		result, err = scanGuild(ctx, tx, characterID)
		if err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.member.joined")
	})
	return result, err
}

func (r *SQLSocialRepository) LeaveGuild(ctx context.Context, characterID int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if _, duplicate, err := replayedGuildCommand(ctx, tx); err != nil || duplicate {
			return err
		}
		var guildID int
		var class byte
		if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD,MEMBER_CLASS FROM GUILD_MEMBER WHERE FK_CHAR=? FOR UPDATE", characterID).Scan(&guildID, &class); err != nil {
			return ErrSocialNotFound
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil || command.duplicate {
			return err
		}
		if class == GuildMemberMaster {
			return ErrSocialAccessDenied
		}
		if err = ensureGuildElectionMemberMovable(ctx, tx, guildID, characterID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM GUILD_MEMBER WHERE FK_CHAR=?", characterID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.member.left")
	})
}

func (r *SQLSocialRepository) KickGuildMember(ctx context.Context, actor, target int) error {
	if actor <= 0 || target <= 0 || actor == target {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var permissions uint32
		if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD,PERMISSIONS FROM GUILD_MEMBER WHERE FK_CHAR=? FOR UPDATE", actor).Scan(&guildID, &permissions); err != nil || permissions&GuildPermissionWithdraw == 0 {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		if err = ensureGuildElectionMemberMovable(ctx, tx, guildID, target); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM GUILD_MEMBER WHERE FK_GUILD=? AND FK_CHAR=? AND MEMBER_CLASS<>?", guildID, target, GuildMemberMaster)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.member.kicked")
	})
}

func ensureGuildElectionMemberMovable(ctx context.Context, tx *sql.Tx, guildID, characterID int) error {
	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM GUILD_ELECTION e JOIN GUILD_ELECTION_CANDIDATE c ON c.FK_ELECTION=e.ID
WHERE e.FK_GUILD=? AND e.STATUS='active' AND c.FK_CHAR=? LIMIT 1 FOR UPDATE`, guildID, characterID).Scan(&exists)
	if err == nil {
		return ErrSocialConflict
	}
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	return err
}

func (r *SQLSocialRepository) UpdateGuildNotice(ctx context.Context, actor int, title, body string) error {
	if err := validateSocialText("guild notice title", title, 131, true); err != nil {
		return err
	}
	if err := validateSocialText("guild notice body", body, 1024, true); err != nil {
		return err
	}
	return r.guildAuthorizedWrite(ctx, actor, GuildPermissionNotice, "guild.notice.updated", "UPDATE GUILD SET NOTICE_TITLE=?,NOTICE_BODY=? WHERE ID=?", title, body)
}

func (r *SQLSocialRepository) guildAuthorizedWrite(ctx context.Context, actor int, permission uint32, topic, query string, args ...any) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var permissions uint32
		if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD,PERMISSIONS FROM GUILD_MEMBER WHERE FK_CHAR=? FOR UPDATE", actor).Scan(&guildID, &permissions); err != nil || permissions&permission == 0 {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		args = append(args, guildID)
		result, err := tx.ExecContext(ctx, query, args...)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, topic)
	})
}

func (r *SQLSocialRepository) UpdateGuildPermissions(ctx context.Context, actor int, updates map[int]uint32) error {
	if len(updates) == 0 || len(updates) > 50 {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var class byte
		if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD,MEMBER_CLASS FROM GUILD_MEMBER WHERE FK_CHAR=? FOR UPDATE", actor).Scan(&guildID, &class); err != nil || class != GuildMemberMaster {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		for memberID, permissions := range updates {
			if memberID <= 0 || permissions&^uint32(GuildPermissionAll) != 0 {
				return ErrSocialInvalidRequest
			}
			result, err := tx.ExecContext(ctx, "UPDATE GUILD_MEMBER SET PERMISSIONS=?,STORAGE_RIGHTS=(?&8<>0) WHERE FK_GUILD=? AND FK_CHAR=? AND MEMBER_CLASS<>?", permissions, permissions, guildID, memberID, GuildMemberMaster)
			if err != nil {
				return err
			}
			if err = requireSocialRow(result); err != nil {
				return err
			}
		}
		return completeGuildCommand(ctx, tx, command, "guild.permissions.updated")
	})
}

func (r *SQLSocialRepository) DonateGuildSP(ctx context.Context, characterID int, amount uint32) (uint32, error) {
	if characterID <= 0 || amount == 0 {
		return 0, ErrSocialInvalidRequest
	}
	var total uint32
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var skillPoints uint32
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,c.SKILL_POINTS FROM GUILD_MEMBER gm JOIN `CHAR` c ON c.id=gm.FK_CHAR WHERE gm.FK_CHAR=? FOR UPDATE", characterID).Scan(&guildID, &skillPoints); err != nil {
			return ErrSocialNotFound
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil {
			return err
		}
		if command.duplicate {
			return tx.QueryRowContext(ctx, "SELECT GATHERED_SP FROM GUILD WHERE ID=?", guildID).Scan(&total)
		}
		if skillPoints < amount {
			return ErrSocialConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET SKILL_POINTS=SKILL_POINTS-? WHERE id=?", amount, characterID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE GUILD SET GATHERED_SP=GATHERED_SP+? WHERE ID=?", amount, guildID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE GUILD_MEMBER SET GP_DONATION=GP_DONATION+? WHERE FK_CHAR=?", amount, characterID); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT GATHERED_SP FROM GUILD WHERE ID=?", guildID).Scan(&total); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.skill-points.donated")
	})
	return total, err
}

func guildPromotionPolicy(nextLevel byte) (uint64, uint32, bool) {
	switch nextLevel {
	case 2:
		return 3000000, 5400, true
	case 3:
		return 5000000, 50400, true
	case 4:
		return 9000000, 135400, true
	case 5:
		return 15000000, 378000, true
	default:
		return 0, 0, false
	}
}

func (r *SQLSocialRepository) PromoteGuild(ctx context.Context, actor int) (GuildSnapshot, uint64, error) {
	var snapshot GuildSnapshot
	var gold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var level byte
		var sp uint32
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,g.LEVEL,g.GATHERED_SP,c.GOLD FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD JOIN `CHAR` c ON c.id=gm.FK_CHAR WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&guildID, &level, &sp, &gold); err != nil {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil {
			return err
		}
		if command.duplicate {
			snapshot, err = scanGuild(ctx, tx, actor)
			return err
		}
		cost, requiredSP, ok := guildPromotionPolicy(level + 1)
		if !ok {
			return ErrSocialCapacity
		}
		if gold < cost {
			return ErrSocialInsufficientGold
		}
		if sp < requiredSP {
			return ErrSocialConflict
		}
		capacity := byte(150)
		if level+1 >= 3 {
			capacity = 225
		}
		if _, err := tx.ExecContext(ctx, "UPDATE GUILD SET LEVEL=LEVEL+1,GATHERED_SP=GATHERED_SP-?,STORAGE_CAPACITY=? WHERE ID=?", requiredSP, capacity, guildID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE id=?", cost, actor); err != nil {
			return err
		}
		gold -= cost
		snapshot, err = scanGuild(ctx, tx, actor)
		if err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.promoted")
	})
	return snapshot, gold, err
}

func (r *SQLSocialRepository) TransferGuild(ctx context.Context, actor, target int) (GuildSnapshot, error) {
	if actor <= 0 || target <= 0 || actor == target {
		return GuildSnapshot{}, ErrSocialInvalidRequest
	}
	var snapshot GuildSnapshot
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var memberClass byte
		if err := tx.QueryRowContext(ctx, "SELECT FK_GUILD,MEMBER_CLASS FROM GUILD_MEMBER WHERE FK_CHAR=? FOR UPDATE", actor).Scan(&guildID, &memberClass); err != nil {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil {
			return err
		}
		if command.duplicate {
			snapshot, err = scanGuild(ctx, tx, target)
			return err
		}
		if memberClass != GuildMemberMaster {
			return ErrSocialAccessDenied
		}
		result, err := tx.ExecContext(ctx, "UPDATE GUILD_MEMBER SET MEMBER_CLASS=?,PERMISSIONS=0,STORAGE_RIGHTS=0 WHERE FK_GUILD=? AND FK_CHAR=?", GuildMemberNormal, guildID, actor)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, "UPDATE GUILD_MEMBER SET MEMBER_CLASS=?,PERMISSIONS=?,STORAGE_RIGHTS=1 WHERE FK_GUILD=? AND FK_CHAR=?", GuildMemberMaster, GuildPermissionAll, guildID, target)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		snapshot, err = scanGuild(ctx, tx, target)
		if err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.leadership.transferred")
	})
	return snapshot, err
}

func (r *SQLSocialRepository) UpdateGuildNickname(ctx context.Context, actor, target int, nickname string) error {
	if err := validateSocialText("guild nickname", nickname, 12, true); err != nil {
		return err
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var guildID int
		var level byte
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,g.LEVEL FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=? FOR UPDATE", actor, GuildMemberMaster).Scan(&guildID, &level); err != nil || level < 3 {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		result, err := tx.ExecContext(ctx, "UPDATE GUILD_MEMBER SET NICKNAME=? WHERE FK_GUILD=? AND FK_CHAR=?", nickname, guildID, target)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.nickname.updated")
	})
}

func scanGuildUnion(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, characterID int) (GuildUnionSnapshot, error) {
	var union GuildUnionSnapshot
	if err := q.QueryRowContext(ctx, "SELECT u.ID,u.LEADER_GUILD FROM GUILD_MEMBER actor JOIN GUILD_UNION_MEMBER own ON own.FK_GUILD=actor.FK_GUILD JOIN GUILD_UNION u ON u.ID=own.FK_UNION WHERE actor.FK_CHAR=?", characterID).Scan(&union.ID, &union.LeaderGuild); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return GuildUnionSnapshot{}, ErrSocialNotFound
		}
		return GuildUnionSnapshot{}, err
	}
	rows, err := q.QueryContext(ctx, "SELECT g.ID,g.NAME,g.LEVEL,c.CHAR_NAME,COUNT(gm.FK_CHAR) FROM GUILD_UNION_MEMBER um JOIN GUILD g ON g.ID=um.FK_GUILD JOIN GUILD_MEMBER master ON master.FK_GUILD=g.ID AND master.MEMBER_CLASS=0 JOIN `CHAR` c ON c.id=master.FK_CHAR JOIN GUILD_MEMBER gm ON gm.FK_GUILD=g.ID WHERE um.FK_UNION=? GROUP BY g.ID,g.NAME,g.LEVEL,c.CHAR_NAME ORDER BY (g.ID=? ) DESC,g.ID", union.ID, union.LeaderGuild)
	if err != nil {
		return GuildUnionSnapshot{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var member GuildUnionMember
		if err = rows.Scan(&member.GuildID, &member.Name, &member.Level, &member.MasterName, &member.Members); err != nil {
			return GuildUnionSnapshot{}, err
		}
		union.Guilds = append(union.Guilds, member)
	}
	return union, rows.Err()
}

func (r *SQLSocialRepository) GuildUnion(ctx context.Context, characterID int) (GuildUnionSnapshot, error) {
	return scanGuildUnion(socialContext(ctx), r.shard, characterID)
}

func (r *SQLSocialRepository) InviteGuildUnion(ctx context.Context, actor, target int) error {
	if actor <= 0 || target <= 0 || actor == target {
		return ErrSocialInvalidRequest
	}
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var inviterGuild, targetGuild int
		var inviterLevel, targetLevel byte
		var permissions uint32
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,g.LEVEL,gm.PERMISSIONS FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 FOR UPDATE", actor).Scan(&inviterGuild, &inviterLevel, &permissions); err != nil || permissions&GuildPermissionUnion == 0 {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, inviterGuild)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,g.LEVEL FROM GUILD_MEMBER gm JOIN GUILD g ON g.ID=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 FOR UPDATE", target).Scan(&targetGuild, &targetLevel); err != nil {
			return ErrSocialAccessDenied
		}
		if inviterGuild == targetGuild {
			return ErrSocialConflict
		}
		if inviterLevel < 2 || targetLevel < 2 {
			return ErrSocialInsufficientLevel
		}
		var targetUnion int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM GUILD_UNION_MEMBER WHERE FK_GUILD=?", targetGuild).Scan(&targetUnion); err != nil {
			return err
		}
		if targetUnion != 0 {
			return ErrSocialConflict
		}
		var unionID sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT FK_UNION FROM GUILD_UNION_MEMBER WHERE FK_GUILD=?", inviterGuild).Scan(&unionID); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if unionID.Valid {
			var leader, count int
			if err := tx.QueryRowContext(ctx, "SELECT u.LEADER_GUILD,COUNT(um.FK_GUILD) FROM GUILD_UNION u JOIN GUILD_UNION_MEMBER um ON um.FK_UNION=u.ID WHERE u.ID=? GROUP BY u.LEADER_GUILD", unionID.Int64).Scan(&leader, &count); err != nil {
				return err
			}
			if leader != inviterGuild {
				return ErrSocialAccessDenied
			}
			if count >= 8 {
				return ErrSocialCapacity
			}
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO GUILD_UNION_INVITATION (INVITER_GUILD,INVITEE_GUILD,EXPIRES_AT) VALUES (?,?,DATE_ADD(CURRENT_TIMESTAMP(6),INTERVAL 60 SECOND)) ON DUPLICATE KEY UPDATE INVITER_GUILD=VALUES(INVITER_GUILD),EXPIRES_AT=VALUES(EXPIRES_AT)", inviterGuild, targetGuild); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.union.invitation.created")
	})
}

func (r *SQLSocialRepository) AnswerGuildUnionInvite(ctx context.Context, actor int, accept bool) (GuildUnionSnapshot, error) {
	var snapshot GuildUnionSnapshot
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if replay, duplicate, err := replayedGuildCommand(ctx, tx); err != nil {
			return err
		} else if duplicate {
			if !strings.HasPrefix(replay.topic, "guild.union.") {
				return ErrSocialNotFound
			}
			if !accept {
				return nil
			}
			snapshot, err = scanGuildUnion(ctx, tx, actor)
			return err
		}
		var targetGuild, inviterGuild int
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,i.INVITER_GUILD FROM GUILD_MEMBER gm JOIN GUILD_UNION_INVITATION i ON i.INVITEE_GUILD=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 AND i.EXPIRES_AT>CURRENT_TIMESTAMP(6) FOR UPDATE", actor).Scan(&targetGuild, &inviterGuild); err != nil {
			return ErrSocialNotFound
		}
		command, err := beginGuildCommand(ctx, tx, targetGuild)
		if err != nil || command.duplicate {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM GUILD_UNION_INVITATION WHERE INVITEE_GUILD=?", targetGuild); err != nil {
			return err
		}
		if !accept {
			return completeGuildCommand(ctx, tx, command, "guild.union.invitation.rejected")
		}
		var unionID int
		err = tx.QueryRowContext(ctx, "SELECT FK_UNION FROM GUILD_UNION_MEMBER WHERE FK_GUILD=?", inviterGuild).Scan(&unionID)
		if errors.Is(err, sql.ErrNoRows) {
			created, createErr := tx.ExecContext(ctx, "INSERT INTO GUILD_UNION (LEADER_GUILD) VALUES (?)", inviterGuild)
			if createErr != nil {
				return createErr
			}
			id, idErr := created.LastInsertId()
			if idErr != nil {
				return idErr
			}
			unionID = int(id)
			if _, err = tx.ExecContext(ctx, "INSERT INTO GUILD_UNION_MEMBER (FK_UNION,FK_GUILD) VALUES (?,?)", unionID, inviterGuild); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "INSERT INTO GUILD_UNION_MEMBER (FK_UNION,FK_GUILD) VALUES (?,?)", unionID, targetGuild); err != nil {
			return errors.Join(ErrSocialConflict, err)
		}
		if _, err = tx.ExecContext(ctx, "UPDATE GUILD SET UNION_ID=? WHERE ID IN (?,?)", unionID, inviterGuild, targetGuild); err != nil {
			return err
		}
		snapshot, err = scanGuildUnion(ctx, tx, actor)
		if err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.union.joined")
	})
	return snapshot, err
}

func (r *SQLSocialRepository) LeaveGuildUnion(ctx context.Context, actor int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if replay, duplicate, err := replayedGuildCommand(ctx, tx); err != nil {
			return err
		} else if duplicate {
			if !strings.HasPrefix(replay.topic, "guild.union.") {
				return ErrSocialConflict
			}
			return nil
		}
		var guildID, unionID, leader int
		if err := tx.QueryRowContext(ctx, "SELECT gm.FK_GUILD,um.FK_UNION,u.LEADER_GUILD FROM GUILD_MEMBER gm JOIN GUILD_UNION_MEMBER um ON um.FK_GUILD=gm.FK_GUILD JOIN GUILD_UNION u ON u.ID=um.FK_UNION WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 FOR UPDATE", actor).Scan(&guildID, &unionID, &leader); err != nil {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, guildID)
		if err != nil || command.duplicate {
			return err
		}
		if leader == guildID {
			if _, err := tx.ExecContext(ctx, "UPDATE GUILD g JOIN GUILD_UNION_MEMBER um ON um.FK_GUILD=g.ID SET g.UNION_ID=NULL WHERE um.FK_UNION=?", unionID); err != nil {
				return err
			}
			result, err := tx.ExecContext(ctx, "DELETE FROM GUILD_UNION WHERE ID=?", unionID)
			if err != nil {
				return err
			}
			if err = requireSocialRow(result); err != nil {
				return err
			}
			return completeGuildCommand(ctx, tx, command, "guild.union.disbanded")
		}
		if _, err := tx.ExecContext(ctx, "UPDATE GUILD SET UNION_ID=NULL WHERE ID=?", guildID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM GUILD_UNION_MEMBER WHERE FK_UNION=? AND FK_GUILD=?", unionID, guildID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.union.left")
	})
}

func (r *SQLSocialRepository) KickGuildUnion(ctx context.Context, actor, targetGuild int) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var unionID, leader int
		if err := tx.QueryRowContext(ctx, "SELECT u.ID,u.LEADER_GUILD FROM GUILD_MEMBER gm JOIN GUILD_UNION u ON u.LEADER_GUILD=gm.FK_GUILD WHERE gm.FK_CHAR=? AND gm.MEMBER_CLASS=0 FOR UPDATE", actor).Scan(&unionID, &leader); err != nil {
			return ErrSocialAccessDenied
		}
		command, err := beginGuildCommand(ctx, tx, leader)
		if err != nil {
			return err
		}
		if command.duplicate {
			return nil
		}
		if targetGuild == leader {
			return ErrSocialInvalidRequest
		}
		if _, err := tx.ExecContext(ctx, "UPDATE GUILD SET UNION_ID=NULL WHERE ID=?", targetGuild); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, "DELETE FROM GUILD_UNION_MEMBER WHERE FK_UNION=? AND FK_GUILD=?", unionID, targetGuild)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		return completeGuildCommand(ctx, tx, command, "guild.union.member.kicked")
	})
}

func requireSocialRow(result sql.Result) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return ErrSocialNotFound
	}
	return nil
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

func validSocialCommandKey(key string) bool {
	if key == "" || len(key) > 128 || strings.TrimSpace(key) != key {
		return false
	}
	for _, character := range key {
		if !((character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || (character >= '0' && character <= '9') || strings.ContainsRune("-_.:", character)) {
			return false
		}
	}
	return true
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

const jobSnapshotColumns = "FK_CHAR,JOB_TYPE,COALESCE(ALIAS,''),JOB_LEVEL,JOB_EXP,CONTRIBUTION,WEEKLY_CONTRIBUTION,PENDING_REWARD_GOLD,ENROLLMENT_FEE,LEAVE_AVAILABLE_AT,PREVIOUS_JOB_TYPE,PREVIOUS_JOB_EXP,PREVIOUS_CONTRIBUTION"

func scanJob(row interface{ Scan(...any) error }, job *JobSnapshot) error {
	return row.Scan(&job.CharacterID, &job.Type, &job.Alias, &job.Level, &job.Experience, &job.Contribution, &job.WeeklyContribution, &job.PendingRewardGold, &job.EnrollmentFee, &job.LeaveAvailableAt, &job.PreviousType, &job.PreviousExperience, &job.PreviousContribution)
}

func (r *SQLSocialRepository) Job(ctx context.Context, characterID int) (JobSnapshot, error) {
	var job JobSnapshot
	err := scanJob(r.shard.QueryRowContext(socialContext(ctx), "SELECT "+jobSnapshotColumns+" FROM CHARACTER_JOB WHERE FK_CHAR=?", characterID), &job)
	if errors.Is(err, sql.ErrNoRows) {
		return JobSnapshot{}, ErrSocialNotFound
	}
	return job, err
}

// JobPreviousInfo returns the active job, when present, together with the
// latest archived job. Unlike Job, it also succeeds for a character that has
// withdrawn and therefore has no CHARACTER_JOB row.
func (r *SQLSocialRepository) JobPreviousInfo(ctx context.Context, characterID int) (JobSnapshot, error) {
	job, err := r.Job(ctx, characterID)
	if err != nil && !errors.Is(err, ErrSocialNotFound) {
		return JobSnapshot{}, err
	}
	if errors.Is(err, ErrSocialNotFound) {
		job = JobSnapshot{CharacterID: characterID, Type: JobNone}
	}
	err = r.shard.QueryRowContext(socialContext(ctx), "SELECT JOB_TYPE,JOB_EXP,CONTRIBUTION FROM CHARACTER_JOB_HISTORY WHERE FK_CHAR=? ORDER BY LEFT_AT DESC,ID DESC LIMIT 1", characterID).
		Scan(&job.PreviousType, &job.PreviousExperience, &job.PreviousContribution)
	if errors.Is(err, sql.ErrNoRows) {
		return job, nil
	}
	return job, err
}

func jobEnrollmentFee(level int) (uint32, bool) {
	if level < 20 {
		return 0, false
	}
	fee := uint64(level-20) * 5000
	if fee > math.MaxUint32 {
		return 0, false
	}
	return uint32(fee), true
}

func jobLeaveFee(level int) (uint32, bool) {
	if level <= 0 {
		return 0, false
	}
	fee := uint64(level) * 50000
	if fee > math.MaxUint32 {
		return 0, false
	}
	return uint32(fee), true
}

func (r *SQLSocialRepository) JoinJob(ctx context.Context, characterID int, jobType byte, npcUID uint32) (JobSnapshot, uint64, error) {
	if jobType < JobTrader || jobType > JobHunter {
		return JobSnapshot{}, 0, ErrSocialInvalidRequest
	}
	// vSRO 1.188 sends the selected NPC UID in the first 0x70E1 field.
	// Enrollment cost is authoritative server state derived from character level.
	_ = npcUID
	var snapshot JobSnapshot
	var gold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var level int
		if err := tx.QueryRowContext(ctx, "SELECT CURRENT_LEVEL,GOLD FROM `CHAR` WHERE id=? FOR UPDATE", characterID).Scan(&level, &gold); err != nil {
			return ErrSocialNotFound
		}
		cost, eligible := jobEnrollmentFee(level)
		if !eligible {
			return ErrSocialInsufficientLevel
		}
		if gold < uint64(cost) {
			return ErrSocialInsufficientGold
		}
		if _, err := tx.ExecContext(ctx, "INSERT INTO CHARACTER_JOB (FK_CHAR,JOB_TYPE,ENROLLMENT_FEE) VALUES (?,?,?)", characterID, jobType, cost); err != nil {
			return errors.Join(ErrSocialConflict, err)
		}
		if jobUsesSafeTradeState(jobType) {
			if _, err := tx.ExecContext(ctx, "INSERT INTO CHARACTER_SAFE_TRADE (FK_CHAR,ABLE_COUNT,STATUS,PROGRESS) VALUES (?,0,0,0)", characterID); err != nil {
				return errors.Join(ErrSocialConflict, err)
			}
		}
		if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE id=?", cost, characterID); err != nil {
			return err
		}
		gold -= uint64(cost)
		return scanJob(tx.QueryRowContext(ctx, "SELECT "+jobSnapshotColumns+" FROM CHARACTER_JOB WHERE FK_CHAR=?", characterID), &snapshot)
	})
	return snapshot, gold, err
}

func jobUsesSafeTradeState(jobType byte) bool {
	return jobType == JobTrader
}

func (r *SQLSocialRepository) LeaveJob(ctx context.Context, characterID int, requestedFee uint32) (uint64, error) {
	var remainingGold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var level int
		var gold uint64
		if err := tx.QueryRowContext(ctx, "SELECT CURRENT_LEVEL,GOLD FROM `CHAR` WHERE id=? FOR UPDATE", characterID).Scan(&level, &gold); err != nil {
			return ErrSocialNotFound
		}
		cost, valid := jobLeaveFee(level)
		if !valid || requestedFee != cost {
			return ErrSocialInvalidRequest
		}
		if gold < uint64(cost) {
			return ErrSocialInsufficientGold
		}
		result, err := tx.ExecContext(ctx, "INSERT INTO CHARACTER_JOB_HISTORY (FK_CHAR,JOB_TYPE,ALIAS,JOB_LEVEL,JOB_EXP,CONTRIBUTION,JOINED_AT) SELECT FK_CHAR,JOB_TYPE,ALIAS,JOB_LEVEL,JOB_EXP,CONTRIBUTION,JOINED_AT FROM CHARACTER_JOB WHERE FK_CHAR=? AND (LEAVE_AVAILABLE_AT IS NULL OR LEAVE_AVAILABLE_AT<=CURRENT_TIMESTAMP(6))", characterID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM CHARACTER_SAFE_TRADE WHERE FK_CHAR=?", characterID); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD-? WHERE id=?", cost, characterID); err != nil {
			return err
		}
		result, err = tx.ExecContext(ctx, "DELETE FROM CHARACTER_JOB WHERE FK_CHAR=?", characterID)
		if err != nil {
			return err
		}
		if err = requireSocialRow(result); err != nil {
			return err
		}
		remainingGold = gold - uint64(cost)
		return nil
	})
	return remainingGold, err
}

func (r *SQLSocialRepository) AdvanceJob(ctx context.Context, characterID int, experience, contribution uint64) (JobSnapshot, error) {
	var snapshot JobSnapshot
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		var err error
		snapshot, err = advanceJobInTransaction(ctx, tx, characterID, experience, contribution)
		return err
	})
	return snapshot, err
}

func advanceJobInTransaction(ctx context.Context, tx *sql.Tx, characterID int, experience, contribution uint64) (JobSnapshot, error) {
	var snapshot JobSnapshot
	var jobType, level byte
	var currentExperience uint64
	if err := tx.QueryRowContext(ctx, "SELECT JOB_TYPE,JOB_LEVEL,JOB_EXP FROM CHARACTER_JOB WHERE FK_CHAR=? FOR UPDATE", characterID).Scan(&jobType, &level, &currentExperience); err != nil {
		return snapshot, ErrSocialNotFound
	}
	if jobType < JobTrader || jobType > JobHunter || level == 0 || level > 7 {
		return snapshot, ErrSocialConflict
	}
	currentExperience = jobSaturatingAdd(currentExperience, experience)
	for level < 7 {
		var required uint64
		err := tx.QueryRowContext(ctx, "SELECT CASE ? WHEN 1 THEN `REQUIRED_JOB_EXPERIENCE` WHEN 2 THEN `REQUIRED_JOB_EXPERIENCE_2` WHEN 3 THEN `REQUIRED_JOB_EXPERIENCE_3` END FROM `REF_LEVEL_DATA` WHERE `LEVEL`=?", jobType, level).Scan(&required)
		if errors.Is(err, sql.ErrNoRows) {
			return snapshot, ErrSocialRepositoryUnavailable
		}
		if err != nil || required == 0 || currentExperience < required {
			if err != nil {
				return snapshot, err
			}
			break
		}
		currentExperience -= required
		level++
	}
	updated, err := tx.ExecContext(ctx, "UPDATE CHARACTER_JOB SET JOB_LEVEL=?,JOB_EXP=?,CONTRIBUTION=LEAST(CONTRIBUTION+?,18446744073709551615),WEEKLY_CONTRIBUTION=LEAST(WEEKLY_CONTRIBUTION+?,18446744073709551615) WHERE FK_CHAR=?", level, currentExperience, contribution, contribution, characterID)
	if err != nil {
		return snapshot, err
	}
	if err = requireSocialRow(updated); err != nil {
		return snapshot, err
	}
	return snapshot, scanJob(tx.QueryRowContext(ctx, "SELECT "+jobSnapshotColumns+" FROM CHARACTER_JOB WHERE FK_CHAR=?", characterID), &snapshot)
}

func jobSaturatingAdd(left, right uint64) uint64 {
	if math.MaxUint64-left < right {
		return math.MaxUint64
	}
	return left + right
}

func CombatJobExperience(levels *LevelTable, characterLevel, targetLevel int, jobType byte) uint64 {
	if levels == nil || jobType < JobTrader || jobType > JobHunter {
		return 0
	}
	character, characterOK := levels.Level(characterLevel)
	target, targetOK := levels.Level(targetLevel)
	if !characterOK || !targetOK || character.JobActivityFactor == 0 || target.JobActivityFactor == 0 {
		return 0
	}
	characterFactor := int(float64(character.JobActivityFactor*10) * 0.125)
	targetFactor := int(float64(target.JobActivityFactor*10) * 0.125)
	if characterFactor <= 0 || targetFactor <= 0 {
		return 0
	}
	ratio := float64(targetFactor) / float64(characterFactor)
	ratio = max(0.5, min(ratio, 1.5))
	reward := float64(targetFactor) * ratio
	if jobType == JobTrader {
		reward *= 0.5
	}
	if reward <= 0 {
		return 1
	}
	return uint64(reward)
}

func PartyCombatJobExperience(base uint64, recipientLevel, totalLevel, recipients int) uint64 {
	if base == 0 || recipientLevel <= 0 || totalLevel <= 0 || recipients <= 0 {
		return 0
	}
	bonus := float64(recipients-1)*0.05000000074505806 + 1.0
	return uint64(float64(base) * (float64(recipientLevel) / float64(totalLevel) * bonus))
}

type weeklyJobRewardMember struct {
	characterID  int
	contribution uint64
	pending      uint64
}

func weeklyJobRewardAllocations(pool uint64, members []weeklyJobRewardMember) map[int]uint64 {
	allocations := make(map[int]uint64, len(members))
	var total uint64
	for _, member := range members {
		total = jobSaturatingAdd(total, member.contribution)
	}
	for _, member := range members {
		share := uint64(0)
		if pool != 0 && total != 0 && member.contribution != 0 {
			product := new(big.Int).Mul(new(big.Int).SetUint64(pool), new(big.Int).SetUint64(member.contribution))
			share = new(big.Int).Quo(product, new(big.Int).SetUint64(total)).Uint64()
		}
		pending := jobSaturatingAdd(member.pending, share)
		allocations[member.characterID] = min(pending, uint64(math.MaxInt32))
	}
	return allocations
}

func (r *SQLSocialRepository) DistributeWeeklyJobRewards(ctx context.Context) error {
	return r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, jobType := range []byte{JobThief, JobHunter} {
			var pool uint64
			if err := tx.QueryRowContext(ctx, "SELECT `REWARD` FROM `TRIJOB_REWARDS` WHERE `JOB_TYPE`=? FOR UPDATE", jobType).Scan(&pool); err != nil {
				return err
			}
			rows, err := tx.QueryContext(ctx, "SELECT `FK_CHAR`,`WEEKLY_CONTRIBUTION`,`PENDING_REWARD_GOLD` FROM `CHARACTER_JOB` WHERE `JOB_TYPE`=? ORDER BY `FK_CHAR` FOR UPDATE", jobType)
			if err != nil {
				return err
			}
			members := make([]weeklyJobRewardMember, 0)
			for rows.Next() {
				var member weeklyJobRewardMember
				if err = rows.Scan(&member.characterID, &member.contribution, &member.pending); err != nil {
					rows.Close()
					return err
				}
				members = append(members, member)
			}
			if err = rows.Close(); err != nil {
				return err
			}
			var totalContribution uint64
			for _, member := range members {
				totalContribution = jobSaturatingAdd(totalContribution, member.contribution)
			}
			if pool != 0 && totalContribution == 0 {
				return ErrSocialConflict
			}
			for characterID, pending := range weeklyJobRewardAllocations(pool, members) {
				if _, err = tx.ExecContext(ctx, "UPDATE `CHARACTER_JOB` SET `PENDING_REWARD_GOLD`=? WHERE `FK_CHAR`=?", pending, characterID); err != nil {
					return err
				}
			}
			if _, err = tx.ExecContext(ctx, "UPDATE `TRIJOB_REWARDS` SET `REWARD`=0 WHERE `JOB_TYPE`=?", jobType); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "UPDATE `CHARACTER_JOB` SET `WEEKLY_CONTRIBUTION`=0")
		return err
	})
}

func (r *SQLSocialRepository) JobAliasAvailable(ctx context.Context, alias string) (bool, error) {
	if err := validateSocialText("job alias", alias, 20, false); err != nil {
		return false, err
	}
	var count int
	err := r.shard.QueryRowContext(socialContext(ctx), "SELECT COUNT(*) FROM CHARACTER_JOB WHERE LOWER(ALIAS)=LOWER(?)", alias).Scan(&count)
	return count == 0, err
}

func (r *SQLSocialRepository) SetJobAlias(ctx context.Context, characterID int, alias string) error {
	if err := validateSocialText("job alias", alias, 20, false); err != nil {
		return err
	}
	result, err := r.shard.ExecContext(socialContext(ctx), "UPDATE CHARACTER_JOB SET ALIAS=? WHERE FK_CHAR=?", alias, characterID)
	if err != nil {
		return errors.Join(ErrSocialConflict, err)
	}
	return requireSocialRow(result)
}

func (r *SQLSocialRepository) JobRanking(ctx context.Context, jobType byte, weekly bool, limit int) ([]JobSnapshot, error) {
	if jobType < JobTrader || jobType > JobHunter {
		return nil, ErrSocialInvalidRequest
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	column := "CONTRIBUTION"
	if weekly {
		column = "WEEKLY_CONTRIBUTION"
	}
	query := fmt.Sprintf("SELECT %s FROM CHARACTER_JOB WHERE JOB_TYPE=? AND ALIAS IS NOT NULL ORDER BY %s DESC,JOB_EXP DESC,FK_CHAR LIMIT ?", jobSnapshotColumns, column)
	rows, err := r.shard.QueryContext(socialContext(ctx), query, jobType, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []JobSnapshot
	for rows.Next() {
		var j JobSnapshot
		if err = scanJob(rows, &j); err != nil {
			return nil, err
		}
		list = append(list, j)
	}
	return list, rows.Err()
}

func (r *SQLSocialRepository) ClaimJobReward(ctx context.Context, characterID int) (uint64, uint64, error) {
	var amount, gold uint64
	err := r.tx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, "SELECT PENDING_REWARD_GOLD FROM CHARACTER_JOB WHERE FK_CHAR=? FOR UPDATE", characterID).Scan(&amount); err != nil {
			return ErrSocialNotFound
		}
		if amount == 0 {
			return ErrSocialConflict
		}
		if _, err := tx.ExecContext(ctx, "UPDATE `CHAR` SET GOLD=GOLD+? WHERE id=?", amount, characterID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE CHARACTER_JOB SET PENDING_REWARD_GOLD=0 WHERE FK_CHAR=?", characterID); err != nil {
			return err
		}
		return tx.QueryRowContext(ctx, "SELECT GOLD FROM `CHAR` WHERE id=?", characterID).Scan(&gold)
	})
	return amount, gold, err
}

// SocialRecipients returns durable membership for chat fan-out. channel 5 is
// guild, 11 is union, and 16 is academy, matching the vSRO chat type values.
func (r *SQLSocialRepository) SocialRecipients(ctx context.Context, characterID int, channel byte) ([]int, error) {
	var query string
	switch channel {
	case 5:
		query = "SELECT gm2.FK_CHAR FROM GUILD_MEMBER gm JOIN GUILD_MEMBER gm2 ON gm2.FK_GUILD=gm.FK_GUILD WHERE gm.FK_CHAR=?"
	case 11:
		query = "SELECT gm2.FK_CHAR FROM GUILD_MEMBER gm JOIN GUILD_UNION_MEMBER um ON um.FK_GUILD=gm.FK_GUILD JOIN GUILD_UNION_MEMBER um2 ON um2.FK_UNION=um.FK_UNION JOIN GUILD_MEMBER gm2 ON gm2.FK_GUILD=um2.FK_GUILD WHERE gm.FK_CHAR=?"
	case 16:
		query = "SELECT am2.FK_CHAR FROM ACADEMY_MEMBER am JOIN ACADEMY_MEMBER am2 ON am2.FK_ACADEMY=am.FK_ACADEMY WHERE am.FK_CHAR=?"
	default:
		return nil, ErrSocialInvalidRequest
	}
	rows, err := r.shard.QueryContext(socialContext(ctx), query, characterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
