-- Any returned row stops this migration. Existing character names are checked
-- with the exact lower/trim expression used by the generated unique key.
SELECT LOWER(TRIM(`CHAR_NAME`)) AS normalized_character_name, COUNT(*) AS row_count
FROM `CHAR`
GROUP BY LOWER(TRIM(`CHAR_NAME`))
HAVING COUNT(*) > 1;
