package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

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

	AcademyRepository

	JobRepository
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
