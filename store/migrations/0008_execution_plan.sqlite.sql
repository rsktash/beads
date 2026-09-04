-- Migration 0008 — execution plans, lanes and typed handoffs (SQLite).
CREATE TABLE IF NOT EXISTS execution_plan (
    id TEXT PRIMARY KEY,
    title TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','done','abandoned')),
    preflight TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMP NOT NULL,
    created_by TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS plan_lane (
    plan_id TEXT NOT NULL REFERENCES execution_plan(id) ON DELETE CASCADE,
    lane TEXT NOT NULL,
    queue TEXT NOT NULL DEFAULT '',
    cursor INTEGER NOT NULL DEFAULT 0,
    mode TEXT NOT NULL DEFAULT 'subagent' CHECK (mode IN ('inline','subagent','codex')),
    rulings TEXT NOT NULL DEFAULT '',
    holder TEXT,
    claimed_at TIMESTAMP,
    PRIMARY KEY (plan_id, lane)
);
CREATE TABLE IF NOT EXISTS plan_session (
    plan_id TEXT NOT NULL REFERENCES execution_plan(id) ON DELETE CASCADE,
    session_id TEXT NOT NULL,
    lane TEXT NOT NULL DEFAULT '',
    joined_at TIMESTAMP NOT NULL,
    PRIMARY KEY (plan_id, session_id)
);
CREATE TABLE IF NOT EXISTS plan_handoff (
    id TEXT PRIMARY KEY,
    plan_id TEXT NOT NULL,
    lane TEXT NOT NULL,
    session_id TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL,
    done_ids TEXT NOT NULL DEFAULT '',
    next_id TEXT NOT NULL DEFAULT '',
    parked TEXT NOT NULL DEFAULT '',
    thread TEXT NOT NULL DEFAULT '',
    FOREIGN KEY (plan_id, lane) REFERENCES plan_lane(plan_id, lane) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_plan_handoff_lane ON plan_handoff(plan_id, lane, created_at);
