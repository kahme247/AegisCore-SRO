-- Continent names are labels shared by many region rows, not unique identities.
ALTER TABLE `GAME_SERVER_REGION`
    DROP FOREIGN KEY `FK_Continent_Name`;
