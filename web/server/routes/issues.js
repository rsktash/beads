import { Hono } from 'hono';
import {
  getIssue,
  listBlockedBy,
  listComments,
  listDependencies,
  listIssues,
  listLabels,
  readyIssues,
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

  return r;
}
