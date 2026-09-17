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
	selectMonsterDirectDrops   = "SELECT d.`REF_MONSTER_ID`,d.`REF_ITEM_ID`,d.`DROP_GROUP_TYPE`,d.`OPT_LEVEL`,d.`DROP_AMOUNT_MIN`,d.`DROP_AMOUNT_MAX`,d.`DROP_RATIO`,d.`REF_MAGIC_OPTION_ID1`,d.`CUSTOM_VALUE1`,d.`REF_MAGIC_OPTION_ID2`,d.`CUSTOM_VALUE2`,d.`REF_MAGIC_OPTION_ID3`,d.`CUSTOM_VALUE3`,d.`REF_MAGIC_OPTION_ID4`,d.`CUSTOM_VALUE4`,d.`REF_MAGIC_OPTION_ID5`,d.`CUSTOM_VALUE5`,d.`REF_MAGIC_OPTION_ID6`,d.`CUSTOM_VALUE6`,d.`REF_MAGIC_OPTION_ID7`,d.`CUSTOM_VALUE7`,d.`REF_MAGIC_OPTION_ID8`,d.`CUSTOM_VALUE8`,d.`REF_MAGIC_OPTION_ID9`,d.`CUSTOM_VALUE9`,d.`RENT_CODE_NAME` FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_DROP` d JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` monster_ref ON monster_ref.`ID`=d.`REF_MONSTER_ID` AND monster_ref.`SERVICE`=1 AND monster_ref.`TYPE_ID_1`=1 JOIN `SRO_SHARD`.`CHAR_REF_DATA` monster ON monster.`RefObjID`=d.`REF_MONSTER_ID` JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` item_ref ON item_ref.`ID`=d.`REF_ITEM_ID` AND item_ref.`SERVICE`=1 AND item_ref.`TYPE_ID_1`=3 JOIN `SRO_SHARD`.`ITEMDATA` item ON item.`ItemID`=d.`REF_ITEM_ID` WHERE d.`SERVICE`=1 ORDER BY d.`REF_MONSTER_ID`,d.`REF_ITEM_ID`"
	selectMonsterRandomDrops   = "SELECT d.`REF_MONSTER_ID`,d.`REF_ITEM_GROUP_ID`,d.`OVERLAP`,d.`DROP_AMOUNT_MIN`,d.`DROP_AMOUNT_MAX`,d.`DROP_RATIO` FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_RND_DROP` d JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` monster_ref ON monster_ref.`ID`=d.`REF_MONSTER_ID` AND monster_ref.`SERVICE`=1 AND monster_ref.`TYPE_ID_1`=1 JOIN `SRO_SHARD`.`CHAR_REF_DATA` monster ON monster.`RefObjID`=d.`REF_MONSTER_ID` WHERE d.`SERVICE`=1 ORDER BY d.`REF_MONSTER_ID`,d.`REF_ITEM_GROUP_ID`"
	selectDropGroups           = "SELECT d.`REF_ITEM_GROUP_ID`,d.`REF_ITEM_ID`,d.`REF_MAGIC_GROUP_ID`,d.`SELECT_RATIO` FROM `SRO_SHARD`.`REF_DROP_ITEM_GROUP` d JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` item_ref ON item_ref.`ID`=d.`REF_ITEM_ID` AND item_ref.`SERVICE`=1 AND item_ref.`TYPE_ID_1`=3 JOIN `SRO_SHARD`.`ITEMDATA` item ON item.`ItemID`=d.`REF_ITEM_ID` WHERE d.`SERVICE`=1 ORDER BY d.`REF_ITEM_GROUP_ID`,d.`REF_ITEM_ID`"
	selectDropGold             = "SELECT `MON_LEVEL`,`DROP_PROB`,`GOLD_MIN`,`GOLD_MAX` FROM `SRO_SHARD`.`REF_DROP_GOLD` WHERE `SERVICE`=1 ORDER BY `MON_LEVEL`"
	selectDropClassProfiles    = "SELECT `FAMILY`,`MON_LEVEL`,`PROB_GROUP`,`PROB` FROM `SRO_SHARD`.`REF_DROP_CLASS_SELECTION` ORDER BY `FAMILY`,`MON_LEVEL`,`PROB_GROUP`"
	selectDropItemProfiles     = "SELECT a.`SOURCE_ROW`,a.`REF_ITEM_ID`,a.`PROB_RELATIVE`,a.`PROB_ABSOLUTE`,a.`ASSIGNED_GROUP`,a.`DROP_COUNT`,i.`TypeID_2`,i.`TypeID_3`,i.`TypeID_4`,i.`Rarity`,i.`Country`,i.`ItemClass`,i.`Dur_L`,i.`Dur_U`,i.`MaxMagicOptCount` FROM `SRO_SHARD`.`REF_DROP_ITEM_ASSIGN` a JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` item_ref ON item_ref.`ID`=a.`REF_ITEM_ID` AND item_ref.`SERVICE`=1 AND item_ref.`TYPE_ID_1`=3 JOIN `SRO_SHARD`.`ITEMDATA` i ON i.`ItemID`=a.`REF_ITEM_ID` WHERE a.`SERVICE`=1 ORDER BY a.`SOURCE_ROW`"
	selectReservedDropItems    = "SELECT a.`SOURCE_ROW`,a.`COUNTRY_MASK`,a.`REF_ITEM_ID`,a.`PROB_RELATIVE`,a.`PROB_ABSOLUTE`,a.`ASSIGNED_GROUP`,a.`DROP_COUNT`,i.`TypeID_2`,i.`TypeID_3`,i.`TypeID_4`,i.`Rarity`,i.`ItemClass`,i.`Dur_L`,i.`Dur_U`,i.`MaxMagicOptCount` FROM `SRO_SHARD`.`REF_CUSTOMIZING_RESERVED_ITEM_DROP_FOR_MONSTER` a JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` item_ref ON item_ref.`ID`=a.`REF_ITEM_ID` AND item_ref.`SERVICE`=1 AND item_ref.`TYPE_ID_1`=3 JOIN `SRO_SHARD`.`ITEMDATA` i ON i.`ItemID`=a.`REF_ITEM_ID` WHERE a.`SERVICE`=1 ORDER BY a.`SOURCE_ROW`"
	selectDropOptProfiles      = "SELECT `OPT_LEVEL`,`PROB`,`REQ_ONLINE_TIME` FROM `SRO_SHARD`.`REF_DROP_OPT_LEVEL_SELECTION` ORDER BY `OPT_LEVEL`"
	selectDropMagicProfiles    = "SELECT a.`COUNTRY`,a.`TYPE_ID3`,a.`TYPE_ID4`,a.`SLOT`,o.`ID`,o.`CODE_NAME`,o.`DROP_RATIO`,MIN(ap.`MIN_ITEM_CLASS`),o.`PARAM_TYPE`,o.`PARAM_VALUE` FROM `SRO_SHARD`.`REF_DROP_MAGIC_OPTION_ASSIGN` a JOIN `SRO_SHARD`.`REF_MAGIC_OPTION` o ON o.`CODE_NAME`=a.`OPTION_CODE_NAME` JOIN `SRO_SHARD`.`REF_MAGIC_OPTION_APPLICABILITY` ap ON ap.`REF_MAGIC_OPTION_ID`=o.`ID` AND (ap.`ITEM_FAMILY`=CASE WHEN a.`TYPE_ID3`=6 THEN 'weapon' WHEN a.`TYPE_ID3`=4 THEN 'shield' WHEN a.`TYPE_ID3` IN (5,12) THEN 'accessory' ELSE 'armor' END OR a.`TYPE_ID3` IN (5,12) AND ap.`ITEM_FAMILY`=CASE a.`TYPE_ID4` WHEN 1 THEN 'earring' WHEN 2 THEN 'necklace' WHEN 3 THEN 'ring' ELSE '' END OR a.`TYPE_ID3` IN (1,2,3,9,10,11) AND ap.`ITEM_FAMILY`=CASE a.`TYPE_ID4` WHEN 1 THEN 'helm' WHEN 2 THEN 'mail_addon' WHEN 3 THEN 'mail' WHEN 4 THEN 'pants' ELSE '' END) WHERE a.`SERVICE`=1 GROUP BY a.`COUNTRY`,a.`TYPE_ID3`,a.`TYPE_ID4`,a.`SLOT`,o.`ID`,o.`CODE_NAME`,o.`DROP_RATIO`,o.`PARAM_TYPE`,o.`PARAM_VALUE` ORDER BY a.`COUNTRY`,a.`TYPE_ID3`,a.`TYPE_ID4`,a.`SLOT`,o.`ID`"
	selectInvalidDropReference = "SELECT invalid.`KIND`,invalid.`SOURCE_ID`,invalid.`REF_ID`,invalid.`REASON` FROM (" +
		"SELECT 'direct_monster' `KIND`,d.`REF_MONSTER_ID` `SOURCE_ID`,d.`REF_MONSTER_ID` `REF_ID`,CASE WHEN c.`ID` IS NULL THEN 'missing common object' WHEN c.`TYPE_ID_1`<>1 THEN 'wrong object type' ELSE 'enabled object missing CHAR_REF_DATA' END `REASON` FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_DROP` d LEFT JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` c ON c.`ID`=d.`REF_MONSTER_ID` LEFT JOIN `SRO_SHARD`.`CHAR_REF_DATA` r ON r.`RefObjID`=d.`REF_MONSTER_ID` WHERE d.`SERVICE`=1 AND (c.`ID` IS NULL OR c.`TYPE_ID_1`<>1 OR c.`SERVICE`=1 AND r.`RefObjID` IS NULL) UNION ALL " +
		"SELECT 'direct_item',d.`REF_MONSTER_ID`,d.`REF_ITEM_ID`,CASE WHEN c.`ID` IS NULL THEN 'missing common object' WHEN c.`TYPE_ID_1`<>3 THEN 'wrong object type' ELSE 'enabled object missing ITEMDATA' END FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_DROP` d LEFT JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` c ON c.`ID`=d.`REF_ITEM_ID` LEFT JOIN `SRO_SHARD`.`ITEMDATA` i ON i.`ItemID`=d.`REF_ITEM_ID` WHERE d.`SERVICE`=1 AND (c.`ID` IS NULL OR c.`TYPE_ID_1`<>3 OR c.`SERVICE`=1 AND i.`ItemID` IS NULL) UNION ALL " +
		"SELECT 'random_monster',d.`REF_MONSTER_ID`,d.`REF_MONSTER_ID`,CASE WHEN c.`ID` IS NULL THEN 'missing common object' WHEN c.`TYPE_ID_1`<>1 THEN 'wrong object type' ELSE 'enabled object missing CHAR_REF_DATA' END FROM `SRO_SHARD`.`REF_MONSTER_ASSIGNED_ITEM_RND_DROP` d LEFT JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` c ON c.`ID`=d.`REF_MONSTER_ID` LEFT JOIN `SRO_SHARD`.`CHAR_REF_DATA` r ON r.`RefObjID`=d.`REF_MONSTER_ID` WHERE d.`SERVICE`=1 AND (c.`ID` IS NULL OR c.`TYPE_ID_1`<>1 OR c.`SERVICE`=1 AND r.`RefObjID` IS NULL) UNION ALL " +
		"SELECT 'group_item',d.`REF_ITEM_GROUP_ID`,d.`REF_ITEM_ID`,CASE WHEN c.`ID` IS NULL THEN 'missing common object' WHEN c.`TYPE_ID_1`<>3 THEN 'wrong object type' ELSE 'enabled object missing ITEMDATA' END FROM `SRO_SHARD`.`REF_DROP_ITEM_GROUP` d LEFT JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` c ON c.`ID`=d.`REF_ITEM_ID` LEFT JOIN `SRO_SHARD`.`ITEMDATA` i ON i.`ItemID`=d.`REF_ITEM_ID` WHERE d.`SERVICE`=1 AND (c.`ID` IS NULL OR c.`TYPE_ID_1`<>3 OR c.`SERVICE`=1 AND i.`ItemID` IS NULL) UNION ALL " +
		"SELECT 'profile_item',a.`SOURCE_ROW`,a.`REF_ITEM_ID`,CASE WHEN c.`ID` IS NULL THEN 'missing common object' WHEN c.`TYPE_ID_1`<>3 THEN 'wrong object type' ELSE 'enabled object missing ITEMDATA' END FROM `SRO_SHARD`.`REF_DROP_ITEM_ASSIGN` a LEFT JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` c ON c.`ID`=a.`REF_ITEM_ID` LEFT JOIN `SRO_SHARD`.`ITEMDATA` i ON i.`ItemID`=a.`REF_ITEM_ID` WHERE a.`SERVICE`=1 AND (c.`ID` IS NULL OR c.`TYPE_ID_1`<>3 OR c.`SERVICE`=1 AND i.`ItemID` IS NULL) UNION ALL " +
		"SELECT 'reserved_item',a.`SOURCE_ROW`,a.`REF_ITEM_ID`,CASE WHEN c.`ID` IS NULL THEN 'missing common object' WHEN c.`TYPE_ID_1`<>3 THEN 'wrong object type' ELSE 'enabled object missing ITEMDATA' END FROM `SRO_SHARD`.`REF_CUSTOMIZING_RESERVED_ITEM_DROP_FOR_MONSTER` a LEFT JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` c ON c.`ID`=a.`REF_ITEM_ID` LEFT JOIN `SRO_SHARD`.`ITEMDATA` i ON i.`ItemID`=a.`REF_ITEM_ID` WHERE a.`SERVICE`=1 AND (c.`ID` IS NULL OR c.`TYPE_ID_1`<>3 OR c.`SERVICE`=1 AND i.`ItemID` IS NULL)" +
		") invalid ORDER BY invalid.`KIND`,invalid.`SOURCE_ID`,invalid.`REF_ID` LIMIT 1"
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
	if err := r.validateReferences(ctx); err != nil {
		return nil, err
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
	if err := validateLoadedDropTables(tables); err != nil {
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

func (r *MonsterDropCatalogRepository) validateReferences(ctx context.Context) error {
	rows, err := r.shard.QueryContext(ctx, selectInvalidDropReference)
	if err != nil {
		return fmt.Errorf("validate monster-drop references: %w", err)
	}
	defer rows.Close()
	if !rows.Next() {
		if err = rows.Err(); err != nil {
			return fmt.Errorf("iterate invalid monster-drop references: %w", err)
		}
		return nil
	}
	var kind, reason string
	var sourceID, refID uint32
	if err = rows.Scan(&kind, &sourceID, &refID, &reason); err != nil {
		return fmt.Errorf("scan invalid monster-drop reference: %w", err)
	}
	return fmt.Errorf("validate monster-drop references: %s source %d reference %d: %s", kind, sourceID, refID, reason)
}

func validateLoadedDropTables(tables map[uint32]service.MonsterDropTable) error {
	for monsterID, table := range tables {
		for _, assignment := range table.Random {
			group, exists := table.Groups[assignment.GroupID]
			if !exists || len(group.Candidates) == 0 {
				return fmt.Errorf("validate monster-drop table %d: group %d has no enabled candidates", monsterID, assignment.GroupID)
			}
		}
	}
	return nil
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

	reservedRows, err := r.shard.QueryContext(ctx, selectReservedDropItems)
	if err != nil {
		return nil, fmt.Errorf("load customizing reserved drop items: %w", err)
	}
	for reservedRows.Next() {
		var row service.DropItemAssignment
		var countryMask, typeID2, typeID3, typeID4, rarity, itemClass int
		if err = reservedRows.Scan(&row.SourceRow, &countryMask, &row.ReferenceItemID, &row.Relative, &row.Absolute, &row.AssignedGroup, &row.DropCount, &typeID2, &typeID3, &typeID4, &rarity, &itemClass, &row.DurabilityLower, &row.DurabilityUpper, &row.MaxMagicOptions); err != nil {
			_ = reservedRows.Close()
			return nil, fmt.Errorf("scan customizing reserved drop item: %w", err)
		}
		if row.AssignedGroup < 0 {
			row.AssignedGroup = itemClass
		}
		row.AssignedGroup--
		row.Family = service.ClassifyDropProfileFamily(typeID2, typeID3, typeID4, rarity)
		row.TypeID3, row.TypeID4, row.ItemClass = typeID3, typeID4, itemClass
		for country := 0; country < 2; country++ {
			if countryMask&(1<<country) == 0 {
				continue
			}
			copy := row
			copy.Country = country
			copy.SourceRow = 0x80000000 | row.SourceRow<<1 | uint32(country)
			assignments = append(assignments, copy)
		}
	}
	if err = reservedRows.Err(); err != nil {
		_ = reservedRows.Close()
		return nil, fmt.Errorf("iterate customizing reserved drop items: %w", err)
	}
	_ = reservedRows.Close()

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
