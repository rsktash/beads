-- Migration 0012 — move design into the body, then drop the column (Postgres).
UPDATE issues
SET description = CASE WHEN description = '' THEN '' ELSE description || E'\n\n' END
                  || '## Design (migrated)' || E'\n\n' || design
WHERE design IS NOT NULL AND design <> '';
ALTER TABLE issues DROP COLUMN IF EXISTS design;
