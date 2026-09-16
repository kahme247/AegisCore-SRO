package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"sort"
	"strings"

	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var (
	errDropCatalogVerifyUsage = errors.New("usage: sro-data verify-drop-catalog")
	errDropCatalogInvalid     = errors.New("drop catalog verification failed")
)

type dropVerifyItem struct {
	typeID2, typeID3, typeID4 int
	rarity, itemClass         int
}

type dropVerifyCommonObject struct {
	service, typeID1 int
}

type dropVerifyDirect struct {
	service, minimum, maximum int
	monsterID, itemID         uint32
	probability               float64
	magicOptionIDs            []uint32
}

type dropVerifyRandom struct {
	service, overlap, minimum, maximum int
	monsterID, groupID                 uint32
	probability                        float64
}

type dropVerifyGroup struct {
	service         int
	groupID, itemID uint32
	probability     float64
}

type dropVerifyGold struct {
	service          int
	level            uint32
	probability      float64
	minimum, maximum uint64
}

type dropVerifyClass struct {
	family       string
	level, group int
	probability  float64
}

type dropVerifyAssignment struct {
	service, sourceRow, assignedGroup int
	itemID                            uint32
	relative, absolute                float64
	dropCount                         uint32
	countryMask                       int
}

type dropVerifyOptLevel struct {
	level       int
	probability float64
	required    uint32
}

type dropVerifyMagicAssignment struct {
	service, country, typeID3, typeID4, slot int
	code                                     string
}

type dropCatalogVerifySnapshot struct {
	items              map[uint32]dropVerifyItem
	monsters           map[uint32]struct{}
	commonObjects      map[uint32]dropVerifyCommonObject
	magicOptionIDs     map[uint32]struct{}
	magicOptionCodes   map[string]int
	magicApplicability map[uint32]int
	direct             []dropVerifyDirect
	random             []dropVerifyRandom
	groups             []dropVerifyGroup
	gold               []dropVerifyGold
	classes            []dropVerifyClass
	assignments        []dropVerifyAssignment
	reserved           []dropVerifyAssignment
	optLevels          []dropVerifyOptLevel
	magicAssignments   []dropVerifyMagicAssignment
}

type dropCatalogVerifyIssues struct {
	missingItems, missingMonsters, missingGroups int
	unclassifiedItems, invalidProbabilities      int
	invalidRanges, duplicateEdges, orphanEdges   int
}

func (i dropCatalogVerifyIssues) total() int {
	return i.missingItems + i.missingMonsters + i.missingGroups + i.unclassifiedItems +
		i.invalidProbabilities + i.invalidRanges + i.duplicateEdges + i.orphanEdges
}

type dropCatalogVerifyReport struct {
	rows     dropCatalogVerifySnapshot
	issues   dropCatalogVerifyIssues
	skipped  dropCatalogVerifySkipped
	warnings []string
}

type dropCatalogVerifySkipped struct {
	disabledItemEdges, disabledMonsterEdges, disabledGroupIDs int
}

func verifyDropCatalog(ctx context.Context, arguments []string, output io.Writer) (runErr error) {
	flags := flag.NewFlagSet("sro-data verify-drop-catalog", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return errDropCatalogVerifyUsage
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if output == nil {
		output = io.Discard
	}
	config.Initialize()
	database, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database: %w", err)
	}
	defer func() { runErr = errors.Join(runErr, database.Close()) }()

	snapshot, err := loadDropCatalogVerifySnapshot(ctx, database)
	if err != nil {
		return err
	}
	report := validateDropCatalogSnapshot(snapshot)
	if err = writeDropCatalogVerifyReport(output, report); err != nil {
		return err
	}
	if report.issues.total() != 0 {
		return fmt.Errorf("%w: %d issue(s)", errDropCatalogInvalid, report.issues.total())
	}
	return nil
}

func loadDropCatalogVerifySnapshot(ctx context.Context, database *sql.DB) (dropCatalogVerifySnapshot, error) {
	if database == nil {
		return dropCatalogVerifySnapshot{}, errors.New("drop catalog database is unavailable")
	}
	snapshot := dropCatalogVerifySnapshot{
		items:              make(map[uint32]dropVerifyItem),
		monsters:           make(map[uint32]struct{}),
		commonObjects:      make(map[uint32]dropVerifyCommonObject),
		magicOptionIDs:     make(map[uint32]struct{}),
		magicOptionCodes:   make(map[string]int),
		magicApplicability: make(map[uint32]int),
	}
	if err := loadDropVerifyItems(ctx, database, &snapshot); err != nil {
		return dropCatalogVerifySnapshot{}, err
	}
	if err := loadDropVerifyCommonObjects(ctx, database, &snapshot); err != nil {
		return dropCatalogVerifySnapshot{}, err
	}
	if err := loadDropVerifyReferenceSets(ctx, database, &snapshot); err != nil {
		return dropCatalogVerifySnapshot{}, err
	}
	loaders := []func(context.Context, *sql.DB, *dropCatalogVerifySnapshot) error{
		loadDropVerifyDirect, loadDropVerifyRandom, loadDropVerifyGroups, loadDropVerifyGold,
		loadDropVerifyClasses, loadDropVerifyAssignments, loadDropVerifyReserved,
		loadDropVerifyOptLevels, loadDropVerifyMagicAssignments,
	}
	for _, load := range loaders {
		if err := load(ctx, database, &snapshot); err != nil {
			return dropCatalogVerifySnapshot{}, err
		}
	}
	return snapshot, nil
}

func loadDropVerifyItems(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `ItemID`,`TypeID_2`,`TypeID_3`,`TypeID_4`,`Rarity`,`ItemClass` FROM `SRO_SHARD`.`ITEMDATA`")
	if err != nil {
		return fmt.Errorf("load drop item references: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uint32
		var item dropVerifyItem
		if err = rows.Scan(&id, &item.typeID2, &item.typeID3, &item.typeID4, &item.rarity, &item.itemClass); err != nil {
			return fmt.Errorf("scan drop item reference: %w", err)
		}
		snapshot.items[id] = item
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate drop item references: %w", err)
	}
	return nil
}

func loadDropVerifyCommonObjects(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `ID`,`SERVICE`,`TYPE_ID_1` FROM `SRO_SHARD`.`REF_OBJ_COMMON_VSRO`")
	if err != nil {
		return fmt.Errorf("load common drop references: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uint32
		var object dropVerifyCommonObject
		if err = rows.Scan(&id, &object.service, &object.typeID1); err != nil {
			return fmt.Errorf("scan common drop reference: %w", err)
		}
		snapshot.commonObjects[id] = object
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate common drop references: %w", err)
	}
	return nil
}

func loadDropVerifyReferenceSets(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `RefObjID` FROM `SRO_SHARD`.`CHAR_REF_DATA`")
	if err != nil {
		return fmt.Errorf("load drop monster references: %w", err)
	}
	for rows.Next() {
		var id uint32
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan drop monster reference: %w", err)
		}
		snapshot.monsters[id] = struct{}{}
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate drop monster references: %w", err)
	}
	_ = rows.Close()

	rows, err = database.QueryContext(ctx, "SELECT `ID`,`CODE_NAME` FROM `SRO_SHARD`.`REF_MAGIC_OPTION`")
	if err != nil {
		return fmt.Errorf("load drop magic-option references: %w", err)
	}
	for rows.Next() {
		var id uint32
		var code string
		if err = rows.Scan(&id, &code); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan drop magic-option reference: %w", err)
		}
		snapshot.magicOptionIDs[id] = struct{}{}
		snapshot.magicOptionCodes[code]++
	}
	if err = rows.Err(); err != nil {
		_ = rows.Close()
		return fmt.Errorf("iterate drop magic-option references: %w", err)
	}
	_ = rows.Close()

	rows, err = database.QueryContext(ctx, "SELECT `REF_MAGIC_OPTION_ID`,COUNT(*) FROM `SRO_SHARD`.`REF_MAGIC_OPTION_APPLICABILITY` GROUP BY `REF_MAGIC_OPTION_ID`")
	if err != nil {
		return fmt.Errorf("load drop magic-option applicability: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id uint32
		var count int
		if err = rows.Scan(&id, &count); err != nil {
			return fmt.Errorf("scan drop magic-option applicability: %w", err)
		}
		snapshot.magicApplicability[id] = count
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate drop magic-option applicability: %w", err)
	}
	return nil
}

func loadDropVerifyDirect(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	query := "SELECT `SERVICE`,`REF_MONSTER_ID`,`REF_ITEM_ID`,`DROP_AMOUNT_MIN`,`DROP_AMOUNT_MAX`,`DROP_RATIO`,`REF_MAGIC_OPTION_ID1`,`REF_MAGIC_OPTION_ID2`,`REF_MAGIC_OPTION_ID3`,`REF_MAGIC_OPTION_ID4`,`REF_MAGIC_OPTION_ID5`,`REF_MAGIC_OPTION_ID6`,`REF_MAGIC_OPTION_ID7`,`REF_MAGIC_OPTION_ID8`,`REF_MAGIC_OPTION_ID9` FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_DROP`"
	rows, err := database.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("load direct drop verification rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dropVerifyDirect
		var options [9]sql.NullInt64
		dest := []any{&row.service, &row.monsterID, &row.itemID, &row.minimum, &row.maximum, &row.probability}
		for index := range options {
			dest = append(dest, &options[index])
		}
		if err = rows.Scan(dest...); err != nil {
			return fmt.Errorf("scan direct drop verification row: %w", err)
		}
		for _, option := range options {
			if option.Valid && option.Int64 > 0 {
				row.magicOptionIDs = append(row.magicOptionIDs, uint32(option.Int64))
			}
		}
		snapshot.direct = append(snapshot.direct, row)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate direct drop verification rows: %w", err)
	}
	return nil
}

func loadDropVerifyRandom(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `SERVICE`,`REF_MONSTER_ID`,`REF_ITEM_GROUP_ID`,`OVERLAP`,`DROP_AMOUNT_MIN`,`DROP_AMOUNT_MAX`,`DROP_RATIO` FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_RND_DROP`")
	if err != nil {
		return fmt.Errorf("load random drop verification rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dropVerifyRandom
		if err = rows.Scan(&row.service, &row.monsterID, &row.groupID, &row.overlap, &row.minimum, &row.maximum, &row.probability); err != nil {
			return fmt.Errorf("scan random drop verification row: %w", err)
		}
		snapshot.random = append(snapshot.random, row)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate random drop verification rows: %w", err)
	}
	return nil
}

func loadDropVerifyGroups(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `SERVICE`,`REF_ITEM_GROUP_ID`,`REF_ITEM_ID`,`SELECT_RATIO` FROM `SRO_SHARD`.`REF_DROP_ITEM_GROUP`")
	if err != nil {
		return fmt.Errorf("load drop-group verification rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dropVerifyGroup
		if err = rows.Scan(&row.service, &row.groupID, &row.itemID, &row.probability); err != nil {
			return fmt.Errorf("scan drop-group verification row: %w", err)
		}
		snapshot.groups = append(snapshot.groups, row)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate drop-group verification rows: %w", err)
	}
	return nil
}

func loadDropVerifyGold(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `SERVICE`,`MON_LEVEL`,`DROP_PROB`,`GOLD_MIN`,`GOLD_MAX` FROM `SRO_SHARD`.`REF_DROP_GOLD`")
	if err != nil {
		return fmt.Errorf("load gold-drop verification rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dropVerifyGold
		if err = rows.Scan(&row.service, &row.level, &row.probability, &row.minimum, &row.maximum); err != nil {
			return fmt.Errorf("scan gold-drop verification row: %w", err)
		}
		snapshot.gold = append(snapshot.gold, row)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate gold-drop verification rows: %w", err)
	}
	return nil
}

func loadDropVerifyClasses(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `FAMILY`,`MON_LEVEL`,`PROB_GROUP`,`PROB` FROM `SRO_SHARD`.`REF_DROP_CLASS_SELECTION`")
	if err != nil {
		return fmt.Errorf("load class-selection verification rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dropVerifyClass
		if err = rows.Scan(&row.family, &row.level, &row.group, &row.probability); err != nil {
			return fmt.Errorf("scan class-selection verification row: %w", err)
		}
		snapshot.classes = append(snapshot.classes, row)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate class-selection verification rows: %w", err)
	}
	return nil
}

func loadDropVerifyAssignments(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	return loadDropVerifyAssignmentTable(ctx, database, "REF_DROP_ITEM_ASSIGN", false, &snapshot.assignments)
}

func loadDropVerifyReserved(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	return loadDropVerifyAssignmentTable(ctx, database, "REF_CUSTOMIZING_RESERVED_ITEM_DROP_FOR_MONSTER", true, &snapshot.reserved)
}

func loadDropVerifyAssignmentTable(ctx context.Context, database *sql.DB, table string, hasCountry bool, target *[]dropVerifyAssignment) error {
	columns := "`SERVICE`,`SOURCE_ROW`,`REF_ITEM_ID`,`PROB_RELATIVE`,`PROB_ABSOLUTE`,`ASSIGNED_GROUP`,`DROP_COUNT`"
	if hasCountry {
		columns += ",`COUNTRY_MASK`"
	}
	rows, err := database.QueryContext(ctx, "SELECT "+columns+" FROM `SRO_SHARD`.`"+table+"`")
	if err != nil {
		return fmt.Errorf("load %s verification rows: %w", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dropVerifyAssignment
		dest := []any{&row.service, &row.sourceRow, &row.itemID, &row.relative, &row.absolute, &row.assignedGroup, &row.dropCount}
		if hasCountry {
			dest = append(dest, &row.countryMask)
		}
		if err = rows.Scan(dest...); err != nil {
			return fmt.Errorf("scan %s verification row: %w", table, err)
		}
		*target = append(*target, row)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate %s verification rows: %w", table, err)
	}
	return nil
}

func loadDropVerifyOptLevels(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `OPT_LEVEL`,`PROB`,`REQ_ONLINE_TIME` FROM `SRO_SHARD`.`REF_DROP_OPT_LEVEL_SELECTION`")
	if err != nil {
		return fmt.Errorf("load option-level verification rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dropVerifyOptLevel
		if err = rows.Scan(&row.level, &row.probability, &row.required); err != nil {
			return fmt.Errorf("scan option-level verification row: %w", err)
		}
		snapshot.optLevels = append(snapshot.optLevels, row)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate option-level verification rows: %w", err)
	}
	return nil
}

func loadDropVerifyMagicAssignments(ctx context.Context, database *sql.DB, snapshot *dropCatalogVerifySnapshot) error {
	rows, err := database.QueryContext(ctx, "SELECT `SERVICE`,`COUNTRY`,`TYPE_ID3`,`TYPE_ID4`,`SLOT`,`OPTION_CODE_NAME` FROM `SRO_SHARD`.`REF_DROP_MAGIC_OPTION_ASSIGN`")
	if err != nil {
		return fmt.Errorf("load magic-option assignment verification rows: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var row dropVerifyMagicAssignment
		if err = rows.Scan(&row.service, &row.country, &row.typeID3, &row.typeID4, &row.slot, &row.code); err != nil {
			return fmt.Errorf("scan magic-option assignment verification row: %w", err)
		}
		snapshot.magicAssignments = append(snapshot.magicAssignments, row)
	}
	if err = rows.Err(); err != nil {
		return fmt.Errorf("iterate magic-option assignment verification rows: %w", err)
	}
	return nil
}

func validateDropCatalogSnapshot(snapshot dropCatalogVerifySnapshot) dropCatalogVerifyReport {
	issues := dropCatalogVerifyIssues{}
	skipped := dropCatalogVerifySkipped{}
	groupIDs := make(map[uint32]struct{})
	referencedGroups := make(map[uint32]struct{})
	disabledReferencedGroups := make(map[uint32]struct{})
	classPairs := make(map[string]struct{})
	assignmentPairs := make(map[string]struct{})
	seenDirect := make(map[[2]uint32]struct{})
	seenRandom := make(map[[2]uint32]struct{})
	seenGroups := make(map[[2]uint32]struct{})
	seenClasses := make(map[string]struct{})
	seenAssignments := make(map[string]struct{})
	seenReserved := make(map[string]struct{})
	seenOptLevels := make(map[int]struct{})
	seenMagicAssignments := make(map[string]struct{})

	for _, row := range snapshot.direct {
		if row.service != 0 && row.service != 1 {
			issues.invalidRanges++
		}
		if row.service != 1 {
			continue
		}
		itemState := dropVerifyItemReferenceState(snapshot, row.itemID)
		monsterState := dropVerifyMonsterReferenceState(snapshot, row.monsterID)
		if itemState == dropVerifyReferenceDisabled {
			skipped.disabledItemEdges++
		}
		if monsterState == dropVerifyReferenceDisabled {
			skipped.disabledMonsterEdges++
		}
		if itemState == dropVerifyReferenceBroken {
			issues.missingItems++
		}
		if monsterState == dropVerifyReferenceBroken {
			issues.missingMonsters++
		}
		if itemState == dropVerifyReferenceDisabled || monsterState == dropVerifyReferenceDisabled {
			continue
		}
		if !unitProbability(row.probability) {
			issues.invalidProbabilities++
		}
		if row.maximum < row.minimum || row.minimum < 0 {
			issues.invalidRanges++
		}
		key := [2]uint32{row.monsterID, row.itemID}
		if _, duplicate := seenDirect[key]; duplicate {
			issues.duplicateEdges++
		}
		seenDirect[key] = struct{}{}
		for _, optionID := range row.magicOptionIDs {
			if _, ok := snapshot.magicOptionIDs[optionID]; !ok || snapshot.magicApplicability[optionID] == 0 {
				issues.orphanEdges++
			}
		}
	}

	for _, row := range snapshot.random {
		if row.service != 0 && row.service != 1 {
			issues.invalidRanges++
		}
		if row.service != 1 {
			continue
		}
		monsterState := dropVerifyMonsterReferenceState(snapshot, row.monsterID)
		if monsterState == dropVerifyReferenceDisabled {
			skipped.disabledMonsterEdges++
			disabledReferencedGroups[row.groupID] = struct{}{}
			continue
		}
		if monsterState == dropVerifyReferenceBroken {
			issues.missingMonsters++
		}
		referencedGroups[row.groupID] = struct{}{}
		if !unitProbability(row.probability) {
			issues.invalidProbabilities++
		}
		if row.maximum < row.minimum || row.overlap < 0 || row.overlap > 1 {
			issues.invalidRanges++
		}
		key := [2]uint32{row.monsterID, row.groupID}
		if _, duplicate := seenRandom[key]; duplicate {
			issues.duplicateEdges++
		}
		seenRandom[key] = struct{}{}
	}

	for _, row := range snapshot.groups {
		if row.service != 0 && row.service != 1 {
			issues.invalidRanges++
		}
		if row.service != 1 {
			continue
		}
		if _, disabled := disabledReferencedGroups[row.groupID]; disabled {
			if _, active := referencedGroups[row.groupID]; !active {
				if dropVerifyItemReferenceState(snapshot, row.itemID) == dropVerifyReferenceDisabled {
					skipped.disabledItemEdges++
				}
				continue
			}
		}
		itemState := dropVerifyItemReferenceState(snapshot, row.itemID)
		if itemState == dropVerifyReferenceDisabled {
			skipped.disabledItemEdges++
			continue
		}
		if itemState == dropVerifyReferenceBroken {
			issues.missingItems++
		}
		groupIDs[row.groupID] = struct{}{}
		if !unitProbability(row.probability) {
			issues.invalidProbabilities++
		}
		key := [2]uint32{row.groupID, row.itemID}
		if _, duplicate := seenGroups[key]; duplicate {
			issues.duplicateEdges++
		}
		seenGroups[key] = struct{}{}
	}
	for groupID := range referencedGroups {
		if _, ok := groupIDs[groupID]; !ok {
			issues.missingGroups++
		}
	}
	for groupID := range groupIDs {
		if _, ok := referencedGroups[groupID]; !ok {
			issues.orphanEdges++
		}
	}
	for groupID := range disabledReferencedGroups {
		if _, active := referencedGroups[groupID]; !active {
			skipped.disabledGroupIDs++
		}
	}

	goldEnabled := false
	seenGold := make(map[uint32]struct{})
	for _, row := range snapshot.gold {
		if row.service != 0 && row.service != 1 {
			issues.invalidRanges++
		}
		if row.service != 1 {
			continue
		}
		goldEnabled = true
		if !percentageProbability(row.probability) {
			issues.invalidProbabilities++
		}
		if row.level == 0 || row.maximum < row.minimum {
			issues.invalidRanges++
		}
		if _, duplicate := seenGold[row.level]; duplicate {
			issues.duplicateEdges++
		}
		seenGold[row.level] = struct{}{}
	}
	if goldEnabled {
		for id := uint32(1); id <= 3; id++ {
			if _, ok := snapshot.items[id]; !ok {
				issues.missingItems++
			}
		}
	}

	validFamilies := dropVerifyFamilies()
	for _, row := range snapshot.classes {
		family := strings.ToUpper(strings.TrimSpace(row.family))
		key := fmt.Sprintf("%s/%d/%d", family, row.level, row.group)
		if _, duplicate := seenClasses[key]; duplicate {
			issues.duplicateEdges++
		}
		seenClasses[key] = struct{}{}
		if row.level <= 0 || row.group <= 0 || !validFamilies[family] {
			issues.invalidRanges++
			continue
		}
		if !unitProbability(row.probability) {
			issues.invalidProbabilities++
		}
		classPairs[fmt.Sprintf("%s/%d", family, row.group-1)] = struct{}{}
	}

	validateAssignments := func(rows []dropVerifyAssignment, reserved bool) {
		seen := seenAssignments
		if reserved {
			seen = seenReserved
		}
		for _, row := range rows {
			if row.service != 0 && row.service != 1 {
				issues.invalidRanges++
			}
			if row.service != 1 {
				continue
			}
			itemState := dropVerifyItemReferenceState(snapshot, row.itemID)
			if itemState == dropVerifyReferenceDisabled {
				skipped.disabledItemEdges++
				continue
			}
			item, exists := snapshot.items[row.itemID]
			if itemState == dropVerifyReferenceBroken || !exists {
				issues.missingItems++
			}
			if !nonNegativeProbability(row.relative) || !percentageProbability(row.absolute) {
				issues.invalidProbabilities++
			}
			if row.dropCount == 0 || row.assignedGroup < -1 || reserved && (row.countryMask < 1 || row.countryMask > 3) {
				issues.invalidRanges++
			}
			key := fmt.Sprintf("%d/%d/%016x/%016x/%d/%d/%d", row.service, row.itemID, math.Float64bits(row.relative), math.Float64bits(row.absolute), row.assignedGroup, row.dropCount, row.countryMask)
			if _, duplicate := seen[key]; duplicate {
				issues.duplicateEdges++
			}
			seen[key] = struct{}{}
			if !exists {
				continue
			}
			family := classifyDropVerifyFamily(item.typeID2, item.typeID3, item.typeID4, item.rarity)
			if family == "" {
				issues.unclassifiedItems++
				continue
			}
			group := row.assignedGroup
			if group < 0 {
				group = item.itemClass
			}
			group--
			if group < 0 {
				issues.invalidRanges++
				continue
			}
			assignmentPairs[fmt.Sprintf("%s/%d", family, group)] = struct{}{}
		}
	}
	validateAssignments(snapshot.assignments, false)
	validateAssignments(snapshot.reserved, true)
	for pair := range classPairs {
		if _, ok := assignmentPairs[pair]; !ok {
			issues.orphanEdges++
		}
	}
	for pair := range assignmentPairs {
		if _, ok := classPairs[pair]; !ok {
			issues.orphanEdges++
		}
	}

	sortedOpts := append([]dropVerifyOptLevel(nil), snapshot.optLevels...)
	sort.Slice(sortedOpts, func(i, j int) bool { return sortedOpts[i].level < sortedOpts[j].level })
	previous := -1.0
	for _, row := range sortedOpts {
		if _, duplicate := seenOptLevels[row.level]; duplicate {
			issues.duplicateEdges++
		}
		seenOptLevels[row.level] = struct{}{}
		if row.level < 0 || row.level > 255 || !unitProbability(row.probability) {
			if !unitProbability(row.probability) {
				issues.invalidProbabilities++
			} else {
				issues.invalidRanges++
			}
		}
		if finite(row.probability) && row.probability < previous {
			issues.invalidRanges++
		}
		previous = row.probability
	}

	for _, row := range snapshot.magicAssignments {
		if row.service != 0 && row.service != 1 {
			issues.invalidRanges++
		}
		if row.service != 1 {
			continue
		}
		key := fmt.Sprintf("%d/%d/%d/%d", row.country, row.typeID3, row.typeID4, row.slot)
		if _, duplicate := seenMagicAssignments[key]; duplicate {
			issues.duplicateEdges++
		}
		seenMagicAssignments[key] = struct{}{}
		if row.country < 0 || row.country > 3 || row.typeID3 <= 0 || row.typeID4 <= 0 || row.slot < 0 {
			issues.invalidRanges++
		}
		if snapshot.magicOptionCodes[row.code] != 1 {
			issues.orphanEdges++
		}
	}

	emptyOptional := make([]string, 0, 5)
	for _, table := range []struct {
		name string
		rows int
	}{
		{"class_selection", len(snapshot.classes)},
		{"item_assign", len(snapshot.assignments)},
		{"opt_levels", len(snapshot.optLevels)},
		{"reserved_assign", len(snapshot.reserved)},
		{"magic_assign", len(snapshot.magicAssignments)},
	} {
		if table.rows == 0 {
			emptyOptional = append(emptyOptional, table.name)
		}
	}
	warnings := make([]string, 0, 2)
	if len(emptyOptional) != 0 {
		warnings = append(warnings, "empty_optional="+strings.Join(emptyOptional, ","))
	}
	if len(snapshot.direct) == 0 || len(snapshot.random) == 0 || len(snapshot.groups) == 0 || len(snapshot.gold) == 0 {
		warnings = append(warnings, "core_catalog_incomplete")
	}
	return dropCatalogVerifyReport{rows: snapshot, issues: issues, skipped: skipped, warnings: warnings}
}

type dropVerifyReferenceState byte

const (
	dropVerifyReferenceEnabled dropVerifyReferenceState = iota
	dropVerifyReferenceDisabled
	dropVerifyReferenceBroken
)

func dropVerifyItemReferenceState(snapshot dropCatalogVerifySnapshot, id uint32) dropVerifyReferenceState {
	if _, exists := snapshot.items[id]; exists {
		return dropVerifyReferenceEnabled
	}
	if object, exists := snapshot.commonObjects[id]; exists && object.service == 0 && object.typeID1 == 3 {
		return dropVerifyReferenceDisabled
	}
	return dropVerifyReferenceBroken
}

func dropVerifyMonsterReferenceState(snapshot dropCatalogVerifySnapshot, id uint32) dropVerifyReferenceState {
	if _, exists := snapshot.monsters[id]; exists {
		return dropVerifyReferenceEnabled
	}
	if object, exists := snapshot.commonObjects[id]; exists && object.service == 0 && object.typeID1 == 1 {
		return dropVerifyReferenceDisabled
	}
	return dropVerifyReferenceBroken
}

func dropVerifyFamilies() map[string]bool {
	return map[string]bool{
		"EQUIP": true, "RAREEQUIP": true, "RECOVER": true, "CURE": true,
		"AMMO": true, "SCROLL": true, "REINFORCE": true, "ALCHEMY_TABLET": true,
		"ALCHEMY_MAGICSTONE": true, "ALCHEMY_ATTRSTONE": true,
	}
}

// classifyDropVerifyFamily intentionally mirrors
// service.ClassifyDropProfileFamily without importing the Agent runtime into
// this maintenance command.
func classifyDropVerifyFamily(typeID2, typeID3, typeID4, rarity int) string {
	if typeID2 == 1 {
		if rarity != 0 {
			return "RAREEQUIP"
		}
		return "EQUIP"
	}
	if typeID2 != 3 {
		return ""
	}
	switch typeID3 {
	case 1:
		return "RECOVER"
	case 2:
		return "CURE"
	case 3:
		return "SCROLL"
	case 4:
		return "AMMO"
	case 10:
		return "REINFORCE"
	case 11:
		switch typeID4 {
		case 1:
			return "ALCHEMY_MAGICSTONE"
		case 2:
			return "ALCHEMY_ATTRSTONE"
		case 3:
			return "ALCHEMY_TABLET"
		}
	}
	return ""
}

func finite(value float64) bool                 { return !math.IsNaN(value) && !math.IsInf(value, 0) }
func unitProbability(value float64) bool        { return finite(value) && value >= 0 && value <= 1 }
func percentageProbability(value float64) bool  { return finite(value) && value >= 0 && value <= 100 }
func nonNegativeProbability(value float64) bool { return finite(value) && value >= 0 }

func writeDropCatalogVerifyReport(output io.Writer, report dropCatalogVerifyReport) error {
	if output == nil {
		return nil
	}
	rows := report.rows
	if _, err := fmt.Fprintf(output,
		"drop-catalog rows items=%d monsters=%d direct=%d random=%d group_members=%d gold=%d class_selection=%d item_assign=%d opt_levels=%d reserved_assign=%d magic_assign=%d\n",
		len(rows.items), len(rows.monsters), len(rows.direct), len(rows.random), len(rows.groups), len(rows.gold),
		len(rows.classes), len(rows.assignments), len(rows.optLevels), len(rows.reserved), len(rows.magicAssignments)); err != nil {
		return err
	}
	s := report.skipped
	if _, err := fmt.Fprintf(output,
		"drop-catalog skipped disabled_item_edges=%d disabled_monster_edges=%d disabled_group_ids=%d\n",
		s.disabledItemEdges, s.disabledMonsterEdges, s.disabledGroupIDs); err != nil {
		return err
	}
	i := report.issues
	if _, err := fmt.Fprintf(output,
		"drop-catalog issues missing_items=%d missing_monsters=%d missing_groups=%d unclassified_items=%d invalid_probabilities=%d invalid_ranges=%d duplicate_edges=%d orphan_edges=%d\n",
		i.missingItems, i.missingMonsters, i.missingGroups, i.unclassifiedItems, i.invalidProbabilities, i.invalidRanges, i.duplicateEdges, i.orphanEdges); err != nil {
		return err
	}
	for _, warning := range report.warnings {
		if _, err := fmt.Fprintf(output, "drop-catalog warning %s\n", warning); err != nil {
			return err
		}
	}
	status := "ok"
	if i.total() != 0 {
		status = "invalid"
	}
	_, err := fmt.Fprintf(output, "drop-catalog status=%s warnings=%d errors=%d\n", status, len(report.warnings), i.total())
	return err
}
