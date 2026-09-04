-- Migration 0012 — move design into the body, then drop the column (SQLite).
UPDATE issues
SET description = CASE WHEN description = '' THEN '' ELSE description || char(10) || char(10) END
                  || '## Design (migrated)' || char(10) || char(10) || design
WHERE design IS NOT NULL AND design <> '';
ALTER TABLE issues DROP COLUMN design;
