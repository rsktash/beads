// Builds a fixture database for a given DSN.
// - Builds bd binary from source at repo root into a temp directory (ensures not using PATH binary)
// - Uses that binary to init the DB and create issues + parent-child edges so migration bookkeeping stays Go store's own
// - Inserts statement rows through adapter returned by openRoot — one code path for both engines.

import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { openRoot } from '../server/db.js';

const __filename = fileURLToPath(import.meta.url);
const __dirname = path.dirname(__filename);
const repoRoot = path.resolve(__dirname, '..', '..');

function ensureSearchPath(dsn, prefix) {
  if (!dsn.startsWith('postgres://') && !dsn.startsWith('postgresql://')) return dsn;
  try {
    const u = new URL(dsn);
    if (!u.searchParams.has('search_path')) {
      u.searchParams.set('search_path', prefix);
      return u.toString();
    }
    return dsn;
  } catch {
    return dsn;
  }
}

function sqliteFileFromDSN(dsn) {
  if (dsn.startsWith('sqlite://')) return dsn.slice('sqlite://'.length);
  if (dsn.startsWith('sqlite:')) return dsn.slice('sqlite:'.length);
  if (dsn.endsWith('.db') || dsn.endsWith('.sqlite') || dsn.endsWith('.sqlite3')) return dsn;
  return null;
}

export async function createFixture(rawDsn) {
  // Derive prefix from DSN's search_path if present (postgres throwaway schema case), else 'ft'
  let prefix = 'ft';
  if (rawDsn.startsWith('postgres://') || rawDsn.startsWith('postgresql://')) {
    try {
      const u = new URL(rawDsn);
      if (u.searchParams.has('search_path')) {
        const sp = u.searchParams.get('search_path');
        if (sp) prefix = sp;
      }
    } catch {}
  }
  const dsn = ensureSearchPath(rawDsn, prefix);

  // 1. Build bd binary from source into temp dir
  const buildDir = fs.mkdtempSync(path.join(os.tmpdir(), 'bd-build-'));
  const bdBin = path.join(buildDir, 'bd');
  // Build step — must be from source, not PATH. This is the harness gate for "builds bd from source".
  execSync(`go build -o ${bdBin} ./cmd/bd`, { cwd: repoRoot, stdio: 'pipe' });

  // 2. Init database via bd binary in a temp workdir
  const workDir = fs.mkdtempSync(path.join(os.tmpdir(), 'bd-work-'));
  // Ensure directory for sqlite file exists
  const sqliteFile = sqliteFileFromDSN(dsn);
  if (sqliteFile) {
    fs.mkdirSync(path.dirname(sqliteFile), { recursive: true });
  }

  // bd init writes .bd/config into workDir and initializes the DB schema
  execSync(`${bdBin} --db ${dsn} init --prefix ${prefix} --id-mode counter`, {
    cwd: workDir,
    stdio: 'pipe',
  });

  function bdCreate(title, opts = {}) {
    const type = opts.type || 'task';
    const priority = opts.priority != null ? opts.priority : 2;
    let cmd = `${bdBin} --db ${dsn} create "${title.replace(/"/g, '\\"')}" --type ${type} --priority ${priority} --json`;
    if (opts.parent) cmd += ` --parent ${opts.parent}`;
    const out = execSync(cmd, { cwd: workDir, encoding: 'utf8', stdio: ['pipe', 'pipe', 'pipe'] });
    const parsed = JSON.parse(out);
    // Normalize id field: Go output may be `id` lower-case
    if (!parsed.id && parsed.ID) parsed.id = parsed.ID;
    return parsed;
  }

  // Create issues in deterministic order (counter mode => ft-1, ft-2, ...)
  // epicA, taskA child of epicA (detail payload)
  const epicA = bdCreate('Epic A', { type: 'epic', priority: 1 });
  const taskA = bdCreate('Task A', { type: 'task', parent: epicA.id, priority: 1 });
  // epicB, taskB child of epicB (ancestor question does not block child)
  const epicB = bdCreate('Epic B', { type: 'epic', priority: 1 });
  const taskB = bdCreate('Task B', { type: 'task', parent: epicB.id, priority: 1 });
  // standalone beads
  const qBlocked = bdCreate('Question Blocked', { type: 'task', priority: 1 });
  const rulingOnly = bdCreate('Ruling Only', { type: 'task', priority: 1 });
  const plain = bdCreate('Plain No Statements', { type: 'task', priority: 2 });
  const countsBead = bdCreate('Counts Bead', { type: 'task', priority: 1 });
  const supersededBead = bdCreate('Superseded Bead', { type: 'task', priority: 1 });
  const commentBead = bdCreate('Comment Bead', { type: 'task', priority: 2 });
  const answeredBead = bdCreate('Answered Bead', { type: 'task', priority: 1 });

  // Insert statements and comments through openRoot adapter (one code path for both engines)
  const root = await openRoot(dsn);
  const db = await root.forProject(prefix);

  const baseTime = Date.now();
  function isoOffset(ms) {
    return new Date(baseTime + ms).toISOString();
  }

  async function insertStatement({ id, kind, issueId, text, createdAt, filedBy = 'tester', status = 'active', scope = 'inherit', supersedesId = null, evidence = '', answeredBy = null }) {
    await db.exec(
      `INSERT INTO statements (id, kind, issue_id, text, created_at, filed_by, status, scope, supersedes_id, evidence, answered_by) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      [id, kind, issueId, text, createdAt, filedBy, status, scope, supersedesId, evidence, answeredBy]
    );
  }

  // Detail payload base: own ruling on taskA, epic ruling on epicA (inherit)
  await insertStatement({ id: 'R-1', kind: 'ruling', issueId: taskA.id, text: 'own ruling for taskA', createdAt: isoOffset(1000), scope: 'inherit' });
  await insertStatement({ id: 'R-2', kind: 'ruling', issueId: epicA.id, text: 'epic ruling for epicA', createdAt: isoOffset(2000), scope: 'inherit' });
  // No project ruling yet — will be inserted by test when needed for detail payload gate

  // Question on qBlocked (active) — should be excluded from ready
  await insertStatement({ id: 'Q-1', kind: 'question', issueId: qBlocked.id, text: 'active question blocks qBlocked', createdAt: isoOffset(3000) });
  // Question on epicB (active) — ancestor of taskB, should NOT block taskB
  await insertStatement({ id: 'Q-2', kind: 'question', issueId: epicB.id, text: 'epic question does not block child', createdAt: isoOffset(4000) });

  // Ruling only bead
  await insertStatement({ id: 'R-3', kind: 'ruling', issueId: rulingOnly.id, text: 'only ruling', createdAt: isoOffset(5000) });

  // Counts bead: two rulings + one question, no project yet so counts are 2 and 1
  await insertStatement({ id: 'R-4', kind: 'ruling', issueId: countsBead.id, text: 'counts ruling 1', createdAt: isoOffset(6000) });
  await insertStatement({ id: 'R-5', kind: 'ruling', issueId: countsBead.id, text: 'counts ruling 2', createdAt: isoOffset(7000) });
  await insertStatement({ id: 'Q-3', kind: 'question', issueId: countsBead.id, text: 'counts question', createdAt: isoOffset(8000) });

  // Superseded bead: R-6 superseded by R-7
  await insertStatement({ id: 'R-6', kind: 'ruling', issueId: supersededBead.id, text: 'old superseded', createdAt: isoOffset(9000) });
  await insertStatement({ id: 'R-7', kind: 'ruling', issueId: supersededBead.id, text: 'new terminal', createdAt: isoOffset(10000), supersedesId: 'R-6' });
  await db.exec(`UPDATE statements SET status = 'superseded' WHERE id = ?`, ['R-6']);

  // Answered bead: R-8 rules, Q-4 is a question answered by R-8 (status answered).
  // Q-4 must be absent from the active view but returned by listAnsweredQuestions.
  await insertStatement({ id: 'R-8', kind: 'ruling', issueId: answeredBead.id, text: 'ruling that answers Q-4', createdAt: isoOffset(10500), scope: 'inherit' });
  await insertStatement({ id: 'Q-4', kind: 'question', issueId: answeredBead.id, text: 'answered question', createdAt: isoOffset(10600), status: 'answered', answeredBy: 'R-8' });

  // Comment bead: one comment, no statements
  await db.exec(`INSERT INTO comments (id, issue_id, author, text, created_at) VALUES (?, ?, ?, ?, ?)`, ['c-1', commentBead.id, 'alice', 'legacy comment', isoOffset(11000)]);
  // Also add a second comment for legacy gate? one is enough
  await db.exec(`INSERT INTO comments (id, issue_id, author, text, created_at) VALUES (?, ?, ?, ?, ?)`, ['c-2', commentBead.id, 'bob', 'second comment', isoOffset(12000)]);

  await root.close();

  return {
    dsn,
    rawDsn,
    buildDir,
    workDir,
    bdBin,
    prefix,
    ids: {
      epicA: epicA.id,
      taskA: taskA.id,
      epicB: epicB.id,
      taskB: taskB.id,
      qBlocked: qBlocked.id,
      rulingOnly: rulingOnly.id,
      plain: plain.id,
      countsBead: countsBead.id,
      supersededBead: supersededBead.id,
      commentBead: commentBead.id,
      answeredBead: answeredBead.id,
    },
  };
}

export async function cleanupFixture(fixture) {
  if (!fixture) return;
  // Remove temp dirs
  try { fs.rmSync(fixture.buildDir, { recursive: true, force: true }); } catch {}
  try { fs.rmSync(fixture.workDir, { recursive: true, force: true }); } catch {}
  // For sqlite, remove db file
  const sqliteFile = sqliteFileFromDSN(fixture.dsn);
  if (sqliteFile) {
    try { fs.unlinkSync(sqliteFile); } catch {}
    // Also try rawDsn file if different (e.g., without search_path handling)
    const rawFile = sqliteFileFromDSN(fixture.rawDsn);
    if (rawFile && rawFile !== sqliteFile) {
      try { fs.unlinkSync(rawFile); } catch {}
    }
  } else if (fixture.dsn.startsWith('postgres')) {
    // For postgres, drop the throwaway schema created for this fixture.
    // The fixture's schema is the prefix (ft) plus maybe random? Our fixture uses fixed prefix ft,
    // but for test isolation we rely on caller to provide a unique DSN with unique search_path.
    // If caller used a throwaway schema via BD_TEST_POSTGRES_DSN, that schema will be dropped by caller.
    // Here we just drop the schema `ft` if it exists in that DSN's database.
    // Use openRoot to drop.
    try {
      const { openRoot: open } = await import('../server/db.js');
      const root = await open(fixture.dsn);
      // root driver is postgres, need to get underlying pool to drop schema
      // For postgres, we can exec DROP SCHEMA IF EXISTS
      // But root is PgRoot that owns rootPool; we need to drop the ft schema.
      // The schema name is fixture.prefix (ft) — but if fixture was postgres with custom schema, prefix may be that schema.
      // We'll attempt to drop.
      if (root.driver === 'postgres') {
        // root.all uses rootPool which has no search_path, so we can drop any schema
        try { await root.exec(`DROP SCHEMA IF EXISTS "${fixture.prefix}" CASCADE`); } catch {}
      }
      await root.close();
    } catch {}
  }
}
