// Database adapter for the web server. Pool-per-schema design so a single
// bd-web instance can serve every project on a shared postgres without
// SET-search-path races between concurrent requests.
//
// Workflow:
//   const root = await openRoot(dsn);                 // for `/api/projects` etc.
//   const project = await root.forProject('yuklar');  // for project-scoped queries

import Database from 'better-sqlite3';
import pg from 'pg';
import { URL } from 'node:url';
import { detectDriver, sqlitePath } from './dsn.js';

const { Pool } = pg;

export async function openRoot(dsn) {
  const driver = detectDriver(dsn);
  if (driver === 'sqlite') return new SqliteAdapter(dsn);
  if (driver === 'postgres') return await PgRoot.create(dsn);
  throw new Error(`unsupported driver: ${driver}`);
}

// SqliteAdapter has only one schema by definition. forProject() returns the
// same adapter regardless of name — projects don't apply.
class SqliteAdapter {
  constructor(dsn) {
    this.driver = 'sqlite';
    this.db = new Database(sqlitePath(dsn), { readonly: false, fileMustExist: true });
    this.db.pragma('foreign_keys = ON');
  }
  all(sql, params = [])  { return this.db.prepare(sql).all(...params); }
  one(sql, params = [])  { return this.db.prepare(sql).get(...params); }
  exec(sql, params = []) { return this.db.prepare(sql).run(...params); }
  async close() { this.db.close(); }
  async forProject(_prefix) { return this; } // single-project file
  async listProjectNames() { return [];      } // no schemas to enumerate
}

// PgRoot owns one Pool with no search_path (queries against
// information_schema.schemata) and a Map<prefix, PgProject> of schema-bound
// pools created lazily.
class PgRoot {
  static async create(dsn) {
    const a = new PgRoot();
    a.driver = 'postgres';
    a.dsn = dsn;
    // Strip any leftover search_path from the boot DSN — we manage it
    // per-project below.
    const u = new URL(dsn);
    u.searchParams.delete('search_path');
    a.rootPool = new Pool({ connectionString: u.toString() });
    a.projects = new Map();
    return a;
  }

  // root pool queries — used by listProjectNames + auth/config endpoints.
  async all(sql, params = []) { return (await this.rootPool.query(rebind(sql), params)).rows; }
  async one(sql, params = []) { return (await this.rootPool.query(rebind(sql), params)).rows[0]; }
  async exec(sql, params = []) { return await this.rootPool.query(rebind(sql), params); }
  async close() {
    await this.rootPool.end();
    for (const p of this.projects.values()) await p.pool.end();
  }

  async forProject(prefix) {
    if (!prefix || !/^[a-z0-9_-]+$/.test(prefix)) {
      throw new Error(`invalid project prefix: ${prefix}`);
    }
    if (this.projects.has(prefix)) return this.projects.get(prefix);

    const u = new URL(this.dsn);
    u.searchParams.delete('search_path');
    const existing = u.searchParams.get('options') || '';
    const suffix = `-c search_path=${prefix}`;
    u.searchParams.set('options', existing ? `${existing} ${suffix}` : suffix);
    const pool = new Pool({ connectionString: u.toString() });
    const proj = new PgProject(pool);
    this.projects.set(prefix, proj);
    return proj;
  }

  async listProjectNames() {
    const rows = await this.all(`
      SELECT s.schema_name FROM information_schema.schemata s
      WHERE s.schema_name NOT IN ('pg_catalog','information_schema','pg_toast','public')
        AND s.schema_name NOT LIKE 'pg_%'
        AND EXISTS (
          SELECT 1 FROM information_schema.tables t
          WHERE t.table_schema = s.schema_name AND t.table_name = 'config'
        )
      ORDER BY s.schema_name`);
    return rows.map((r) => r.schema_name);
  }
}

// PgProject is the schema-bound view a route handler queries.
class PgProject {
  constructor(pool) {
    this.driver = 'postgres';
    this.pool = pool;
  }
  async all(sql, params = []) { return (await this.pool.query(rebind(sql), params)).rows; }
  async one(sql, params = []) { return (await this.pool.query(rebind(sql), params)).rows[0]; }
  async exec(sql, params = []) { return await this.pool.query(rebind(sql), params); }
  async forProject(_p) { return this; }
}

function rebind(sql) {
  let n = 0;
  return sql.replace(/\?/g, () => `$${++n}`);
}
