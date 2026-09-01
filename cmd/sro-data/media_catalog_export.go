package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type mediaExportCatalog struct {
	Name string
	Rows [][]string
}

type rawCatalogSpec struct {
	name        string
	query       string
	columns     []string
	overlayKeys []string
}

var itemMediaColumns = []string{
	"Service", "ID", "CodeName128", "ObjName128", "OrgObjCodeName128", "NameStrID128", "DescStrID128", "CashItem",
	"Bionic", "TypeID1", "TypeID2", "TypeID3", "TypeID4", "DecayTime", "Country", "Rarity",
	"CanTrade", "CanSell", "CanBuy", "CanBorrow", "CanDrop", "CanPick", "CanRepair", "CanRevive",
	"CanUse", "CanThrow", "Price", "CostRepair", "CostRevive", "CostBorrow", "KeepingFee", "SellPrice",
	"ReqLevelType1", "ReqLevel1", "ReqLevelType2", "ReqLevel2", "ReqLevelType3", "ReqLevel3", "ReqLevelType4", "ReqLevel4",
	"MaxContain", "RegionID", "Dir", "OffsetX", "OffsetY", "OffsetZ", "Speed1", "Speed2",
	"Scale", "BCHeight", "BCRadius", "EventID", "AssocFileObj128", "AssocFileDrop128", "AssocFileIcon128", "AssocFile1_128",
	"AssocFile2_128", "MaxStack", "ReqGender", "ReqStr", "ReqInt", "ItemClass", "SetID", "Dur_L",
	"Dur_U", "PD_L", "PD_U", "PDInc", "ER_L", "ER_U", "ERInc", "PAR_L",
	"PAR_U", "PARInc", "BR_L", "BR_U", "MD_L", "MD_U", "MDInc", "MAR_L",
	"MAR_U", "MARInc", "PDStr_L", "PDStr_U", "MDInt_L", "MDInt_U", "Quivered", "Ammo1_TID4",
	"Ammo2_TID4", "Ammo3_TID4", "Ammo4_TID4", "Ammo5_TID4", "SpeedClass", "TwoHanded", "Range", "PAttackMin_L",
	"PAttackMin_U", "PAttackMax_L", "PAttackMax_U", "PAttackInc", "MAttackMin_L", "MAttackMin_U", "MAttackMax_L", "MAttackMax_U",
	"MAttackInc", "PAStrMin_L", "PAStrMin_U", "PAStrMax_L", "PAStrMax_U", "MAInt_Min_L", "MAInt_Min_U", "MAInt_Max_L",
	"MAInt_Max_U", "HR_L", "HR_U", "HRInc", "CHR_L", "CHR_U", "Param1", "Desc1_128",
	"Param2", "Desc2_128", "Param3", "Desc3_128", "Param4", "Desc4_128", "Param5", "Desc5_128",
	"Param6", "Desc6_128", "Param7", "Desc7_128", "Param8", "Desc8_128", "Param9", "Desc9_128",
	"Param10", "Desc10_128", "Param11", "Desc11_128", "Param12", "Desc12_128", "Param13", "Desc13_128",
	"Param14", "Desc14_128", "Param15", "Desc15_128", "Param16", "Desc16_128", "Param17", "Desc17_128",
	"Param18", "Desc18_128", "Param19", "Desc19_128", "Param20", "Desc20_128", "MaxMagicOptCount", "ChildItemCount",
}

var rawMediaCatalogs = []rawCatalogSpec{
	{
		name: "refpackageitem.txt",
		query: `SELECT RAW_DATA,COUNTRY,ID,CODE_NAME FROM SRO_SHARD.REF_PACKAGE_ITEM_VSRO
            WHERE JSON_UNQUOTE(JSON_EXTRACT(RAW_DATA,'$.Service'))='1' ORDER BY COUNTRY,ID`,
		columns:     []string{"Service", "Country", "ID", "CodeName128", "SaleTag", "ExpandTerm", "NameStrID", "DescStrID", "AssocFileIcon", "Param1", "Param1_Desc128", "Param2", "Param2_Desc128", "Param3", "Param3_Desc128", "Param4", "Param4_Desc128"},
		overlayKeys: []string{"Country", "ID", "CodeName128"},
	},
	{
		name: "refpricepolicyofitem.txt",
		query: `SELECT RAW_DATA,COUNTRY,PACKAGE_CODE_NAME,PAYMENT_DEVICE FROM SRO_SHARD.REF_PRICE_POLICY_ITEM_VSRO
            WHERE JSON_UNQUOTE(JSON_EXTRACT(RAW_DATA,'$.Service'))='1' ORDER BY COUNTRY,PACKAGE_CODE_NAME,PAYMENT_DEVICE`,
		columns:     []string{"Service", "Country", "RefPackageItemCodeName", "PaymentDevice", "PreviousCost", "Cost", "Param1", "Param1_Desc128", "Param2", "Param2_Desc128", "Param3", "Param3_Desc128", "Param4", "Param4_Desc128"},
		overlayKeys: []string{"Country", "RefPackageItemCodeName", "PaymentDevice"},
	},
	{
		name:        "refabilitybyitemoptleveldata.txt",
		query:       `SELECT RAW_DATA,ID,REF_ITEM_ID,ITEM_OPT_LEVEL FROM SRO_SHARD.REF_ABILITY_ITEM_OPT_LEVEL_VSRO ORDER BY ID`,
		columns:     []string{"Service", "ID", "RefItemID", "ItemOptLevel"},
		overlayKeys: []string{"ID", "RefItemID", "ItemOptLevel"},
	},
	{
		name:        "refmagicoptbyitemoptleveldata.txt",
		query:       `SELECT RAW_DATA,LINK,REF_MAGIC_OPT_ID FROM SRO_SHARD.REF_MAGIC_OPT_ITEM_OPT_LEVEL_VSRO ORDER BY LINK,REF_MAGIC_OPT_ID`,
		columns:     []string{"Link", "RefMagicOptID", "MagicOptValue", "TooltipType", "TooltipCodename"},
		overlayKeys: []string{"Link", "RefMagicOptID"},
	},
	{
		name: "refsetitemgroup.txt",
		query: `SELECT RAW_DATA,ID,CODE_NAME FROM SRO_SHARD.REF_SET_ITEM_GROUP_VSRO
            WHERE JSON_UNQUOTE(JSON_EXTRACT(RAW_DATA,'$.Service'))='1' ORDER BY ID`,
		columns:     []string{"Service", "ID", "CodeName128", "ObjName128", "NameStrID128", "DescStrID128", "SetEffectMask", "SetMagicMask", "2SetMOptGroupID", "3SetMOptGroupID", "4SetMOptGroupID", "5SetMOptGroupID", "6SetMOptGroupID", "7SetMOptGroupID", "8SetMOptGroupID", "9SetMOptGroupID", "10SetMOptGroupID", "11SetMOptGroupID"},
		overlayKeys: []string{"ID", "CodeName128"},
	},
}

func loadMediaExportCatalogs(ctx context.Context, tx *sql.Tx) ([]mediaExportCatalog, error) {
	items, err := loadItemMediaCatalogs(ctx, tx)
	if err != nil {
		return nil, err
	}
	result := append([]mediaExportCatalog(nil), items...)
	for _, spec := range rawMediaCatalogs {
		rows, err := loadRawMediaCatalog(ctx, tx, spec)
		if err != nil {
			return nil, err
		}
		result = append(result, mediaExportCatalog{Name: spec.name, Rows: rows})
	}
	quests, err := loadQuestMediaCatalogs(ctx, tx)
	if err != nil {
		return nil, err
	}
	result = append(result, quests...)
	events, err := loadEventMediaCatalogs(ctx, tx)
	if err != nil {
		return nil, err
	}
	result = append(result, events...)
	passthrough, err := loadPassthroughMediaCatalogs(ctx, tx)
	if err != nil {
		return nil, err
	}
	result = append(result, passthrough...)
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result, nil
}

func loadPassthroughMediaCatalogs(ctx context.Context, tx *sql.Tx) ([]mediaExportCatalog, error) {
	rows, err := tx.QueryContext(ctx, `SELECT catalog.FILE_NAME,rows.RAW_FIELDS
        FROM SRO_SHARD.REF_MEDIA_TEXT_CATALOG catalog
        LEFT JOIN SRO_SHARD.REF_MEDIA_TEXT_CATALOG_ROW rows ON rows.FILE_NAME=catalog.FILE_NAME
        ORDER BY catalog.FILE_NAME,rows.ROW_ORDINAL`)
	if err != nil {
		return nil, fmt.Errorf("query passthrough Media catalogs: %w", err)
	}
	defer rows.Close()
	byName := make(map[string]*mediaExportCatalog)
	var order []string
	for rows.Next() {
		var name string
		var raw sql.NullString
		if err := rows.Scan(&name, &raw); err != nil {
			return nil, fmt.Errorf("scan passthrough Media catalog: %w", err)
		}
		if !isPassthroughMediaCatalog(name) {
			return nil, fmt.Errorf("database contains unapproved passthrough Media catalog %q", name)
		}
		catalog := byName[name]
		if catalog == nil {
			catalog = &mediaExportCatalog{Name: name}
			byName[name] = catalog
			order = append(order, name)
		}
		if raw.Valid {
			fields, err := rawMediaFields(raw.String, 1)
			if err != nil {
				return nil, fmt.Errorf("passthrough Media catalog %s: %w", name, err)
			}
			catalog.Rows = append(catalog.Rows, fields)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read passthrough Media catalogs: %w", err)
	}
	result := make([]mediaExportCatalog, 0, len(order))
	for _, name := range order {
		result = append(result, *byName[name])
	}
	return result, nil
}

func loadEventMediaCatalogs(ctx context.Context, tx *sql.Tx) ([]mediaExportCatalog, error) {
	specs := []struct {
		name  string
		query string
	}{
		{"refeventreward.txt", `SELECT SERVICE,EVENT_ID,EVENT_CODE_NAME,IS_VIEW,IS_BASIC_REWARD,IS_ITEM_REWARD,IS_CHECK_CONDITION,IS_CHECK_COUNTRY,IS_CHECK_CLASS,IS_CHECK_GENDER,GOLD,EXPERIENCE,SKILL_EXPERIENCE,SKILL_POINTS,HWAN,INVENTORY_SLOTS,ITEM_REWARD_TYPE,SELECTION_COUNT,PARAM_1,PARAM_1_DESCRIPTION,PARAM_2,PARAM_2_DESCRIPTION,PARAM_3,PARAM_3_DESCRIPTION FROM SRO_SHARD.REF_EVENT_REWARD WHERE SERVICE=1 ORDER BY EVENT_ID`},
		{"refeventrewarditems.txt", `SELECT SERVICE,EVENT_ID,EVENT_CODE_NAME,ITEM_CODE_NAME,PAY_COUNT,ACHIEVE_RATIO,RENT_ITEM_CODE_NAME,PARAM_1,PARAM_1_DESCRIPTION,PARAM_2,PARAM_2_DESCRIPTION FROM SRO_SHARD.REF_EVENT_REWARD_ITEM WHERE SERVICE=1 ORDER BY EVENT_ID,ID`},
		{"refservereventreward.txt", `SELECT REWARD_ID,REF_SKILL_ID,REWARD_CLASS,FLAGS,APPLY_TIME,REF_VALUE_1,REF_VALUE_2,REF_VALUE_3,EXP_RATIO,SKILL_EXP_RATIO FROM SRO_SHARD.REF_SERVER_EVENT_REWARD ORDER BY REWARD_ID`},
		{"refservereventreward_expupforplayers.txt", `SELECT REWARD_ID,EXP_RATIO,SKILL_EXP_RATIO FROM SRO_SHARD.REF_SERVER_EVENT_REWARD_EXP_UP_FOR_PLAYERS ORDER BY REWARD_ID`},
		{"refservereventreward_spawnmonster.txt", `SELECT REWARD_ID,REF_OBJ_ID,MONSTER_COUNT,LEVEL,PARAM_1,PARAM_2 FROM SRO_SHARD.REF_SERVER_EVENT_REWARD_SPAWN_MONSTER ORDER BY REWARD_ID`},
		{"refservereventreward_event.txt", `SELECT SERVER_EVENT_ID,REWARD_ID,AQ_TYPE FROM SRO_SHARD.REF_SERVER_EVENT_REWARD_EVENT ORDER BY SERVER_EVENT_ID,REWARD_ID`},
	}
	result := make([]mediaExportCatalog, 0, len(specs))
	for _, spec := range specs {
		rows, err := queryStringRows(ctx, tx, spec.query)
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", spec.name, err)
		}
		result = append(result, mediaExportCatalog{Name: spec.name, Rows: rows})
	}
	return result, nil
}

func loadItemMediaCatalogs(ctx context.Context, tx *sql.Tx) ([]mediaExportCatalog, error) {
	rows, err := tx.QueryContext(ctx, `SELECT common.ID,common.SERVICE,common.CODE_NAME,common.TYPE_ID_1,
        common.TYPE_ID_2,common.TYPE_ID_3,common.TYPE_ID_4,common.RAW_DATA,item.SET_ID,item.ITEM_CLASS,item.RAW_DATA
        FROM SRO_SHARD.REF_OBJ_COMMON_VSRO common JOIN SRO_SHARD.REF_OBJ_ITEM_VSRO item ON item.ID=common.ID
        WHERE common.SERVICE=1 AND common.TYPE_ID_1=3 ORDER BY common.ID`)
	if err != nil {
		return nil, fmt.Errorf("query item Media data: %w", err)
	}
	defer rows.Close()
	chunks := make(map[uint64][][]string)
	for rows.Next() {
		var id, service, type1, type2, type3, type4, setID, itemClass uint64
		var codeName, commonRaw, itemRaw string
		if err := rows.Scan(&id, &service, &codeName, &type1, &type2, &type3, &type4, &commonRaw, &setID, &itemClass, &itemRaw); err != nil {
			return nil, fmt.Errorf("scan item Media data: %w", err)
		}
		common, err := decodeRawObject(commonRaw)
		if err != nil {
			return nil, fmt.Errorf("item %d common RAW_DATA: %w", id, err)
		}
		item, err := decodeRawObject(itemRaw)
		if err != nil {
			return nil, fmt.Errorf("item %d item RAW_DATA: %w", id, err)
		}
		common["ID"], common["Service"], common["CodeName128"] = id, service, codeName
		common["TypeID1"], common["TypeID2"], common["TypeID3"], common["TypeID4"] = type1, type2, type3, type4
		item["SetID"], item["ItemClass"] = setID, itemClass
		fields, err := rawObjectFields(common, itemMediaColumns[:57])
		if err != nil {
			return nil, fmt.Errorf("item %d common fields: %w", id, err)
		}
		itemFields, err := rawObjectFields(item, itemMediaColumns[57:])
		if err != nil {
			return nil, fmt.Errorf("item %d fields: %w", id, err)
		}
		fields = append(fields, itemFields...)
		boundary := (id/5000 + 1) * 5000
		chunks[boundary] = append(chunks[boundary], fields)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read item Media data: %w", err)
	}
	if len(chunks) == 0 {
		return nil, errors.New("vSRO item catalog contains no enabled items")
	}
	boundaries := make([]uint64, 0, len(chunks))
	for boundary := range chunks {
		boundaries = append(boundaries, boundary)
	}
	sort.Slice(boundaries, func(i, j int) bool { return boundaries[i] < boundaries[j] })
	result := make([]mediaExportCatalog, 0, len(boundaries))
	for _, boundary := range boundaries {
		result = append(result, mediaExportCatalog{Name: fmt.Sprintf("itemdata_%d.txt", boundary), Rows: chunks[boundary]})
	}
	return result, nil
}

func loadRawMediaCatalog(ctx context.Context, tx *sql.Tx, spec rawCatalogSpec) ([][]string, error) {
	rows, err := tx.QueryContext(ctx, spec.query)
	if err != nil {
		return nil, fmt.Errorf("query %s: %w", spec.name, err)
	}
	defer rows.Close()
	var result [][]string
	for rows.Next() {
		var raw string
		overlays := make([]sql.NullString, len(spec.overlayKeys))
		destinations := []any{&raw}
		for index := range overlays {
			destinations = append(destinations, &overlays[index])
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, fmt.Errorf("scan %s: %w", spec.name, err)
		}
		object, err := decodeRawObject(raw)
		if err != nil {
			return nil, fmt.Errorf("decode %s RAW_DATA: %w", spec.name, err)
		}
		for index, key := range spec.overlayKeys {
			if overlays[index].Valid {
				object[key] = overlays[index].String
			}
		}
		fields, err := rawObjectFields(object, spec.columns)
		if err != nil {
			return nil, fmt.Errorf("build %s row: %w", spec.name, err)
		}
		result = append(result, fields)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read %s: %w", spec.name, err)
	}
	return result, nil
}

func loadQuestMediaCatalogs(ctx context.Context, tx *sql.Tx) ([]mediaExportCatalog, error) {
	specs := []struct {
		name  string
		query string
	}{
		{"questdata.txt", `SELECT SERVICE,ID,CODE_NAME,LEVEL,DESCRIPTION_NAME,NAME_STRING,PAY_STRING,CONTENTS_STRING,PAY_CONTENTS,NOTICE_NPC,NOTICE_CONDITION FROM SRO_SHARD.REF_QUEST WHERE SERVICE=1 ORDER BY ID`},
		{"refqusetreward.txt", `SELECT QUEST_ID,QUEST_CODE_NAME,IS_VIEW,IS_BASIC_REWARD,IS_ITEM_REWARD,IS_CHECK_CONDITION,IS_CHECK_COUNTRY,SELECTION_COUNT,IS_CHECK_CLASS,IS_CHECK_GENDER,GOLD,EXPERIENCE,SKILL_EXPERIENCE,SKILL_POINTS,ACADEMY_POINTS,ACADEMY_POINT_TYPE,HWAN,INVENTORY_SLOTS,ITEM_REWARD_TYPE,PARAM_1,PARAM_1_DESCRIPTION,PARAM_2,PARAM_2_DESCRIPTION,PARAM_3,PARAM_3_DESCRIPTION FROM SRO_SHARD.REF_QUEST_REWARD WHERE SERVICE=1 ORDER BY QUEST_ID`},
		{"refquestrewarditems.txt", `SELECT QUEST_ID,QUEST_CODE_NAME,REWARD_TYPE,ITEM_CODE_NAME,RENT_ITEM_CODE_NAME,OPTIONAL_ITEM_CODE,OPTIONAL_ITEM_COUNT,ACHIEVE_QUANTITY,PARAM_1,PARAM_1_DESCRIPTION,PARAM_2,PARAM_2_DESCRIPTION FROM SRO_SHARD.REF_QUEST_REWARD_ITEM WHERE SERVICE=1 ORDER BY QUEST_ID,ID`},
	}
	result := make([]mediaExportCatalog, 0, len(specs))
	for _, spec := range specs {
		rows, err := queryStringRows(ctx, tx, spec.query)
		if err != nil {
			return nil, fmt.Errorf("query %s: %w", spec.name, err)
		}
		result = append(result, mediaExportCatalog{Name: spec.name, Rows: rows})
	}
	return result, nil
}

func queryStringRows(ctx context.Context, tx *sql.Tx, query string) ([][]string, error) {
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	columns, err := rows.Columns()
	if err != nil {
		return nil, err
	}
	var result [][]string
	for rows.Next() {
		values := make([]sql.NullString, len(columns))
		destinations := make([]any, len(columns))
		for index := range values {
			destinations[index] = &values[index]
		}
		if err := rows.Scan(destinations...); err != nil {
			return nil, err
		}
		fields := make([]string, len(columns))
		for index, value := range values {
			if value.Valid {
				fields[index] = value.String
			}
		}
		result = append(result, fields)
	}
	return result, rows.Err()
}

func decodeRawObject(raw string) (map[string]any, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		return nil, err
	}
	return result, nil
}

func rawObjectFields(objects map[string]any, columns []string) ([]string, error) {
	result := make([]string, len(columns))
	for index, column := range columns {
		value, exists := objects[column]
		if !exists {
			return nil, fmt.Errorf("missing source column %s", column)
		}
		var err error
		result[index], err = mediaJSONValue(value)
		if err != nil {
			return nil, fmt.Errorf("column %s: %w", column, err)
		}
	}
	return result, nil
}

func mediaJSONValue(value any) (string, error) {
	switch typed := value.(type) {
	case nil:
		return "", nil
	case string:
		return typed, nil
	case json.Number:
		return typed.String(), nil
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), nil
	case bool:
		if typed {
			return "1", nil
		}
		return "0", nil
	case int:
		return strconv.Itoa(typed), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	default:
		return "", fmt.Errorf("unsupported JSON type %T", value)
	}
}
