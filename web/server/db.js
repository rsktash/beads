// Database adapter for the web server. Wraps sqlite (better-sqlite3, sync) and
// postgres (pg, async) behind a small uniform API used by routes/. The schema
// is owned by the Go CLI — we never CREATE/ALTER here, only SELECT and the
// occasional INSERT/UPDATE.

import Database from 'better-sqlite3';
import pg from 'pg';
import { URL } from 'node:url';
import { detectDriver, sqlitePath } from './dsn.js';

const { Pool } = pg;

export async function open(dsn) {
  const driver = detectDriver(dsn);
  if (driver === 'sqlite') return new SqliteAdapter(dsn);
  if (driver === 'postgres') return await PgAdapter.create(dsn);
  throw new Error(`unsupported driver: ${driver}`);
}

class SqliteAdapter {
  constructor(dsn) {
    this.driver = 'sqlite';
    this.db = new Database(sqlitePath(dsn), { readonly: false, fileMustExist: true });
    this.db.pragma('foreign_keys = ON');
  }

  // queries below take parameter arrays (`?`-style for both engines) and
  // route to the right placeholder format.

  all(sql, params = []) {
    return this.db.prepare(sql).all(...params);
  }

  one(sql, params = []) {
    return this.db.prepare(sql).get(...params);
  }

  exec(sql, params = []) {
    return this.db.prepare(sql).run(...params);
  }

  async close() { this.db.close(); }
}

class PgAdapter {
  static async create(dsn) {
    const a = new PgAdapter();
    a.driver = 'postgres';

    // node-postgres does NOT recognise `?search_path=...` URL params (unlike
    // Go's lib/pq). To set it for every pooled connection without races, fold
    // it into the libpq `options` startup parameter, which pg DOES forward.
    const u = new URL(dsn);
    a.searchPath = u.searchParams.get('search_path') || '';
    if (a.searchPath) {
      u.searchParams.delete('search_path');
      const existing = u.searchParams.get('options') || '';
      const suffix = `-c search_path=${a.searchPath}`;
      u.searchParams.set('options', existing ? `${existing} ${suffix}` : suffix);
    }
    a.pool = new Pool({ connectionString: u.toString() });
    return a;
  }

  // Translate `?` placeholders to `$N` for pg.
  static rebind(sql) {
    let n = 0;
    return sql.replace(/\?/g, () => `$${++n}`);
  }

  async all(sql, params = []) {
    const r = await this.pool.query(PgAdapter.rebind(sql), params);
    return r.rows;
  }

  async one(sql, params = []) {
    const r = await this.pool.query(PgAdapter.rebind(sql), params);
    return r.rows[0];
  }

  async exec(sql, params = []) {
    return await this.pool.query(PgAdapter.rebind(sql), params);
  }

  async close() { await this.pool.end(); }
}
