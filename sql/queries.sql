-- Dialect-portable queries: named args via sqlc.arg() so both `?` (sqlite)
-- and `$N` (postgres) backends generate clean code. Keep this file ASCII
-- only and avoid apostrophes in comments (sqlc parser is fragile here).

-- name: CreateIssue :exec
INSERT INTO issues (
    id, content_hash, title, description, design, acceptance_criteria, notes,
    status, priority, issue_type, assignee, estimated_minutes,
    created_at, created_by, owner, updated_at, started_at, closed_at, closed_by_session,
    external_ref, spec_id, metadata, source_repo, source_system, close_reason,
    sender, ephemeral, pinned, is_template,
    wisp_type, mol_type, role_type,
    event_kind, actor, target, payload,
    due_at, defer_until
) VALUES (
    sqlc.arg('id'), sqlc.arg('content_hash'), sqlc.arg('title'),
    sqlc.arg('description'), sqlc.arg('design'), sqlc.arg('acceptance_criteria'),
    sqlc.arg('notes'),
    sqlc.arg('status'), sqlc.arg('priority'), sqlc.arg('issue_type'),
    sqlc.arg('assignee'), sqlc.arg('estimated_minutes'),
    sqlc.arg('created_at'), sqlc.arg('created_by'), sqlc.arg('owner'),
    sqlc.arg('updated_at'), sqlc.arg('started_at'), sqlc.arg('closed_at'),
    sqlc.arg('closed_by_session'),
    sqlc.arg('external_ref'), sqlc.arg('spec_id'), sqlc.arg('metadata'),
    sqlc.arg('source_repo'), sqlc.arg('source_system'), sqlc.arg('close_reason'),
    sqlc.arg('sender'), sqlc.arg('ephemeral'), sqlc.arg('pinned'),
    sqlc.arg('is_template'),
    sqlc.arg('wisp_type'), sqlc.arg('mol_type'), sqlc.arg('role_type'),
    sqlc.arg('event_kind'), sqlc.arg('actor'), sqlc.arg('target'),
    sqlc.arg('payload'),
    sqlc.arg('due_at'), sqlc.arg('defer_until')
);

-- name: GetIssue :one
SELECT * FROM issues WHERE id = sqlc.arg('id');

-- name: DeleteIssue :execrows
DELETE FROM issues WHERE id = sqlc.arg('id');

-- name: CountIssuesWithPrefix :one
-- Used by the adaptive id-length selector: more issues -> longer hash.
SELECT COUNT(*) FROM issues
WHERE id LIKE sqlc.arg('like_pattern');

-- name: AddDependency :exec
INSERT INTO dependencies (issue_id, depends_on_id, type, created_at, created_by, metadata, thread_id)
VALUES (
    sqlc.arg('issue_id'), sqlc.arg('depends_on_id'), sqlc.arg('type'),
    sqlc.arg('created_at'), sqlc.arg('created_by'), sqlc.arg('metadata'),
    sqlc.arg('thread_id')
);

-- name: RemoveDependency :execrows
DELETE FROM dependencies
WHERE issue_id = sqlc.arg('issue_id') AND depends_on_id = sqlc.arg('depends_on_id');

-- name: ListDependenciesTouching :many
SELECT * FROM dependencies
WHERE issue_id = sqlc.arg('id') OR depends_on_id = sqlc.arg('id')
ORDER BY created_at;

-- name: BlocksReachableFrom :many
SELECT depends_on_id FROM dependencies
WHERE issue_id = sqlc.arg('issue_id') AND type = sqlc.arg('type');

-- name: AddLabel :exec
INSERT INTO labels (issue_id, label) VALUES (sqlc.arg('issue_id'), sqlc.arg('label'));

-- name: RemoveLabel :execrows
DELETE FROM labels WHERE issue_id = sqlc.arg('issue_id') AND label = sqlc.arg('label');

-- name: ListLabels :many
SELECT label FROM labels WHERE issue_id = sqlc.arg('issue_id') ORDER BY label;

-- name: AddComment :exec
INSERT INTO comments (id, issue_id, author, text, created_at)
VALUES (sqlc.arg('id'), sqlc.arg('issue_id'), sqlc.arg('author'),
        sqlc.arg('text'), sqlc.arg('created_at'));

-- name: ListComments :many
SELECT * FROM comments WHERE issue_id = sqlc.arg('issue_id') ORDER BY created_at;

-- name: GetConfigValue :one
SELECT value FROM config WHERE key = sqlc.arg('key');

-- name: SetConfigValue :exec
-- Use `excluded.value` (the standard ON CONFLICT pseudo-row) to avoid binding
-- the value parameter twice. sqlc named-arg expansion mangles repeated args
-- in INSERT...ON CONFLICT...DO UPDATE statements.
INSERT INTO config (key, value) VALUES (sqlc.arg('key'), sqlc.arg('value'))
ON CONFLICT(key) DO UPDATE SET value = excluded.value;

-- name: ListConfig :many
SELECT key, value FROM config ORDER BY key;

-- name: NextChildIndex :one
INSERT INTO child_counters (parent_id, last_child)
VALUES (sqlc.arg('parent_id'), 1)
ON CONFLICT(parent_id) DO UPDATE SET last_child = child_counters.last_child + 1
RETURNING last_child;

-- name: NextCounterID :one
INSERT INTO issue_counter (prefix, last_id)
VALUES (sqlc.arg('prefix'), 1)
ON CONFLICT(prefix) DO UPDATE SET last_id = issue_counter.last_id + 1
RETURNING last_id;

-- name: ReadyAt :many
SELECT i.* FROM issues i
WHERE i.status = 'open'
  AND i.ephemeral = 0
  AND i.is_template = 0
  AND (i.defer_until IS NULL OR i.defer_until <= sqlc.arg('now'))
  AND NOT EXISTS (
      SELECT 1 FROM dependencies d
      JOIN issues blocker ON blocker.id = d.depends_on_id
      WHERE d.issue_id = i.id
        AND d.type = 'blocks'
        AND blocker.status NOT IN ('closed', 'pinned')
  )
ORDER BY i.priority ASC, i.created_at ASC;
