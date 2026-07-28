-- Any returned row stops this migration. USERNAME becomes NOT NULL in the
-- forward migration, and the generated normalized value is unique.
SELECT 'NULL_USERNAME' AS normalized_username, COUNT(*) AS row_count
FROM `USER`
WHERE `USERNAME` IS NULL
HAVING COUNT(*) > 0
UNION ALL
SELECT LOWER(TRIM(`USERNAME`)) AS normalized_username, COUNT(*) AS row_count
FROM `USER`
WHERE `USERNAME` IS NOT NULL
GROUP BY LOWER(TRIM(`USERNAME`))
HAVING COUNT(*) > 1;
