package model

import (
	"context"
	"database/sql/driver"
	"errors"
	"testing"
)

func TestCOSPolicyRepositoryLoadsSettingsAndAllKindPolicies(t *testing.T) {
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(SelectCOSGameplaySettings, catalogRepositoryTestResponse{
		columns: []string{"FOLLOW_BASE_RADIUS", "FOLLOW_RADIUS_STEP", "ORBIT_MIN_RADIUS", "ORBIT_RADIUS_VARIANCE", "PICKUP_IDLE_MIN_RADIUS", "PICKUP_IDLE_VARIANCE", "PICKUP_COLLECTION_RANGE", "COMPANION_RELOCATE_RANGE", "PICKUP_PET_RELOCATE_RANGE", "ORBIT_MIN_ANGLE_STEP", "ORBIT_ANGLE_STEP_RANGE", "ARRIVAL_RADIUS", "DESTINATION_MATCH_RANGE", "COMBAT_LEASH_RANGE", "FOLLOW_UPDATE_MS", "RENTAL_RETRY_MS", "DEFAULT_ATTACK_COOLDOWN_MS", "IDLE_WAYPOINT_MIN_MS", "IDLE_WAYPOINT_VARIANCE_MS"},
		rows:    [][]driver.Value{{float64(30), float64(4), float64(38), int64(2001), float64(30), int64(2001), float64(175), float64(80), float64(190), float64(45), int64(41), float64(3), float64(2.5), float64(250), int64(500), int64(5000), int64(1500), int64(2000), int64(3000)}},
	})
	kindRows := make([][]driver.Value, 0, 9)
	for kind := int64(1); kind <= 9; kind++ {
		kindRows = append(kindRows, []driver.Value{kind, kind == 3 || kind == 4})
	}
	setCatalogRepositoryTestResponse(SelectCOSKindPolicies, catalogRepositoryTestResponse{columns: []string{"COS_KIND", "INHERIT_OWNER_SPEED"}, rows: kindRows})
	repository, err := NewCOSPolicyRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	policy, err := repository.COSPolicy(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if policy.Settings.PickupCollectionRange != 175 || !policy.ForKind(COSGrowthPet).InheritOwnerSpeed || !policy.ForKind(COSAbilityPet).InheritOwnerSpeed || policy.ForKind(COSRideTransport).InheritOwnerSpeed || policy.ForKind(COSJobTransport).InheritOwnerSpeed {
		t.Fatalf("loaded policy = %#v", policy)
	}
}

func TestCOSPolicyRepositoryRejectsMissingRowsAndDependency(t *testing.T) {
	if repository, err := NewCOSPolicyRepository(nil); repository != nil || !errors.Is(err, ErrCOSPolicyRepositoryUnavailable) {
		t.Fatalf("NewCOSPolicyRepository(nil) = (%v, %v)", repository, err)
	}
	resetCatalogRepositoryTestState()
	setCatalogRepositoryTestResponse(SelectCOSGameplaySettings, catalogRepositoryTestResponse{
		columns: []string{"FOLLOW_BASE_RADIUS", "FOLLOW_RADIUS_STEP", "ORBIT_MIN_RADIUS", "ORBIT_RADIUS_VARIANCE", "PICKUP_IDLE_MIN_RADIUS", "PICKUP_IDLE_VARIANCE", "PICKUP_COLLECTION_RANGE", "COMPANION_RELOCATE_RANGE", "PICKUP_PET_RELOCATE_RANGE", "ORBIT_MIN_ANGLE_STEP", "ORBIT_ANGLE_STEP_RANGE", "ARRIVAL_RADIUS", "DESTINATION_MATCH_RANGE", "COMBAT_LEASH_RANGE", "FOLLOW_UPDATE_MS", "RENTAL_RETRY_MS", "DEFAULT_ATTACK_COOLDOWN_MS", "IDLE_WAYPOINT_MIN_MS", "IDLE_WAYPOINT_VARIANCE_MS"},
		rows:    [][]driver.Value{{float64(30), float64(4), float64(38), int64(2001), float64(30), int64(2001), float64(160), float64(80), float64(190), float64(45), int64(41), float64(3), float64(2.5), float64(250), int64(500), int64(5000), int64(1500), int64(2000), int64(3000)}},
	})
	setCatalogRepositoryTestResponse(SelectCOSKindPolicies, catalogRepositoryTestResponse{columns: []string{"COS_KIND", "INHERIT_OWNER_SPEED"}, rows: [][]driver.Value{{int64(3), true}}})
	repository, err := NewCOSPolicyRepository(openCatalogRepositoryTestDB(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = repository.COSPolicy(context.Background()); !errors.Is(err, ErrCOSPolicyInvalid) {
		t.Fatalf("incomplete kind policy error = %v", err)
	}
}
