-- Exact level-one, enabled rows selected by the original _AddNewChar from
-- _RefCharDefault_Quest joined to _RefQuest. RACE 3 is shared by both races.
CREATE TABLE `REF_CHAR_DEFAULT_QUEST` (
    `RACE` TINYINT UNSIGNED NOT NULL,
    `QUEST_ID` INT NOT NULL,
    `CODE_NAME` VARCHAR(128) NOT NULL,
    `REQUIRED_LEVEL` TINYINT UNSIGNED NOT NULL,
    `SERVICE` TINYINT UNSIGNED NOT NULL,
    PRIMARY KEY (`RACE`, `QUEST_ID`),
    UNIQUE KEY `UX_REF_CHAR_DEFAULT_QUEST_CODE` (`RACE`, `CODE_NAME`),
    CONSTRAINT `chk_ref_char_default_quest_race`
        CHECK (`RACE` IN (0, 1, 3)),
    CONSTRAINT `chk_ref_char_default_quest_service`
        CHECK (`SERVICE` IN (0, 1))
) ENGINE=InnoDB;

INSERT INTO `REF_CHAR_DEFAULT_QUEST`
    (`RACE`, `QUEST_ID`, `CODE_NAME`, `REQUIRED_LEVEL`, `SERVICE`) VALUES
    (0, 2, 'QTUTORIAL_CH', 1, 1),
    (3, 1, 'QEVENT_GUIDE', 1, 1),
    (3, 397, 'QEV_ALL_BASIC_0', 1, 1);

INSERT IGNORE INTO `CHAR_QUEST`
    (`FK_CHAR`, `QUEST_ID`, `STATUS`, `ACHIEVEMENT_COUNT`, `QUEST_DATA_1`, `QUEST_DATA_2`)
SELECT c.`ID`, defaults.`QUEST_ID`, 1, 0, 0, 0
FROM `CHAR` c
JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
JOIN `REF_CHAR_DEFAULT_QUEST` defaults
  ON defaults.`RACE` IN (r.`Country`, 3)
WHERE defaults.`REQUIRED_LEVEL` = 1
  AND defaults.`SERVICE` = 1;
