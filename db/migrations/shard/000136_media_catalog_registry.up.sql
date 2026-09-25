CREATE TABLE `SRO_SHARD`.`MEDIA_CATALOG_MAPPING` (
    `MEDIA_PATTERN` VARCHAR(128) NOT NULL,
    `DATABASE_TABLES` VARCHAR(512) NOT NULL,
    `AUTHORITY` VARCHAR(32) NOT NULL,
    `DATABASE_UPDATE` VARCHAR(64) NOT NULL,
    `MEDIA_SYNC` VARCHAR(32) NOT NULL,
    `RECONCILE_MODE` VARCHAR(16) NOT NULL,
    `NOTES` VARCHAR(512) NOT NULL DEFAULT '',
    `SERVICE` TINYINT UNSIGNED NOT NULL DEFAULT 1,
    PRIMARY KEY (`MEDIA_PATTERN`),
    KEY `IX_MEDIA_CATALOG_MAPPING_AUTHORITY` (`AUTHORITY`,`SERVICE`),
    CONSTRAINT `CK_MEDIA_CATALOG_MAPPING_SYNC` CHECK (`MEDIA_SYNC` IN ('export-media','not-supported')),
    CONSTRAINT `CK_MEDIA_CATALOG_MAPPING_RECONCILE` CHECK (`RECONCILE_MODE` IN ('snapshot','overlay','none'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO `SRO_SHARD`.`MEDIA_CATALOG_MAPPING`
(`MEDIA_PATTERN`,`DATABASE_TABLES`,`AUTHORITY`,`DATABASE_UPDATE`,`MEDIA_SYNC`,`RECONCILE_MODE`,`NOTES`) VALUES
('leveldata.txt','REF_LEVEL_DATA','database','import-reference','export-media','snapshot','Level, experience, skill points, and job experience'),
('levelgold.txt','REF_LEVEL_DATA','database','import-reference','export-media','snapshot','DROP_GOLD_MIN and DROP_GOLD_MAX overlay'),
('skilldata.txt','REF_SKILL_DATA','database','import-reference','export-media','snapshot','Manifest listing skill-data chunks'),
('skilldata_*.txt','REF_SKILL_DATA','database','import-reference','export-media','snapshot','Enabled skill rows; encrypted chunks are generated output'),
('magicoption.txt','REF_MAGIC_OPTION; REF_MAGIC_OPTION_APPLICABILITY','database','import-reference','not-supported','overlay','Magic-option definitions and applicability'),
('magicoptionassign.txt','REF_DROP_MAGIC_OPTION_ASSIGN','database','import-reference','not-supported','snapshot','Item-family magic-option assignment slots'),
('gameworlddata.txt','REF_GAME_WORLD','database','import-reference','not-supported','snapshot','World metadata and server lifecycle columns'),
('refregion.txt','REF_GAME_WORLD_REGION','database','import-reference','not-supported','snapshot','Dimension Hole allowed regions'),
('refeventreward.txt','REF_EVENT_REWARD','database','import-event-rewards','export-media','snapshot','Event reward headers'),
('refeventrewarditems.txt','REF_EVENT_REWARD_ITEM','database','import-event-rewards','export-media','snapshot','Event reward items'),
('refservereventreward*.txt','REF_SERVER_EVENT_REWARD*','database','import-server-event-rewards','export-media','snapshot','Server-event reward families'),
('refoptionalteleport.txt','REF_OPTIONAL_TELEPORT','database','import-optional-teleports','not-supported','snapshot','Reverse-return destinations'),
('refshopgroup.txt','REF_SHOP_GROUP_VSRO; ITEM_MALL_GOODS','database','import-item-mall','not-supported','snapshot','Shop-group source and Item Mall placement input'),
('refmappingshopgroup.txt','REF_MAPPING_SHOP_GROUP_VSRO; ITEM_MALL_GOODS','database','import-item-mall','not-supported','snapshot','Shop-group mapping'),
('refmappingshopwithtab.txt','REF_MAPPING_SHOP_WITH_TAB_VSRO; ITEM_MALL_GOODS','database','import-item-mall','not-supported','snapshot','Shop-to-tab mapping'),
('refshoptab.txt','REF_SHOP_TAB_VSRO; ITEM_MALL_GOODS','database','import-item-mall','not-supported','snapshot','Shop tabs'),
('refshopgoods.txt','REF_SHOP_GOODS_PACKAGE_VSRO; ITEM_MALL_GOODS','database','import-item-mall','not-supported','snapshot','Package placement'),
('refpackageitem.txt','REF_PACKAGE_ITEM_VSRO; ITEM_MALL_GOODS','database','import-item-mall','export-media','snapshot','Package definitions'),
('refscrapofpackageitem.txt','REF_SCRAP_OF_PACKAGE_ITEM_VSRO; ITEM_MALL_GOODS','database','import-item-mall','not-supported','snapshot','Package members'),
('refpricepolicyofitem.txt','REF_PRICE_POLICY_ITEM_VSRO; ITEM_MALL_GOODS','database','import-item-mall','export-media','snapshot','Package prices'),
('refabilitybyitemoptleveldata.txt','REF_ABILITY_ITEM_OPT_LEVEL_VSRO','database','import-media-catalogs','export-media','snapshot','Typed option-level catalog'),
('refmagicoptbyitemoptleveldata.txt','REF_MAGIC_OPT_ITEM_OPT_LEVEL_VSRO','database','import-media-catalogs','export-media','snapshot','Typed magic-option-level catalog'),
('refsetitemgroup.txt','REF_SET_ITEM_GROUP_VSRO','database','import-media-catalogs','export-media','snapshot','Set-item groups'),
('questdata.txt','REF_QUEST','database','import-vsro-quests','export-media','snapshot','Runtime quest headers'),
('refqusetreward.txt','REF_QUEST_REWARD','database','import-vsro-quests','export-media','snapshot','Stock filename preserves the misspelling'),
('refquestrewarditems.txt','REF_QUEST_REWARD_ITEM','database','import-vsro-quests','export-media','snapshot','Quest reward items'),
('itemdata_*.txt','ITEMDATA','database','import-vsro-items','export-media','snapshot','Runtime item catalog'),
('characterdata_*.txt','CHAR_REF_DATA','database','import-media-catalogs','not-supported','snapshot','Runtime character and monster reference catalog'),
('teleportdata.txt','DUNGEON_TELEPORT_POINT; TELEPORT_POINT','superseded','migrations 000006 and 000134','not-supported','none','Agent points are database-backed; client synchronization is not implemented'),
('teleportlink.txt','DUNGEON_TELEPORT_LINK; TELEPORT_LINK','superseded','migrations 000006 and 000132-000133','not-supported','none','Agent routes are database-backed; client synchronization is not implemented'),
('teleportbuilding.txt','TELEPORT_BUILDING_SPAWN','database','import-media-catalogs','not-supported','overlay','Raw interaction bytes are preserved'),
('gachaitemset.txt','GACHA_REWARD_POOL','candidate','not-normalized','not-supported','none','Useful source for replacing seeded reward rows'),
('gachanpcmap.txt','GACHA operator mapping','candidate','not-normalized','not-supported','none','Useful source for replacing hardcoded Gacha NPC code names'),
('npcpos.txt','SPAWN_REF_NESTS; SPAWN_REF_TACTICS','candidate','reference-database import','not-supported','none','Compare with authoritative spawn imports before normalization'),
('hwanleveldata.txt','Hwan progression catalog','candidate','not-normalized','not-supported','none','Balance and progression data suitable for a typed table'),
('questcontentsdata.txt','typed quest objective tables','candidate','import-vsro-quest-script','not-supported','none','Useful discovery source; strict script import remains authoritative'),
('eventzonedata.txt','none','file-only','none','not-supported','none','Client and world geometry metadata'),
('dungeoninfo.txt','none','file-only','none','not-supported','none','Client dungeon presentation metadata'),
('text*.txt','none','client-only','none','not-supported','none','Localization and UI strings');

CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_LEVELDATA` AS SELECT * FROM `SRO_SHARD`.`REF_LEVEL_DATA`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_LEVELGOLD` AS SELECT `LEVEL`,`DROP_GOLD_MIN`,`DROP_GOLD_MAX` FROM `SRO_SHARD`.`REF_LEVEL_DATA`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_SKILLDATA` AS SELECT * FROM `SRO_SHARD`.`REF_SKILL_DATA`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_MAGICOPTION` AS SELECT * FROM `SRO_SHARD`.`REF_MAGIC_OPTION`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_MAGICOPTIONASSIGN` AS SELECT * FROM `SRO_SHARD`.`REF_DROP_MAGIC_OPTION_ASSIGN`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_GAMEWORLDDATA` AS SELECT * FROM `SRO_SHARD`.`REF_GAME_WORLD`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_REFREGION` AS SELECT * FROM `SRO_SHARD`.`REF_GAME_WORLD_REGION`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_QUESTDATA` AS SELECT * FROM `SRO_SHARD`.`REF_QUEST`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_REFQUSETREWARD` AS SELECT * FROM `SRO_SHARD`.`REF_QUEST_REWARD`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_REFQUESTREWARDITEMS` AS SELECT * FROM `SRO_SHARD`.`REF_QUEST_REWARD_ITEM`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_TELEPORTDATA_NORMAL` AS SELECT * FROM `SRO_SHARD`.`TELEPORT_POINT`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_TELEPORTDATA_DUNGEON` AS SELECT * FROM `SRO_SHARD`.`DUNGEON_TELEPORT_POINT`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_TELEPORTLINK_NORMAL` AS SELECT * FROM `SRO_SHARD`.`TELEPORT_LINK`;
CREATE OR REPLACE VIEW `SRO_SHARD`.`MEDIA_TELEPORTLINK_DUNGEON` AS SELECT * FROM `SRO_SHARD`.`DUNGEON_TELEPORT_LINK`;
