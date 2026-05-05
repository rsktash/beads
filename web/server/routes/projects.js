import { Hono } from 'hono';
import { listProjects } from '../queries.js';

export function projectsRouter(deps) {
  const { db } = deps;
  const r = new Hono();
  r.get('/', async (c) => {
    const projects = await listProjects(db);
    return c.json({ projects });
  });
  return r;
}
