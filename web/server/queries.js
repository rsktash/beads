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

export async function listIssues(db, filters = {}, limit = 0) {
  const where = [];
  const args = [];
  if (filters.status) {
    where.push('status = ?');
    args.push(filters.status);
  }
  if (filters.type) {
    where.push('issue_type = ?');
    args.push(filters.type);
  }
  if (filters.assignee) {
    where.push('assignee = ?');
    args.push(filters.assignee);
  }
  if (filters.priority !== undefined && filters.priority !== null && filters.priority !== '') {
    where.push('priority = ?');
    args.push(Number(filters.priority));
  }
  let sql = 'SELECT * FROM issues';
  if (where.length) sql += ' WHERE ' + where.join(' AND ');
  sql += ' ORDER BY priority ASC, created_at ASC';
  if (limit > 0) sql += ` LIMIT ${Number(limit) | 0}`;
  const rows = await db.all(sql, args);
  return rows.map(rowToIssue);
}

export async function getIssue(db, id) {
  const r = await db.one('SELECT * FROM issues WHERE id = ?', [id]);
  return r ? rowToIssue(r) : null;
}

export async function listLabels(db, issueId) {
  const rows = await db.all(
    'SELECT label FROM labels WHERE issue_id = ? ORDER BY label',
    [issueId],
  );
  return rows.map((r) => r.label);
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
  // Mirrors store.Ready() in the Go side: open, non-ephemeral, non-template,
  // not deferred, no `blocks` dep from a non-{closed,pinned} issue.
  const sql = `
    SELECT i.* FROM issues i
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

// listProjects scans postgres for schemas that look like a beads project
// (i.e. contain a `config` table with an `issue_prefix` row). Returns []
// for sqlite — there is only one project per file.
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
