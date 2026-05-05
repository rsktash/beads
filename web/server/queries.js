// SQL used by the read-only Hono routes. Schema is owned by the Go CLI; we
// only SELECT + the occasional mutation. Both engines accept the same ANSI
// SQL we use here (LIKE patterns, IN clauses, parameterised values).
//
// Placeholders are `?` style. The pg adapter rewrites them to $N at runtime.

import { rowToComment, rowToDependency, rowToIssue } from './types.js';

export async function getConfigValue(db, key) {
  const r = await db.one('SELECT value FROM config WHERE key = ?', [key]);
  return r ? String(r.value) : '';
}

// Computed columns shared by list/ready/detail. Keeps the JSON shape stable
// across endpoints (parent_id, parent_title, total_children, closed_children,
// blocked_by_count, comment_count).
const ENRICHED_COMPUTED = `
  (SELECT depends_on_id FROM dependencies d
    WHERE d.issue_id = i.id AND d.type = 'parent-child' LIMIT 1) AS parent_id,
  (SELECT p.title FROM dependencies d JOIN issues p ON p.id = d.depends_on_id
    WHERE d.issue_id = i.id AND d.type = 'parent-child' LIMIT 1) AS parent_title,
  (SELECT COUNT(*) FROM dependencies d
    WHERE d.depends_on_id = i.id AND d.type = 'parent-child') AS total_children,
  (SELECT COUNT(*) FROM dependencies d JOIN issues c ON c.id = d.issue_id
    WHERE d.depends_on_id = i.id AND d.type = 'parent-child'
      AND c.status = 'closed') AS closed_children,
  (SELECT COUNT(*) FROM dependencies d JOIN issues b ON b.id = d.depends_on_id
    WHERE d.issue_id = i.id AND d.type = 'blocks'
      AND b.status NOT IN ('closed', 'pinned')) AS blocked_by_count,
  (SELECT COUNT(*) FROM comments c WHERE c.issue_id = i.id) AS comment_count
`;

// Full row + enrichment — used by getIssue (detail page wants everything).
const ENRICHED_FULL = `i.*, ${ENRICHED_COMPUTED}`;

// Slim row + enrichment — used by listIssues and readyIssues. Drops the heavy
// markdown bodies (description/design/acceptance_criteria/notes) and the JSON
// blobs (metadata/payload) which the board, list, and search dialog never
// render. On epic-heavy projects this cuts the polled response by 50–80%.
// rowToIssue (server/types.js) defaults missing fields to '' so the public
// JSON shape stays the same — clients still see those keys, just empty.
const ENRICHED_SLIM = `
  i.id, i.title, i.status, i.priority, i.issue_type,
  i.assignee, i.estimated_minutes, i.content_hash,
  i.created_at, i.created_by, i.owner,
  i.updated_at, i.started_at, i.closed_at, i.due_at, i.defer_until,
  i.closed_by_session, i.close_reason,
  i.external_ref, i.spec_id, i.source_repo, i.source_system,
  i.sender, i.ephemeral, i.pinned, i.is_template,
  i.wisp_type, i.mol_type, i.role_type, i.event_kind,
  i.actor, i.target,
  ${ENRICHED_COMPUTED}
`;

export async function listIssues(db, filters = {}, limit = 0) {
  const where = [];
  const args = [];
  if (filters.status) {
    where.push('i.status = ?');
    args.push(filters.status);
  }
  if (filters.type) {
    where.push('i.issue_type = ?');
    args.push(filters.type);
  }
  if (filters.assignee) {
    where.push('i.assignee = ?');
    args.push(filters.assignee);
  }
  if (filters.priority !== undefined && filters.priority !== null && filters.priority !== '') {
    where.push('i.priority = ?');
    args.push(Number(filters.priority));
  }
  let sql = `SELECT ${ENRICHED_SLIM} FROM issues i`;
  if (where.length) sql += ' WHERE ' + where.join(' AND ');
  sql += ' ORDER BY i.priority ASC, i.created_at ASC';
  if (limit > 0) sql += ` LIMIT ${Number(limit) | 0}`;
  const rows = await db.all(sql, args);
  return rows.map(rowToIssue);
}

export async function getIssue(db, id) {
  const sql = `SELECT ${ENRICHED_FULL} FROM issues i WHERE i.id = ?`;
  const r = await db.one(sql, [id]);
  return r ? rowToIssue(r) : null;
}

export async function listLabels(db, issueId) {
  const rows = await db.all(
    'SELECT label FROM labels WHERE issue_id = ? ORDER BY label',
    [issueId],
  );
  return rows.map((r) => r.label);
}

// listBlockedBy returns up to `limit` issue ids/titles that currently block
// the given issue. Used by the IssueCard "blocked by" badges.
export async function listBlockedBy(db, issueId, limit = 5) {
  const rows = await db.all(
    `SELECT b.id, b.title FROM dependencies d
       JOIN issues b ON b.id = d.depends_on_id
       WHERE d.issue_id = ? AND d.type = 'blocks'
         AND b.status NOT IN ('closed', 'pinned')
       ORDER BY b.created_at
       LIMIT ${Number(limit) | 0}`,
    [issueId],
  );
  return rows;
}

// listChildren returns the parent-child children of an issue (where THIS
// issue is the depends_on_id, type='parent-child'). Used by the metadata
// sidebar's Children card.
export async function listChildren(db, parentId) {
  const rows = await db.all(
    `SELECT c.id, c.title, c.status, c.priority, c.issue_type
       FROM dependencies d
       JOIN issues c ON c.id = d.issue_id
       WHERE d.depends_on_id = ? AND d.type = 'parent-child'
       ORDER BY c.priority ASC, c.created_at ASC`,
    [parentId],
  );
  return rows;
}

export async function listDependencies(db, issueId) {
  const rows = await db.all(
    'SELECT * FROM dependencies WHERE issue_id = ? OR depends_on_id = ? ORDER BY created_at',
    [issueId, issueId],
  );
  return rows.map(rowToDependency);
}

export async function listComments(db, issueId) {
  const rows = await db.all(
    'SELECT * FROM comments WHERE issue_id = ? ORDER BY created_at',
    [issueId],
  );
  return rows.map(rowToComment);
}

export async function readyIssues(db) {
  const sql = `
    SELECT ${ENRICHED_SLIM} FROM issues i
    WHERE i.status = 'open'
      AND i.ephemeral = 0
      AND i.is_template = 0
      AND (i.defer_until IS NULL OR i.defer_until <= ?)
      AND NOT EXISTS (
          SELECT 1 FROM dependencies d
          JOIN issues blocker ON blocker.id = d.depends_on_id
          WHERE d.issue_id = i.id
            AND d.type = 'blocks'
            AND blocker.status NOT IN ('closed', 'pinned')
      )
    ORDER BY i.priority ASC, i.created_at ASC`;
  const now = new Date().toISOString();
  const rows = await db.all(sql, [now]);
  return rows.map(rowToIssue);
}

// ---------- mutations (subset: comments + labels only) ----------

export async function addComment(db, { id, issueId, author, text }) {
  const now = new Date().toISOString();
  await db.exec(
    `INSERT INTO comments (id, issue_id, author, text, created_at)
     VALUES (?, ?, ?, ?, ?)`,
    [id, issueId, author, text, now],
  );
  return { id, issue_id: issueId, author, text, created_at: now };
}

export async function deleteComment(db, commentId) {
  await db.exec('DELETE FROM comments WHERE id = ?', [commentId]);
}

export async function addLabel(db, issueId, label) {
  // idempotent — re-adding the same (issue, label) is a no-op
  try {
    await db.exec(
      'INSERT INTO labels (issue_id, label) VALUES (?, ?)',
      [issueId, label],
    );
  } catch (err) {
    if (!/UNIQUE|duplicate/i.test(String(err?.message))) throw err;
  }
}

export async function removeLabel(db, issueId, label) {
  await db.exec(
    'DELETE FROM labels WHERE issue_id = ? AND label = ?',
    [issueId, label],
  );
}

export async function listProjects(db) {
  if (db.driver !== 'postgres') return [];
  const sql = `
    SELECT s.schema_name AS prefix
    FROM information_schema.schemata s
    WHERE s.schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast', 'public')
      AND s.schema_name NOT LIKE 'pg_%'
      AND EXISTS (
        SELECT 1 FROM information_schema.tables t
        WHERE t.table_schema = s.schema_name AND t.table_name = 'config'
      )
    ORDER BY s.schema_name`;
  const rows = await db.all(sql);
  return rows.map((r) => ({ prefix: r.prefix }));
}
