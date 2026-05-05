// Entry point for `bd-web`. Builds the Hono app, wires routes, and serves the
// prebuilt React bundle from dist/ in production. In dev (`npm run dev`),
// only this server runs; Vite serves the client on its own port and proxies
// `/api` here.

import { serve } from '@hono/node-server';
import { serveStatic } from '@hono/node-server/serve-static';
import { Hono } from 'hono';
import { existsSync } from 'node:fs';
import { dirname, resolve as resolvePath } from 'node:path';
import { fileURLToPath } from 'node:url';

import { open as openDB } from './db.js';
import { resolve as resolveDSN } from './dsn.js';
import { getConfigValue } from './queries.js';
import { authMiddleware, fingerprint, loadAuth } from './auth.js';
import { authRouter } from './routes/auth.js';
import { issuesRouter } from './routes/issues.js';
import { projectsRouter } from './routes/projects.js';

const HOST = process.env.HOST || '127.0.0.1';
const PORT = Number(process.env.PORT || 3333);

// dist/ lives next to server/ in the package, regardless of where the user
// runs `bd-web` from. Resolve relative to this file, not process.cwd().
const PKG_DIR = resolvePath(dirname(fileURLToPath(import.meta.url)), '..');
const DIST_DIR = resolvePath(PKG_DIR, 'dist');

async function main() {
  const { dsn, beadDir } = resolveDSN();
  const db = await openDB(dsn);
  const auth = loadAuth();

  const app = new Hono();

  // tiny request log; replace with hono/logger if you want a richer one.
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
  app.route('/api/issues', issuesRouter({ db }));
  app.route('/api/projects', projectsRouter({ db }));

  // /api/me — the client always pulls this on boot to know what to render.
  // Bundles per-deployment config (file_attachment_base_url, version) so the
  // client doesn't need a separate /api/config call.
  app.get('/api/me', async (c) => {
    const user = c.get('user') || { username: 'anon', role: 'Anonymous' };
    const prefix = (await getConfigValue(db, 'issue_prefix')) || '';
    const idMode = (await getConfigValue(db, 'issue_id_mode')) || 'hash';
    return c.json({
      project: { prefix, id_mode: idMode },
      user,
      driver: db.driver,
      auth_enabled: auth.enabled,
      auth_fingerprint: fingerprint(auth),
      bead_dir: beadDir,
      version: process.env.npm_package_version || '0.0.0',
      file_attachment_base_url: (process.env.FILE_ATTACHMENT_BASE_URL || '').replace(/\/$/, ''),
    });
  });

  app.notFound((c) => c.json({ error: 'not found' }, 404));
  app.onError((err, c) => {
    console.error('error:', err);
    return c.json({ error: err.message || 'internal error' }, 500);
  });

  // serve built React app in production. We serve only when dist/ exists so
  // `npm run dev:server` (no client build) doesn't 404 on `/` cryptically.
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
    console.log(`  driver: ${db.driver}`);
    console.log(`  prefix: ${'(loaded lazily on first request)'}`);
    console.log(`  auth:   ${auth.enabled ? 'enabled' : 'disabled (BD_WEB_AUTH_FILE unset)'}`);
    if (process.env.BD_WEB_OPEN === '1') tryOpen(`http://${HOST === '0.0.0.0' ? '127.0.0.1' : HOST}:${info.port}`);
  });

  const stop = async () => {
    try { await db.close(); } catch {}
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
