DELETE quests
FROM `CHAR_QUEST` quests
JOIN `CHAR` c ON c.`ID` = quests.`FK_CHAR`
JOIN `CHAR_REF_DATA` r ON r.`RefObjID` = c.`REF_OBJ_ID`
JOIN `REF_CHAR_DEFAULT_QUEST` defaults
  ON defaults.`QUEST_ID` = quests.`QUEST_ID`
 AND defaults.`RACE` IN (r.`Country`, 3)
WHERE defaults.`REQUIRED_LEVEL` = 1
  AND defaults.`SERVICE` = 1;

DROP TABLE `REF_CHAR_DEFAULT_QUEST`;
