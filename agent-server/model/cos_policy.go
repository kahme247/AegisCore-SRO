package model

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

const (
	SelectCOSGameplaySettings = "SELECT `FOLLOW_BASE_RADIUS`,`FOLLOW_RADIUS_STEP`,`ORBIT_MIN_RADIUS`,`ORBIT_RADIUS_VARIANCE`,`PICKUP_IDLE_MIN_RADIUS`,`PICKUP_IDLE_VARIANCE`,`PICKUP_COLLECTION_RANGE`,`COMPANION_RELOCATE_RANGE`,`PICKUP_PET_RELOCATE_RANGE`,`ORBIT_MIN_ANGLE_STEP`,`ORBIT_ANGLE_STEP_RANGE`,`ARRIVAL_RADIUS`,`DESTINATION_MATCH_RANGE`,`COMBAT_LEASH_RANGE`,`FOLLOW_UPDATE_MS`,`RENTAL_RETRY_MS`,`DEFAULT_ATTACK_COOLDOWN_MS`,`IDLE_WAYPOINT_MIN_MS`,`IDLE_WAYPOINT_VARIANCE_MS` FROM `SRO_SHARD`.`GAMEPLAY_COS_SETTINGS` WHERE `SETTING_ID`=1"
	SelectCOSKindPolicies     = "SELECT `COS_KIND`,`INHERIT_OWNER_SPEED` FROM `SRO_SHARD`.`GAMEPLAY_COS_KIND_POLICY` ORDER BY `COS_KIND`"
)

var (
	ErrCOSPolicyRepositoryUnavailable = errors.New("COS policy repository is unavailable")
	ErrCOSPolicyInvalid               = errors.New("COS gameplay policy is invalid")
)

type COSGameplaySettings struct {
	FollowBaseRadius        float32
	FollowRadiusStep        float32
	OrbitMinRadius          float32
	OrbitRadiusVariance     uint32
	PickupIdleMinRadius     float32
	PickupIdleVariance      uint32
	PickupCollectionRange   float32
	CompanionRelocateRange  float32
	PickupPetRelocateRange  float32
	OrbitMinAngleStep       float32
	OrbitAngleStepRange     uint32
	ArrivalRadius           float32
	DestinationMatchRange   float32
	CombatLeashRange        float32
	FollowUpdateInterval    time.Duration
	RentalRetryInterval     time.Duration
	DefaultAttackCooldown   time.Duration
	IdleWaypointMinInterval time.Duration
	IdleWaypointVariance    time.Duration
}

type COSKindPolicy struct {
	InheritOwnerSpeed bool
}

type COSPolicySnapshot struct {
	Settings COSGameplaySettings
	Kinds    [10]COSKindPolicy
}

func DefaultCOSPolicySnapshot() COSPolicySnapshot {
	settings := COSGameplaySettings{
		FollowBaseRadius: 30, FollowRadiusStep: 4, OrbitMinRadius: 38, OrbitRadiusVariance: 2001,
		PickupIdleMinRadius: 30, PickupIdleVariance: 2001, PickupCollectionRange: 300,
		CompanionRelocateRange: 80, PickupPetRelocateRange: 350, OrbitMinAngleStep: 45,
		OrbitAngleStepRange: 41, ArrivalRadius: 3, DestinationMatchRange: 2.5, CombatLeashRange: 250,
		FollowUpdateInterval: 500 * time.Millisecond, RentalRetryInterval: 5 * time.Second,
		DefaultAttackCooldown: 1500 * time.Millisecond, IdleWaypointMinInterval: 2 * time.Second,
		IdleWaypointVariance: 3 * time.Second,
	}
	var kinds [10]COSKindPolicy
	kinds[COSGrowthPet].InheritOwnerSpeed = true
	kinds[COSAbilityPet].InheritOwnerSpeed = true
	return COSPolicySnapshot{Settings: settings, Kinds: kinds}
}

func (s COSPolicySnapshot) ForKind(kind COSKind) COSKindPolicy {
	if !kind.Valid() {
		return COSKindPolicy{}
	}
	return s.Kinds[kind]
}

func (s COSPolicySnapshot) Valid() bool {
	v := s.Settings
	positiveFloat := func(value float32) bool {
		return value > 0 && !math.IsNaN(float64(value)) && !math.IsInf(float64(value), 0)
	}
	return positiveFloat(v.FollowBaseRadius) && positiveFloat(v.FollowRadiusStep) && positiveFloat(v.OrbitMinRadius) &&
		v.OrbitRadiusVariance > 0 && positiveFloat(v.PickupIdleMinRadius) && v.PickupIdleVariance > 0 &&
		positiveFloat(v.PickupCollectionRange) && positiveFloat(v.CompanionRelocateRange) && positiveFloat(v.PickupPetRelocateRange) &&
		positiveFloat(v.OrbitMinAngleStep) && v.OrbitAngleStepRange > 0 && positiveFloat(v.ArrivalRadius) &&
		positiveFloat(v.DestinationMatchRange) && positiveFloat(v.CombatLeashRange) && v.FollowUpdateInterval > 0 &&
		v.RentalRetryInterval > 0 && v.DefaultAttackCooldown > 0 && v.IdleWaypointMinInterval > 0 && v.IdleWaypointVariance >= 0
}

type COSPolicyRepository interface {
	COSPolicy(context.Context) (COSPolicySnapshot, error)
}

type SQLCOSPolicyRepository struct {
	shard frameworkdb.Queryer
}

func NewCOSPolicyRepository(shard frameworkdb.Queryer) (*SQLCOSPolicyRepository, error) {
	if shard == nil {
		return nil, ErrCOSPolicyRepositoryUnavailable
	}
	return &SQLCOSPolicyRepository{shard: shard}, nil
}

func (r *SQLCOSPolicyRepository) COSPolicy(ctx context.Context) (COSPolicySnapshot, error) {
	if r == nil || r.shard == nil {
		return COSPolicySnapshot{}, ErrCOSPolicyRepositoryUnavailable
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var result COSPolicySnapshot
	var followMS, rentalMS, cooldownMS, waypointMinMS, waypointVarianceMS uint32
	rows, err := r.shard.QueryContext(ctx, SelectCOSGameplaySettings)
	if err != nil {
		return COSPolicySnapshot{}, fmt.Errorf("load COS gameplay settings: %w", err)
	}
	if !rows.Next() {
		rows.Close()
		return COSPolicySnapshot{}, fmt.Errorf("load COS gameplay settings: no singleton row")
	}
	err = rows.Scan(
		&result.Settings.FollowBaseRadius, &result.Settings.FollowRadiusStep, &result.Settings.OrbitMinRadius,
		&result.Settings.OrbitRadiusVariance, &result.Settings.PickupIdleMinRadius, &result.Settings.PickupIdleVariance,
		&result.Settings.PickupCollectionRange, &result.Settings.CompanionRelocateRange, &result.Settings.PickupPetRelocateRange,
		&result.Settings.OrbitMinAngleStep, &result.Settings.OrbitAngleStepRange, &result.Settings.ArrivalRadius,
		&result.Settings.DestinationMatchRange, &result.Settings.CombatLeashRange, &followMS, &rentalMS, &cooldownMS,
		&waypointMinMS, &waypointVarianceMS,
	)
	if err != nil {
		rows.Close()
		return COSPolicySnapshot{}, fmt.Errorf("scan COS gameplay settings: %w", err)
	}
	if rows.Next() {
		rows.Close()
		return COSPolicySnapshot{}, fmt.Errorf("%w: multiple singleton settings rows", ErrCOSPolicyInvalid)
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return COSPolicySnapshot{}, fmt.Errorf("iterate COS gameplay settings: %w", err)
	}
	rows.Close()
	result.Settings.FollowUpdateInterval = time.Duration(followMS) * time.Millisecond
	result.Settings.RentalRetryInterval = time.Duration(rentalMS) * time.Millisecond
	result.Settings.DefaultAttackCooldown = time.Duration(cooldownMS) * time.Millisecond
	result.Settings.IdleWaypointMinInterval = time.Duration(waypointMinMS) * time.Millisecond
	result.Settings.IdleWaypointVariance = time.Duration(waypointVarianceMS) * time.Millisecond

	policyRows, err := r.shard.QueryContext(ctx, SelectCOSKindPolicies)
	if err != nil {
		return COSPolicySnapshot{}, fmt.Errorf("load COS kind policies: %w", err)
	}
	defer policyRows.Close()
	seen := make(map[COSKind]struct{}, 9)
	for policyRows.Next() {
		var kind uint8
		var inherit bool
		if err = policyRows.Scan(&kind, &inherit); err != nil {
			return COSPolicySnapshot{}, fmt.Errorf("scan COS kind policy: %w", err)
		}
		cosKind := COSKind(kind)
		if !cosKind.Valid() {
			return COSPolicySnapshot{}, fmt.Errorf("%w: COS kind %d", ErrCOSPolicyInvalid, kind)
		}
		if _, exists := seen[cosKind]; exists {
			return COSPolicySnapshot{}, fmt.Errorf("%w: duplicate COS kind %d", ErrCOSPolicyInvalid, kind)
		}
		seen[cosKind] = struct{}{}
		result.Kinds[cosKind] = COSKindPolicy{InheritOwnerSpeed: inherit}
	}
	if err = policyRows.Err(); err != nil {
		return COSPolicySnapshot{}, fmt.Errorf("iterate COS kind policies: %w", err)
	}
	if len(seen) != 9 || !result.Valid() {
		return COSPolicySnapshot{}, ErrCOSPolicyInvalid
	}
	return result, nil
}
