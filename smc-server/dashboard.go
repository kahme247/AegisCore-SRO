package main

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"sync"
	"time"
)

type ServiceHealth struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	State     string `json:"state"`
	LatencyMS int64  `json:"latencyMs"`
}

type ShardSummary struct {
	ID            int    `json:"id"`
	Name          string `json:"name"`
	ContentID     int    `json:"contentId"`
	Capacity      int    `json:"capacity"`
	OnlinePlayers int    `json:"onlinePlayers"`
	Status        int    `json:"status"`
}

type DashboardTotals struct {
	Accounts       int64 `json:"accounts"`
	ActiveAccounts int64 `json:"activeAccounts"`
	GameMasters    int64 `json:"gameMasters"`
	Characters     int64 `json:"characters"`
	Guilds         int64 `json:"guilds"`
	Items          int64 `json:"items"`
	Notices        int64 `json:"notices"`
}

type GameplayRateSnapshot struct {
	Experience                uint32 `json:"experience"`
	PartyExperience           uint32 `json:"partyExperience"`
	SkillExperience           uint32 `json:"skillExperience"`
	MobCount                  uint32 `json:"mobCount"`
	MobRespawn                uint32 `json:"mobRespawn"`
	LootDrop                  uint32 `json:"lootDrop"`
	GoldDropAmount            uint32 `json:"goldDropAmount"`
	PartyEXPRange             uint32 `json:"partyExpRange"`
	GroundPickupRange         uint32 `json:"groundPickupRange"`
	JobSuitEquipDelayMS       uint32 `json:"jobSuitEquipDelayMs"`
	PartyInvitationTTLMS      uint32 `json:"partyInvitationTtlMs"`
	MobAttackCooldownMS       uint32 `json:"mobAttackCooldownMs"`
	MobWanderDelayMS          uint32 `json:"mobWanderDelayMs"`
	MobAggroReactionMinMS     uint32 `json:"mobAggroReactionMinMs"`
	MobAggroReactionMaxMS     uint32 `json:"mobAggroReactionMaxMs"`
	MobPursuitRefreshMS       uint32 `json:"mobPursuitRefreshMs"`
	GuildCreationCost         uint64 `json:"guildCreationCost"`
	AcademyCreationCost       uint64 `json:"academyCreationCost"`
	AcademyMinimumLevel       uint32 `json:"academyMinimumLevel"`
	GuildWarMaximumWager      uint64 `json:"guildWarMaximumWager"`
	MarketLifetimeMinutes     uint32 `json:"marketLifetimeMinutes"`
	CharacterDeletionHours    uint32 `json:"characterDeletionHours"`
	RecallConfirmationTTLMS   uint32 `json:"recallConfirmationTtlMs"`
	GuildElectionDurationMin  uint32 `json:"guildElectionDurationMin"`
	GuildMasterAbsenceDays    uint32 `json:"guildMasterAbsenceDays"`
	GuildRecentMemberDays     uint32 `json:"guildRecentMemberDays"`
	FriendLimit               uint32 `json:"friendLimit"`
	BlockLimit                uint32 `json:"blockLimit"`
	MemoLimit                 uint32 `json:"memoLimit"`
	PKPenaltyPerKill          uint32 `json:"pkPenaltyPerKill"`
	BerserkDurationPerPointMS uint32 `json:"berserkDurationPerPointMs"`
	WinterDropChanceBP        uint32 `json:"winterDropChanceBp"`
	WinterDropItemCount       uint32 `json:"winterDropItemCount"`
	BattleArenaTicketRate     uint32 `json:"battleArenaTicketRate"`
	BattleArenaWorldIDs       string `json:"battleArenaWorldIds"`
	HwanBaseChanceBP          uint32 `json:"hwanBaseChanceBp"`
	HwanOverlevelGrace        uint32 `json:"hwanOverlevelGrace"`
	HwanPenaltyPerLevelBP     uint32 `json:"hwanPenaltyPerLevelBp"`
	HwanMinimumChanceBP       uint32 `json:"hwanMinimumChanceBp"`
	HwanGiantMultiplier       uint32 `json:"hwanGiantMultiplier"`
	HwanEliteMultiplier       uint32 `json:"hwanEliteMultiplier"`
	ServerMaxLevel            uint32 `json:"serverMaxLevel"`
	EUMasteryMaxTotal         uint32 `json:"euMasteryMaxTotal"`
	EUMasteryPointsPerLevel   uint32 `json:"euMasteryPointsPerLevel"`
	CHMasteryMaxTotal         uint32 `json:"chMasteryMaxTotal"`
	CHMasteryPointsPerLevel   uint32 `json:"chMasteryPointsPerLevel"`
	MaxSpeedEffectMinutes     uint32 `json:"maxSpeedEffectMinutes"`
}

type RuntimeGameplaySettings struct {
	PartyEXPRange             uint32 `json:"partyExpRange"`
	GroundPickupRange         uint32 `json:"groundPickupRange"`
	JobSuitEquipDelayMS       uint32 `json:"jobSuitEquipDelayMs"`
	PartyInvitationTTLMS      uint32 `json:"partyInvitationTtlMs"`
	MobAttackCooldownMS       uint32 `json:"mobAttackCooldownMs"`
	MobWanderDelayMS          uint32 `json:"mobWanderDelayMs"`
	MobAggroReactionMinMS     uint32 `json:"mobAggroReactionMinMs"`
	MobAggroReactionMaxMS     uint32 `json:"mobAggroReactionMaxMs"`
	MobPursuitRefreshMS       uint32 `json:"mobPursuitRefreshMs"`
	GuildCreationCost         uint64 `json:"guildCreationCost"`
	AcademyCreationCost       uint64 `json:"academyCreationCost"`
	AcademyMinimumLevel       uint32 `json:"academyMinimumLevel"`
	GuildWarMaximumWager      uint64 `json:"guildWarMaximumWager"`
	MarketLifetimeMinutes     uint32 `json:"marketLifetimeMinutes"`
	CharacterDeletionHours    uint32 `json:"characterDeletionHours"`
	RecallConfirmationTTLMS   uint32 `json:"recallConfirmationTtlMs"`
	GuildElectionDurationMin  uint32 `json:"guildElectionDurationMin"`
	GuildMasterAbsenceDays    uint32 `json:"guildMasterAbsenceDays"`
	GuildRecentMemberDays     uint32 `json:"guildRecentMemberDays"`
	FriendLimit               uint32 `json:"friendLimit"`
	BlockLimit                uint32 `json:"blockLimit"`
	MemoLimit                 uint32 `json:"memoLimit"`
	PKPenaltyPerKill          uint32 `json:"pkPenaltyPerKill"`
	BerserkDurationPerPointMS uint32 `json:"berserkDurationPerPointMs"`
	WinterDropChanceBP        uint32 `json:"winterDropChanceBp"`
	WinterDropItemCount       uint32 `json:"winterDropItemCount"`
	BattleArenaTicketRate     uint32 `json:"battleArenaTicketRate"`
	BattleArenaWorldIDs       string `json:"battleArenaWorldIds"`
	HwanBaseChanceBP          uint32 `json:"hwanBaseChanceBp"`
	HwanOverlevelGrace        uint32 `json:"hwanOverlevelGrace"`
	HwanPenaltyPerLevelBP     uint32 `json:"hwanPenaltyPerLevelBp"`
	HwanMinimumChanceBP       uint32 `json:"hwanMinimumChanceBp"`
	HwanGiantMultiplier       uint32 `json:"hwanGiantMultiplier"`
	HwanEliteMultiplier       uint32 `json:"hwanEliteMultiplier"`
	ServerMaxLevel            uint32 `json:"serverMaxLevel"`
	EUMasteryMaxTotal         uint32 `json:"euMasteryMaxTotal"`
	EUMasteryPointsPerLevel   uint32 `json:"euMasteryPointsPerLevel"`
	CHMasteryMaxTotal         uint32 `json:"chMasteryMaxTotal"`
	CHMasteryPointsPerLevel   uint32 `json:"chMasteryPointsPerLevel"`
	MaxSpeedEffectMinutes     uint32 `json:"maxSpeedEffectMinutes"`
}

type RuntimeGameplaySettingsWriter interface {
	UpdateRuntimeGameplaySettings(context.Context, RuntimeGameplaySettings) error
}

func (r *DashboardRepository) UpdateRuntimeGameplaySettings(ctx context.Context, s RuntimeGameplaySettings) error {
	if s.PartyEXPRange == 0 || s.GroundPickupRange == 0 || s.JobSuitEquipDelayMS < 1000 || s.JobSuitEquipDelayMS > 255000 || s.PartyInvitationTTLMS == 0 || s.MobAttackCooldownMS == 0 || s.MobWanderDelayMS == 0 || s.MobAggroReactionMinMS == 0 || s.MobAggroReactionMaxMS < s.MobAggroReactionMinMS || s.MobPursuitRefreshMS == 0 || s.GuildCreationCost == 0 || s.AcademyCreationCost == 0 || s.AcademyMinimumLevel == 0 || s.AcademyMinimumLevel > 255 || s.GuildWarMaximumWager == 0 || s.GuildWarMaximumWager > 4294967295 || s.MarketLifetimeMinutes == 0 || s.CharacterDeletionHours == 0 || s.RecallConfirmationTTLMS == 0 || s.GuildElectionDurationMin == 0 || s.GuildMasterAbsenceDays == 0 || s.GuildRecentMemberDays == 0 || s.FriendLimit == 0 || s.FriendLimit > 255 || s.BlockLimit == 0 || s.BlockLimit > 255 || s.MemoLimit == 0 || s.MemoLimit > 255 || s.PKPenaltyPerKill == 0 || s.PKPenaltyPerKill > 2147483647 || s.BerserkDurationPerPointMS == 0 || s.WinterDropChanceBP > 10000 || s.WinterDropItemCount == 0 || s.BattleArenaTicketRate > 100 || len(s.BattleArenaWorldIDs) > 255 || s.HwanBaseChanceBP > 10000 || s.HwanPenaltyPerLevelBP > 10000 || s.HwanMinimumChanceBP > 10000 || s.HwanGiantMultiplier == 0 || s.HwanEliteMultiplier == 0 || s.ServerMaxLevel == 0 || s.ServerMaxLevel > 255 || s.EUMasteryMaxTotal == 0 || s.EUMasteryPointsPerLevel == 0 || s.CHMasteryMaxTotal == 0 || s.CHMasteryPointsPerLevel == 0 || s.MaxSpeedEffectMinutes == 0 {
		return fmt.Errorf("invalid runtime gameplay settings")
	}
	_, err := r.shard.ExecContext(ctx, `UPDATE GAMEPLAY_MOB_RATES SET PARTY_EXP_RANGE=?,GROUND_PICKUP_RANGE=?,JOB_SUIT_EQUIP_DELAY_MS=?,PARTY_INVITATION_TTL_MS=?,MOB_ATTACK_COOLDOWN_MS=?,MOB_WANDER_DELAY_MS=?,MOB_AGGRO_REACTION_MIN_MS=?,MOB_AGGRO_REACTION_MAX_MS=?,MOB_PURSUIT_REFRESH_MS=?,GUILD_CREATION_COST=?,ACADEMY_CREATION_COST=?,ACADEMY_MIN_LEVEL=?,GUILD_WAR_MAX_WAGER=?,MARKET_LIFETIME_MINUTES=?,CHARACTER_DELETION_RETENTION_HOURS=?,RECALL_CONFIRMATION_TTL_MS=?,GUILD_ELECTION_DURATION_MINUTES=?,GUILD_MASTER_ABSENCE_DAYS=?,GUILD_ELECTION_RECENT_MEMBER_DAYS=?,FRIEND_LIMIT=?,BLOCK_LIMIT=?,MEMO_LIMIT=?,PK_PENALTY_PER_KILL=?,BERSERK_DURATION_PER_POINT_MS=?,WINTER_DROP_CHANCE_BP=?,WINTER_DROP_ITEM_COUNT=?,BATTLE_ARENA_TICKET_RATE=?,BATTLE_ARENA_WORLD_IDS=?,HWAN_BASE_CHANCE_BP=?,HWAN_OVERLEVEL_GRACE=?,HWAN_PENALTY_PER_LEVEL_BP=?,HWAN_MIN_CHANCE_BP=?,HWAN_GIANT_MULTIPLIER=?,HWAN_ELITE_MULTIPLIER=?,SERVER_MAX_LEVEL=?,EU_MASTERY_MAX_TOTAL=?,EU_MASTERY_POINTS_PER_LEVEL=?,CH_MASTERY_MAX_TOTAL=?,CH_MASTERY_POINTS_PER_LEVEL=?,MAX_SPEED_EFFECT_MINUTES=? WHERE SETTING_ID=1`, s.PartyEXPRange, s.GroundPickupRange, s.JobSuitEquipDelayMS, s.PartyInvitationTTLMS, s.MobAttackCooldownMS, s.MobWanderDelayMS, s.MobAggroReactionMinMS, s.MobAggroReactionMaxMS, s.MobPursuitRefreshMS, s.GuildCreationCost, s.AcademyCreationCost, s.AcademyMinimumLevel, s.GuildWarMaximumWager, s.MarketLifetimeMinutes, s.CharacterDeletionHours, s.RecallConfirmationTTLMS, s.GuildElectionDurationMin, s.GuildMasterAbsenceDays, s.GuildRecentMemberDays, s.FriendLimit, s.BlockLimit, s.MemoLimit, s.PKPenaltyPerKill, s.BerserkDurationPerPointMS, s.WinterDropChanceBP, s.WinterDropItemCount, s.BattleArenaTicketRate, s.BattleArenaWorldIDs, s.HwanBaseChanceBP, s.HwanOverlevelGrace, s.HwanPenaltyPerLevelBP, s.HwanMinimumChanceBP, s.HwanGiantMultiplier, s.HwanEliteMultiplier, s.ServerMaxLevel, s.EUMasteryMaxTotal, s.EUMasteryPointsPerLevel, s.CHMasteryMaxTotal, s.CHMasteryPointsPerLevel, s.MaxSpeedEffectMinutes)
	return err
}

type DashboardSnapshot struct {
	GeneratedAt   time.Time            `json:"generatedAt"`
	Totals        DashboardTotals      `json:"totals"`
	Services      []ServiceHealth      `json:"services"`
	Shards        []ShardSummary       `json:"shards"`
	GameplayRates GameplayRateSnapshot `json:"gameplayRates"`
}

type DashboardSource interface {
	Snapshot(context.Context) (DashboardSnapshot, error)
}

type DashboardRepository struct {
	account        *sql.DB
	shard          *sql.DB
	gatewayAddress string
	agentAddress   string
	probeTimeout   time.Duration
}

func NewDashboardRepository(account, shard *sql.DB, gatewayAddress, agentAddress string) (*DashboardRepository, error) {
	if account == nil || shard == nil {
		return nil, fmt.Errorf("SMC dashboard requires account and shard database pools")
	}
	return &DashboardRepository{
		account: account, shard: shard,
		gatewayAddress: gatewayAddress, agentAddress: agentAddress,
		probeTimeout: 750 * time.Millisecond,
	}, nil
}

func (r *DashboardRepository) Snapshot(ctx context.Context) (DashboardSnapshot, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	snapshot := DashboardSnapshot{GeneratedAt: time.Now().UTC()}
	const accountTotalsQuery = "SELECT COUNT(*), " +
		"COALESCE(SUM(CASE WHEN STATUS = 1 THEN 1 ELSE 0 END), 0), " +
		"COALESCE(SUM(CASE WHEN IS_GM = 1 THEN 1 ELSE 0 END), 0) " +
		"FROM `USER`"
	if err := r.account.QueryRowContext(ctx, accountTotalsQuery).Scan(
		&snapshot.Totals.Accounts,
		&snapshot.Totals.ActiveAccounts,
		&snapshot.Totals.GameMasters,
	); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC account totals: %w", err)
	}
	if err := r.account.QueryRowContext(ctx, `SELECT COUNT(*) FROM NOTICE`).Scan(&snapshot.Totals.Notices); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC notice total: %w", err)
	}
	rows, err := r.account.QueryContext(ctx, `
		SELECT ID, NAME, CONTENT_ID, CAPACITY, ONLINE_PLAYERS, STATUS
		FROM SHARD ORDER BY ID`)
	if err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC shards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var item ShardSummary
		if err := rows.Scan(&item.ID, &item.Name, &item.ContentID, &item.Capacity, &item.OnlinePlayers, &item.Status); err != nil {
			return DashboardSnapshot{}, fmt.Errorf("scan SMC shard: %w", err)
		}
		snapshot.Shards = append(snapshot.Shards, item)
	}
	if err := rows.Err(); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("iterate SMC shards: %w", err)
	}
	if err := r.shard.QueryRowContext(ctx, `
		SELECT (SELECT COUNT(*) FROM `+"`CHAR`"+`),
		       (SELECT COUNT(*) FROM GUILD),
		       (SELECT COUNT(*) FROM ITEM)`).Scan(
		&snapshot.Totals.Characters,
		&snapshot.Totals.Guilds,
		&snapshot.Totals.Items,
	); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC shard totals: %w", err)
	}
	if err := r.shard.QueryRowContext(ctx, `SELECT COALESCE(o.EXPERIENCE_RATE_PERCENT,p.EXPERIENCE_RATE_PERCENT),COALESCE(o.PARTY_EXPERIENCE_RATE_PERCENT,p.PARTY_EXPERIENCE_RATE_PERCENT),COALESCE(o.SKILL_EXPERIENCE_RATE_PERCENT,p.SKILL_EXPERIENCE_RATE_PERCENT),COALESCE(mo.MOB_COUNT_RATE_PERCENT,m.MOB_COUNT_RATE_PERCENT),COALESCE(mo.MOB_RESPAWN_RATE_PERCENT,m.MOB_RESPAWN_RATE_PERCENT),COALESCE(mo.LOOT_DROP_RATE_PERCENT,m.LOOT_DROP_RATE_PERCENT),m.GOLD_DROP_AMOUNT_RATE_PERCENT,m.PARTY_EXP_RANGE,m.GROUND_PICKUP_RANGE,m.JOB_SUIT_EQUIP_DELAY_MS,m.PARTY_INVITATION_TTL_MS,m.MOB_ATTACK_COOLDOWN_MS,m.MOB_WANDER_DELAY_MS,m.MOB_AGGRO_REACTION_MIN_MS,m.MOB_AGGRO_REACTION_MAX_MS,m.MOB_PURSUIT_REFRESH_MS FROM GAMEPLAY_PROGRESSION_RATES p JOIN GAMEPLAY_MOB_RATES m ON m.SETTING_ID=p.SETTING_ID LEFT JOIN AEGIS_GAMEPLAY_RATE_OVERRIDE o ON o.EVENT_ID=(SELECT EVENT_ID FROM AEGIS_GAMEPLAY_RATE_OVERRIDE ORDER BY ACTIVATED_AT DESC,EVENT_ID DESC LIMIT 1) LEFT JOIN AEGIS_GAMEPLAY_MOB_RATE_OVERRIDE mo ON mo.EVENT_ID=(SELECT EVENT_ID FROM AEGIS_GAMEPLAY_MOB_RATE_OVERRIDE ORDER BY ACTIVATED_AT DESC,EVENT_ID DESC LIMIT 1) WHERE p.SETTING_ID=1`).Scan(&snapshot.GameplayRates.Experience, &snapshot.GameplayRates.PartyExperience, &snapshot.GameplayRates.SkillExperience, &snapshot.GameplayRates.MobCount, &snapshot.GameplayRates.MobRespawn, &snapshot.GameplayRates.LootDrop, &snapshot.GameplayRates.GoldDropAmount, &snapshot.GameplayRates.PartyEXPRange, &snapshot.GameplayRates.GroundPickupRange, &snapshot.GameplayRates.JobSuitEquipDelayMS, &snapshot.GameplayRates.PartyInvitationTTLMS, &snapshot.GameplayRates.MobAttackCooldownMS, &snapshot.GameplayRates.MobWanderDelayMS, &snapshot.GameplayRates.MobAggroReactionMinMS, &snapshot.GameplayRates.MobAggroReactionMaxMS, &snapshot.GameplayRates.MobPursuitRefreshMS); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC effective gameplay rates: %w", err)
	}
	if err := r.shard.QueryRowContext(ctx, `SELECT GUILD_CREATION_COST,ACADEMY_CREATION_COST,ACADEMY_MIN_LEVEL,GUILD_WAR_MAX_WAGER,MARKET_LIFETIME_MINUTES,CHARACTER_DELETION_RETENTION_HOURS,RECALL_CONFIRMATION_TTL_MS,GUILD_ELECTION_DURATION_MINUTES,GUILD_MASTER_ABSENCE_DAYS,GUILD_ELECTION_RECENT_MEMBER_DAYS,FRIEND_LIMIT,BLOCK_LIMIT,MEMO_LIMIT,PK_PENALTY_PER_KILL,BERSERK_DURATION_PER_POINT_MS,WINTER_DROP_CHANCE_BP,WINTER_DROP_ITEM_COUNT,BATTLE_ARENA_TICKET_RATE,BATTLE_ARENA_WORLD_IDS,HWAN_BASE_CHANCE_BP,HWAN_OVERLEVEL_GRACE,HWAN_PENALTY_PER_LEVEL_BP,HWAN_MIN_CHANCE_BP,HWAN_GIANT_MULTIPLIER,HWAN_ELITE_MULTIPLIER,SERVER_MAX_LEVEL,EU_MASTERY_MAX_TOTAL,EU_MASTERY_POINTS_PER_LEVEL,CH_MASTERY_MAX_TOTAL,CH_MASTERY_POINTS_PER_LEVEL,MAX_SPEED_EFFECT_MINUTES FROM GAMEPLAY_MOB_RATES WHERE SETTING_ID=1`).Scan(&snapshot.GameplayRates.GuildCreationCost, &snapshot.GameplayRates.AcademyCreationCost, &snapshot.GameplayRates.AcademyMinimumLevel, &snapshot.GameplayRates.GuildWarMaximumWager, &snapshot.GameplayRates.MarketLifetimeMinutes, &snapshot.GameplayRates.CharacterDeletionHours, &snapshot.GameplayRates.RecallConfirmationTTLMS, &snapshot.GameplayRates.GuildElectionDurationMin, &snapshot.GameplayRates.GuildMasterAbsenceDays, &snapshot.GameplayRates.GuildRecentMemberDays, &snapshot.GameplayRates.FriendLimit, &snapshot.GameplayRates.BlockLimit, &snapshot.GameplayRates.MemoLimit, &snapshot.GameplayRates.PKPenaltyPerKill, &snapshot.GameplayRates.BerserkDurationPerPointMS, &snapshot.GameplayRates.WinterDropChanceBP, &snapshot.GameplayRates.WinterDropItemCount, &snapshot.GameplayRates.BattleArenaTicketRate, &snapshot.GameplayRates.BattleArenaWorldIDs, &snapshot.GameplayRates.HwanBaseChanceBP, &snapshot.GameplayRates.HwanOverlevelGrace, &snapshot.GameplayRates.HwanPenaltyPerLevelBP, &snapshot.GameplayRates.HwanMinimumChanceBP, &snapshot.GameplayRates.HwanGiantMultiplier, &snapshot.GameplayRates.HwanEliteMultiplier, &snapshot.GameplayRates.ServerMaxLevel, &snapshot.GameplayRates.EUMasteryMaxTotal, &snapshot.GameplayRates.EUMasteryPointsPerLevel, &snapshot.GameplayRates.CHMasteryMaxTotal, &snapshot.GameplayRates.CHMasteryPointsPerLevel, &snapshot.GameplayRates.MaxSpeedEffectMinutes); err != nil {
		return DashboardSnapshot{}, fmt.Errorf("load SMC runtime gameplay policy: %w", err)
	}

	snapshot.Services = make([]ServiceHealth, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	go func() {
		defer wait.Done()
		snapshot.Services[0] = r.probe("gateway", "Gateway", r.gatewayAddress)
	}()
	go func() {
		defer wait.Done()
		snapshot.Services[1] = r.probe("agent", "Agent", r.agentAddress)
	}()
	wait.Wait()
	return snapshot, nil
}

func (r *DashboardRepository) probe(id, name, address string) ServiceHealth {
	started := time.Now()
	connection, err := net.DialTimeout("tcp", address, r.probeTimeout)
	latency := time.Since(started).Milliseconds()
	if err != nil {
		return ServiceHealth{ID: id, Name: name, Address: address, State: "offline", LatencyMS: latency}
	}
	_ = connection.Close()
	return ServiceHealth{ID: id, Name: name, Address: address, State: "reachable", LatencyMS: latency}
}
