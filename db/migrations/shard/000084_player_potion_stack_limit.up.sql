UPDATE `ITEMDATA`
SET `Stack_Size` = 50000
WHERE `TypeID_1` = 3
  AND `TypeID_2` = 3
  AND `TypeID_3` = 1
  AND `TypeID_4` IN (1, 2)
  AND (`CodeName` LIKE 'ITEM\_ETC\_HP\_POTION\_%' ESCAPE '\\'
    OR `CodeName` LIKE 'ITEM\_ETC\_MP\_POTION\_%' ESCAPE '\\');
