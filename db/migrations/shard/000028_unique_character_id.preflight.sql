-- MySQL 8.4 requires referenced columns to be backed by a unique key.
SELECT `id`, COUNT(*) AS row_count
FROM `CHAR`
GROUP BY `id`
HAVING COUNT(*) > 1;
