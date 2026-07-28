-- The original _CharSkillMastery table stores every race-default mastery at
-- level zero when a character is created. Migration 000008 incorrectly
-- rejected that valid baseline state.
ALTER TABLE `CHAR_MASTERY`
    DROP CHECK `chk_char_mastery_level`;

INSERT IGNORE INTO `CHAR_MASTERY` (`CHAR_ID`, `MASTERY_ID`, `LEVEL`)
SELECT c.`ID`, defaults.`MASTERY_ID`, 0
FROM `CHAR` c
JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
JOIN (
    SELECT 0 AS `COUNTRY`, 257 AS `MASTERY_ID`
    UNION ALL SELECT 0, 258
    UNION ALL SELECT 0, 259
    UNION ALL SELECT 0, 273
    UNION ALL SELECT 0, 274
    UNION ALL SELECT 0, 275
    UNION ALL SELECT 0, 276
    UNION ALL SELECT 1, 513
    UNION ALL SELECT 1, 514
    UNION ALL SELECT 1, 515
    UNION ALL SELECT 1, 516
    UNION ALL SELECT 1, 517
    UNION ALL SELECT 1, 518
) defaults ON defaults.`COUNTRY` = r.`Country`;
