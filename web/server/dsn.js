// DSN resolution for the web server. Mirrors internal/config/config.go in the
// Go CLI: read .bd/config (or $BD_DB), inject password from $BD_DB_PASSWORD or
// a .env file. Sqlite paths and postgres URIs are both supported.

import fs from 'node:fs';
import path from 'node:path';

export const DIR_NAME = '.bd';
export const CONFIG_NAME = 'config';
export const ENV_FILE_NAME = '.env';
export const ENV_DSN = 'BD_DB';
export const ENV_DIR = 'BD_DIR';
export const ENV_DB_PASSWORD = 'BD_DB_PASSWORD';

export function resolve({ cwd = process.cwd(), env = process.env } = {}) {
  let dsn = '';
  let beadDir = '';

  if (env[ENV_DSN]) dsn = env[ENV_DSN];

  beadDir = findBeadDir(cwd, env);
  if (!dsn && beadDir) dsn = readDSNFromFile(beadDir);
  if (!dsn) {
    if (!beadDir) {
      throw new Error('no .bd directory found — run `bd init` first');
    }
    dsn = path.join(beadDir, 'bd.db');
  }

  const pw = lookupDBPassword(beadDir, env);
  dsn = injectPassword(dsn, pw);
  return { dsn, beadDir };
}

function findBeadDir(cwd, env) {
  if (env[ENV_DIR]) return env[ENV_DIR];
  let cur = path.resolve(cwd);
  for (;;) {
    const candidate = path.join(cur, DIR_NAME);
    try {
      if (fs.statSync(candidate).isDirectory()) return candidate;
    } catch {}
    const parent = path.dirname(cur);
    if (parent === cur) break;
    cur = parent;
  }
  return '';
}

function readDSNFromFile(dir) {
  try {
    const text = fs.readFileSync(path.join(dir, CONFIG_NAME), 'utf8');
    for (const line of text.split('\n')) {
      const t = line.trim();
      if (!t || t.startsWith('#')) continue;
      const eq = t.indexOf('=');
      if (eq < 0) continue;
      const k = t.slice(0, eq).trim();
      const v = t.slice(eq + 1).trim();
      if (k === 'db') return v;
    }
  } catch {}
  return '';
}

function lookupDBPassword(beadDir, env) {
  if (env[ENV_DB_PASSWORD]) return env[ENV_DB_PASSWORD];
  const candidates = [path.join(process.cwd(), ENV_FILE_NAME)];
  if (beadDir) candidates.push(path.join(beadDir, ENV_FILE_NAME));
  for (const p of candidates) {
    const v = readEnvKey(p, ENV_DB_PASSWORD);
    if (v) return v;
  }
  return '';
}

function readEnvKey(file, key) {
  try {
    const text = fs.readFileSync(file, 'utf8');
    const prefix = `${key}=`;
    const exportPrefix = `export ${prefix}`;
    for (const raw of text.split('\n')) {
      let line = raw.trim();
      if (!line || line.startsWith('#')) continue;
      if (line.startsWith(exportPrefix)) line = line.slice('export '.length);
      if (!line.startsWith(prefix)) continue;
      let v = line.slice(prefix.length).trim();
      if ((v.startsWith('"') && v.endsWith('"')) || (v.startsWith("'") && v.endsWith("'"))) {
        v = v.slice(1, -1);
      }
      return v;
    }
  } catch {}
  return '';
}

export function injectPassword(dsn, pw) {
  if (!pw) return dsn;
  if (dsn.startsWith('postgres://') || dsn.startsWith('postgresql://')) {
    return injectURIPassword(dsn, pw);
  }
  return dsn;
}

function injectURIPassword(dsn, pw) {
  const idx = dsn.indexOf('://');
  if (idx < 0) return dsn;
  const scheme = dsn.slice(0, idx);
  const rest = dsn.slice(idx + 3);
  const at = rest.indexOf('@');
  if (at <= 0) return dsn;
  const cred = rest.slice(0, at);
  const hostPath = rest.slice(at);
  if (cred.includes(':')) return dsn;
  return `${scheme}://${cred}:${encodeURIComponent(pw)}${hostPath}`;
}

export function detectDriver(dsn) {
  if (dsn.startsWith('postgres://') || dsn.startsWith('postgresql://')) return 'postgres';
  if (dsn.startsWith('sqlite://')) return 'sqlite';
  if (dsn.startsWith('sqlite:')) return 'sqlite';
  if (dsn.endsWith('.db') || dsn.endsWith('.sqlite') || dsn.endsWith('.sqlite3')) return 'sqlite';
  throw new Error(`cannot determine driver from DSN ${dsn}`);
}

export function sqlitePath(dsn) {
  if (dsn.startsWith('sqlite://')) return dsn.slice('sqlite://'.length);
  if (dsn.startsWith('sqlite:')) return dsn.slice('sqlite:'.length);
  return dsn;
}
