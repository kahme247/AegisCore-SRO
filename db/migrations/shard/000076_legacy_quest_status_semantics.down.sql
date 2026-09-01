-- The old statuses 3 and 4 both map to legacy closed state 5, so rollback
-- cannot recover which old completion label was originally used.
UPDATE `CHAR_QUEST`
SET `COMPLETED_AT` = CASE
        WHEN `STATUS` IN (4, 8) THEN NULL
        ELSE `COMPLETED_AT`
    END,
    `STATUS` = CASE
        WHEN `STATUS` = 4 THEN 5
        WHEN `STATUS` = 5 THEN 3
        WHEN `STATUS` = 8 THEN 2
        ELSE `STATUS`
    END;
