INSERT INTO `GAME_SERVER_REGION` (`Continent_Name`, `Game_Server_ID`)
VALUES ('SD', 1)
ON DUPLICATE KEY UPDATE `Game_Server_ID` = VALUES(`Game_Server_ID`);
