INSERT INTO `GAME_SERVER_REGION` (`Continent_Name`, `Game_Server_ID`) VALUES
('Am',1),('ARENA_FLAG',1),('ARENA_OCCUPY',1),('ARENA_SCORE',1),
('FORT_BJ_AREA',1),('FORT_HT_AREA',1),('FORT_JA_AREA',1),('GM_EVENT',1),
('GOD_FLAME',1),('GOD_TOGUI',1),('GOD_WRECK_IN',1),('GOD_WRECK_OUT',1),
('JUPITER',1),('KingsValley',1),('Pharaoh',1),('PRISON',1),('Roc',1),
('SIEGE_DUNGEON',1),('TEMPLE',1),('Thief Village',1),('TQ',1)
ON DUPLICATE KEY UPDATE `Game_Server_ID`=VALUES(`Game_Server_ID`);
