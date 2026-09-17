package model

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestLocalDimensionHoleScopeAllocatorUsesLowestLayerAndKeepsWorldsIndependent(t *testing.T) {
	allocator := newDimensionHoleScopeTestAllocator(t)
	firstDefinition := DimensionHoleCatalog()[0]
	secondDefinition := DimensionHoleCatalog()[1]
	first, err := allocator.ReserveDimensionHoleScope(context.Background(), firstDefinition, 100)
	if err != nil {
		t.Fatal(err)
	}
	second, err := allocator.ReserveDimensionHoleScope(context.Background(), firstDefinition, 101)
	if err != nil {
		t.Fatal(err)
	}
	otherWorld, err := allocator.ReserveDimensionHoleScope(context.Background(), secondDefinition, 102)
	if err != nil {
		t.Fatal(err)
	}
	if first.Scope.WorldID() != firstDefinition.WorldID || first.Scope.LayerID() != 1 || second.Scope.LayerID() != 2 || otherWorld.Scope.WorldID() != secondDefinition.WorldID || otherWorld.Scope.LayerID() != 1 {
		t.Fatalf("leases = %#v %#v %#v", first, second, otherWorld)
	}
	if found, ok := allocator.DimensionHoleLeaseForScope(first.Scope); !ok || found != first {
		t.Fatalf("DimensionHoleLeaseForScope() = %#v, %t", found, ok)
	}
	if err = allocator.ReleaseDimensionHoleScope(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	reused, err := allocator.ReserveDimensionHoleScope(context.Background(), firstDefinition, 103)
	if err != nil {
		t.Fatal(err)
	}
	if reused.Scope != first.Scope || reused.token == first.token {
		t.Fatalf("reused lease = %#v, first = %#v", reused, first)
	}
	if err = allocator.ReleaseDimensionHoleScope(context.Background(), first); !errors.Is(err, ErrDimensionHoleScopeOwnership) {
		t.Fatalf("stale release error = %v", err)
	}
	if retained, ok := allocator.DimensionHoleLeaseForScope(reused.Scope); !ok || retained != reused {
		t.Fatal("stale release removed the replacement lease")
	}
}

func TestLocalDimensionHoleScopeAllocatorEnforcesReservationOwnership(t *testing.T) {
	allocator := newDimensionHoleScopeTestAllocator(t)
	definition := DimensionHoleCatalog()[0]
	lease, err := allocator.ReserveDimensionHoleScope(context.Background(), definition, 100)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = allocator.ReserveDimensionHoleScope(context.Background(), definition, 100); !errors.Is(err, ErrDimensionHoleScopeReservationExists) {
		t.Fatalf("duplicate reservation error = %v", err)
	}
	forged := lease
	forged.token++
	if err = allocator.ReleaseDimensionHoleScope(context.Background(), forged); !errors.Is(err, ErrDimensionHoleScopeOwnership) {
		t.Fatalf("forged release error = %v", err)
	}
	if retained, ok := allocator.DimensionHoleLeaseForScope(lease.Scope); !ok || retained != lease {
		t.Fatal("forged release removed the owned lease")
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err = allocator.ReleaseDimensionHoleScope(canceled, lease); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled release error = %v", err)
	}
	if retained, ok := allocator.DimensionHoleLeaseForScope(lease.Scope); !ok || retained != lease {
		t.Fatal("canceled release removed the owned lease")
	}
	if err = allocator.ReleaseDimensionHoleScope(context.Background(), lease); err != nil {
		t.Fatal(err)
	}
	if err = allocator.ReleaseDimensionHoleScope(context.Background(), lease); !errors.Is(err, ErrDimensionHoleScopeMissing) {
		t.Fatalf("double release error = %v", err)
	}
}

func TestLocalDimensionHoleScopeAllocatorExhaustsAtAuthoredInstanceCount(t *testing.T) {
	allocator := newDimensionHoleScopeTestAllocator(t)
	definition := DimensionHoleCatalog()[0]
	for key := uint32(1); key <= uint32(DimensionHoleWorldMaxInstanceCount); key++ {
		lease, err := allocator.ReserveDimensionHoleScope(context.Background(), definition, key)
		if err != nil || lease.Scope.LayerID() != uint16(key) {
			t.Fatalf("reservation %d = %#v, %v", key, lease, err)
		}
	}
	if lease, err := allocator.ReserveDimensionHoleScope(context.Background(), definition, 1000); lease.Valid() || !errors.Is(err, ErrDimensionHoleScopeExhausted) {
		t.Fatalf("overflow reservation = %#v, %v", lease, err)
	}
}

func TestLocalDimensionHoleScopeAllocatorConcurrentReservationsAreUnique(t *testing.T) {
	allocator := newDimensionHoleScopeTestAllocator(t)
	definition := DimensionHoleCatalog()[0]
	const attempts = 51
	type result struct {
		lease DimensionHoleScopeLease
		err   error
	}
	results := make(chan result, attempts)
	var wait sync.WaitGroup
	for key := uint32(1); key <= attempts; key++ {
		wait.Add(1)
		go func(key uint32) {
			defer wait.Done()
			lease, err := allocator.ReserveDimensionHoleScope(context.Background(), definition, key)
			results <- result{lease: lease, err: err}
		}(key)
	}
	wait.Wait()
	close(results)

	scopes := make(map[GameWorldScope]bool)
	exhausted := 0
	for result := range results {
		if errors.Is(result.err, ErrDimensionHoleScopeExhausted) {
			exhausted++
			continue
		}
		if result.err != nil || !result.lease.Valid() || scopes[result.lease.Scope] {
			t.Fatalf("concurrent result = %#v", result)
		}
		scopes[result.lease.Scope] = true
	}
	if len(scopes) != int(DimensionHoleWorldMaxInstanceCount) || exhausted != 1 {
		t.Fatalf("unique scopes=%d exhausted=%d", len(scopes), exhausted)
	}
}

func TestLocalDimensionHoleScopeAllocatorRequiresValidatedCatalogDefinitionAndContext(t *testing.T) {
	if allocator, err := NewLocalDimensionHoleScopeAllocator(nil); allocator != nil || !errors.Is(err, ErrDimensionHoleScopeAllocatorUnavailable) {
		t.Fatalf("NewLocalDimensionHoleScopeAllocator(nil) = %#v, %v", allocator, err)
	}
	tampered := dimensionHoleScopeTestCatalog()
	tampered.worlds[0].MaxInstanceCount--
	if allocator, err := NewLocalDimensionHoleScopeAllocator(tampered); allocator != nil || !errors.Is(err, ErrDimensionHoleScopeAllocatorUnavailable) {
		t.Fatalf("NewLocalDimensionHoleScopeAllocator(tampered) = %#v, %v", allocator, err)
	}

	allocator := newDimensionHoleScopeTestAllocator(t)
	definition := DimensionHoleCatalog()[0]
	mutated := definition
	mutated.Capacity++
	if _, err := allocator.ReserveDimensionHoleScope(context.Background(), mutated, 1); !errors.Is(err, ErrDimensionHoleScopeInvalid) {
		t.Fatalf("mutated definition error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := allocator.ReserveDimensionHoleScope(ctx, definition, 1); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled reservation error = %v", err)
	}
}

func newDimensionHoleScopeTestAllocator(t *testing.T) *LocalDimensionHoleScopeAllocator {
	t.Helper()
	allocator, err := NewLocalDimensionHoleScopeAllocator(dimensionHoleScopeTestCatalog())
	if err != nil {
		t.Fatal(err)
	}
	return allocator
}

func dimensionHoleScopeTestCatalog() *DimensionHoleWorldCatalog {
	definitions := DimensionHoleCatalog()
	worlds := make([]DimensionHoleWorldReference, 0, len(definitions))
	for _, definition := range definitions {
		worlds = append(worlds, DimensionHoleWorldReference{
			WorldID: definition.WorldID, CodeName: definition.WorldCodeName,
			WorldType: DimensionHoleWorldType, MaxInstanceCount: DimensionHoleWorldMaxInstanceCount,
			MaxUserCount: definition.Capacity, WorldEntryType: DimensionHoleWorldEntryType,
			WorldEntranceType: DimensionHoleWorldEntranceType, WorldLeaveType: DimensionHoleWorldLeaveType,
			WorldDurationMS: DimensionHoleWorldDurationMS, WorldEmptyRemainMS: DimensionHoleWorldEmptyRemainMS,
			ConfigGroupCodeName: "GROUP_" + strings.TrimPrefix(definition.WorldCodeName, "INS_"),
		})
	}
	return newDimensionHoleWorldCatalog(worlds)
}
