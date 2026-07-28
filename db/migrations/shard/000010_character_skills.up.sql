CREATE TABLE `CHAR_SKILL` (
    `CHAR_ID` INT NOT NULL,
    `SKILL_ID` INT UNSIGNED NOT NULL,
    `ENABLED` TINYINT UNSIGNED NOT NULL DEFAULT 1,
    PRIMARY KEY (`CHAR_ID`, `SKILL_ID`),
    CONSTRAINT `fk_char_skill_character`
        FOREIGN KEY (`CHAR_ID`) REFERENCES `CHAR` (`ID`) ON DELETE CASCADE,
    CONSTRAINT `chk_char_skill_enabled`
        CHECK (`ENABLED` IN (0, 1))
) ENGINE=InnoDB;

-- Existing characters predate character-skill persistence. Seed the same
-- non-zero default skills that character creation now persists atomically.
INSERT INTO `CHAR_SKILL` (`CHAR_ID`, `SKILL_ID`, `ENABLED`)
SELECT DISTINCT existing_skills.`CHAR_ID`, existing_skills.`SKILL_ID`, 1
FROM (
    SELECT c.`ID` AS `CHAR_ID`, r.`Default_Skill_1` AS `SKILL_ID` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_2` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_3` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_4` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_5` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_6` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_7` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_8` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_9` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
    UNION ALL
    SELECT c.`ID`, r.`Default_Skill_10` FROM `CHAR` c JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
) existing_skills
WHERE existing_skills.`SKILL_ID` IS NOT NULL
  AND existing_skills.`SKILL_ID` > 0;
