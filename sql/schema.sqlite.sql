-- bd v0.2 schema for SQLite. Faithful core of upstream beads
-- (gastownhall/beads), stripped of versioning/history (Dolt's job) and AI
-- memory-decay/agent-coordination columns.

CREATE TABLE issues (
    id                  TEXT PRIMARY KEY,
    content_hash        TEXT NOT NULL DEFAULT '',
    title               TEXT NOT NULL,
    description         TEXT NOT NULL DEFAULT '',
    design              TEXT NOT NULL DEFAULT '',
    acceptance_criteria TEXT NOT NULL DEFAULT '',
    notes               TEXT NOT NULL DEFAULT '',

    status              TEXT NOT NULL DEFAULT 'open',
    priority            INTEGER NOT NULL DEFAULT 2 CHECK (priority BETWEEN 0 AND 4),
    issue_type          TEXT NOT NULL DEFAULT 'task',
    assignee            TEXT NOT NULL DEFAULT '',
    estimated_minutes   INTEGER NOT NULL DEFAULT 0,

    created_at          TIMESTAMP NOT NULL,
    created_by          TEXT NOT NULL DEFAULT '',
    owner               TEXT NOT NULL DEFAULT '',
    updated_at          TIMESTAMP NOT NULL,
    started_at          TIMESTAMP,
    closed_at           TIMESTAMP,
    closed_by_session   TEXT NOT NULL DEFAULT '',

    external_ref        TEXT NOT NULL DEFAULT '',
    spec_id             TEXT NOT NULL DEFAULT '',
    metadata            TEXT NOT NULL DEFAULT '{}',
    source_repo         TEXT NOT NULL DEFAULT '',
    source_system       TEXT NOT NULL DEFAULT '',
    close_reason        TEXT NOT NULL DEFAULT '',

    -- type discriminator columns (data only):
    sender              TEXT NOT NULL DEFAULT '',
    ephemeral           INTEGER NOT NULL DEFAULT 0,
    pinned              INTEGER NOT NULL DEFAULT 0,
    is_template         INTEGER NOT NULL DEFAULT 0,
    wisp_type           TEXT NOT NULL DEFAULT '',
    mol_type            TEXT NOT NULL DEFAULT '',
    role_type           TEXT NOT NULL DEFAULT '',
    event_kind          TEXT NOT NULL DEFAULT '',
    actor               TEXT NOT NULL DEFAULT '',
    target              TEXT NOT NULL DEFAULT '',
    payload             TEXT NOT NULL DEFAULT '',

    due_at              TIMESTAMP,
    defer_until         TIMESTAMP
);
CREATE INDEX idx_issues_status     ON issues(status);
CREATE INDEX idx_issues_priority   ON issues(priority);
CREATE INDEX idx_issues_issue_type ON issues(issue_type);
CREATE INDEX idx_issues_assignee   ON issues(assignee);
CREATE INDEX idx_issues_created_at ON issues(created_at);

CREATE TABLE dependencies (
    issue_id      TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    depends_on_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    -- type is intentionally unconstrained: upstream allows custom dependency
    -- types via config; CHECK constraints would reject migration data.
    type          TEXT NOT NULL DEFAULT 'blocks',
    created_at    TIMESTAMP NOT NULL,
    created_by    TEXT NOT NULL DEFAULT '',
    metadata      TEXT NOT NULL DEFAULT '{}',
    thread_id     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (issue_id, depends_on_id)
);
CREATE INDEX idx_dependencies_depends_on      ON dependencies(depends_on_id);
CREATE INDEX idx_dependencies_depends_on_type ON dependencies(depends_on_id, type);
CREATE INDEX idx_dependencies_thread          ON dependencies(thread_id);

CREATE TABLE labels (
    issue_id TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    label    TEXT NOT NULL,
    PRIMARY KEY (issue_id, label)
);
CREATE INDEX idx_labels_label ON labels(label);

CREATE TABLE comments (
    id         TEXT PRIMARY KEY,
    issue_id   TEXT NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    author     TEXT NOT NULL,
    text       TEXT NOT NULL,
    created_at TIMESTAMP NOT NULL
);
CREATE INDEX idx_comments_issue      ON comments(issue_id);
CREATE INDEX idx_comments_created_at ON comments(created_at);

-- Project-level settings stored alongside the data so all clients see the
-- same prefix, id mode, custom statuses, etc.
CREATE TABLE config (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

-- Per-parent atomic counter for hierarchical ids ("yuklar-a3f8.1", ".2", ...).
CREATE TABLE child_counters (
    parent_id  TEXT PRIMARY KEY REFERENCES issues(id) ON DELETE CASCADE,
    last_child INTEGER NOT NULL DEFAULT 0
);

-- Per-prefix sequential counter when issue_id_mode=counter (e.g. "bd-1, bd-2").
CREATE TABLE issue_counter (
    prefix  TEXT PRIMARY KEY,
    last_id INTEGER NOT NULL DEFAULT 0
);
