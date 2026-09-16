package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/framework/config"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var (
	errItemCoverageUsage = errors.New("usage: sro-data audit-item-coverage --output-dir <directory> [--fail-on broken|unsupported]")
	errItemCoverageGate  = errors.New("item coverage gate failed")
)

const (
	itemCoverageSupported     = "supported"
	itemCoverageUnsupported   = "unsupported"
	itemCoverageBroken        = "broken"
	itemCoverageConditional   = "conditional"
	itemCoverageNotApplicable = "not_applicable"
)

type itemCoverageOptions struct {
	outputDirectory string
	failOn          string
}

type itemCoverageItem struct {
	id                         uint32
	codeName                   string
	typeID1, typeID2           int
	typeID3, typeID4           int
	canDrop, canUse, decayTime int
	params                     [20]int64
	descs                      [20]string
	runtime                    model.RefItem
}

type itemCoverageSkill struct {
	id                                                  uint32
	codeName                                            string
	basicActivity                                       int
	targetRequired, targetSelf, targetAlly, targetParty int
	targetEnemyMob, targetEnemyPlayer, targetDeadBody   int
	actionDuration, actionRange                         int
	params                                              [50]int64
	runtime                                             model.RefSkill
}

type itemCoverageMonster struct {
	id       uint32
	codeName string
	runtime  model.RefChar
}

type itemCoverageCommonObject struct {
	service, typeID1 int
	codeName         string
}

type itemCoverageDirectDrop struct {
	service                    int
	monsterID, itemID          uint32
	dropGroupType, optionLevel int
	amountMin, amountMax       int
	dropRatio                  float64
}

type itemCoverageGroupMember struct {
	service                       int
	groupID, itemID, magicGroupID uint32
	selectRatio                   float64
}

type itemCoverageRandomDrop struct {
	service            int
	monsterID, groupID uint32
	overlap, amountMin int
	amountMax          int
	dropRatio          float64
}

type itemCoverageAssignment struct {
	sourceRow, service int
	itemID             uint32
	relative, absolute float64
	assignedGroup      int
	dropCount          uint32
}

type itemCoverageReservedAssignment struct {
	sourceRow, service int
	countryMask        int
	itemID             uint32
	relative, absolute float64
	assignedGroup      int
	dropCount          uint32
}

type itemCoverageCatalog struct {
	items             []itemCoverageItem
	skills            []itemCoverageSkill
	monsters          []itemCoverageMonster
	direct            []itemCoverageDirectDrop
	groups            []itemCoverageGroupMember
	random            []itemCoverageRandomDrop
	assignments       []itemCoverageAssignment
	reserved          []itemCoverageReservedAssignment
	commonObjects     map[uint32]itemCoverageCommonObject
	collectionItemIDs []uint32
}

// itemCoverageSource deliberately exposes only reads. Keeping report creation
// behind this interface makes a database mutation impossible in the audit
// path and gives tests a proprietary-data-free catalog seam.
type itemCoverageSource interface {
	load(context.Context) (itemCoverageCatalog, error)
}

type sqlItemCoverageSource struct{ queryer frameworkdb.Queryer }

type itemCoverageAssessment struct {
	status string
	effect string
	reason string
	skill  *itemCoverageSkill
}

type itemCoverageItemRow struct {
	item       itemCoverageItem
	assessment itemCoverageAssessment
	dropCount  int
}

type itemCoverageDropRow struct {
	itemID         uint32
	itemCode       string
	sourceKind     string
	sourceID       uint32
	sourceCode     string
	groupID        uint32
	service        int
	dropRatio      *float64
	selectionRatio *float64
	amountMin      *int
	amountMax      *int
	magicGroupID   uint32
	assignedGroup  *int
	dropCount      *uint32
	countryMask    *int
	status         string
	reason         string
}

type itemCoverageReport struct {
	items               []itemCoverageItemRow
	drops               []itemCoverageDropRow
	totalItems          int
	usableItems         int
	droppable           int
	supported           int
	conditional         int
	unsupported         int
	brokenItems         int
	brokenUsable        int
	nonUsable           int
	linkedItems         int
	brokenDrops         int
	disabledDrops       int
	dropKinds           map[string]int
	unsupportedByReason map[string]int
}

// auditItemCoverage is the integration point for the sro-data command router.
// It opens only the shard pool and delegates every database operation to a
// repeatable-read, read-only transaction.
func auditItemCoverage(ctx context.Context, arguments []string, output io.Writer) error {
	options, err := parseItemCoverageOptions(arguments)
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	config.Initialize()
	database, err := frameworkdb.OpenPool(ctx, frameworkdb.ShardPool, frameworkdb.ShardPoolConfig())
	if err != nil {
		return fmt.Errorf("open shard database for item coverage: %w", err)
	}
	defer database.Close()
	return auditItemCoverageWithDB(ctx, options, output, database)
}

func auditItemCoverageWithDB(ctx context.Context, options itemCoverageOptions, output io.Writer, database *sql.DB) error {
	if database == nil {
		return errors.New("item coverage database is unavailable")
	}
	transaction, err := database.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return fmt.Errorf("begin read-only item coverage snapshot: %w", err)
	}
	defer func() { _ = transaction.Rollback() }()
	catalog, err := (&sqlItemCoverageSource{queryer: transaction}).load(ctx)
	if err != nil {
		return err
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("close read-only item coverage snapshot: %w", err)
	}
	return emitItemCoverage(options, output, catalog)
}

func parseItemCoverageOptions(arguments []string) (itemCoverageOptions, error) {
	flags := flag.NewFlagSet("sro-data audit-item-coverage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	outputDirectory := flags.String("output-dir", "", "directory for item coverage artifacts")
	failOn := flags.String("fail-on", "", "fail after writing reports when broken or unsupported rows exist")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 {
		return itemCoverageOptions{}, errItemCoverageUsage
	}
	options := itemCoverageOptions{
		outputDirectory: strings.TrimSpace(*outputDirectory),
		failOn:          strings.ToLower(strings.TrimSpace(*failOn)),
	}
	if options.outputDirectory == "" {
		return itemCoverageOptions{}, errItemCoverageUsage
	}
	if options.failOn != "" && options.failOn != itemCoverageBroken && options.failOn != itemCoverageUnsupported {
		return itemCoverageOptions{}, errItemCoverageUsage
	}
	return options, nil
}

func (source *sqlItemCoverageSource) load(ctx context.Context) (itemCoverageCatalog, error) {
	if source == nil || source.queryer == nil {
		return itemCoverageCatalog{}, errors.New("item coverage source is unavailable")
	}
	references, err := model.NewReferenceDataRepository(source.queryer)
	if err != nil {
		return itemCoverageCatalog{}, fmt.Errorf("configure item coverage reference reader: %w", err)
	}
	snapshot, err := references.Load(ctx)
	if err != nil {
		return itemCoverageCatalog{}, fmt.Errorf("load item coverage reference catalogs: %w", err)
	}
	var catalog itemCoverageCatalog
	for _, ref := range snapshot.Items {
		catalog.items = append(catalog.items, itemCoverageItemFromModel(ref))
	}
	for _, ref := range snapshot.Skills {
		catalog.skills = append(catalog.skills, itemCoverageSkillFromModel(ref))
	}
	for _, ref := range snapshot.Characters {
		catalog.monsters = append(catalog.monsters, itemCoverageMonster{id: ref.ID, codeName: ref.CodeName, runtime: ref})
	}
	sort.Slice(catalog.items, func(left, right int) bool { return catalog.items[left].id < catalog.items[right].id })
	sort.Slice(catalog.skills, func(left, right int) bool { return catalog.skills[left].id < catalog.skills[right].id })
	sort.Slice(catalog.monsters, func(left, right int) bool { return catalog.monsters[left].id < catalog.monsters[right].id })
	if catalog.direct, err = source.loadDirectDrops(ctx); err != nil {
		return itemCoverageCatalog{}, err
	}
	if catalog.groups, err = source.loadGroupMembers(ctx); err != nil {
		return itemCoverageCatalog{}, err
	}
	if catalog.random, err = source.loadRandomDrops(ctx); err != nil {
		return itemCoverageCatalog{}, err
	}
	if catalog.assignments, err = source.loadAssignments(ctx); err != nil {
		return itemCoverageCatalog{}, err
	}
	if catalog.reserved, err = source.loadReservedAssignments(ctx); err != nil {
		return itemCoverageCatalog{}, err
	}
	if catalog.commonObjects, err = source.loadCommonObjects(ctx); err != nil {
		return itemCoverageCatalog{}, err
	}
	if catalog.collectionItemIDs, err = source.loadCollectionItemIDs(ctx); err != nil {
		return itemCoverageCatalog{}, err
	}
	return catalog, nil
}

func (source *sqlItemCoverageSource) loadCommonObjects(ctx context.Context) (map[uint32]itemCoverageCommonObject, error) {
	rows, err := source.queryer.QueryContext(ctx, "SELECT `ID`,`SERVICE`,`CODE_NAME`,`TYPE_ID_1` FROM `REF_OBJ_COMMON_VSRO`")
	if err != nil {
		return nil, fmt.Errorf("query common item coverage references: %w", err)
	}
	defer rows.Close()
	objects := make(map[uint32]itemCoverageCommonObject)
	for rows.Next() {
		var id uint32
		var object itemCoverageCommonObject
		if err = rows.Scan(&id, &object.service, &object.codeName, &object.typeID1); err != nil {
			return nil, fmt.Errorf("scan common item coverage reference: %w", err)
		}
		objects[id] = object
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate common item coverage references: %w", err)
	}
	return objects, nil
}

func itemCoverageItemFromModel(ref model.RefItem) itemCoverageItem {
	return itemCoverageItem{
		id: ref.ID, codeName: ref.CodeName,
		typeID1: ref.TypeID1, typeID2: ref.TypeID2, typeID3: ref.TypeID3, typeID4: ref.TypeID4,
		canDrop: coverageFlag(ref.IsDropable), canUse: coverageFlag(ref.IsUseable), decayTime: ref.DecayTime,
		params: [20]int64{
			int64(ref.Param1), int64(ref.Param2), int64(ref.Param3), int64(ref.Param4), int64(ref.Param5),
			int64(ref.Param6), int64(ref.Param7), int64(ref.Param8), int64(ref.Param9), int64(ref.Param10),
			int64(ref.Param11), int64(ref.Param12), int64(ref.Param13), int64(ref.Param14), int64(ref.Param15),
			int64(ref.Param16), int64(ref.Param17), int64(ref.Param18), int64(ref.Param19), int64(ref.Param20),
		},
		descs: [20]string{
			ref.Param1Desc, ref.Param2Desc, ref.Param3Desc, ref.Param4Desc, ref.Param5Desc,
			ref.Param6Desc, ref.Param7Desc, ref.Param8Desc, ref.Param9Desc, ref.Param10Desc,
			ref.Param11Desc, ref.Param12Desc, ref.Param13Desc, ref.Param14Desc, ref.Param15Desc,
			ref.Param16Desc, ref.Param17Desc, ref.Param18Desc, ref.Param19Desc, ref.Param20Desc,
		},
		runtime: ref,
	}
}

func itemCoverageSkillFromModel(ref model.RefSkill) itemCoverageSkill {
	skill := itemCoverageSkill{
		id: ref.ID, codeName: ref.CodeName, basicActivity: int(ref.BasicActivity),
		targetRequired: coverageFlag(ref.TargetRequired), targetSelf: coverageFlag(ref.TargetSelf),
		targetAlly: coverageFlag(ref.TargetAlly), targetParty: coverageFlag(ref.TargetParty),
		targetEnemyMob: coverageFlag(ref.TargetEnemyMob), targetEnemyPlayer: coverageFlag(ref.TargetEnemyPVP),
		targetDeadBody: coverageFlag(ref.TargetDeadBody), actionDuration: ref.ActionDuration, actionRange: ref.ActionRange,
		runtime: ref,
	}
	for index, value := range ref.Params {
		skill.params[index] = int64(value)
	}
	return skill
}

func coverageFlag(value bool) int {
	if value {
		return 1
	}
	return 0
}

func (source *sqlItemCoverageSource) loadDirectDrops(ctx context.Context) ([]itemCoverageDirectDrop, error) {
	const query = "SELECT `SERVICE`,`REF_MONSTER_ID`,`REF_ITEM_ID`,`DROP_GROUP_TYPE`,`OPT_LEVEL`,`DROP_AMOUNT_MIN`,`DROP_AMOUNT_MAX`,`DROP_RATIO` FROM `REF_MONSTER_ASSIGNED_ITEM_DROP` ORDER BY `REF_MONSTER_ID`,`REF_ITEM_ID`"
	rows, err := source.queryer.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query direct monster drop catalog: %w", err)
	}
	defer rows.Close()
	var drops []itemCoverageDirectDrop
	for rows.Next() {
		var drop itemCoverageDirectDrop
		if err := rows.Scan(&drop.service, &drop.monsterID, &drop.itemID, &drop.dropGroupType, &drop.optionLevel, &drop.amountMin, &drop.amountMax, &drop.dropRatio); err != nil {
			return nil, fmt.Errorf("scan direct monster drop: %w", err)
		}
		drops = append(drops, drop)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate direct monster drops: %w", err)
	}
	return drops, nil
}

func (source *sqlItemCoverageSource) loadGroupMembers(ctx context.Context) ([]itemCoverageGroupMember, error) {
	const query = "SELECT `SERVICE`,`REF_ITEM_GROUP_ID`,`REF_ITEM_ID`,`REF_MAGIC_GROUP_ID`,`SELECT_RATIO` FROM `REF_DROP_ITEM_GROUP` ORDER BY `REF_ITEM_GROUP_ID`,`REF_ITEM_ID`"
	rows, err := source.queryer.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query random drop group catalog: %w", err)
	}
	defer rows.Close()
	var members []itemCoverageGroupMember
	for rows.Next() {
		var member itemCoverageGroupMember
		if err := rows.Scan(&member.service, &member.groupID, &member.itemID, &member.magicGroupID, &member.selectRatio); err != nil {
			return nil, fmt.Errorf("scan random drop group member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate random drop group members: %w", err)
	}
	return members, nil
}

func (source *sqlItemCoverageSource) loadRandomDrops(ctx context.Context) ([]itemCoverageRandomDrop, error) {
	const query = "SELECT `SERVICE`,`REF_MONSTER_ID`,`REF_ITEM_GROUP_ID`,`OVERLAP`,`DROP_AMOUNT_MIN`,`DROP_AMOUNT_MAX`,`DROP_RATIO` FROM `REF_MONSTER_ASSIGNED_ITEM_RND_DROP` ORDER BY `REF_MONSTER_ID`,`REF_ITEM_GROUP_ID`"
	rows, err := source.queryer.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query monster random-drop assignments: %w", err)
	}
	defer rows.Close()
	var drops []itemCoverageRandomDrop
	for rows.Next() {
		var drop itemCoverageRandomDrop
		if err := rows.Scan(&drop.service, &drop.monsterID, &drop.groupID, &drop.overlap, &drop.amountMin, &drop.amountMax, &drop.dropRatio); err != nil {
			return nil, fmt.Errorf("scan monster random-drop assignment: %w", err)
		}
		drops = append(drops, drop)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monster random-drop assignments: %w", err)
	}
	return drops, nil
}

func (source *sqlItemCoverageSource) loadAssignments(ctx context.Context) ([]itemCoverageAssignment, error) {
	const query = "SELECT `SOURCE_ROW`,`SERVICE`,`REF_ITEM_ID`,`PROB_RELATIVE`,`PROB_ABSOLUTE`,`ASSIGNED_GROUP`,`DROP_COUNT` FROM `REF_DROP_ITEM_ASSIGN` ORDER BY `SOURCE_ROW`"
	rows, err := source.queryer.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query general drop-item assignments: %w", err)
	}
	defer rows.Close()
	var assignments []itemCoverageAssignment
	for rows.Next() {
		var assignment itemCoverageAssignment
		if err := rows.Scan(&assignment.sourceRow, &assignment.service, &assignment.itemID, &assignment.relative, &assignment.absolute, &assignment.assignedGroup, &assignment.dropCount); err != nil {
			return nil, fmt.Errorf("scan general drop-item assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate general drop-item assignments: %w", err)
	}
	return assignments, nil
}

func (source *sqlItemCoverageSource) loadReservedAssignments(ctx context.Context) ([]itemCoverageReservedAssignment, error) {
	const query = "SELECT `SOURCE_ROW`,`SERVICE`,`COUNTRY_MASK`,`REF_ITEM_ID`,`PROB_RELATIVE`,`PROB_ABSOLUTE`,`ASSIGNED_GROUP`,`DROP_COUNT` FROM `REF_CUSTOMIZING_RESERVED_ITEM_DROP_FOR_MONSTER` ORDER BY `SOURCE_ROW`"
	rows, err := source.queryer.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query reserved drop-item assignments: %w", err)
	}
	defer rows.Close()
	var assignments []itemCoverageReservedAssignment
	for rows.Next() {
		var assignment itemCoverageReservedAssignment
		if err := rows.Scan(&assignment.sourceRow, &assignment.service, &assignment.countryMask, &assignment.itemID, &assignment.relative, &assignment.absolute, &assignment.assignedGroup, &assignment.dropCount); err != nil {
			return nil, fmt.Errorf("scan reserved drop-item assignment: %w", err)
		}
		assignments = append(assignments, assignment)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate reserved drop-item assignments: %w", err)
	}
	return assignments, nil
}

func (source *sqlItemCoverageSource) loadCollectionItemIDs(ctx context.Context) ([]uint32, error) {
	rows, err := source.queryer.QueryContext(ctx, "SELECT `ITEM_REF_OBJECT_ID` FROM `REF_COLLECTION_BOOK_ITEM` ORDER BY `ITEM_REF_OBJECT_ID`")
	if err != nil {
		return nil, fmt.Errorf("query collection-book item references: %w", err)
	}
	defer rows.Close()
	var itemIDs []uint32
	for rows.Next() {
		var itemID uint32
		if err := rows.Scan(&itemID); err != nil {
			return nil, fmt.Errorf("scan collection-book item reference: %w", err)
		}
		itemIDs = append(itemIDs, itemID)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate collection-book item references: %w", err)
	}
	return itemIDs, nil
}

func emitItemCoverage(options itemCoverageOptions, output io.Writer, catalog itemCoverageCatalog) error {
	report := buildItemCoverageReport(catalog)
	itemsCSV, err := encodeItemCoverageItems(report.items)
	if err != nil {
		return err
	}
	dropsCSV, err := encodeItemCoverageDrops(report.drops)
	if err != nil {
		return err
	}
	summary := encodeItemCoverageSummary(report, options.failOn)
	if err := writeItemCoverageFiles(options.outputDirectory, map[string][]byte{
		"items.csv":        itemsCSV,
		"drop_sources.csv": dropsCSV,
		"summary.md":       summary,
	}); err != nil {
		return err
	}
	if output == nil {
		output = io.Discard
	}
	if _, err := fmt.Fprintf(output, "wrote item coverage for %d items and %d drop-source rows to %s\n", report.totalItems, len(report.drops), filepath.Clean(options.outputDirectory)); err != nil {
		return fmt.Errorf("write item coverage command output: %w", err)
	}
	if options.failOn == itemCoverageBroken && (report.brokenItems > 0 || report.brokenDrops > 0) {
		return fmt.Errorf("%w: %d broken items and %d broken drop sources", errItemCoverageGate, report.brokenItems, report.brokenDrops)
	}
	if options.failOn == itemCoverageUnsupported && (report.unsupported > 0 || report.brokenItems > 0 || report.brokenDrops > 0) {
		return fmt.Errorf("%w: %d unsupported items, %d broken items, and %d broken drop sources", errItemCoverageGate, report.unsupported, report.brokenItems, report.brokenDrops)
	}
	return nil
}

func buildItemCoverageReport(catalog itemCoverageCatalog) itemCoverageReport {
	itemsByID := make(map[uint32]itemCoverageItem, len(catalog.items))
	for _, item := range catalog.items {
		itemsByID[item.id] = item
	}
	monstersByID := make(map[uint32]string, len(catalog.monsters))
	for _, monster := range catalog.monsters {
		monstersByID[monster.id] = monster.codeName
	}
	skillsByCode := make(map[string][]itemCoverageSkill)
	skillReferences := make(map[uint32]model.RefSkill, len(catalog.skills))
	for _, skill := range catalog.skills {
		code := normalizedCoverageCode(skill.codeName)
		skillsByCode[code] = append(skillsByCode[code], skill)
		skillReferences[skill.id] = runtimeItemCoverageSkill(skill)
	}
	characterReferences := make(map[uint32]model.RefChar, len(catalog.monsters))
	for _, character := range catalog.monsters {
		ref := character.runtime
		if ref.ID == 0 {
			ref.ID, ref.CodeName = character.id, character.codeName
		}
		characterReferences[character.id] = ref
	}
	collectionItemIDs := make(map[uint32]bool, len(catalog.collectionItemIDs))
	for _, itemID := range catalog.collectionItemIDs {
		collectionItemIDs[itemID] = true
	}

	drops := buildItemCoverageDropRows(catalog, itemsByID, monstersByID)
	dropCounts := make(map[uint32]int)
	report := itemCoverageReport{
		totalItems: len(catalog.items),
		drops:      drops,
		dropKinds: map[string]int{
			"direct_monster": 0, "random_group": 0, "random_group_member": 0,
			"level_profile": 0, "reserved_profile": 0,
		},
		unsupportedByReason: make(map[string]int),
	}
	for _, drop := range drops {
		report.dropKinds[drop.sourceKind]++
		if drop.status == itemCoverageBroken {
			report.brokenDrops++
		}
		if drop.status == "disabled" {
			report.disabledDrops++
		}
		if drop.itemID != 0 && drop.status == "available" {
			dropCounts[drop.itemID]++
		}
	}
	sort.Slice(catalog.items, func(left, right int) bool { return catalog.items[left].id < catalog.items[right].id })
	for _, item := range catalog.items {
		assessment := classifyItemCoverage(item, skillsByCode, skillReferences, characterReferences, collectionItemIDs)
		report.items = append(report.items, itemCoverageItemRow{item: item, assessment: assessment, dropCount: dropCounts[item.id]})
		if item.canUse != 0 {
			report.usableItems++
		} else {
			report.nonUsable++
		}
		if item.canDrop != 0 {
			report.droppable++
		}
		if item.canUse != 0 && strings.HasPrefix(normalizedCoverageCode(item.descs[0]), "SKILL_") {
			report.linkedItems++
		}
		switch assessment.status {
		case itemCoverageSupported:
			if item.canUse != 0 {
				report.supported++
			}
		case itemCoverageConditional:
			if item.canUse != 0 {
				report.conditional++
			}
		case itemCoverageUnsupported:
			if item.canUse != 0 {
				report.unsupported++
				report.unsupportedByReason[assessment.effect+": "+assessment.reason]++
			}
		case itemCoverageBroken:
			report.brokenItems++
			if item.canUse != 0 {
				report.brokenUsable++
			}
		}
	}
	return report
}

func buildItemCoverageDropRows(catalog itemCoverageCatalog, items map[uint32]itemCoverageItem, monsters map[uint32]string) []itemCoverageDropRow {
	var rows []itemCoverageDropRow
	for _, drop := range catalog.direct {
		minimum, maximum, ratio := drop.amountMin, drop.amountMax, drop.dropRatio
		row := itemCoverageDropRow{
			itemID: drop.itemID, sourceKind: "direct_monster", sourceID: drop.monsterID,
			service: drop.service, dropRatio: &ratio, amountMin: &minimum, amountMax: &maximum,
		}
		row.itemCode, row.sourceCode, row.status, row.reason = validateItemCoverageDrop(row.itemID, row.sourceID, drop.service, items, monsters, catalog.commonObjects)
		rows = append(rows, row)
	}

	membersByGroup := make(map[uint32][]itemCoverageGroupMember)
	for _, member := range catalog.groups {
		membersByGroup[member.groupID] = append(membersByGroup[member.groupID], member)
		ratio := member.selectRatio
		row := itemCoverageDropRow{
			itemID: member.itemID, sourceKind: "random_group_member", sourceID: member.groupID,
			groupID: member.groupID, service: member.service, selectionRatio: &ratio, magicGroupID: member.magicGroupID,
		}
		item, exists := items[member.itemID]
		row.itemCode = item.codeName
		switch {
		case !exists && itemCoverageDisabledReference(catalog.commonObjects, member.itemID, 3):
			row.itemCode = catalog.commonObjects[member.itemID].codeName
			row.status, row.reason = "disabled", "REF_ITEM_ID is SERVICE=0 in REF_OBJ_COMMON_VSRO"
		case !exists:
			row.status, row.reason = itemCoverageBroken, "REF_ITEM_ID is absent from ITEMDATA"
		case member.service == 0:
			row.status, row.reason = "disabled", "drop group member SERVICE=0"
		default:
			row.status = "available"
		}
		rows = append(rows, row)
	}

	for _, drop := range catalog.random {
		members := membersByGroup[drop.groupID]
		if len(members) == 0 {
			minimum, maximum, ratio := drop.amountMin, drop.amountMax, drop.dropRatio
			row := itemCoverageDropRow{
				sourceKind: "random_group", sourceID: drop.monsterID, groupID: drop.groupID,
				service: drop.service, dropRatio: &ratio, amountMin: &minimum, amountMax: &maximum,
				status: itemCoverageBroken, reason: "REF_ITEM_GROUP_ID has no members",
			}
			row.sourceCode = monsters[drop.monsterID]
			if _, exists := monsters[drop.monsterID]; !exists {
				row.reason += "; REF_MONSTER_ID is absent from CHAR_REF_DATA"
			}
			rows = append(rows, row)
			continue
		}
		for _, member := range members {
			minimum, maximum, ratio, selection := drop.amountMin, drop.amountMax, drop.dropRatio, member.selectRatio
			row := itemCoverageDropRow{
				itemID: member.itemID, sourceKind: "random_group", sourceID: drop.monsterID,
				groupID: drop.groupID, service: minCoverageService(drop.service, member.service),
				dropRatio: &ratio, selectionRatio: &selection, amountMin: &minimum, amountMax: &maximum,
				magicGroupID: member.magicGroupID,
			}
			row.itemCode, row.sourceCode, row.status, row.reason = validateItemCoverageDrop(row.itemID, row.sourceID, row.service, items, monsters, catalog.commonObjects)
			if row.status == "disabled" && row.service == 0 {
				row.reason = "monster assignment or group member SERVICE=0"
			}
			rows = append(rows, row)
		}
	}

	for _, assignment := range catalog.assignments {
		assignedGroup, count, relative, absolute := assignment.assignedGroup, assignment.dropCount, assignment.relative, assignment.absolute
		row := itemCoverageDropRow{
			itemID: assignment.itemID, sourceKind: "level_profile", sourceID: uint32(assignment.sourceRow),
			service: assignment.service, dropRatio: &absolute, selectionRatio: &relative,
			assignedGroup: &assignedGroup, dropCount: &count,
		}
		item, exists := items[assignment.itemID]
		row.itemCode = item.codeName
		switch {
		case !exists && itemCoverageDisabledReference(catalog.commonObjects, assignment.itemID, 3):
			row.itemCode = catalog.commonObjects[assignment.itemID].codeName
			row.status, row.reason = "disabled", "REF_ITEM_ID is SERVICE=0 in REF_OBJ_COMMON_VSRO"
		case !exists:
			row.status, row.reason = itemCoverageBroken, "REF_ITEM_ID is absent from ITEMDATA"
		case assignment.service == 0:
			row.status, row.reason = "disabled", "general assignment SERVICE=0"
		default:
			row.status = "available"
		}
		rows = append(rows, row)
	}
	for _, assignment := range catalog.reserved {
		assignedGroup, count := assignment.assignedGroup, assignment.dropCount
		relative, absolute, countryMask := assignment.relative, assignment.absolute, assignment.countryMask
		row := itemCoverageDropRow{
			itemID: assignment.itemID, sourceKind: "reserved_profile", sourceID: uint32(assignment.sourceRow),
			service: assignment.service, dropRatio: &absolute, selectionRatio: &relative,
			assignedGroup: &assignedGroup, dropCount: &count, countryMask: &countryMask,
		}
		item, exists := items[assignment.itemID]
		row.itemCode = item.codeName
		switch {
		case !exists && itemCoverageDisabledReference(catalog.commonObjects, assignment.itemID, 3):
			row.itemCode = catalog.commonObjects[assignment.itemID].codeName
			row.status, row.reason = "disabled", "REF_ITEM_ID is SERVICE=0 in REF_OBJ_COMMON_VSRO"
		case !exists:
			row.status, row.reason = itemCoverageBroken, "REF_ITEM_ID is absent from ITEMDATA"
		case assignment.service == 0:
			row.status, row.reason = "disabled", "reserved assignment SERVICE=0"
		default:
			row.status = "available"
		}
		rows = append(rows, row)
	}

	sort.SliceStable(rows, func(left, right int) bool {
		a, b := rows[left], rows[right]
		if a.itemID != b.itemID {
			return a.itemID < b.itemID
		}
		if a.sourceKind != b.sourceKind {
			return a.sourceKind < b.sourceKind
		}
		if a.sourceID != b.sourceID {
			return a.sourceID < b.sourceID
		}
		if a.groupID != b.groupID {
			return a.groupID < b.groupID
		}
		return a.magicGroupID < b.magicGroupID
	})
	return rows
}

func validateItemCoverageDrop(itemID, monsterID uint32, service int, items map[uint32]itemCoverageItem, monsters map[uint32]string, common map[uint32]itemCoverageCommonObject) (itemCode, monsterCode, status, reason string) {
	item, itemExists := items[itemID]
	monsterCode, monsterExists := monsters[monsterID]
	itemCode = item.codeName
	itemDisabled := !itemExists && itemCoverageDisabledReference(common, itemID, 3)
	monsterDisabled := !monsterExists && itemCoverageDisabledReference(common, monsterID, 1)
	if itemDisabled {
		itemCode = common[itemID].codeName
	}
	if monsterDisabled {
		monsterCode = common[monsterID].codeName
	}
	var problems []string
	if !itemExists && !itemDisabled {
		problems = append(problems, "REF_ITEM_ID is absent from ITEMDATA")
	}
	if !monsterExists && !monsterDisabled {
		problems = append(problems, "REF_MONSTER_ID is absent from CHAR_REF_DATA")
	}
	if len(problems) > 0 {
		return itemCode, monsterCode, itemCoverageBroken, strings.Join(problems, "; ")
	}
	if service == 0 {
		return itemCode, monsterCode, "disabled", "drop source SERVICE=0"
	}
	if itemDisabled || monsterDisabled {
		var disabled []string
		if itemDisabled {
			disabled = append(disabled, "REF_ITEM_ID is SERVICE=0")
		}
		if monsterDisabled {
			disabled = append(disabled, "REF_MONSTER_ID is SERVICE=0")
		}
		return itemCode, monsterCode, "disabled", strings.Join(disabled, "; ") + " in REF_OBJ_COMMON_VSRO"
	}
	return itemCode, monsterCode, "available", ""
}

func itemCoverageDisabledReference(common map[uint32]itemCoverageCommonObject, id uint32, typeID1 int) bool {
	object, exists := common[id]
	return exists && object.service == 0 && object.typeID1 == typeID1
}

func minCoverageService(left, right int) int {
	if left == 0 || right == 0 {
		return 0
	}
	return 1
}

// classifyItemCoverage converts audit rows into the same immutable summaries
// used by the runtime classifier. Only duplicate-link integrity and report
// wording live here; executable family decisions remain in model.
func classifyItemCoverage(item itemCoverageItem, skills map[string][]itemCoverageSkill, skillReferences map[uint32]model.RefSkill, characterReferences map[uint32]model.RefChar, collectionItems map[uint32]bool) itemCoverageAssessment {
	if strings.TrimSpace(item.codeName) == "" {
		return itemCoverageAssessment{status: itemCoverageBroken, effect: "unknown", reason: "ITEMDATA.CodeName is empty"}
	}
	var linkedSkill *itemCoverageSkill
	linked := skills[normalizedCoverageCode(item.descs[0])]
	if len(linked) > 1 {
		return itemCoverageAssessment{status: itemCoverageBroken, effect: string(model.ItemUseFamilyLinkedSkillEffect), reason: "Desc_1 matches multiple active REF_SKILL rows"}
	}
	if len(linked) == 1 {
		candidate := linked[0]
		linkedSkill = &candidate
	}
	input := model.ItemUseCapabilityInput{
		RefItem:             runtimeItemCoverageItem(item),
		SkillReferences:     skillReferences,
		CharacterReferences: characterReferences,
	}
	if item.typeID1 == 3 && item.typeID2 == 3 && item.typeID3 == 12 && item.typeID4 == 5 {
		if item.typeID1 == 3 && item.typeID2 == 3 && item.typeID3 == 12 && item.typeID4 == 5 {
			collectionReferenceExists := collectionItems[item.id]
			input.CollectionReferenceExists = &collectionReferenceExists
		}
	}
	if linkedSkill != nil {
		ref := runtimeItemCoverageSkill(*linkedSkill)
		input.LinkedSkill = &ref
	}
	capability := model.ClassifyItemUseCapability(input)
	assessment := itemCoverageAssessment{
		status: renderItemCoverageStatus(capability), effect: string(capability.Family),
		reason: renderItemCoverageReason(capability), skill: linkedSkill,
	}
	if (capability.Family == model.ItemUseFamilyLinkedSkillEffect || capability.Family == model.ItemUseFamilyLinkedSkillBuff) && input.LinkedSkill != nil {
		assessment.effect = renderItemCoverageExecutionKind(input.LinkedSkill.ExecutionKind())
	}
	if capability.Family == model.ItemUseFamilySpeedDrug {
		assessment.effect = "movement_speed"
	}
	return assessment
}

func runtimeItemCoverageItem(item itemCoverageItem) model.RefItem {
	if item.runtime.ID != 0 {
		return item.runtime
	}
	return itemCoverageItemFromModel(model.RefItem{
		RefObject: model.RefObject{
			ID: item.id, CodeName: item.codeName, IsUseable: item.canUse != 0, IsDropable: item.canDrop != 0,
			DecayTime: item.decayTime, TypeInfo: model.TypeInfo{TypeID1: item.typeID1, TypeID2: item.typeID2, TypeID3: item.typeID3, TypeID4: item.typeID4},
		},
		Param1: int(item.params[0]), Param2: int(item.params[1]), Param3: int(item.params[2]), Param4: int(item.params[3]), Param5: int(item.params[4]),
		Param6: int(item.params[5]), Param7: int(item.params[6]), Param8: int(item.params[7]), Param9: int(item.params[8]), Param10: int(item.params[9]),
		Param11: int(item.params[10]), Param12: int(item.params[11]), Param13: int(item.params[12]), Param14: int(item.params[13]), Param15: int(item.params[14]),
		Param16: int(item.params[15]), Param17: int(item.params[16]), Param18: int(item.params[17]), Param19: int(item.params[18]), Param20: int(item.params[19]),
		Param1Desc: item.descs[0], Param2Desc: item.descs[1], Param3Desc: item.descs[2], Param4Desc: item.descs[3], Param5Desc: item.descs[4],
		Param6Desc: item.descs[5], Param7Desc: item.descs[6], Param8Desc: item.descs[7], Param9Desc: item.descs[8], Param10Desc: item.descs[9],
		Param11Desc: item.descs[10], Param12Desc: item.descs[11], Param13Desc: item.descs[12], Param14Desc: item.descs[13], Param15Desc: item.descs[14],
		Param16Desc: item.descs[15], Param17Desc: item.descs[16], Param18Desc: item.descs[17], Param19Desc: item.descs[18], Param20Desc: item.descs[19],
	}).runtime
}

func runtimeItemCoverageSkill(skill itemCoverageSkill) model.RefSkill {
	if skill.runtime.ID != 0 {
		return skill.runtime
	}
	ref := model.RefSkill{ID: skill.id, CodeName: skill.codeName, BasicActivity: byte(skill.basicActivity), ActionDuration: skill.actionDuration, ActionRange: skill.actionRange}
	for index, value := range skill.params {
		ref.Params[index] = int(value)
	}
	return ref
}

func renderItemCoverageStatus(capability model.ItemUseCapability) string {
	switch capability.Status {
	case model.ItemUseCapabilitySupported:
		return itemCoverageSupported
	case model.ItemUseCapabilityConditional:
		return itemCoverageConditional
	case model.ItemUseCapabilityBrokenReference:
		return itemCoverageBroken
	case model.ItemUseCapabilityUnsupported:
		if capability.Reason == model.ItemUseReasonItemNotUseable {
			return itemCoverageNotApplicable
		}
		return itemCoverageUnsupported
	default:
		return itemCoverageBroken
	}
}

func renderItemCoverageReason(capability model.ItemUseCapability) string {
	reason := string(capability.Reason)
	if len(capability.Prerequisites) == 0 {
		return reason
	}
	prerequisites := make([]string, len(capability.Prerequisites))
	for index, prerequisite := range capability.Prerequisites {
		prerequisites[index] = string(prerequisite)
	}
	return reason + "; prerequisites=" + strings.Join(prerequisites, "+")
}

func renderItemCoverageExecutionKind(kind model.SkillExecutionKind) string {
	switch kind {
	case model.SkillExecutionPassive:
		return "passive"
	case model.SkillExecutionPhysicalAttack:
		return "physical_attack"
	case model.SkillExecutionMagicalAttack:
		return "magical_attack"
	case model.SkillExecutionBuff:
		return "buff"
	case model.SkillExecutionHealing:
		return "healing"
	case model.SkillExecutionResurrection:
		return "resurrection"
	case model.SkillExecutionStatusAttack:
		return "status_attack"
	case model.SkillExecutionBarrier:
		return "barrier"
	case model.SkillExecutionMovement:
		return "movement"
	case model.SkillExecutionCure:
		return "cure"
	case model.SkillExecutionAggro:
		return "aggro"
	case model.SkillExecutionTransform:
		return "transform"
	case model.SkillExecutionSystem:
		return "system"
	default:
		return "unsupported_skill_effect"
	}
}

func normalizedCoverageCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func encodeItemCoverageItems(rows []itemCoverageItemRow) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	header := []string{
		"item_id", "code_name", "type_id_1", "type_id_2", "type_id_3", "type_id_4", "can_use", "can_drop",
		"item_parameters", "linked_skill_id", "linked_skill_code", "skill_parameters", "decoded_effect",
		"implementation_status", "reason", "drop_source_count",
	}
	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("write items.csv header: %w", err)
	}
	for _, row := range rows {
		skillID, skillCode, skillParameters := "", "", ""
		if row.assessment.skill != nil {
			skillID = strconv.FormatUint(uint64(row.assessment.skill.id), 10)
			skillCode = row.assessment.skill.codeName
			skillParameters = compactCoverageSkillParameters(*row.assessment.skill)
		}
		record := []string{
			strconv.FormatUint(uint64(row.item.id), 10), row.item.codeName,
			strconv.Itoa(row.item.typeID1), strconv.Itoa(row.item.typeID2), strconv.Itoa(row.item.typeID3), strconv.Itoa(row.item.typeID4),
			coverageBool(row.item.canUse), coverageBool(row.item.canDrop), compactCoverageItemParameters(row.item),
			skillID, skillCode, skillParameters, row.assessment.effect, row.assessment.status, row.assessment.reason,
			strconv.Itoa(row.dropCount),
		}
		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("write items.csv item %d: %w", row.item.id, err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flush items.csv: %w", err)
	}
	return buffer.Bytes(), nil
}

func encodeItemCoverageDrops(rows []itemCoverageDropRow) ([]byte, error) {
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	header := []string{
		"item_id", "item_code", "source_kind", "source_id", "source_code", "group_id", "service",
		"drop_ratio_or_absolute", "selection_ratio_or_relative", "amount_min", "amount_max", "magic_group_id",
		"assigned_group", "drop_count", "country_mask", "catalog_status", "reason",
	}
	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("write drop_sources.csv header: %w", err)
	}
	for _, row := range rows {
		record := []string{
			coverageUint32(row.itemID), row.itemCode, row.sourceKind, coverageUint32(row.sourceID), row.sourceCode,
			coverageUint32(row.groupID), strconv.Itoa(row.service), coverageFloat(row.dropRatio), coverageFloat(row.selectionRatio),
			coverageInt(row.amountMin), coverageInt(row.amountMax), coverageUint32(row.magicGroupID), coverageInt(row.assignedGroup),
			coverageUint32Pointer(row.dropCount), coverageInt(row.countryMask), row.status, row.reason,
		}
		if err := writer.Write(record); err != nil {
			return nil, fmt.Errorf("write drop_sources.csv row: %w", err)
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("flush drop_sources.csv: %w", err)
	}
	return buffer.Bytes(), nil
}

func encodeItemCoverageSummary(report itemCoverageReport, failOn string) []byte {
	var buffer bytes.Buffer
	buffer.WriteString("# Item coverage audit\n\n")
	buffer.WriteString("This report is a deterministic, read-only comparison of `ITEMDATA`, active `REF_SKILL` links, and normalized loot catalogs.\n\n")
	buffer.WriteString("| Metric | Count |\n|---|---:|\n")
	metrics := []struct {
		label string
		value int
	}{
		{"ITEMDATA rows", report.totalItems}, {"Usable items", report.usableItems}, {"CanDrop items", report.droppable},
		{"Supported usable items", report.supported}, {"Conditional usable items", report.conditional},
		{"Unsupported usable items", report.unsupported}, {"Broken usable items", report.brokenUsable},
		{"Non-usable items", report.nonUsable}, {"Broken item references", report.brokenItems},
		{"Usable items with a SKILL_* Desc_1 link", report.linkedItems}, {"Drop-source rows", len(report.drops)},
		{"Disabled drop-source rows", report.disabledDrops}, {"Broken drop-source rows", report.brokenDrops},
	}
	for _, metric := range metrics {
		fmt.Fprintf(&buffer, "| %s | %d |\n", metric.label, metric.value)
	}
	buffer.WriteString("\n## Drop source coverage\n\n| Source kind | Rows |\n|---|---:|\n")
	for _, key := range sortedCoverageKeys(report.dropKinds) {
		fmt.Fprintf(&buffer, "| `%s` | %d |\n", key, report.dropKinds[key])
	}
	buffer.WriteString("\n## Highest unsupported families\n\n")
	if len(report.unsupportedByReason) == 0 {
		buffer.WriteString("No unsupported usable item families were found.\n")
	} else {
		buffer.WriteString("| Effect and reason | Items |\n|---|---:|\n")
		for _, entry := range sortedCoverageCounts(report.unsupportedByReason) {
			fmt.Fprintf(&buffer, "| %s | %d |\n", strings.ReplaceAll(entry.key, "|", "\\|"), entry.count)
		}
	}
	buffer.WriteString("\n## Gate\n\n")
	if failOn == "" {
		buffer.WriteString("No failure threshold requested.\n")
	} else {
		fmt.Fprintf(&buffer, "Requested threshold: `%s`. Reports are written before a failing exit is returned.\n", failOn)
	}
	buffer.WriteString("\nStatuses: `supported` means an explicit runtime branch exists; `unsupported` means a usable family is not executed; `disabled` means an authoritative `SERVICE=0` reference is intentionally skipped; `broken` means an authoritative reference is absent or ambiguous; `not_applicable` means `CanUse=0`.\n")
	return buffer.Bytes()
}

func writeItemCoverageFiles(directory string, files map[string][]byte) error {
	directory = filepath.Clean(directory)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create item coverage output directory %q: %w", directory, err)
	}
	for _, name := range []string{"items.csv", "drop_sources.csv", "summary.md"} {
		if err := os.WriteFile(filepath.Join(directory, name), files[name], 0o644); err != nil {
			return fmt.Errorf("write item coverage artifact %q: %w", name, err)
		}
	}
	return nil
}

func compactCoverageItemParameters(item itemCoverageItem) string {
	var values []string
	for index := range item.params {
		desc := strings.TrimSpace(item.descs[index])
		if item.params[index] == 0 && (desc == "" || strings.EqualFold(desc, "xxx")) {
			continue
		}
		value := fmt.Sprintf("%d=%d", index+1, item.params[index])
		if desc != "" && !strings.EqualFold(desc, "xxx") {
			value += ":" + desc
		}
		values = append(values, value)
	}
	return strings.Join(values, ";")
}

func compactCoverageSkillParameters(skill itemCoverageSkill) string {
	var values []string
	for index, value := range skill.params {
		if value != 0 {
			values = append(values, fmt.Sprintf("%d=%d", index+1, value))
		}
	}
	return strings.Join(values, ";")
}

func coverageBool(value int) string {
	if value == 0 {
		return "false"
	}
	return "true"
}

func coverageUint32(value uint32) string {
	if value == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(value), 10)
}

func coverageUint32Pointer(value *uint32) string {
	if value == nil {
		return ""
	}
	return strconv.FormatUint(uint64(*value), 10)
}

func coverageInt(value *int) string {
	if value == nil {
		return ""
	}
	return strconv.Itoa(*value)
}

func coverageFloat(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'g', -1, 64)
}

func sortedCoverageKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

type coverageCount struct {
	key   string
	count int
}

func sortedCoverageCounts(values map[string]int) []coverageCount {
	result := make([]coverageCount, 0, len(values))
	for key, count := range values {
		result = append(result, coverageCount{key: key, count: count})
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].count != result[right].count {
			return result[left].count > result[right].count
		}
		return result[left].key < result[right].key
	})
	return result
}
