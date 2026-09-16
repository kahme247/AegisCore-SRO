SELECT 'item_common_missing_detail' AS `violation`, common.`ID` AS `reference_id`
FROM `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` common
LEFT JOIN `SRO_SHARD`.`REF_OBJ_ITEM_VSRO` item
    ON item.`ID` = CAST(JSON_UNQUOTE(JSON_EXTRACT(common.`RAW_DATA`, '$.Link')) AS SIGNED)
WHERE common.`TYPE_ID_1` = 3
  AND (JSON_EXTRACT(common.`RAW_DATA`, '$.Link') IS NULL OR item.`ID` IS NULL)
UNION ALL
SELECT 'item_common_duplicate_link', MIN(common.`ID`)
FROM `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` common
WHERE common.`TYPE_ID_1` = 3
GROUP BY CAST(JSON_UNQUOTE(JSON_EXTRACT(common.`RAW_DATA`, '$.Link')) AS SIGNED)
HAVING COUNT(*) > 1
UNION ALL
SELECT 'item_detail_orphan', item.`ID`
FROM `SRO_SHARD`.`REF_OBJ_ITEM_VSRO` item
LEFT JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` common
    ON common.`TYPE_ID_1` = 3
   AND CAST(JSON_UNQUOTE(JSON_EXTRACT(common.`RAW_DATA`, '$.Link')) AS SIGNED) = item.`ID`
WHERE common.`ID` IS NULL
UNION ALL
SELECT 'struct_common_missing_detail', common.`ID`
FROM `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` common
LEFT JOIN `SRO_SHARD`.`REF_OBJ_STRUCT_VSRO` structure
    ON structure.`ID` = CAST(JSON_UNQUOTE(JSON_EXTRACT(common.`RAW_DATA`, '$.Link')) AS SIGNED)
WHERE common.`TYPE_ID_1` = 4
  AND (JSON_EXTRACT(common.`RAW_DATA`, '$.Link') IS NULL OR structure.`ID` IS NULL)
UNION ALL
SELECT 'struct_common_duplicate_link', MIN(common.`ID`)
FROM `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` common
WHERE common.`TYPE_ID_1` = 4
GROUP BY CAST(JSON_UNQUOTE(JSON_EXTRACT(common.`RAW_DATA`, '$.Link')) AS SIGNED)
HAVING COUNT(*) > 1
UNION ALL
SELECT 'struct_detail_orphan', structure.`ID`
FROM `SRO_SHARD`.`REF_OBJ_STRUCT_VSRO` structure
LEFT JOIN `SRO_SHARD`.`REF_OBJ_COMMON_VSRO` common
    ON common.`TYPE_ID_1` = 4
   AND CAST(JSON_UNQUOTE(JSON_EXTRACT(common.`RAW_DATA`, '$.Link')) AS SIGNED) = structure.`ID`
WHERE common.`ID` IS NULL;
