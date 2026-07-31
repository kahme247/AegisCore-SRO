-- Media refshoptab.txt defines four stable tabs. The third is intentionally
-- empty; cure supplies belong to source TAB4 and the client requests it as
-- wire tab 3. The original SHOP_GOODS seed collapsed that empty tab.
UPDATE `SHOP_GOODS`
SET `TAB_INDEX` = 3
WHERE `NPC_CODE_NAME` IN (
    'NPC_CA_HORSE',
    'NPC_CH_HORSE',
    'NPC_EU_HORSE',
    'NPC_KT_HORSE',
    'NPC_SD_M_AREA_HORSE',
    'NPC_WC_HORSE'
)
  AND `TAB_INDEX` = 2
  AND `FK_REF_ITEM` IN (2143, 2144, 7552, 7553, 9226, 9227, 9237);
