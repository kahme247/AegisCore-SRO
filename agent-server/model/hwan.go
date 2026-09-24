package model

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
	"github.com/Harvi8/AegisCore/framework/network"
	"github.com/Harvi8/AegisCore/framework/network/opcode"
)

const (
	MaxHwanPoints           byte          = 5
	BlueBerserkHwanLevel    byte          = 5
	BerserkAttackMultiplier               = 2
	BerserkSpeedMultiplier  float32       = 2
	BerserkDurationPerPoint time.Duration = 12 * time.Second
	BerserkDuration                       = time.Duration(MaxHwanPoints) * BerserkDurationPerPoint
)

var (
	ErrHwanRepositoryUnavailable = errors.New("HWAN repository is unavailable")
	ErrHwanCharacterIDRequired   = errors.New("HWAN character ID is required")
)

type HwanSnapshot struct {
	Points        byte
	GatheredPoint uint32
	Level         byte
}

type HwanKillReward struct {
	ChanceBasisPoints int
	Points            byte
}

// HwanRewardForMonsterKill reproduces SR_GameServer's default HWAN roll.
// The stock server rolls rand() % 10000. Ordinary level-appropriate monsters
// use 800 (8%); over-level penalties are 25 basis points per level after a
// five-level grace window, floored at 25 (0.25%). Rarity 4 is fivefold,
// rarity 6 is twofold, and unique/special-unique classes fill all five points.
func HwanRewardForMonsterKill(playerLevel, monsterLevel int, rarity MonsterRarity) HwanKillReward {
	reward := HwanKillReward{ChanceBasisPoints: RuntimeHwanBaseChanceBP(), Points: 1}
	grace := RuntimeHwanOverlevelGrace()
	if difference := playerLevel - monsterLevel; difference > grace {
		reward.ChanceBasisPoints = max(RuntimeHwanMinimumChanceBP(), reward.ChanceBasisPoints-(difference-grace)*RuntimeHwanPenaltyPerLevelBP())
	}
	switch rarity.BaseClass() {
	case MonsterUnique, MonsterRarity(8):
		reward.ChanceBasisPoints = 10000
		reward.Points = MaxHwanPoints
	case MonsterGiant:
		reward.ChanceBasisPoints = min(10000, reward.ChanceBasisPoints*RuntimeHwanGiantMultiplier())
	case MonsterElite:
		reward.ChanceBasisPoints = min(10000, reward.ChanceBasisPoints*RuntimeHwanEliteMultiplier())
	}
	return reward
}

func BerserkDurationForPoints(points byte) time.Duration {
	return time.Duration(min(points, MaxHwanPoints)) * RuntimeBerserkDurationPerPoint()
}

func (p *Player) BerserkDurationForPoints(points byte, now time.Time) time.Duration {
	duration := BerserkDurationForPoints(points)
	if p == nil {
		return duration
	}
	return duration + time.Duration(p.ItemServiceEffectValue(ItemServiceHwanDuration, now))*time.Second
}

// BerserkEffectVariant is the boolean effect selector consumed after a
// Berserker body-state value by the stock vSRO 1.188 client. It is not the
// numeric HWAN title level: zero selects red Berserk and one selects Blue
// Berserk. Level five is the first Blue Berserk title tier in this profile.
func BerserkEffectVariant(hwanLevel byte) byte {
	if hwanLevel >= BlueBerserkHwanLevel {
		return 1
	}
	return 0
}

type HwanRepository interface {
	Load(context.Context, int) (HwanSnapshot, error)
	ConsumeBerserk(context.Context, int) (HwanSnapshot, byte, error)
	AwardPoints(context.Context, int, byte) (HwanSnapshot, bool, error)
	SetLevel(context.Context, int, byte) error
}

type hwanDBTX interface {
	frameworkdb.DBTX
	BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
}

type SQLHwanRepository struct{ shard hwanDBTX }

func NewHwanRepository(shard hwanDBTX) (*SQLHwanRepository, error) {
	if shard == nil {
		return nil, ErrHwanRepositoryUnavailable
	}
	return &SQLHwanRepository{shard: shard}, nil
}

func (r *SQLHwanRepository) Load(ctx context.Context, characterID int) (HwanSnapshot, error) {
	if r == nil || r.shard == nil {
		return HwanSnapshot{}, ErrHwanRepositoryUnavailable
	}
	if characterID <= 0 {
		return HwanSnapshot{}, ErrHwanCharacterIDRequired
	}
	var snapshot HwanSnapshot
	err := r.shard.QueryRowContext(ctx, `SELECT REMAIN_HWAN_COUNT, GATHERED_HWAN_POINT, HWAN_LEVEL FROM SRO_SHARD.CHAR WHERE ID=?`, characterID).
		Scan(&snapshot.Points, &snapshot.GatheredPoint, &snapshot.Level)
	if err != nil {
		return HwanSnapshot{}, fmt.Errorf("load character HWAN state: %w", err)
	}
	return snapshot, nil
}

func (r *SQLHwanRepository) ConsumeBerserk(ctx context.Context, characterID int) (snapshot HwanSnapshot, consumed byte, resultErr error) {
	if r == nil || r.shard == nil {
		return HwanSnapshot{}, 0, ErrHwanRepositoryUnavailable
	}
	if characterID <= 0 {
		return HwanSnapshot{}, 0, ErrHwanCharacterIDRequired
	}
	tx, err := r.shard.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return HwanSnapshot{}, 0, fmt.Errorf("begin character Berserk consumption: %w", err)
	}
	defer func() {
		if resultErr != nil {
			_ = tx.Rollback()
		}
	}()
	var available byte
	err = tx.QueryRowContext(ctx, `SELECT REMAIN_HWAN_COUNT, GATHERED_HWAN_POINT, HWAN_LEVEL FROM SRO_SHARD.CHAR WHERE ID=? FOR UPDATE`, characterID).
		Scan(&available, &snapshot.GatheredPoint, &snapshot.Level)
	if err != nil {
		return HwanSnapshot{}, 0, fmt.Errorf("lock character Berserk points: %w", err)
	}
	consumed = min(available, MaxHwanPoints)
	if consumed != 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE SRO_SHARD.CHAR SET REMAIN_HWAN_COUNT=0 WHERE ID=?`, characterID); err != nil {
			return HwanSnapshot{}, 0, fmt.Errorf("consume character Berserk points: %w", err)
		}
	}
	snapshot.Points = 0
	if err = tx.Commit(); err != nil {
		return HwanSnapshot{}, 0, fmt.Errorf("commit character Berserk consumption: %w", err)
	}
	return snapshot, consumed, nil
}

func (r *SQLHwanRepository) AwardPoints(ctx context.Context, characterID int, points byte) (HwanSnapshot, bool, error) {
	if r == nil || r.shard == nil {
		return HwanSnapshot{}, false, ErrHwanRepositoryUnavailable
	}
	if characterID <= 0 {
		return HwanSnapshot{}, false, ErrHwanCharacterIDRequired
	}
	if points == 0 {
		snapshot, err := r.Load(ctx, characterID)
		return snapshot, false, err
	}
	points = min(points, MaxHwanPoints)
	result, err := r.shard.ExecContext(ctx, `UPDATE SRO_SHARD.CHAR SET REMAIN_HWAN_COUNT=LEAST(REMAIN_HWAN_COUNT+?, ?) WHERE ID=? AND REMAIN_HWAN_COUNT<?`, points, MaxHwanPoints, characterID, MaxHwanPoints)
	if err != nil {
		return HwanSnapshot{}, false, fmt.Errorf("award character Berserk point: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return HwanSnapshot{}, false, fmt.Errorf("inspect character Berserk award: %w", err)
	}
	snapshot, loadErr := r.Load(ctx, characterID)
	return snapshot, rows == 1, loadErr
}

func (r *SQLHwanRepository) SetLevel(ctx context.Context, characterID int, level byte) error {
	if r == nil || r.shard == nil {
		return ErrHwanRepositoryUnavailable
	}
	if characterID <= 0 {
		return ErrHwanCharacterIDRequired
	}
	result, err := r.shard.ExecContext(ctx, `UPDATE SRO_SHARD.CHAR SET HWAN_LEVEL=? WHERE ID=?`, level, characterID)
	if err != nil {
		return fmt.Errorf("set character HWAN level: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil || rows != 1 {
		return fmt.Errorf("set character HWAN level: affected %d rows: %w", rows, err)
	}
	return nil
}

func (p *Player) CommitHwanSnapshot(snapshot HwanSnapshot) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.RemainHwanCount = min(snapshot.Points, MaxHwanPoints)
	p.GatheredHwanPoint = snapshot.GatheredPoint
	p.HwanLevel = snapshot.Level
	p.Mutex.Unlock()
}

func (p *Player) HwanSnapshot() HwanSnapshot {
	if p == nil {
		return HwanSnapshot{}
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return HwanSnapshot{Points: p.RemainHwanCount, GatheredPoint: p.GatheredHwanPoint, Level: p.HwanLevel}
}

func (p *Player) CommitHwanLevel(level byte) {
	if p == nil {
		return
	}
	p.Mutex.Lock()
	p.HwanLevel = level
	p.Mutex.Unlock()
}

func (p *Player) ActivateBerserk(expiresAt time.Time) uint64 {
	if p == nil || expiresAt.IsZero() {
		return 0
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	p.berserkGeneration++
	p.berserkExpiresAt = expiresAt
	p.BodyState = Berserker
	return p.berserkGeneration
}

func (p *Player) ExpireBerserk(generation uint64) bool {
	if p == nil || generation == 0 {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.berserkGeneration != generation || p.BodyState != Berserker {
		return false
	}
	p.BodyState = Normal
	p.berserkExpiresAt = time.Time{}
	return true
}

// CancelBerserk invalidates a pending expiry and restores the normal body state.
func (p *Player) CancelBerserk() bool {
	if p == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	if p.BodyState != Berserker {
		return false
	}
	p.berserkGeneration++
	p.BodyState = Normal
	p.berserkExpiresAt = time.Time{}
	return true
}

func (p *Player) clearBerserkLocked() {
	if p.BodyState != Berserker {
		return
	}
	p.berserkGeneration++
	p.BodyState = Normal
	p.berserkExpiresAt = time.Time{}
}

func (p *Player) IsBerserk(now time.Time) bool {
	if p == nil {
		return false
	}
	p.Mutex.Lock()
	defer p.Mutex.Unlock()
	return p.BodyState == Berserker && p.berserkExpiresAt.After(now)
}

func BuildHwanPointUpdatePacket(points byte, sourceUID uint32) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdatePoints
	packet.WriteByte(4)
	packet.WriteByte(min(points, MaxHwanPoints))
	packet.WriteUInt32(sourceUID)
	return packet
}

func BuildHwanLevelUpdatePacket(uniqueID uint32, level byte) network.Packet {
	packet := network.EmptyPacket()
	packet.MessageID = opcode.EntityUpdateHwanLevel
	packet.WriteUInt32(uniqueID)
	packet.WriteByte(level)
	return packet
}
