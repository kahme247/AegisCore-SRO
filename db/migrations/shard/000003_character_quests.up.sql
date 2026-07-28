-- Mirrors the stable character-state portion of the original _CharQuest
-- table. Quest definitions and script execution remain server-owned rather
-- than being copied wholesale from the legacy SQL Server database.
CREATE TABLE `CHAR_QUEST` (
    `FK_CHAR` INT NOT NULL,
    `QUEST_ID` INT NOT NULL,
    `STATUS` TINYINT UNSIGNED NOT NULL,
    `ACHIEVEMENT_COUNT` SMALLINT UNSIGNED NOT NULL DEFAULT 0,
    `STARTED_AT` TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `COMPLETED_AT` TIMESTAMP NULL DEFAULT NULL,
    `QUEST_DATA_1` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `QUEST_DATA_2` BIGINT UNSIGNED NOT NULL DEFAULT 0,
    PRIMARY KEY (`FK_CHAR`, `QUEST_ID`),
    CONSTRAINT `FK_CHAR_QUEST_CHARACTER`
        FOREIGN KEY (`FK_CHAR`) REFERENCES `CHAR` (`id`)
        ON DELETE CASCADE
);
