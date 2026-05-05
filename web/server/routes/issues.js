import { Hono } from 'hono';
import { randomUUID } from 'node:crypto';
import {
  addComment,
  addLabel,
  deleteComment,
  getIssue,
  listBlockedBy,
  listComments,
  listDependencies,
  listIssues,
  listLabels,
  readyIssues,
  removeLabel,
} from '../queries.js';

export function issuesRouter(deps) {
  const { db } = deps;
  const r = new Hono();

  r.get('/', async (c) => {
    const q = c.req.query();
    const limit = q.limit ? Number(q.limit) : 0;
    const issues = await listIssues(db, q, limit);
    return c.json({ issues });
  });

  r.get('/ready', async (c) => {
    const issues = await readyIssues(db);
    return c.json({ issues });
  });

  r.get('/:id', async (c) => {
    const id = c.req.param('id');
    const issue = await getIssue(db, id);
    if (!issue) return c.json({ error: 'not found' }, 404);
    const [labels, deps, comments, blockedBy] = await Promise.all([
      listLabels(db, id),
      listDependencies(db, id),
      listComments(db, id),
      listBlockedBy(db, id, 5),
    ]);
    return c.json({ issue, labels, dependencies: deps, comments, blocked_by: blockedBy });
  });

  // ---------- comments ----------
  r.post('/:id/comments', async (c) => {
    const issueId = c.req.param('id');
    const body = await c.req.json().catch(() => ({}));
    const text = String(body.text || '').trim();
    if (!text) return c.json({ error: 'text is required' }, 400);
    const user = c.get('user') || { username: 'anon' };
    const comment = await addComment(db, {
      id: randomUUID(),
      issueId,
      author: user.username,
      text,
    });
    return c.json({ comment });
  });

  r.delete('/:id/comments/:commentId', async (c) => {
    await deleteComment(db, c.req.param('commentId'));
    return c.json({ ok: true });
  });

  // ---------- labels ----------
  r.post('/:id/labels', async (c) => {
    const issueId = c.req.param('id');
    const body = await c.req.json().catch(() => ({}));
    const label = String(body.label || '').trim();
    if (!label) return c.json({ error: 'label is required' }, 400);
    await addLabel(db, issueId, label);
    return c.json({ ok: true, label });
  });

  r.delete('/:id/labels/:label', async (c) => {
    await removeLabel(db, c.req.param('id'), c.req.param('label'));
    return c.json({ ok: true });
  });

  return r;
}
