-- Mirror the original _RefCharDefault_SkillMastery catalog. RACE values are
-- 0 for Chinese, 1 for European, and 3 for defaults shared by both races.
CREATE TABLE `REF_CHAR_DEFAULT_MASTERY` (
    `RACE` TINYINT UNSIGNED NOT NULL,
    `MASTERY_ID` INT UNSIGNED NOT NULL,
    `LEVEL` TINYINT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (`RACE`, `MASTERY_ID`),
    CONSTRAINT `chk_ref_char_default_mastery_race`
        CHECK (`RACE` IN (0, 1, 3))
) ENGINE=InnoDB;

INSERT INTO `REF_CHAR_DEFAULT_MASTERY` (`RACE`, `MASTERY_ID`, `LEVEL`) VALUES
    (0, 257, 0),
    (0, 258, 0),
    (0, 259, 0),
    (0, 273, 0),
    (0, 274, 0),
    (0, 275, 0),
    (0, 276, 0),
    (1, 513, 0),
    (1, 514, 0),
    (1, 515, 0),
    (1, 516, 0),
    (1, 517, 0),
    (1, 518, 0);

-- Keep this migration independently restorative for characters created after
-- migration 000011 but before the Agent switched to the reference catalog.
INSERT IGNORE INTO `CHAR_MASTERY` (`CHAR_ID`, `MASTERY_ID`, `LEVEL`)
SELECT c.`ID`, defaults.`MASTERY_ID`, defaults.`LEVEL`
FROM `CHAR` c
JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
JOIN `REF_CHAR_DEFAULT_MASTERY` defaults
  ON defaults.`RACE` IN (r.`Country`, 3);
