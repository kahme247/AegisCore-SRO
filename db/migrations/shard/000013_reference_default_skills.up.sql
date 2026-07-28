-- Mirror the original _RefCharDefault_Skill row catalog. RACE values are
-- 0 for Chinese, 1 for European, and 3 for skills shared by both races.
CREATE TABLE `REF_CHAR_DEFAULT_SKILL` (
    `RACE` TINYINT UNSIGNED NOT NULL,
    `SKILL_ID` INT UNSIGNED NOT NULL,
    `ENABLED` TINYINT UNSIGNED NOT NULL DEFAULT 1,
    PRIMARY KEY (`RACE`, `SKILL_ID`),
    CONSTRAINT `chk_ref_char_default_skill_race`
        CHECK (`RACE` IN (0, 1, 3)),
    CONSTRAINT `chk_ref_char_default_skill_enabled`
        CHECK (`ENABLED` IN (0, 1))
) ENGINE=InnoDB;

INSERT INTO `REF_CHAR_DEFAULT_SKILL` (`RACE`, `SKILL_ID`, `ENABLED`) VALUES
    (0, 2, 1),
    (0, 40, 1),
    (0, 70, 1),
    (1, 8419, 1),
    (1, 8420, 1),
    (1, 8421, 1),
    (1, 9354, 1),
    (1, 9355, 1),
    (1, 9944, 1),
    (1, 10625, 1),
    (1, 11162, 1),
    (1, 11526, 1),
    (3, 1, 1);

INSERT IGNORE INTO `CHAR_SKILL` (`CHAR_ID`, `SKILL_ID`, `ENABLED`)
SELECT c.`ID`, defaults.`SKILL_ID`, defaults.`ENABLED`
FROM `CHAR` c
JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
JOIN `REF_CHAR_DEFAULT_SKILL` defaults
  ON defaults.`RACE` IN (r.`Country`, 3);
