import { describe, it, before, after } from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import os from 'node:os';
import path from 'node:path';
import { execSync } from 'node:child_process';
import { fileURLToPath } from 'node:url';
import { openRoot } from '../server/db.js';
import { getIssue, listIssues, readyIssues, listStatements, listComments, listLabels, listDependencies } from '../server/queries.js';
import { rowToIssue } from '../server/types.js';
import { createFixture, cleanupFixture } from './fixture.mjs';

// Helper to run the full gate suite against a DSN
async function runSuite(rawDsn, label) {
  const fixture = await createFixture(rawDsn);
  const root = await openRoot(fixture.dsn);
  const db = await root.forProject(fixture.prefix);
  const ids = fixture.ids;

  try {
    // ---- Parity: readyIssues vs bd ready --json ----
    {
      const readyNode = await readyIssues(db);
      const readyNodeIds = new Set(readyNode.map(i => i.id));
      const bdOut = execSync(`${fixture.bdBin} --db ${fixture.dsn} ready --json`, {
        cwd: fixture.workDir,
        encoding: 'utf8',
        stdio: ['pipe', 'pipe', 'pipe'],
      });
      const readyBd = JSON.parse(bdOut);
      const readyBdIds = new Set(readyBd.map(r => r.id));
      assert.deepStrictEqual(readyNodeIds, readyBdIds, `[${label}] Parity: readyIssues set must equal bd ready --json set`);
      // Fixture contains at least one bead with active question, at least one with only rulings, at least one with no statements
      // So ready list differs from unfiltered list
      const allIssues = await listIssues(db, {}, 0);
      const allIds = new Set(allIssues.map(i => i.id));
      assert.notDeepStrictEqual(allIds, readyNodeIds, `[${label}] ready should differ from unfiltered list`);
      assert.notDeepStrictEqual(allIds, readyBdIds, `[${label}] bd ready should differ from unfiltered`);
      // Ensure at least one blocked, one ruling-only, one plain present in all
      assert(allIds.has(ids.qBlocked), `[${label}] all should contain qBlocked`);
      assert(allIds.has(ids.rulingOnly), `[${label}] all should contain rulingOnly`);
      assert(allIds.has(ids.plain), `[${label}] all should contain plain`);
      assert(!readyNodeIds.has(ids.qBlocked), `[${label}] qBlocked with active question should not be ready`);
      assert(readyNodeIds.has(ids.rulingOnly), `[${label}] rulingOnly should be ready`);
      assert(readyNodeIds.has(ids.plain), `[${label}] plain should be ready`);
    }

    // ---- readyIssues drops bead carrying active question of its own ----
    {
      const ready = await readyIssues(db);
      const readyIds = new Set(ready.map(i => i.id));
      assert(!readyIds.has(ids.qBlocked), `[${label}] qBlocked should be excluded (own active question)`);
    }

    // ---- question on ancestor epic does not drop child ----
    {
      const ready = await readyIssues(db);
      const readyIds = new Set(ready.map(i => i.id));
      // epicB has Q-2 active, taskB is child
      assert(!readyIds.has(ids.epicB), `[${label}] epicB with own question should be excluded`);
      assert(readyIds.has(ids.taskB), `[${label}] taskB child of epicB should NOT be excluded by ancestor question`);
    }

    // ---- project-scoped question drops nothing ----
    {
      const readyBefore = await readyIssues(db);
      const beforeIds = new Set(readyBefore.map(i => i.id));
      // Insert project-scoped question (issue_id NULL)
      const now = new Date().toISOString();
      await db.exec(
        `INSERT INTO statements (id, kind, issue_id, text, created_at, filed_by, status, scope, evidence) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        ['Q-PROJ', 'question', null, 'project scoped question', now, 'tester', 'active', 'inherit', '']
      );
      const readyAfter = await readyIssues(db);
      const afterIds = new Set(readyAfter.map(i => i.id));
      assert.deepStrictEqual(beforeIds, afterIds, `[${label}] project-scoped question should not change ready`);
      // Also verify parity holds with bd (both should ignore project question)
      const bdOut = execSync(`${fixture.bdBin} --db ${fixture.dsn} ready --json`, {
        cwd: fixture.workDir,
        encoding: 'utf8',
      });
      const readyBd = JSON.parse(bdOut);
      const bdIds = new Set(readyBd.map(r => r.id));
      assert.deepStrictEqual(afterIds, bdIds, `[${label}] parity after project question`);
      // cleanup
      await db.exec(`DELETE FROM statements WHERE id = ?`, ['Q-PROJ']);
    }

    // ---- Answering a question returns bead to readyIssues ----
    {
      const readyBefore = await readyIssues(db);
      const beforeIds = new Set(readyBefore.map(i => i.id));
      assert(!beforeIds.has(ids.qBlocked), `[${label}] before answering, qBlocked not ready`);
      // Flip Q-1 to answered
      await db.exec(`UPDATE statements SET status = 'answered' WHERE id = ?`, ['Q-1']);
      const readyAfter = await readyIssues(db);
      const afterIds = new Set(readyAfter.map(i => i.id));
      assert(afterIds.has(ids.qBlocked), `[${label}] after answering, qBlocked should be ready`);
      // Parity after answer
      const bdOut = execSync(`${fixture.bdBin} --db ${fixture.dsn} ready --json`, { cwd: fixture.workDir, encoding: 'utf8' });
      const readyBd = JSON.parse(bdOut);
      const bdIds = new Set(readyBd.map(r => r.id));
      assert.deepStrictEqual(afterIds, bdIds, `[${label}] parity after answering`);
      // restore
      await db.exec(`UPDATE statements SET status = 'active' WHERE id = ?`, ['Q-1']);
      const readyRestored = await readyIssues(db);
      assert(!new Set(readyRestored.map(i => i.id)).has(ids.qBlocked), `[${label}] restored qBlocked not ready again`);
    }

    // ---- Superseded ruling appears in no payload ----
    {
      // supersededBead has R-6 superseded by R-7
      const stmts = await listStatements(db, ids.supersededBead);
      const stmtIds = stmts.map(s => s.statement_id);
      assert(!stmtIds.includes('R-6'), `[${label}] superseded R-6 should not appear in statements`);
      assert(stmtIds.includes('R-7'), `[${label}] terminal R-7 should appear`);
      const issue = await getIssue(db, ids.supersededBead);
      assert.equal(issue.ruling_count, 1, `[${label}] ruling_count should be 1 (only terminal) before project`);
      // Also verify via direct view count
      const viewRows = await db.all(`SELECT * FROM resolved_statements WHERE issue_id = ? AND kind = 'ruling'`, [ids.supersededBead]);
      assert.equal(viewRows.length, 1, `[${label}] view should have 1 ruling for supersededBead`);
      assert.equal(viewRows[0].statement_id, 'R-7');
    }

    // ---- Counts: on bead with two in-force rulings and one active question slim row reports ruling_count 2 and open_question_count 1; on bead with no statements both are 0 ----
    {
      const countsIssue = await getIssue(db, ids.countsBead);
      assert.equal(countsIssue.ruling_count, 2, `[${label}] countsBead ruling_count should be 2`);
      assert.equal(countsIssue.open_question_count, 1, `[${label}] countsBead open_question_count should be 1`);
      // Verify ordering of statements for countsBead: rulings first sorted by created_at, then question
      const countsStmts = await listStatements(db, ids.countsBead);
      assert.deepStrictEqual(countsStmts.map(s => s.statement_id), ['R-4','R-5','Q-3'], `[${label}] countsBead statements should be rulings then question ordered by created_at`);
      // Also via listIssues slim
      const all = await listIssues(db, {}, 0);
      const countsViaList = all.find(i => i.id === ids.countsBead);
      assert(countsViaList, `[${label}] countsBead found via listIssues`);
      assert.equal(countsViaList.ruling_count, 2);
      assert.equal(countsViaList.open_question_count, 1);

      const plainIssue = await getIssue(db, ids.plain);
      assert.equal(plainIssue.ruling_count, 0, `[${label}] plain ruling_count should be 0`);
      assert.equal(plainIssue.open_question_count, 0, `[${label}] plain open_question_count should be 0`);
      const plainViaList = all.find(i => i.id === ids.plain);
      assert.equal(plainViaList.ruling_count, 0);
      assert.equal(plainViaList.open_question_count, 0);

      // Also verify taskA before project has ruling_count 2 (own + epic)
      const taskAIssue = await getIssue(db, ids.taskA);
      assert.equal(taskAIssue.ruling_count, 2, `[${label}] taskA ruling_count before project should be 2 (own+epic)`);
      assert.equal(taskAIssue.open_question_count, 0, `[${label}] taskA open_question_count should be 0`);
    }

    // ---- Byte-compatibility: for bead with no statements, JSON produced by rowToIssue is identical to pre-change output except for two new keys ----
    {
      const plainIssue = await getIssue(db, ids.plain);
      // Old keys from pre-change types.js (without two new keys) — captured from git show HEAD:web/server/types.js
      const oldKeys = [
        'id','content_hash','title','description','design','acceptance_criteria','notes','status','priority','issue_type','assignee','estimated_minutes','created_at','created_by','owner','updated_at','started_at','closed_at','closed_by_session','external_ref','spec_id','metadata','source_repo','source_system','close_reason','sender','ephemeral','pinned','is_template','wisp_type','mol_type','role_type','event_kind','actor','target','payload','due_at','defer_until','parent_id','parent_title','total_children','closed_children','blocked_by_count','blocked_by_id','blocked_by_title','comment_count'
      ];
      const newKeys = Object.keys(plainIssue);
      for (const k of oldKeys) {
        assert(k in plainIssue, `[${label}] old key ${k} should still exist`);
      }
      const extra = newKeys.filter(k => !oldKeys.includes(k));
      assert.deepStrictEqual(new Set(extra), new Set(['open_question_count','ruling_count']), `[${label}] exactly two new keys should be present`);
      assert.equal(newKeys.length, oldKeys.length + 2, `[${label}] total key count should be old +2`);
      assert.equal(typeof plainIssue.open_question_count, 'number');
      assert.equal(typeof plainIssue.ruling_count, 'number');
      assert.equal(typeof plainIssue.comment_count, 'number');
      assert.equal(typeof plainIssue.total_children, 'number');
      assert.equal(typeof plainIssue.id, 'string');
      assert.equal(typeof plainIssue.title, 'string');
      assert.equal(plainIssue.open_question_count, 0);
      assert.equal(plainIssue.ruling_count, 0);
      // Verify representative old keys keep expected values for plain bead (byte-compat)
      assert.equal(plainIssue.comment_count, 0, `[${label}] plain comment_count should be 0 (byte-compat)`);
      assert.equal(plainIssue.total_children, 0);
      assert.equal(plainIssue.blocked_by_count, 0);
      // Test that rowToIssue defaults for raw insert without enriched columns returns 0 for new keys (not throw)
      const raw = { id: 'x', title: 't', status: 'open', priority: 1, issue_type: 'task', created_at: new Date().toISOString(), updated_at: new Date().toISOString() };
      const mapped = rowToIssue(raw);
      assert.equal(mapped.open_question_count, 0, `[${label}] rowToIssue should default open_question_count to 0 for raw row`);
      assert.equal(mapped.ruling_count, 0, `[${label}] rowToIssue should default ruling_count to 0`);
      // Also verify that rowToIssue on a row that never went through enriched select doesn't throw and returns numbers for new keys
      const minimal = { id: ids.plain, title: 'x', status: 'open', priority: 1, issue_type: 'task' };
      const minimalMapped = rowToIssue(minimal);
      assert.equal(typeof minimalMapped.open_question_count, 'number');
      assert.equal(typeof minimalMapped.ruling_count, 'number');
    }

    // ---- Legacy: detail response for bead with comments and no statements carries empty statements array and its comments array unchanged ----
    {
      const stmts = await listStatements(db, ids.commentBead);
      assert(Array.isArray(stmts), `[${label}] statements should be array`);
      assert.equal(stmts.length, 0, `[${label}] commentBead should have empty statements`);
      const comments = await listComments(db, ids.commentBead);
      assert.equal(comments.length, 2, `[${label}] commentBead should have 2 comments`);
      const texts = comments.map(c => c.text).sort();
      assert.deepStrictEqual(texts, ['legacy comment','second comment']);
      // Ensure comments unchanged after our changes (compare to direct DB query)
      const direct = await db.all(`SELECT * FROM comments WHERE issue_id = ? ORDER BY created_at`, [ids.commentBead]);
      assert.equal(direct.length, 2);
    }

    // ---- Detail payload from getIssue plus new statement query carries, for task under epic ----
    {
      // Insert project-scoped ruling for this test
      const now = new Date(Date.now() + 20000).toISOString();
      await db.exec(
        `INSERT INTO statements (id, kind, issue_id, text, created_at, filed_by, status, scope, evidence) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
        ['R-PROJ', 'ruling', null, 'project ruling', now, 'tester', 'active', 'inherit', '']
      );
      const stmts = await listStatements(db, ids.taskA);
      const byId = new Map(stmts.map(s => [s.statement_id, s]));
      // Should have own ruling R-1, epic ruling R-2, and project R-PROJ
      assert(byId.has('R-1'), `[${label}] taskA should have own ruling R-1`);
      assert(byId.has('R-2'), `[${label}] taskA should have epic ruling R-2`);
      assert(byId.has('R-PROJ'), `[${label}] taskA should have project ruling R-PROJ`);
      // Check origin for own
      const own = byId.get('R-1');
      assert.equal(own.origin_kind, 'self', `[${label}] own ruling origin_kind should be self`);
      assert.equal(own.origin_issue_id, null, `[${label}] own ruling origin_issue_id should be null`);
      // Check epic
      const epicStmt = byId.get('R-2');
      assert.equal(epicStmt.origin_kind, 'epic', `[${label}] epic ruling origin_kind should be epic`);
      assert.equal(epicStmt.origin_issue_id, ids.epicA, `[${label}] epic ruling origin_issue_id should be epicA id`);
      // Check project
      const proj = byId.get('R-PROJ');
      assert.equal(proj.origin_kind, 'project', `[${label}] project ruling origin_kind should be project`);
      assert.equal(proj.origin_issue_id, null, `[${label}] project ruling origin_issue_id should be null`);
      // Check ordering: rulings then questions then findings, within each kind by created_at ASC
      // For taskA, all are rulings, so order by created_at asc
      // Our R-1 (1000), R-2 (2000), R-PROJ (now ~20000) => should be in that order
      assert.deepStrictEqual(stmts.map(s => s.statement_id), ['R-1','R-2','R-PROJ'], `[${label}] statements order should be by created_at ASC within rulings`);
      // Also verify that every statement has required keys
      for (const s of stmts) {
        assert('statement_id' in s);
        assert('kind' in s);
        assert('text' in s);
        assert('created_at' in s);
        assert('filed_by' in s);
        assert('evidence' in s);
        assert('origin_kind' in s);
        assert('origin_issue_id' in s);
        assert(s.created_at !== null);
      }
      // Check ruling_count for taskA now includes project (should be 3)
      const issue = await getIssue(db, ids.taskA);
      assert.equal(issue.ruling_count, 3, `[${label}] taskA ruling_count should be 3 after project`);
      // Cleanup
      await db.exec(`DELETE FROM statements WHERE id = ?`, ['R-PROJ']);
      const issueAfter = await getIssue(db, ids.taskA);
      assert.equal(issueAfter.ruling_count, 2, `[${label}] after cleanup, taskA ruling_count back to 2`);
    }

    // ---- No route added or removed: router registers exactly paths it registers today ----
    {
      const routesPath = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../server/routes/issues.js');
      const src = fs.readFileSync(routesPath, 'utf8');
      const expectedRoutes = [
        "r.get('/',",
        "r.get('/ready',",
        "r.get('/:id',",
        "r.post('/:id/comments',",
        "r.delete('/:id/comments/:commentId',",
        "r.post('/:id/labels',",
        "r.delete('/:id/labels/:label',",
      ];
      for (const route of expectedRoutes) {
        assert(src.includes(route), `[${label}] expected route ${route} missing`);
      }
      assert(!src.includes("'/statements'"), `[${label}] should not contain extra /statements route`);
      assert(!src.includes('"/statements"'), `[${label}] should not contain extra statements route`);
      // Ensure no extra route was added: count occurrences of r.get/r.post/r.delete for issues router
      const routeMatches = src.match(/r\.(get|post|delete)\('/g) || [];
      assert.equal(routeMatches.length, expectedRoutes.length, `[${label}] router should register exactly ${expectedRoutes.length} routes, got ${routeMatches.length}`);
      // Byte-identical check: compare current handler blocks for POST /:id/comments and label routes against HEAD
      let headSrc = '';
      try {
        const repoRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../..');
        headSrc = execSync('git show HEAD:web/server/routes/issues.js', { cwd: repoRoot, encoding: 'utf8' });
      } catch {}
      if (headSrc) {
        // Extract handlers for those routes from both src and headSrc and compare
        function extractHandler(source, routePattern) {
          const idx = source.indexOf(routePattern);
          if (idx < 0) return null;
          const nextIdx = source.indexOf('r.', idx + routePattern.length);
          return nextIdx > 0 ? source.slice(idx, nextIdx) : source.slice(idx);
        }
        for (const pat of ["r.post('/:id/comments'", "r.delete('/:id/comments/:commentId'", "r.post('/:id/labels'", "r.delete('/:id/labels/:label'"]) {
          const cur = extractHandler(src, pat);
          const head = extractHandler(headSrc, pat);
          assert(cur && head, `[${label}] handler ${pat} not found`);
          assert.equal(cur, head, `[${label}] handler ${pat} should be byte-identical to HEAD`);
        }
      } else {
        // Fallback to simple string checks if git not available
        assert(src.includes("if (!text) return c.json({ error: 'text is required' }, 400);"), `[${label}] POST comments handler should be byte-identical`);
        assert(src.includes("if (!label) return c.json({ error: 'label is required' }, 400);"), `[${label}] POST labels handler should be byte-identical`);
        assert(src.includes("await deleteComment(db, c.req.param('commentId'));"), `[${label}] DELETE comment handler byte-identical`);
        assert(src.includes("await removeLabel(db, c.req.param('id'), c.req.param('label'));"), `[${label}] DELETE label handler byte-identical`);
      }
    }

  } finally {
    await root.close();
    await cleanupFixture(fixture);
  }
}

// Helpers to generate DSNs
function makeSqliteDsn() {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), 'bd-sqlite-test-'));
  const file = path.join(dir, 'test.db');
  // Return sqlite path as DSN (also works as sqlite://)
  return file;
}

function makePostgresDsn(base) {
  const schema = `test_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`;
  const sep = base.includes('?') ? '&' : '?';
  return `${base}${sep}search_path=${schema}`;
}

describe('statements web API', () => {
  it('sqlite harness covers all gates', async () => {
    const dsn = makeSqliteDsn();
    await runSuite(dsn, 'sqlite');
    // Cleanup dir containing file
    try { fs.rmSync(path.dirname(dsn), { recursive: true, force: true }); } catch {}
  });

  it('postgres harness (if BD_TEST_POSTGRES_DSN set)', async () => {
    const base = process.env.BD_TEST_POSTGRES_DSN;
    if (!base) {
      console.log('Skipped Postgres — BD_TEST_POSTGRES_DSN not set');
      return;
    }
    const dsn = makePostgresDsn(base);
    await runSuite(dsn, 'postgres');
    // Cleanup schema: createFixture's cleanup will drop ft? But our dsn uses unique schema, need to drop it.
    // We'll drop manually via openRoot
    try {
      const root = await openRoot(dsn);
      const u = new URL(dsn);
      const schema = u.searchParams.get('search_path');
      if (root.driver === 'postgres' && schema) {
        try { await root.exec(`DROP SCHEMA IF EXISTS "${schema}" CASCADE`); } catch {}
      }
      await root.close();
    } catch {}
  });
});
