package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/Harvi8/AegisCore/agent-server/model"
	"github.com/Harvi8/AegisCore/agent-server/service"
	frameworkdb "github.com/Harvi8/AegisCore/framework/db"
)

var ErrMonsterDropCatalogUnavailable = errors.New("monster-drop catalog repository is unavailable")

// These projections intentionally use the normalized names created by the
// drop-reference migration. The client CSV names are preserved in the import
// command, while runtime code consumes only this typed boundary.
const (
	selectMonsterDirectDrops = "SELECT `REF_MONSTER_ID`,`REF_ITEM_ID`,`DROP_GROUP_TYPE`,`OPT_LEVEL`,`DROP_AMOUNT_MIN`,`DROP_AMOUNT_MAX`,`DROP_RATIO`,`REF_MAGIC_OPTION_ID1`,`CUSTOM_VALUE1`,`REF_MAGIC_OPTION_ID2`,`CUSTOM_VALUE2`,`REF_MAGIC_OPTION_ID3`,`CUSTOM_VALUE3`,`REF_MAGIC_OPTION_ID4`,`CUSTOM_VALUE4`,`REF_MAGIC_OPTION_ID5`,`CUSTOM_VALUE5`,`REF_MAGIC_OPTION_ID6`,`CUSTOM_VALUE6`,`REF_MAGIC_OPTION_ID7`,`CUSTOM_VALUE7`,`REF_MAGIC_OPTION_ID8`,`CUSTOM_VALUE8`,`REF_MAGIC_OPTION_ID9`,`CUSTOM_VALUE9`,`RENT_CODE_NAME` FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_DROP` WHERE `SERVICE`=1 ORDER BY `REF_MONSTER_ID`,`REF_ITEM_ID`"
	selectMonsterRandomDrops = "SELECT `REF_MONSTER_ID`,`REF_ITEM_GROUP_ID`,`OVERLAP`,`DROP_AMOUNT_MIN`,`DROP_AMOUNT_MAX`,`DROP_RATIO` FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_RND_DROP` WHERE `SERVICE`=1 ORDER BY `REF_MONSTER_ID`,`REF_ITEM_GROUP_ID`"
	selectDropGroups         = "SELECT `REF_ITEM_GROUP_ID`,`REF_ITEM_ID`,`REF_MAGIC_GROUP_ID`,`SELECT_RATIO` FROM `SRO_SHARD`.`REF_DROP_ITEM_GROUP` WHERE `SERVICE`=1 ORDER BY `REF_ITEM_GROUP_ID`,`REF_ITEM_ID`"
	selectDropGold           = "SELECT `MON_LEVEL`,`DROP_PROB`,`GOLD_MIN`,`GOLD_MAX` FROM `SRO_SHARD`.`REF_DROP_GOLD` WHERE `SERVICE`=1 ORDER BY `MON_LEVEL`"
	selectDropClassProfiles  = "SELECT `FAMILY`,`MON_LEVEL`,`PROB_GROUP`,`PROB` FROM `SRO_SHARD`.`REF_DROP_CLASS_SELECTION` ORDER BY `FAMILY`,`MON_LEVEL`,`PROB_GROUP`"
	selectDropItemProfiles   = "SELECT a.`SOURCE_ROW`,a.`REF_ITEM_ID`,a.`PROB_RELATIVE`,a.`PROB_ABSOLUTE`,a.`ASSIGNED_GROUP`,a.`DROP_COUNT`,i.`TypeID_2`,i.`TypeID_3`,i.`TypeID_4`,i.`Rarity`,i.`Country`,i.`ItemClass`,i.`Dur_L`,i.`Dur_U`,i.`MaxMagicOptCount` FROM `SRO_SHARD`.`REF_DROP_ITEM_ASSIGN` a JOIN `SRO_SHARD`.`ITEMDATA` i ON i.`ItemID`=a.`REF_ITEM_ID` WHERE a.`SERVICE`=1 ORDER BY a.`SOURCE_ROW`"
	selectDropOptProfiles    = "SELECT `OPT_LEVEL`,`PROB`,`REQ_ONLINE_TIME` FROM `SRO_SHARD`.`REF_DROP_OPT_LEVEL_SELECTION` ORDER BY `OPT_LEVEL`"
	selectDropMagicProfiles  = "SELECT a.`COUNTRY`,a.`TYPE_ID3`,a.`TYPE_ID4`,a.`SLOT`,o.`ID`,o.`CODE_NAME`,o.`DROP_RATIO`,MIN(ap.`MIN_ITEM_CLASS`),o.`PARAM_TYPE`,o.`PARAM_VALUE` FROM `SRO_SHARD`.`REF_DROP_MAGIC_OPTION_ASSIGN` a JOIN `SRO_SHARD`.`REF_MAGIC_OPTION` o ON o.`CODE_NAME`=a.`OPTION_CODE_NAME` JOIN `SRO_SHARD`.`REF_MAGIC_OPTION_APPLICABILITY` ap ON ap.`REF_MAGIC_OPTION_ID`=o.`ID` AND (ap.`ITEM_FAMILY`=CASE WHEN a.`TYPE_ID3`=6 THEN 'weapon' WHEN a.`TYPE_ID3`=4 THEN 'shield' WHEN a.`TYPE_ID3` IN (5,12) THEN 'accessory' ELSE 'armor' END OR a.`TYPE_ID3` IN (5,12) AND ap.`ITEM_FAMILY`=CASE a.`TYPE_ID4` WHEN 1 THEN 'earring' WHEN 2 THEN 'necklace' WHEN 3 THEN 'ring' ELSE '' END OR a.`TYPE_ID3` IN (1,2,3,9,10,11) AND ap.`ITEM_FAMILY`=CASE a.`TYPE_ID4` WHEN 1 THEN 'helm' WHEN 2 THEN 'mail_addon' WHEN 3 THEN 'mail' WHEN 4 THEN 'pants' ELSE '' END) WHERE a.`SERVICE`=1 GROUP BY a.`COUNTRY`,a.`TYPE_ID3`,a.`TYPE_ID4`,a.`SLOT`,o.`ID`,o.`CODE_NAME`,o.`DROP_RATIO`,o.`PARAM_TYPE`,o.`PARAM_VALUE` ORDER BY a.`COUNTRY`,a.`TYPE_ID3`,a.`TYPE_ID4`,a.`SLOT`,o.`ID`"
)

type MonsterDropCatalogRepository struct {
	shard frameworkdb.Queryer
}

var _ service.MonsterDropCatalogLoader = (*MonsterDropCatalogRepository)(nil)

func NewMonsterDropCatalogRepository(shard frameworkdb.Queryer) (*MonsterDropCatalogRepository, error) {
	if shard == nil {
		return nil, ErrMonsterDropCatalogUnavailable
	}
	return &MonsterDropCatalogRepository{shard: shard}, nil
}

func (r *MonsterDropCatalogRepository) Load(ctx context.Context) (*service.MonsterDropCatalog, error) {
	if r == nil || r.shard == nil {
		return nil, ErrMonsterDropCatalogUnavailable
	}
	tables := make(map[uint32]service.MonsterDropTable)
	if err := r.loadDirect(ctx, tables); err != nil {
		return nil, err
	}
	if err := r.loadRandom(ctx, tables); err != nil {
		return nil, err
	}
	if err := r.loadGroups(ctx, tables); err != nil {
		return nil, err
	}
	goldByLevel, err := r.loadGold(ctx, tables)
	if err != nil {
		return nil, err
	}
	profiles, err := r.loadProfiles(ctx)
	if err != nil {
		return nil, err
	}
	return service.NewMonsterDropCatalogWithProfiles(tables, goldByLevel, profiles), nil
}

func (r *MonsterDropCatalogRepository) loadProfiles(ctx context.Context) (*service.DropProfileCatalog, error) {
	classRows, err := r.shard.QueryContext(ctx, selectDropClassProfiles)
	if err != nil {
		return nil, fmt.Errorf("load drop class profiles: %w", err)
	}
	var classes []service.DropClassSelection
	for classRows.Next() {
		var row service.DropClassSelection
		if err = classRows.Scan(&row.Family, &row.MonsterLevel, &row.Group, &row.Probability); err != nil {
			_ = classRows.Close()
			return nil, fmt.Errorf("scan drop class profile: %w", err)
		}
		row.Group--
		classes = append(classes, row)
	}
	if err = classRows.Err(); err != nil {
		_ = classRows.Close()
		return nil, fmt.Errorf("iterate drop class profiles: %w", err)
	}
	_ = classRows.Close()

	assignmentRows, err := r.shard.QueryContext(ctx, selectDropItemProfiles)
	if err != nil {
		return nil, fmt.Errorf("load drop item profiles: %w", err)
	}
	var assignments []service.DropItemAssignment
	for assignmentRows.Next() {
		var row service.DropItemAssignment
		var typeID2, typeID3, typeID4, rarity, itemClass int
		if err = assignmentRows.Scan(&row.SourceRow, &row.ReferenceItemID, &row.Relative, &row.Absolute, &row.AssignedGroup, &row.DropCount, &typeID2, &typeID3, &typeID4, &rarity, &row.Country, &itemClass, &row.DurabilityLower, &row.DurabilityUpper, &row.MaxMagicOptions); err != nil {
			_ = assignmentRows.Close()
			return nil, fmt.Errorf("scan drop item profile: %w", err)
		}
		if row.AssignedGroup < 0 {
			row.AssignedGroup = itemClass
		}
		row.AssignedGroup--
		row.Family = service.ClassifyDropProfileFamily(typeID2, typeID3, typeID4, rarity)
		row.TypeID3 = typeID3
		row.TypeID4 = typeID4
		row.ItemClass = itemClass
		assignments = append(assignments, row)
	}
	if err = assignmentRows.Err(); err != nil {
		_ = assignmentRows.Close()
		return nil, fmt.Errorf("iterate drop item profiles: %w", err)
	}
	_ = assignmentRows.Close()

	optRows, err := r.shard.QueryContext(ctx, selectDropOptProfiles)
	if err != nil {
		return nil, fmt.Errorf("load drop option profiles: %w", err)
	}
	var optLevels []service.DropOptLevelSelection
	for optRows.Next() {
		var row service.DropOptLevelSelection
		if err = optRows.Scan(&row.Level, &row.Probability, &row.RequiredOnlineTime); err != nil {
			_ = optRows.Close()
			return nil, fmt.Errorf("scan drop option profile: %w", err)
		}
		optLevels = append(optLevels, row)
	}
	if err = optRows.Err(); err != nil {
		_ = optRows.Close()
		return nil, fmt.Errorf("iterate drop option profiles: %w", err)
	}
	_ = optRows.Close()
	magicRows, err := r.shard.QueryContext(ctx, selectDropMagicProfiles)
	if err != nil {
		return nil, fmt.Errorf("load drop magic-option profiles: %w", err)
	}
	var magic []service.DropMagicOptionCandidate
	for magicRows.Next() {
		var row service.DropMagicOptionCandidate
		if err = magicRows.Scan(&row.Country, &row.TypeID3, &row.TypeID4, &row.AssignmentSlot, &row.OptionID, &row.CodeName, &row.DropRatio, &row.MinItemClass, &row.ParamType, &row.PackedValue); err != nil {
			_ = magicRows.Close()
			return nil, fmt.Errorf("scan drop magic-option profile: %w", err)
		}
		magic = append(magic, row)
	}
	if err = magicRows.Err(); err != nil {
		_ = magicRows.Close()
		return nil, fmt.Errorf("iterate drop magic-option profiles: %w", err)
	}
	_ = magicRows.Close()
	return service.NewDropProfileCatalog(classes, assignments, optLevels, magic), nil
}

func (r *MonsterDropCatalogRepository) loadDirect(ctx context.Context, tables map[uint32]service.MonsterDropTable) error {
	rows, err := r.shard.QueryContext(ctx, selectMonsterDirectDrops)
	if err != nil {
		return fmt.Errorf("load direct monster drops: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var monsterID, itemID uint32
		var dropType, optLevel, minimum, maximum byte
		var ratio float64
		var optionIDs [9]sql.NullInt64
		var customValues [9]sql.NullInt64
		var rentCode sql.NullString
		dest := []any{&monsterID, &itemID, &dropType, &optLevel, &minimum, &maximum, &ratio}
		for index := range optionIDs {
			dest = append(dest, &optionIDs[index], &customValues[index])
		}
		dest = append(dest, &rentCode)
		if err := rows.Scan(dest...); err != nil {
			return fmt.Errorf("scan direct monster drop: %w", err)
		}
		table := tables[monsterID]
		table.Direct = append(table.Direct, service.DropRecord{
			ReferenceItemID: itemID,
			RentCodeName:    rentCode.String,
			DropType:        dropType,
			OptLevel:        optLevel,
			MinCount:        minimum,
			MaxCount:        maximum,
			Chance:          ratio,
			MagicOptions:    magicOptions(optionIDs, customValues),
		})
		tables[monsterID] = table
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate direct monster drops: %w", err)
	}
	return nil
}

func (r *MonsterDropCatalogRepository) loadRandom(ctx context.Context, tables map[uint32]service.MonsterDropTable) error {
	rows, err := r.shard.QueryContext(ctx, selectMonsterRandomDrops)
	if err != nil {
		return fmt.Errorf("load random monster drops: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var monsterID, groupID uint32
		var overlap, minimum, maximum byte
		var ratio float64
		if err := rows.Scan(&monsterID, &groupID, &overlap, &minimum, &maximum, &ratio); err != nil {
			return fmt.Errorf("scan random monster drop: %w", err)
		}
		table := tables[monsterID]
		table.Random = append(table.Random, service.DropGroupAssignment{
			GroupID: groupID, MinCount: minimum, MaxCount: maximum, Chance: ratio, Overlap: overlap,
		})
		tables[monsterID] = table
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate random monster drops: %w", err)
	}
	return nil
}

func (r *MonsterDropCatalogRepository) loadGroups(ctx context.Context, tables map[uint32]service.MonsterDropTable) error {
	rows, err := r.shard.QueryContext(ctx, selectDropGroups)
	if err != nil {
		return fmt.Errorf("load drop groups: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var groupID, itemID, magicGroupID uint32
		var weight float64
		if err := rows.Scan(&groupID, &itemID, &magicGroupID, &weight); err != nil {
			return fmt.Errorf("scan drop group: %w", err)
		}
		for monsterID, table := range tables {
			for _, assignment := range table.Random {
				if assignment.GroupID != groupID {
					continue
				}
				if table.Groups == nil {
					table.Groups = make(map[uint32]service.DropGroup)
				}
				group := table.Groups[groupID]
				group.ID = groupID
				group.Candidates = append(group.Candidates, service.DropGroupCandidate{ReferenceItemID: itemID, MagicGroupID: magicGroupID, Weight: weight, Overlap: assignment.Overlap})
				table.Groups[groupID] = group
				tables[monsterID] = table
				break
			}
		}
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate drop groups: %w", err)
	}
	return nil
}

func (r *MonsterDropCatalogRepository) loadGold(ctx context.Context, tables map[uint32]service.MonsterDropTable) (map[int]service.GoldDropRecord, error) {
	rows, err := r.shard.QueryContext(ctx, selectDropGold)
	if err != nil {
		return nil, fmt.Errorf("load drop gold: %w", err)
	}
	defer rows.Close()
	goldByLevel := make(map[int]service.GoldDropRecord)
	for rows.Next() {
		var level int
		var gold service.GoldDropRecord
		if err := rows.Scan(&level, &gold.Chance, &gold.Min, &gold.Max); err != nil {
			return nil, fmt.Errorf("scan drop gold: %w", err)
		}
		goldByLevel[level] = gold
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate drop gold: %w", err)
	}
	for monsterID, table := range tables {
		table.GoldByLevel = goldByLevel
		tables[monsterID] = table
	}
	return goldByLevel, nil
}

func magicOptions(ids, values [9]sql.NullInt64) []model.MagicOption {
	options := make([]model.MagicOption, 0, len(ids))
	for index := range ids {
		if !ids[index].Valid || ids[index].Int64 <= 0 {
			continue
		}
		value := uint32(0)
		if values[index].Valid && values[index].Int64 > 0 {
			value = uint32(values[index].Int64)
		}
		options = append(options, model.MagicOption{Type: uint32(ids[index].Int64), Value: value})
	}
	return options
}
