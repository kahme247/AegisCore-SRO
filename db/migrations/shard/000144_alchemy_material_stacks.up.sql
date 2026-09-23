UPDATE `ITEMDATA`
SET `Stack_Size` = 50
WHERE `TypeID_1` = 3
  AND `TypeID_2` = 3
  AND (
      (`TypeID_3` = 10 AND `TypeID_4` IN (1, 4))
      OR (`TypeID_3` = 11 AND `TypeID_4` IN (1, 2, 7, 8))
  );
