-- SR_GameServer skill/mastery restoration prices use _RefDropGold.GoldMin,
-- keyed by the character's historical maximum level. Values are imported
-- explicitly from client-matched levelgold.txt by cmd/sro-data.
ALTER TABLE `REF_LEVEL_DATA`
    ADD COLUMN `DROP_GOLD_MIN` INT UNSIGNED NULL AFTER `REQUIRED_SKILL_POINTS`,
    ADD COLUMN `DROP_GOLD_MAX` INT UNSIGNED NULL AFTER `DROP_GOLD_MIN`;
