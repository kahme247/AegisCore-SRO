package main

import (
	"bytes"
	"encoding/csv"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Harvi8/AegisCore/agent-server/model"
)

func TestEmitItemCoverageWritesDeterministicReportsAndResolvesDropPaths(t *testing.T) {
	t.Parallel()

	catalog := syntheticItemCoverageCatalog()
	firstDirectory := filepath.Join(t.TempDir(), "first")
	secondDirectory := filepath.Join(t.TempDir(), "second")
	var output bytes.Buffer
	if err := emitItemCoverage(itemCoverageOptions{outputDirectory: firstDirectory}, &output, catalog); err != nil {
		t.Fatalf("emitItemCoverage() error = %v", err)
	}
	if !strings.Contains(output.String(), "10 items and 7 drop-source rows") {
		t.Fatalf("command output = %q", output.String())
	}
	if err := emitItemCoverage(itemCoverageOptions{outputDirectory: secondDirectory}, nil, catalog); err != nil {
		t.Fatalf("second emitItemCoverage() error = %v", err)
	}
	for _, name := range []string{"items.csv", "drop_sources.csv", "summary.md"} {
		first, err := os.ReadFile(filepath.Join(firstDirectory, name))
		if err != nil {
			t.Fatalf("read first %s: %v", name, err)
		}
		second, err := os.ReadFile(filepath.Join(secondDirectory, name))
		if err != nil {
			t.Fatalf("read second %s: %v", name, err)
		}
		if !bytes.Equal(first, second) {
			t.Fatalf("%s is not deterministic", name)
		}
	}

	items := readCoverageCSV(t, filepath.Join(firstDirectory, "items.csv"))
	if got := items[1][0]; got != "1" {
		t.Fatalf("first sorted item ID = %q, want 1", got)
	}
	statuses := coverageCSVColumn(items, "item_id", "implementation_status")
	if statuses["1"] != itemCoverageNotApplicable || statuses["2"] != itemCoverageSupported ||
		statuses["3"] != itemCoverageUnsupported || statuses["4"] != itemCoverageBroken ||
		statuses["5"] != itemCoverageSupported || statuses["6"] != itemCoverageSupported ||
		statuses["7"] != itemCoverageSupported || statuses["9"] != itemCoverageSupported ||
		statuses["10"] != itemCoverageSupported {
		t.Fatalf("item statuses = %#v", statuses)
	}
	effects := coverageCSVColumn(items, "item_id", "decoded_effect")
	if effects["3"] != "cure" || effects["5"] != "buff" || effects["6"] != "movement_speed" ||
		effects["7"] != string(model.ItemUseFamilyCharacterResurrection) ||
		effects["9"] != string(model.ItemUseFamilyCharacterStatusCure) ||
		effects["10"] != string(model.ItemUseFamilyCharacterStatusCure) {
		t.Fatalf("decoded effects = %#v", effects)
	}

	drops := readCoverageCSV(t, filepath.Join(firstDirectory, "drop_sources.csv"))
	kinds := coverageCSVValues(drops, "source_kind")
	wantKinds := map[string]int{"direct_monster": 2, "level_profile": 1, "random_group": 2, "random_group_member": 1, "reserved_profile": 1}
	if !reflect.DeepEqual(kinds, wantKinds) {
		t.Fatalf("drop kinds = %#v, want %#v", kinds, wantKinds)
	}
	statusesByKind := coverageCSVValues(drops, "catalog_status")
	if statusesByKind[itemCoverageBroken] != 2 || statusesByKind["available"] != 5 {
		t.Fatalf("drop statuses = %#v", statusesByKind)
	}

	summary, err := os.ReadFile(filepath.Join(firstDirectory, "summary.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"| ITEMDATA rows | 10 |", "| Unsupported usable items | 1 |", "| Broken usable items | 1 |", "| Broken drop-source rows | 2 |"} {
		if !strings.Contains(string(summary), expected) {
			t.Fatalf("summary missing %q:\n%s", expected, summary)
		}
	}
}

func TestEmitItemCoverageFailureThresholdWritesArtifactsBeforeReturning(t *testing.T) {
	t.Parallel()

	catalog := itemCoverageCatalog{items: []itemCoverageItem{{
		id: 8, codeName: "ITEM_UNMAPPED", typeID1: 3, typeID2: 3, typeID3: 99, typeID4: 1, canUse: 1,
	}}}
	brokenDirectory := filepath.Join(t.TempDir(), "broken")
	if err := emitItemCoverage(itemCoverageOptions{outputDirectory: brokenDirectory, failOn: itemCoverageBroken}, nil, catalog); err != nil {
		t.Fatalf("broken threshold rejected an unsupported-only catalog: %v", err)
	}
	unsupportedDirectory := filepath.Join(t.TempDir(), "unsupported")
	err := emitItemCoverage(itemCoverageOptions{outputDirectory: unsupportedDirectory, failOn: itemCoverageUnsupported}, nil, catalog)
	if !errors.Is(err, errItemCoverageGate) {
		t.Fatalf("unsupported threshold error = %v, want coverage gate", err)
	}
	if _, statErr := os.Stat(filepath.Join(unsupportedDirectory, "summary.md")); statErr != nil {
		t.Fatalf("gate failure did not retain summary.md: %v", statErr)
	}
}

func TestItemCoverageSeparatesDisabledAndBrokenDropReferences(t *testing.T) {
	catalog := itemCoverageCatalog{
		groups: []itemCoverageGroupMember{
			{service: 1, groupID: 9, itemID: 900, selectRatio: 1},
			{service: 1, groupID: 10, itemID: 902, selectRatio: 1},
		},
		random: []itemCoverageRandomDrop{{service: 1, monsterID: 901, groupID: 9, amountMin: 1, amountMax: 1, dropRatio: 1}},
		commonObjects: map[uint32]itemCoverageCommonObject{
			900: {service: 0, typeID1: 3, codeName: "ITEM_DISABLED"},
			901: {service: 0, typeID1: 1, codeName: "MOB_DISABLED"},
			902: {service: 1, typeID1: 3, codeName: "ITEM_ENABLED_BUT_UNLOADED"},
		},
	}
	report := buildItemCoverageReport(catalog)
	if report.disabledDrops != 2 || report.brokenDrops != 1 {
		t.Fatalf("drop status counts = disabled %d broken %d", report.disabledDrops, report.brokenDrops)
	}
	for _, row := range report.drops {
		if row.itemID == 900 && row.status != "disabled" {
			t.Fatalf("disabled row = %+v", row)
		}
		if row.itemID == 902 && row.status != itemCoverageBroken {
			t.Fatalf("enabled-but-unloaded row = %+v", row)
		}
	}
}

func TestParseItemCoverageOptionsRequiresOutputAndKnownThreshold(t *testing.T) {
	t.Parallel()

	options, err := parseItemCoverageOptions([]string{"--output-dir", " audit ", "--fail-on", "BROKEN"})
	if err != nil {
		t.Fatal(err)
	}
	if options.outputDirectory != "audit" || options.failOn != itemCoverageBroken {
		t.Fatalf("options = %#v", options)
	}
	for _, arguments := range [][]string{
		nil,
		{"--output-dir", ""},
		{"--output-dir", "audit", "--fail-on", "all"},
		{"--output-dir", "audit", "positional"},
	} {
		if _, err := parseItemCoverageOptions(arguments); !errors.Is(err, errItemCoverageUsage) {
			t.Fatalf("parseItemCoverageOptions(%q) error = %v, want usage", arguments, err)
		}
	}
}

func syntheticItemCoverageCatalog() itemCoverageCatalog {
	items := []itemCoverageItem{
		{id: 7, codeName: "ITEM_RESURRECTION", typeID1: 3, typeID2: 3, typeID3: 13, typeID4: 6, canUse: 1},
		{id: 3, codeName: "ITEM_CURE", typeID1: 3, typeID2: 3, typeID3: 13, typeID4: 1, canUse: 1, descs: coverageDescriptions("SKILL_CH_CURE")},
		{id: 1, codeName: "ITEM_SWORD", typeID1: 3, typeID2: 1, typeID3: 1, typeID4: 1, canDrop: 1},
		{id: 6, codeName: "ITEM_SPEED", typeID1: 3, typeID2: 3, typeID3: 13, typeID4: 1, canUse: 1, descs: coverageDescriptions("SKILL_CH_SPEED")},
		{id: 2, codeName: "ITEM_HP_POTION", typeID1: 3, typeID2: 3, typeID3: 1, typeID4: 1, canUse: 1, canDrop: 1, params: coverageParameters(100)},
		{id: 5, codeName: "ITEM_HP_BUFF", typeID1: 3, typeID2: 3, typeID3: 13, typeID4: 1, canUse: 1, descs: coverageDescriptions("SKILL_CH_HP_BUFF")},
		{id: 4, codeName: "ITEM_MISSING_SKILL", typeID1: 3, typeID2: 3, typeID3: 13, typeID4: 1, canUse: 1, descs: coverageDescriptions("SKILL_MISSING")},
		{id: 8, codeName: "ITEM_TALISMAN", typeID1: 3, typeID2: 3, typeID3: 12, typeID4: 5, canUse: 1},
		{id: 9, codeName: "ITEM_CURE_RANDOM", typeID1: 3, typeID2: 3, typeID3: 2, typeID4: 1, canUse: 1, params: coverageCureParameters(25_145_280, 9, 100)},
		{id: 10, codeName: "ITEM_CURE_ALL", typeID1: 3, typeID2: 3, typeID3: 2, typeID4: 6, canUse: 1, params: coverageCureParameters(36, 36, 36, 36, 36, 36)},
	}
	cureRef := model.RefSkill{ID: 300, CodeName: "SKILL_CH_CURE", BasicActivity: 2, CureStatusMask: 1}
	cureRef.Params[0], cureRef.Params[1], cureRef.Params[2] = 1668641396, 1, 100
	cure := itemCoverageSkillFromModel(cureRef)
	buffRef := model.RefSkill{ID: 500, CodeName: "SKILL_CH_HP_BUFF", BasicActivity: 2, BuffDuration: 60000, BuffModifiers: model.BuffModifiers{HP: 100}}
	buffRef.Params[0], buffRef.Params[1], buffRef.Params[2], buffRef.Params[3] = 1685418593, 60000, 6844521, 100
	buff := itemCoverageSkillFromModel(buffRef)
	speedRef := model.RefSkill{ID: 600, CodeName: "SKILL_CH_SPEED", BasicActivity: 2}
	speedRef.Params[2], speedRef.Params[3], speedRef.Params[4], speedRef.Params[5] = 1685418593, 60000, 1752396901, 20
	speed := itemCoverageSkillFromModel(speedRef)
	return itemCoverageCatalog{
		items:    items,
		skills:   []itemCoverageSkill{speed, cure, buff},
		monsters: []itemCoverageMonster{{id: 100, codeName: "MOB_TEST"}},
		direct: []itemCoverageDirectDrop{
			{service: 1, monsterID: 100, itemID: 2, amountMin: 1, amountMax: 1, dropRatio: 0.5},
			{service: 1, monsterID: 100, itemID: 99, amountMin: 1, amountMax: 1, dropRatio: 0.1},
		},
		groups: []itemCoverageGroupMember{{service: 1, groupID: 5, itemID: 3, magicGroupID: 9, selectRatio: 1}},
		random: []itemCoverageRandomDrop{
			{service: 1, monsterID: 100, groupID: 5, amountMin: 1, amountMax: 2, dropRatio: 0.25},
			{service: 1, monsterID: 101, groupID: 6, amountMin: 1, amountMax: 1, dropRatio: 0.25},
		},
		assignments:       []itemCoverageAssignment{{sourceRow: 42, service: 1, itemID: 2, relative: 2, absolute: 0.02, assignedGroup: -1, dropCount: 1}},
		reserved:          []itemCoverageReservedAssignment{{sourceRow: 43, service: 1, countryMask: 3, itemID: 8, relative: 1, absolute: 0.01, assignedGroup: -1, dropCount: 1}},
		collectionItemIDs: []uint32{8},
	}
}

func coverageParameters(first int64) (parameters [20]int64) {
	parameters[0] = first
	return parameters
}

func coverageCureParameters(values ...int64) (parameters [20]int64) {
	copy(parameters[:], values)
	return parameters
}

func coverageDescriptions(first string) (descriptions [20]string) {
	descriptions[0] = first
	return descriptions
}

func readCoverageCSV(t *testing.T, path string) [][]string {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	records, err := csv.NewReader(file).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return records
}

func coverageCSVColumn(records [][]string, keyColumn, valueColumn string) map[string]string {
	keyIndex, valueIndex := coverageCSVIndex(records[0], keyColumn), coverageCSVIndex(records[0], valueColumn)
	values := make(map[string]string)
	for _, record := range records[1:] {
		values[record[keyIndex]] = record[valueIndex]
	}
	return values
}

func coverageCSVValues(records [][]string, column string) map[string]int {
	index := coverageCSVIndex(records[0], column)
	values := make(map[string]int)
	for _, record := range records[1:] {
		values[record[index]]++
	}
	return values
}

func coverageCSVIndex(header []string, name string) int {
	for index, value := range header {
		if value == name {
			return index
		}
	}
	panic("missing CSV column " + name)
}
