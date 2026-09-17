package model

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

const SelectGameplayRates = "SELECT COALESCE(o.`EXPERIENCE_RATE_PERCENT`,p.`EXPERIENCE_RATE_PERCENT`)+COALESCE(se.`EXP_BONUS`,0), COALESCE(o.`PARTY_EXPERIENCE_RATE_PERCENT`,p.`PARTY_EXPERIENCE_RATE_PERCENT`)+COALESCE(se.`EXP_BONUS`,0), COALESCE(o.`SKILL_EXPERIENCE_RATE_PERCENT`,p.`SKILL_EXPERIENCE_RATE_PERCENT`)+COALESCE(se.`SKILL_EXP_BONUS`,0), COALESCE(mo.`MOB_COUNT_RATE_PERCENT`,m.`MOB_COUNT_RATE_PERCENT`), COALESCE(mo.`MOB_RESPAWN_RATE_PERCENT`,m.`MOB_RESPAWN_RATE_PERCENT`), COALESCE(mo.`LOOT_DROP_RATE_PERCENT`,m.`LOOT_DROP_RATE_PERCENT`), m.`GOLD_DROP_AMOUNT_RATE_PERCENT`, m.`PARTY_EXP_RANGE`, m.`GROUND_PICKUP_RANGE`, m.`JOB_SUIT_EQUIP_DELAY_MS`, m.`PARTY_INVITATION_TTL_MS`, m.`MOB_ATTACK_COOLDOWN_MS`, m.`MOB_WANDER_DELAY_MS`, m.`MOB_AGGRO_REACTION_MIN_MS`, m.`MOB_AGGRO_REACTION_MAX_MS`, m.`MOB_PURSUIT_REFRESH_MS` FROM `SRO_SHARD`.`GAMEPLAY_PROGRESSION_RATES` p JOIN `SRO_SHARD`.`GAMEPLAY_MOB_RATES` m ON m.`SETTING_ID`=p.`SETTING_ID` LEFT JOIN `SRO_SHARD`.`AEGIS_GAMEPLAY_RATE_OVERRIDE` o ON o.`EVENT_ID`=(SELECT newest.`EVENT_ID` FROM `SRO_SHARD`.`AEGIS_GAMEPLAY_RATE_OVERRIDE` newest ORDER BY newest.`ACTIVATED_AT` DESC,newest.`EVENT_ID` DESC LIMIT 1) LEFT JOIN `SRO_SHARD`.`AEGIS_GAMEPLAY_MOB_RATE_OVERRIDE` mo ON mo.`EVENT_ID`=(SELECT newest.`EVENT_ID` FROM `SRO_SHARD`.`AEGIS_GAMEPLAY_MOB_RATE_OVERRIDE` newest ORDER BY newest.`ACTIVATED_AT` DESC,newest.`EVENT_ID` DESC LIMIT 1) LEFT JOIN (SELECT LEAST(100,COALESCE(SUM(GREATEST(CAST(`EXP_RATE_PERCENT` AS SIGNED)-100,0)),0)) `EXP_BONUS`,LEAST(100,COALESCE(SUM(GREATEST(CAST(`SKILL_EXP_RATE_PERCENT` AS SIGNED)-100,0)),0)) `SKILL_EXP_BONUS` FROM `SRO_SHARD`.`SERVER_EVENT_TIMED_EFFECT` WHERE `ROLLED_BACK_AT` IS NULL AND `ENDS_AT`>CURRENT_TIMESTAMP(6)) se ON TRUE WHERE p.`SETTING_ID`=1"

const activateGameplayRateOverride = "INSERT INTO `SRO_SHARD`.`AEGIS_GAMEPLAY_RATE_OVERRIDE` (`EVENT_ID`,`EXPERIENCE_RATE_PERCENT`,`PARTY_EXPERIENCE_RATE_PERCENT`,`SKILL_EXPERIENCE_RATE_PERCENT`,`ACTIVATED_AT`) VALUES (?,?,?,?,CURRENT_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE `EXPERIENCE_RATE_PERCENT`=VALUES(`EXPERIENCE_RATE_PERCENT`),`PARTY_EXPERIENCE_RATE_PERCENT`=VALUES(`PARTY_EXPERIENCE_RATE_PERCENT`),`SKILL_EXPERIENCE_RATE_PERCENT`=VALUES(`SKILL_EXPERIENCE_RATE_PERCENT`),`ACTIVATED_AT`=VALUES(`ACTIVATED_AT`)"
const deactivateGameplayRateOverride = "DELETE FROM `SRO_SHARD`.`AEGIS_GAMEPLAY_RATE_OVERRIDE` WHERE `EVENT_ID`=?"
const activateGameplayMobRateOverride = "INSERT INTO `SRO_SHARD`.`AEGIS_GAMEPLAY_MOB_RATE_OVERRIDE` (`EVENT_ID`,`MOB_COUNT_RATE_PERCENT`,`MOB_RESPAWN_RATE_PERCENT`,`LOOT_DROP_RATE_PERCENT`,`ACTIVATED_AT`) VALUES (?,?,?,?,CURRENT_TIMESTAMP(6)) ON DUPLICATE KEY UPDATE `MOB_COUNT_RATE_PERCENT`=VALUES(`MOB_COUNT_RATE_PERCENT`),`MOB_RESPAWN_RATE_PERCENT`=VALUES(`MOB_RESPAWN_RATE_PERCENT`),`LOOT_DROP_RATE_PERCENT`=VALUES(`LOOT_DROP_RATE_PERCENT`),`ACTIVATED_AT`=VALUES(`ACTIVATED_AT`)"
const deactivateGameplayMobRateOverride = "DELETE FROM `SRO_SHARD`.`AEGIS_GAMEPLAY_MOB_RATE_OVERRIDE` WHERE `EVENT_ID`=?"
const selectRuntimePolicy = "SELECT `GUILD_CREATION_COST`,`ACADEMY_CREATION_COST`,`ACADEMY_MIN_LEVEL`,`GUILD_WAR_MAX_WAGER`,`MARKET_LIFETIME_MINUTES`,`CHARACTER_DELETION_RETENTION_HOURS`,`RECALL_CONFIRMATION_TTL_MS`,`GUILD_ELECTION_DURATION_MINUTES`,`GUILD_MASTER_ABSENCE_DAYS`,`GUILD_ELECTION_RECENT_MEMBER_DAYS`,`FRIEND_LIMIT`,`BLOCK_LIMIT`,`MEMO_LIMIT`,`PK_PENALTY_PER_KILL`,`BERSERK_DURATION_PER_POINT_MS`,`WINTER_DROP_CHANCE_BP`,`WINTER_DROP_ITEM_COUNT`,`BATTLE_ARENA_TICKET_RATE`,`BATTLE_ARENA_WORLD_IDS`,`HWAN_BASE_CHANCE_BP`,`HWAN_OVERLEVEL_GRACE`,`HWAN_PENALTY_PER_LEVEL_BP`,`HWAN_MIN_CHANCE_BP`,`HWAN_GIANT_MULTIPLIER`,`HWAN_ELITE_MULTIPLIER`,`SERVER_MAX_LEVEL`,`EU_MASTERY_MAX_TOTAL`,`EU_MASTERY_POINTS_PER_LEVEL`,`CH_MASTERY_MAX_TOTAL`,`CH_MASTERY_POINTS_PER_LEVEL`,`MAX_SPEED_EFFECT_MINUTES` FROM `SRO_SHARD`.`GAMEPLAY_MOB_RATES` WHERE `SETTING_ID`=1"

var ErrGameplayRateRepositoryUnavailable = errors.New("gameplay rate repository is unavailable")
var ErrGameplayMobRatesInvalid = errors.New("gameplay mob rates must be positive")

type GameplayRates struct {
	ExperienceRatePercent      uint32
	PartyExperienceRatePercent uint32
	SkillExperienceRatePercent uint32
	MobCountRatePercent        uint32
	MobRespawnRatePercent      uint32
	LootDropRatePercent        uint32
	GoldDropAmountRatePercent  uint32
	PartyExperienceRange       uint32
	GroundPickupRange          uint32
	JobSuitEquipDelayMS        uint32
	PartyInvitationTTLMS       uint32
	MobAttackCooldownMS        uint32
	MobWanderDelayMS           uint32
	MobAggroReactionMinMS      uint32
	MobAggroReactionMaxMS      uint32
	MobPursuitRefreshMS        uint32
	GuildCreationCost          uint64
	AcademyCreationCost        uint64
	AcademyMinimumLevel        uint32
	GuildWarMaximumWager       uint64
	MarketLifetimeMinutes      uint32
	CharacterDeletionHours     uint32
	RecallConfirmationTTLMS    uint32
	GuildElectionDurationMin   uint32
	GuildMasterAbsenceDays     uint32
	GuildRecentMemberDays      uint32
	FriendLimit                uint32
	BlockLimit                 uint32
	MemoLimit                  uint32
	PKPenaltyPerKill           uint32
	BerserkDurationPerPointMS  uint32
	WinterDropChanceBP         uint32
	WinterDropItemCount        uint32
	BattleArenaTicketRate      uint32
	BattleArenaWorldIDs        string
	HwanBaseChanceBP           uint32
	HwanOverlevelGrace         uint32
	HwanPenaltyPerLevelBP      uint32
	HwanMinimumChanceBP        uint32
	HwanGiantMultiplier        uint32
	HwanEliteMultiplier        uint32
	ServerMaxLevel             uint32
	EUMasteryMaxTotal          uint32
	EUMasteryPointsPerLevel    uint32
	CHMasteryMaxTotal          uint32
	CHMasteryPointsPerLevel    uint32
	MaxSpeedEffectMinutes      uint32
}

var runtimeGameplayPolicy atomic.Pointer[GameplayRates]

func StoreRuntimeGameplayPolicy(rates GameplayRates) { runtimeGameplayPolicy.Store(&rates) }
func RuntimeGameplayPolicy() GameplayRates {
	if value := runtimeGameplayPolicy.Load(); value != nil {
		return *value
	}
	return GameplayRates{}
}
func RuntimeGameplayPolicyLoaded() bool { return runtimeGameplayPolicy.Load() != nil }
func RuntimeGuildCreationCost() uint64 {
	if v := RuntimeGameplayPolicy().GuildCreationCost; v > 0 {
		return v
	}
	return 500000
}
func RuntimeAcademyCreationCost() uint64 {
	if v := RuntimeGameplayPolicy().AcademyCreationCost; v > 0 {
		return v
	}
	return 100000
}
func RuntimeAcademyMinimumLevel() int {
	if v := RuntimeGameplayPolicy().AcademyMinimumLevel; v > 0 {
		return int(v)
	}
	return 60
}
func RuntimeGuildWarMaximumWager() uint32 {
	if v := RuntimeGameplayPolicy().GuildWarMaximumWager; v > 0 && v <= uint64(^uint32(0)) {
		return uint32(v)
	}
	return 500000000
}
func RuntimeMarketLifetime() time.Duration {
	if v := RuntimeGameplayPolicy().MarketLifetimeMinutes; v > 0 {
		return time.Duration(v) * time.Minute
	}
	return 72 * time.Hour
}
func RuntimeCharacterDeletionRetention() time.Duration {
	if v := RuntimeGameplayPolicy().CharacterDeletionHours; v > 0 {
		return time.Duration(v) * time.Hour
	}
	return 7 * 24 * time.Hour
}
func RuntimeRecallConfirmationTTL() time.Duration {
	if v := RuntimeGameplayPolicy().RecallConfirmationTTLMS; v > 0 {
		return time.Duration(v) * time.Millisecond
	}
	return time.Minute
}
func RuntimeGuildElectionDuration() time.Duration {
	if v := RuntimeGameplayPolicy().GuildElectionDurationMin; v > 0 {
		return time.Duration(v) * time.Minute
	}
	return 10 * time.Minute
}
func RuntimeGuildMasterAbsence() time.Duration {
	if v := RuntimeGameplayPolicy().GuildMasterAbsenceDays; v > 0 {
		return time.Duration(v) * 24 * time.Hour
	}
	return 45 * 24 * time.Hour
}
func RuntimeGuildRecentMemberWindow() time.Duration {
	if v := RuntimeGameplayPolicy().GuildRecentMemberDays; v > 0 {
		return time.Duration(v) * 24 * time.Hour
	}
	return 15 * 24 * time.Hour
}
func RuntimeFriendLimit() int {
	if v := RuntimeGameplayPolicy().FriendLimit; v > 0 {
		return int(v)
	}
	return 20
}
func RuntimeBlockLimit() int {
	if v := RuntimeGameplayPolicy().BlockLimit; v > 0 {
		return int(v)
	}
	return 20
}
func RuntimeMemoLimit() int {
	if v := RuntimeGameplayPolicy().MemoLimit; v > 0 {
		return int(v)
	}
	return 20
}
func RuntimePKPenaltyPerKill() int32 {
	if v := RuntimeGameplayPolicy().PKPenaltyPerKill; v > 0 && v <= uint32(^uint32(0)>>1) {
		return int32(v)
	}
	return 10000
}
func RuntimeBerserkDurationPerPoint() time.Duration {
	if v := RuntimeGameplayPolicy().BerserkDurationPerPointMS; v > 0 {
		return time.Duration(v) * time.Millisecond
	}
	return 12 * time.Second
}
func RuntimeWinterDropChanceBP() uint32 {
	if RuntimeGameplayPolicyLoaded() {
		return RuntimeGameplayPolicy().WinterDropChanceBP
	}
	return 5000
}
func RuntimeWinterDropItemCount() uint32 {
	if v := RuntimeGameplayPolicy().WinterDropItemCount; v > 0 {
		return v
	}
	return 1
}
func RuntimeBattleArenaTicketRate() int {
	if RuntimeGameplayPolicyLoaded() {
		return int(RuntimeGameplayPolicy().BattleArenaTicketRate)
	}
	return 0
}
func RuntimeBattleArenaWorldIDs() string {
	if v := RuntimeGameplayPolicy().BattleArenaWorldIDs; v != "" {
		return v
	}
	return "42,43,44,45,46,47,48,49,50"
}
func RuntimeHwanBaseChanceBP() int {
	if RuntimeGameplayPolicyLoaded() {
		return int(RuntimeGameplayPolicy().HwanBaseChanceBP)
	}
	return 800
}
func RuntimeHwanOverlevelGrace() int {
	if RuntimeGameplayPolicyLoaded() {
		return int(RuntimeGameplayPolicy().HwanOverlevelGrace)
	}
	return 5
}
func RuntimeHwanPenaltyPerLevelBP() int {
	if RuntimeGameplayPolicyLoaded() {
		return int(RuntimeGameplayPolicy().HwanPenaltyPerLevelBP)
	}
	return 25
}
func RuntimeHwanMinimumChanceBP() int {
	if RuntimeGameplayPolicyLoaded() {
		return int(RuntimeGameplayPolicy().HwanMinimumChanceBP)
	}
	return 25
}
func RuntimeHwanGiantMultiplier() int {
	if v := RuntimeGameplayPolicy().HwanGiantMultiplier; v > 0 {
		return int(v)
	}
	return 5
}
func RuntimeHwanEliteMultiplier() int {
	if v := RuntimeGameplayPolicy().HwanEliteMultiplier; v > 0 {
		return int(v)
	}
	return 2
}
func RuntimeServerMaxLevel() int {
	if v := RuntimeGameplayPolicy().ServerMaxLevel; v > 0 && v <= 255 {
		return int(v)
	}
	return 110
}
func RuntimeEUMasteryMaxTotal() int {
	if v := RuntimeGameplayPolicy().EUMasteryMaxTotal; v > 0 {
		return int(v)
	}
	return 240
}
func RuntimeEUMasteryPointsPerLevel() int {
	if v := RuntimeGameplayPolicy().EUMasteryPointsPerLevel; v > 0 {
		return int(v)
	}
	return 2
}
func RuntimeCHMasteryMaxTotal() int {
	if v := RuntimeGameplayPolicy().CHMasteryMaxTotal; v > 0 {
		return int(v)
	}
	return 330
}
func RuntimeCHMasteryPointsPerLevel() int {
	if v := RuntimeGameplayPolicy().CHMasteryPointsPerLevel; v > 0 {
		return int(v)
	}
	return 3
}
func RuntimeMaxSpeedEffectDuration() time.Duration {
	if v := RuntimeGameplayPolicy().MaxSpeedEffectMinutes; v > 0 {
		return time.Duration(v) * time.Minute
	}
	return 24 * time.Hour
}

type GameplayRateRepository interface {
	GameplayRates(context.Context) (GameplayRates, error)
}

type GameplayRateOverrideRepository interface {
	GameplayRateRepository
	ActivateRateOverride(context.Context, string, GameplayRateOverride) error
	DeactivateRateOverride(context.Context, string) error
	ActivateMobRateOverride(context.Context, string, GameplayMobRateOverride) error
	DeactivateMobRateOverride(context.Context, string) error
}

type GameplayMobRateOverride struct {
	MobCountRatePercent   uint32
	MobRespawnRatePercent uint32
	LootDropRatePercent   uint32
}

type GameplayRateOverride struct {
	ExperienceRatePercent      uint32
	PartyExperienceRatePercent uint32
	SkillExperienceRatePercent uint32
}

type SQLGameplayRateRepository struct {
	shard frameworkdb.DBTX
}

func NewGameplayRateRepository(shard frameworkdb.DBTX) (*SQLGameplayRateRepository, error) {
	if shard == nil {
		return nil, ErrGameplayRateRepositoryUnavailable
	}
	return &SQLGameplayRateRepository{shard: shard}, nil
}

func (r *SQLGameplayRateRepository) ActivateRateOverride(ctx context.Context, eventID string, rates GameplayRateOverride) error {
	if r == nil || r.shard == nil {
		return ErrGameplayRateRepositoryUnavailable
	}
	_, err := r.shard.ExecContext(ctx, activateGameplayRateOverride, eventID, rates.ExperienceRatePercent, rates.PartyExperienceRatePercent, rates.SkillExperienceRatePercent)
	if err != nil {
		return fmt.Errorf("activate gameplay rate override: %w", err)
	}
	return nil
}

func (r *SQLGameplayRateRepository) DeactivateRateOverride(ctx context.Context, eventID string) error {
	if r == nil || r.shard == nil {
		return ErrGameplayRateRepositoryUnavailable
	}
	_, err := r.shard.ExecContext(ctx, deactivateGameplayRateOverride, eventID)
	if err != nil {
		return fmt.Errorf("deactivate gameplay rate override: %w", err)
	}
	return nil
}

func (r *SQLGameplayRateRepository) ActivateMobRateOverride(ctx context.Context, eventID string, rates GameplayMobRateOverride) error {
	if r == nil || r.shard == nil {
		return ErrGameplayRateRepositoryUnavailable
	}
	_, err := r.shard.ExecContext(ctx, activateGameplayMobRateOverride, eventID, rates.MobCountRatePercent, rates.MobRespawnRatePercent, rates.LootDropRatePercent)
	if err != nil {
		return fmt.Errorf("activate gameplay mob rate override: %w", err)
	}
	return nil
}

func (r *SQLGameplayRateRepository) DeactivateMobRateOverride(ctx context.Context, eventID string) error {
	if r == nil || r.shard == nil {
		return ErrGameplayRateRepositoryUnavailable
	}
	_, err := r.shard.ExecContext(ctx, deactivateGameplayMobRateOverride, eventID)
	if err != nil {
		return fmt.Errorf("deactivate gameplay mob rate override: %w", err)
	}
	return nil
}

func (r *SQLGameplayRateRepository) GameplayRates(ctx context.Context) (GameplayRates, error) {
	if r == nil || r.shard == nil {
		return GameplayRates{}, ErrGameplayRateRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var rates GameplayRates
	if err := r.shard.QueryRowContext(ctx, SelectGameplayRates).Scan(
		&rates.ExperienceRatePercent,
		&rates.PartyExperienceRatePercent,
		&rates.SkillExperienceRatePercent,
		&rates.MobCountRatePercent,
		&rates.MobRespawnRatePercent,
		&rates.LootDropRatePercent,
		&rates.GoldDropAmountRatePercent,
		&rates.PartyExperienceRange,
		&rates.GroundPickupRange,
		&rates.JobSuitEquipDelayMS,
		&rates.PartyInvitationTTLMS,
		&rates.MobAttackCooldownMS,
		&rates.MobWanderDelayMS,
		&rates.MobAggroReactionMinMS,
		&rates.MobAggroReactionMaxMS,
		&rates.MobPursuitRefreshMS,
	); err != nil {
		return GameplayRates{}, fmt.Errorf("load gameplay progression rates: %w", err)
	}
	if err := r.shard.QueryRowContext(ctx, selectRuntimePolicy).Scan(&rates.GuildCreationCost, &rates.AcademyCreationCost, &rates.AcademyMinimumLevel, &rates.GuildWarMaximumWager, &rates.MarketLifetimeMinutes, &rates.CharacterDeletionHours, &rates.RecallConfirmationTTLMS, &rates.GuildElectionDurationMin, &rates.GuildMasterAbsenceDays, &rates.GuildRecentMemberDays, &rates.FriendLimit, &rates.BlockLimit, &rates.MemoLimit, &rates.PKPenaltyPerKill, &rates.BerserkDurationPerPointMS, &rates.WinterDropChanceBP, &rates.WinterDropItemCount, &rates.BattleArenaTicketRate, &rates.BattleArenaWorldIDs, &rates.HwanBaseChanceBP, &rates.HwanOverlevelGrace, &rates.HwanPenaltyPerLevelBP, &rates.HwanMinimumChanceBP, &rates.HwanGiantMultiplier, &rates.HwanEliteMultiplier, &rates.ServerMaxLevel, &rates.EUMasteryMaxTotal, &rates.EUMasteryPointsPerLevel, &rates.CHMasteryMaxTotal, &rates.CHMasteryPointsPerLevel, &rates.MaxSpeedEffectMinutes); err != nil {
		return GameplayRates{}, fmt.Errorf("load runtime gameplay policy: %w", err)
	}
	if rates.MobCountRatePercent == 0 || rates.MobRespawnRatePercent == 0 || rates.LootDropRatePercent == 0 || rates.GoldDropAmountRatePercent == 0 || rates.PartyExperienceRange == 0 || rates.GroundPickupRange == 0 || rates.JobSuitEquipDelayMS == 0 || rates.PartyInvitationTTLMS == 0 || rates.MobAttackCooldownMS == 0 || rates.MobWanderDelayMS == 0 || rates.MobAggroReactionMinMS == 0 || rates.MobAggroReactionMaxMS < rates.MobAggroReactionMinMS || rates.MobPursuitRefreshMS == 0 {
		return GameplayRates{}, ErrGameplayMobRatesInvalid
	}
	if rates.GuildCreationCost == 0 || rates.AcademyCreationCost == 0 || rates.AcademyMinimumLevel == 0 || rates.GuildWarMaximumWager == 0 || rates.MarketLifetimeMinutes == 0 || rates.CharacterDeletionHours == 0 || rates.RecallConfirmationTTLMS == 0 || rates.GuildElectionDurationMin == 0 || rates.GuildMasterAbsenceDays == 0 || rates.GuildRecentMemberDays == 0 || rates.FriendLimit == 0 || rates.BlockLimit == 0 || rates.MemoLimit == 0 || rates.PKPenaltyPerKill == 0 || rates.BerserkDurationPerPointMS == 0 {
		return GameplayRates{}, ErrGameplayMobRatesInvalid
	}
	if rates.WinterDropChanceBP > 10000 || rates.WinterDropItemCount == 0 || rates.BattleArenaTicketRate > 100 || rates.HwanBaseChanceBP > 10000 || rates.HwanPenaltyPerLevelBP > 10000 || rates.HwanMinimumChanceBP > 10000 || rates.HwanGiantMultiplier == 0 || rates.HwanEliteMultiplier == 0 || rates.ServerMaxLevel == 0 || rates.ServerMaxLevel > 255 || rates.EUMasteryMaxTotal == 0 || rates.EUMasteryPointsPerLevel == 0 || rates.CHMasteryMaxTotal == 0 || rates.CHMasteryPointsPerLevel == 0 || rates.MaxSpeedEffectMinutes == 0 {
		return GameplayRates{}, ErrGameplayMobRatesInvalid
	}
	return rates, nil
}
