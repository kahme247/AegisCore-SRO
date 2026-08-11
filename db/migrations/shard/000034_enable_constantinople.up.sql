INSERT INTO `GAME_SERVER_REGION` (`Continent_Name`, `Game_Server_ID`)
VALUES ('Constantinople', 1)
ON DUPLICATE KEY UPDATE `Game_Server_ID` = VALUES(`Game_Server_ID`);
