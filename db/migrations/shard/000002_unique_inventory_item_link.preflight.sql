-- An item instance may occupy exactly one inventory row. Any returned row
-- stops the migration so an operator can reconcile duplicated ownership
-- before a uniqueness constraint is added.
SELECT `FK_ITEM`, COUNT(*) AS row_count
FROM `INVENTORY`
GROUP BY `FK_ITEM`
HAVING COUNT(*) > 1;
