-- Migration 0007 — path to area map (Postgres). Identical to the SQLite half
-- apart from BIGSERIAL PRIMARY KEY and ON CONFLICT (kind, name) DO NOTHING.
CREATE TABLE IF NOT EXISTS path_area_map (
    id BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL CHECK (kind IN ('workspace','concern')),
    name TEXT NOT NULL,
    root TEXT NOT NULL DEFAULT '',
    paths TEXT NOT NULL DEFAULT '',
    UNIQUE (kind, name)
);
CREATE INDEX IF NOT EXISTS idx_path_area_map_kind ON path_area_map(kind);

INSERT INTO path_area_map (kind, name, root, paths) VALUES
  ('workspace','server','server/',''),
  ('workspace','web-app','web-app/',''),
  ('workspace','shared','shared/',''),
  ('workspace','tasnif-sync','tasnif-sync/',''),
  ('workspace','db','db/',''),
  ('workspace','mobile/cm-app','mobile/cm-app/',''),
  ('workspace','mobile/owner-app','mobile/owner-app/','')
ON CONFLICT (kind, name) DO NOTHING;

INSERT INTO path_area_map (kind, name, root, paths) VALUES
  ('concern','authority','','server/src/auth.ts,server/src/auth-better.ts,server/src/context.ts,server/src/routes/admin*.ts,server/src/routes/grants.ts,shared/permissions.ts,web-app/src/sessionRole.ts,web-app/src/actingBusiness.ts,web-app/src/actingRefusalVerify.ts'),
  ('concern','sync','','tasnif-sync/**,server/src/routes/sync.ts,server/src/routes/package-info.ts,server/src/routes/imports.ts'),
  ('concern','tokens','','server/src/routes/auth.ts,server/src/routes/price-requests.ts,server/src/routes/handover.ts,server/src/otp-eskiz.ts'),
  ('concern','schema','','db/migrations/**,db/*.sql,server/src/migrations.ts,server/src/db.ts,server/src/kysely.ts'),
  ('concern','catalog','','server/src/routes/catalog.ts,server/src/routes/mxik.ts,server/src/routes/items.ts,tasnif-sync/**,classify/**,db/bc_seed*.csv'),
  ('concern','frontend','','web-app/src/**,shared/design-tokens.json,shared/icons/**'),
  ('concern','all','','**')
ON CONFLICT (kind, name) DO NOTHING;
