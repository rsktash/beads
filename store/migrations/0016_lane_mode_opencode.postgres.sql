-- Migration 0016 — lane mode 'opencode'; 'codex' stays allowed for existing lanes (Postgres).
-- Migration 0008 left the mode CHECK unnamed, so it is found by its definition in
-- the schema bd runs in, never by an assumed name; finding none fails the migration.
DO $$
DECLARE
    con record;
    dropped integer := 0;
BEGIN
    FOR con IN
        SELECT p.conname
        FROM pg_constraint p
        JOIN pg_class c ON c.oid = p.conrelid
        WHERE c.relname = 'plan_lane'
          AND c.relnamespace = current_schema()::regnamespace
          AND p.contype = 'c'
          AND pg_get_constraintdef(p.oid) ~ '\mmode\M'
    LOOP
        EXECUTE format('ALTER TABLE plan_lane DROP CONSTRAINT %I', con.conname);
        dropped := dropped + 1;
    END LOOP;
    IF dropped = 0 THEN
        RAISE EXCEPTION 'migration 0016: no CHECK on plan_lane.mode in schema %', current_schema();
    END IF;
END
$$;
ALTER TABLE plan_lane ADD CONSTRAINT plan_lane_mode_check
    CHECK (mode IN ('inline','subagent','codex','opencode'));
