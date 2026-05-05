// Entry point for `bd-web`. Multi-project capable: a single instance routes
// project-scoped requests to per-schema connection pools (see db.js). URLs
// look like /api/p/<prefix>/issues, /api/p/<prefix>/issues/:id, etc.
// `/api/me` and `/api/projects` are project-agnostic.

import { serve } from '@hono/node-server';
import { serveStatic } from '@hono/node-server/serve-static';
import { Hono } from 'hono';
import { existsSync } from 'node:fs';
import { dirname, resolve as resolvePath } from 'node:path';
import { fileURLToPath } from 'node:url';

import { openRoot } from './db.js';
import { resolve as resolveDSN } from './dsn.js';
import { getConfigValue } from './queries.js';
import { authMiddleware, fingerprint, loadAuth } from './auth.js';
import { authRouter } from './routes/auth.js';
import { issuesRouter } from './routes/issues.js';

const HOST = process.env.HOST || '127.0.0.1';
const PORT = Number(process.env.PORT || 3333);

const PKG_DIR = resolvePath(dirname(fileURLToPath(import.meta.url)), '..');
const DIST_DIR = resolvePath(PKG_DIR, 'dist');

async function main() {
  const { dsn, beadDir } = resolveDSN();
  const root = await openRoot(dsn);
  const auth = loadAuth();

  const app = new Hono();

  app.use('*', async (c, next) => {
    const t = Date.now();
    await next();
    if (process.env.DEBUG) {
      console.log(`${c.req.method} ${c.req.path} ${c.res.status} ${Date.now() - t}ms`);
    }
  });

  app.get('/api/healthz', (c) => c.json({ ok: true }));
  app.use('/api/*', authMiddleware(auth));
  app.route('/api/auth', authRouter(auth));

  // /api/me — returns auth + the active set of projects.
  app.get('/api/me', async (c) => {
    const user = c.get('user') || { username: 'anon', role: 'Anonymous' };
    let projects = [];
    try {
      const names = await root.listProjectNames();
      projects = await Promise.all(names.map(async (prefix) => {
        try {
          const p = await root.forProject(prefix);
          const idMode = (await getConfigValue(p, 'issue_id_mode')) || 'hash';
          return { prefix, id_mode: idMode };
        } catch {
          return { prefix, id_mode: 'hash' };
        }
      }));
    } catch (err) {
      // sqlite or unreachable — return empty list, the client falls back to
      // the project-less single-DB UI.
      if (process.env.DEBUG) console.error('listProjectNames failed:', err.message);
    }
    return c.json({
      user,
      driver: root.driver,
      projects,
      auth_enabled: auth.enabled,
      auth_fingerprint: fingerprint(auth),
      bead_dir: beadDir,
      version: process.env.npm_package_version || '0.0.0',
      file_attachment_base_url: (process.env.FILE_ATTACHMENT_BASE_URL || '').replace(/\/$/, ''),
    });
  });

  // /api/projects — same as /api/me's projects field, kept as a separate
  // endpoint for the (now path-prefixed) project picker page.
  app.get('/api/projects', async (c) => {
    const names = await root.listProjectNames();
    return c.json({ projects: names.map((prefix) => ({ prefix })) });
  });

  // Project-scoped middleware: validates the prefix and attaches the pool.
  const projectScope = async (c, next) => {
    const prefix = c.req.param('prefix');
    if (!prefix || !/^[a-z0-9_-]+$/.test(prefix)) {
      return c.json({ error: 'invalid project prefix' }, 400);
    }
    let p;
    try {
      p = await root.forProject(prefix);
      // sanity-check: schema has a config table
      await p.one('SELECT 1 FROM config LIMIT 1');
    } catch (err) {
      return c.json({ error: `project not found: ${prefix}` }, 404);
    }
    c.set('db', p);
    c.set('project', prefix);
    await next();
  };

  // Per-project endpoints. Sub-routers read the db from context.
  app.use('/api/p/:prefix/*', projectScope);

  app.get('/api/p/:prefix/me', async (c) => {
    const db = c.get('db');
    const prefix = c.get('project');
    const idMode = (await getConfigValue(db, 'issue_id_mode')) || 'hash';
    return c.json({ project: { prefix, id_mode: idMode } });
  });

  app.route('/api/p/:prefix/issues', issuesRouter());

  app.notFound((c) => c.json({ error: 'not found' }, 404));
  app.onError((err, c) => {
    console.error('error:', err);
    return c.json({ error: err.message || 'internal error' }, 500);
  });

  if (existsSync(DIST_DIR)) {
    app.use('/*', serveStatic({ root: DIST_DIR }));
    app.get('/*', serveStatic({ path: resolvePath(DIST_DIR, 'index.html') }));
  } else {
    app.get('/', (c) => c.json({
      hint: `client build not found at ${DIST_DIR}; run \`npm run build\` or use vite dev (5173).`,
    }));
  }

  serve({ fetch: app.fetch, hostname: HOST, port: PORT }, (info) => {
    console.log(`bd-web ${PORT}/${info.port} listening on http://${HOST}:${info.port}`);
    console.log(`  driver: ${root.driver}`);
    console.log(`  auth:   ${auth.enabled ? 'enabled' : 'disabled (BD_WEB_AUTH_FILE unset)'}`);
    if (process.env.BD_WEB_OPEN === '1') {
      tryOpen(`http://${HOST === '0.0.0.0' ? '127.0.0.1' : HOST}:${info.port}`);
    }
  });

  const stop = async () => {
    try { await root.close(); } catch {}
    process.exit(0);
  };
  process.on('SIGINT', stop);
  process.on('SIGTERM', stop);
}

function tryOpen(url) {
  const cmd = process.platform === 'darwin' ? 'open'
    : process.platform === 'win32' ? 'start'
    : 'xdg-open';
  import('node:child_process').then(({ spawn }) => {
    spawn(cmd, [url], { detached: true, stdio: 'ignore' }).unref();
  }).catch(() => {});
}

main().catch((err) => {
  console.error('fatal:', err.message);
  process.exit(1);
});
