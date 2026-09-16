ALTER TABLE `SRO_SHARD`.`GAMEPLAY_MOB_RATES`
    DROP CHECK `chk_gameplay_gold_drop_amount_rate_positive`,
    DROP COLUMN `GOLD_DROP_AMOUNT_RATE_PERCENT`;
