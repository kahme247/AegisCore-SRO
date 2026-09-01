-- Translate rows written with AegisCore's former semantic names to the
-- status meanings confirmed in the pinned SR_GameServer executable.
UPDATE `CHAR_QUEST`
SET `COMPLETED_AT` = CASE
        WHEN `STATUS` IN (3, 4) THEN COALESCE(`COMPLETED_AT`, CURRENT_TIMESTAMP)
        WHEN (`STATUS` = 1 AND `ACHIEVEMENT_COUNT` = 0) OR `STATUS` = 5 THEN NULL
        ELSE `COMPLETED_AT`
    END,
    `STATUS` = CASE
        WHEN `STATUS` = 1 AND `ACHIEVEMENT_COUNT` = 0 THEN 4
        WHEN `STATUS` = 2 THEN 8
        WHEN `STATUS` IN (3, 4) THEN 5
        WHEN `STATUS` = 5 THEN 4
        ELSE `STATUS`
    END;
