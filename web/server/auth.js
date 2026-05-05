// Optional auth: if BD_WEB_AUTH_FILE points at a JSON users file, the server
// requires a Bearer token on /api/* (except /api/auth/*). Tokens are kept in
// process memory only — restarting invalidates everyone.
//
// Users file shape (matches beads-ui):
//   { "users": [ {"username": "alice", "password": "secret", "role": "Developer"} ] }

import { createHash, randomBytes } from 'node:crypto';
import { readFileSync } from 'node:fs';

export function loadAuth() {
  const file = process.env.BD_WEB_AUTH_FILE;
  if (!file) return { enabled: false, users: [], tokens: new Map() };
  let raw;
  try {
    raw = JSON.parse(readFileSync(file, 'utf8'));
  } catch (err) {
    throw new Error(`failed to read BD_WEB_AUTH_FILE=${file}: ${err.message}`);
  }
  const users = Array.isArray(raw?.users) ? raw.users : [];
  if (users.length === 0) {
    throw new Error(`auth file ${file} has no users[]`);
  }
  return {
    enabled: true,
    users,
    tokens: new Map(), // token -> { username, role, issuedAt }
  };
}

export function login(auth, username, password) {
  const u = auth.users.find((x) => x.username === username && x.password === password);
  if (!u) return null;
  const token = randomBytes(24).toString('hex');
  auth.tokens.set(token, {
    username: u.username,
    role: u.role || 'User',
    issuedAt: Date.now(),
  });
  return { token, user: { username: u.username, role: u.role || 'User' } };
}

export function logout(auth, token) {
  auth.tokens.delete(token);
}

export function authMiddleware(auth) {
  return async (c, next) => {
    if (!auth.enabled) {
      c.set('user', { username: 'anon', role: 'Anonymous' });
      return next();
    }
    // public endpoints
    const p = c.req.path;
    if (p === '/api/auth/login' || p === '/api/healthz') return next();

    // Token sources: Authorization: Bearer header (default) OR ?token= query
    // param (fallback for SSE — EventSource can't set custom headers).
    let token = '';
    const header = c.req.header('authorization') || '';
    const m = /^Bearer\s+(\S+)$/i.exec(header);
    if (m) token = m[1];
    else token = c.req.query('token') || '';
    if (!token) return c.json({ error: 'unauthorized' }, 401);
    const session = auth.tokens.get(token);
    if (!session) return c.json({ error: 'unauthorized' }, 401);
    c.set('user', { username: session.username, role: session.role });
    c.set('token', token);
    return next();
  };
}

// fingerprint is a stable, non-reversible identifier for the auth file
// content, surfaced via /api/me so a client can know which auth realm it's
// talking to.
export function fingerprint(auth) {
  if (!auth.enabled) return '';
  const h = createHash('sha256');
  for (const u of auth.users) h.update(u.username + '\n');
  return h.digest('hex').slice(0, 16);
}
