package main

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"strings"
	"testing"
)

func validDropVerifySnapshot() dropCatalogVerifySnapshot {
	items := map[uint32]dropVerifyItem{
		1: {}, 2: {}, 3: {},
		100: {typeID2: 3, typeID3: 1, typeID4: 1, itemClass: 1},
		101: {typeID2: 1, typeID3: 6, typeID4: 2, itemClass: 1},
	}
	return dropCatalogVerifySnapshot{
		items:              items,
		monsters:           map[uint32]struct{}{200: {}},
		magicOptionIDs:     map[uint32]struct{}{300: {}},
		magicOptionCodes:   map[string]int{"MATTR_TEST": 1},
		magicApplicability: map[uint32]int{300: 1},
		direct:             []dropVerifyDirect{{service: 1, monsterID: 200, itemID: 100, minimum: 1, maximum: 1, probability: 0.25, magicOptionIDs: []uint32{300}}},
		random:             []dropVerifyRandom{{service: 1, monsterID: 200, groupID: 400, overlap: 1, minimum: 1, maximum: 2, probability: 0.5}},
		groups:             []dropVerifyGroup{{service: 1, groupID: 400, itemID: 101, probability: 0.5}},
		gold:               []dropVerifyGold{{service: 1, level: 1, probability: 0.8, minimum: 1, maximum: 2}},
		classes: []dropVerifyClass{
			{family: "RECOVER", level: 1, group: 1, probability: 0.2},
			{family: "EQUIP", level: 1, group: 1, probability: 0.2},
		},
		assignments: []dropVerifyAssignment{
			{service: 1, sourceRow: 1, itemID: 100, relative: 100, absolute: 100, assignedGroup: 1, dropCount: 1},
			{service: 1, sourceRow: 2, itemID: 101, relative: 100, absolute: 100, assignedGroup: 1, dropCount: 1},
		},
		reserved:         []dropVerifyAssignment{{service: 1, sourceRow: 3, countryMask: 3, itemID: 100, relative: 1, absolute: 100, assignedGroup: 1, dropCount: 1}},
		optLevels:        []dropVerifyOptLevel{{level: 0, probability: 0.5}, {level: 1, probability: 1}},
		magicAssignments: []dropVerifyMagicAssignment{{service: 1, country: 0, typeID3: 6, typeID4: 2, slot: 1, code: "MATTR_TEST"}},
	}
}

func TestValidateDropCatalogSnapshotAcceptsRuntimeCompleteCatalog(t *testing.T) {
	report := validateDropCatalogSnapshot(validDropVerifySnapshot())
	if report.issues.total() != 0 {
		t.Fatalf("valid catalog issues = %+v", report.issues)
	}
	if len(report.warnings) != 0 {
		t.Fatalf("valid catalog warnings = %v", report.warnings)
	}
	var output bytes.Buffer
	if err := writeDropCatalogVerifyReport(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "drop-catalog status=ok warnings=0 errors=0\n") {
		t.Fatalf("report = %q", output.String())
	}
}

func TestValidateDropCatalogSnapshotReportsBrokenEdgesAndValues(t *testing.T) {
	snapshot := validDropVerifySnapshot()
	snapshot.direct = append(snapshot.direct,
		dropVerifyDirect{service: 1, monsterID: 999, itemID: 999, minimum: 3, maximum: 1, probability: 2, magicOptionIDs: []uint32{999}},
		dropVerifyDirect{service: 1, monsterID: 999, itemID: 999, minimum: 1, maximum: 1, probability: 0.5},
	)
	snapshot.random = append(snapshot.random, dropVerifyRandom{service: 1, monsterID: 999, groupID: 999, overlap: 2, minimum: 2, maximum: 1, probability: math.NaN()})
	snapshot.groups = append(snapshot.groups, dropVerifyGroup{service: 1, groupID: 500, itemID: 999, probability: -1})
	snapshot.items[102] = dropVerifyItem{typeID2: 2, typeID3: 9, typeID4: 9, itemClass: 1}
	snapshot.assignments = append(snapshot.assignments,
		dropVerifyAssignment{service: 1, sourceRow: 4, itemID: 102, relative: 1, absolute: 101, assignedGroup: 1, dropCount: 1},
		dropVerifyAssignment{service: 1, sourceRow: 5, itemID: 999, relative: 1, absolute: 100, assignedGroup: -2, dropCount: 0},
	)
	snapshot.classes = append(snapshot.classes, dropVerifyClass{family: "UNKNOWN", level: 0, group: 0, probability: 3})
	snapshot.optLevels = append(snapshot.optLevels, dropVerifyOptLevel{level: 2, probability: 0.25})
	snapshot.magicAssignments = append(snapshot.magicAssignments, dropVerifyMagicAssignment{service: 1, country: 9, typeID3: 0, typeID4: 0, slot: -1, code: "MISSING"})

	report := validateDropCatalogSnapshot(snapshot)
	for name, count := range map[string]int{
		"missing items": report.issues.missingItems, "missing monsters": report.issues.missingMonsters,
		"missing groups": report.issues.missingGroups, "unclassified": report.issues.unclassifiedItems,
		"probabilities": report.issues.invalidProbabilities, "ranges": report.issues.invalidRanges,
		"duplicates": report.issues.duplicateEdges, "orphans": report.issues.orphanEdges,
	} {
		if count == 0 {
			t.Errorf("%s was not detected: %+v", name, report.issues)
		}
	}
	var output bytes.Buffer
	if err := writeDropCatalogVerifyReport(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "drop-catalog status=invalid") {
		t.Fatalf("invalid report = %q", output.String())
	}
}

func TestValidateDropCatalogSnapshotSkipsAuthoritativelyDisabledReferences(t *testing.T) {
	snapshot := validDropVerifySnapshot()
	snapshot.commonObjects = map[uint32]dropVerifyCommonObject{
		900: {service: 0, typeID1: 1},
		901: {service: 0, typeID1: 3},
	}
	snapshot.random = append(snapshot.random, dropVerifyRandom{service: 1, monsterID: 900, groupID: 999, minimum: 1, maximum: 1, probability: 1})
	snapshot.groups = append(snapshot.groups, dropVerifyGroup{service: 1, groupID: 999, itemID: 901, probability: 1})

	report := validateDropCatalogSnapshot(snapshot)
	if report.issues.total() != 0 {
		t.Fatalf("disabled references produced issues: %+v", report.issues)
	}
	if report.skipped.disabledMonsterEdges != 1 || report.skipped.disabledItemEdges != 1 || report.skipped.disabledGroupIDs != 1 {
		t.Fatalf("disabled counters = %+v", report.skipped)
	}
	var output bytes.Buffer
	if err := writeDropCatalogVerifyReport(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "disabled_item_edges=1 disabled_monster_edges=1 disabled_group_ids=1") {
		t.Fatalf("disabled report = %q", output.String())
	}
}

func TestValidateDropCatalogSnapshotWarnsForEmptyOptionalProfiles(t *testing.T) {
	snapshot := validDropVerifySnapshot()
	snapshot.classes = nil
	snapshot.assignments = nil
	snapshot.optLevels = nil
	snapshot.reserved = nil
	snapshot.magicAssignments = nil
	report := validateDropCatalogSnapshot(snapshot)
	if len(report.warnings) != 1 || report.warnings[0] != "empty_optional=class_selection,item_assign,opt_levels,reserved_assign,magic_assign" {
		t.Fatalf("warnings = %v", report.warnings)
	}
	var output bytes.Buffer
	if err := writeDropCatalogVerifyReport(&output, report); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "drop-catalog warning empty_optional=class_selection,item_assign,opt_levels,reserved_assign,magic_assign\n") {
		t.Fatalf("warning report = %q", output.String())
	}
}

func TestVerifyDropCatalogRejectsArgumentsBeforeOpeningDatabase(t *testing.T) {
	err := verifyDropCatalog(nil, []string{"unexpected"}, nil)
	if !errors.Is(err, errDropCatalogVerifyUsage) {
		t.Fatalf("verifyDropCatalog error = %v", err)
	}
}

func TestVerifyDropCatalogLive(t *testing.T) {
	if os.Getenv("SRO_DATA_LIVE_DROP_VERIFY") != "1" {
		t.Skip("set SRO_DATA_LIVE_DROP_VERIFY=1 for the local read-only catalog check")
	}
	var output bytes.Buffer
	err := verifyDropCatalog(context.Background(), nil, &output)
	t.Log("\n" + output.String())
	if err != nil {
		t.Fatal(err)
	}
}
