// SSE: per-project change stream. Server polls a cheap "fingerprint" query
// every WATCH_MS and broadcasts a `tick` event when it changes. Clients open
// an EventSource and invalidate their cached queries on tick.
//
// Why poll-and-broadcast: bd is mutated by the CLI, agents, and the web
// server. Postgres LISTEN/NOTIFY would catch all of them but only on pg, and
// requires triggers. A 1.5s SQL fingerprint (max(updated_at), count(*)) is
// cheap enough for the polling rate we want, works on both engines, and
// scales: N connected browsers per project share one watcher.
//
// Fingerprint covers issues, dependencies, comments, and labels — anything
// the board / list / detail page renders.

const WATCH_MS = 1500;        // server poll cadence
const HEARTBEAT_MS = 25000;   // SSE ping to keep proxies from killing the stream

// project prefix -> { db, hash, timer, subs:Set<{enqueue,close}> }
const watchers = new Map();

// stamp normalises whatever the driver returns for a timestamp column
// (string from sqlite, Date from pg) into a stable string for hashing.
const stamp = (v) => {
  if (v == null) return '';
  if (v instanceof Date) return v.toISOString();
  return String(v);
};

async function fingerprint(db) {
  // Cheap "did anything change?" probe: max-of-mutable-timestamp + row count
  // for each table the UI cares about. Portable across sqlite/postgres.
  const i = await db.one(`SELECT MAX(updated_at) AS m, COUNT(*) AS c FROM issues`);
  const d = await db.one(`SELECT MAX(created_at) AS m, COUNT(*) AS c FROM dependencies`);
  const cm = await db.one(`SELECT MAX(created_at) AS m, COUNT(*) AS c FROM comments`);
  const l = await db.one(`SELECT COUNT(*) AS c FROM labels`);
  return [
    `i:${stamp(i?.m)}/${i?.c ?? 0}`,
    `d:${stamp(d?.m)}/${d?.c ?? 0}`,
    `c:${stamp(cm?.m)}/${cm?.c ?? 0}`,
    `l:${l?.c ?? 0}`,
  ].join('|');
}

function startWatcher(prefix, db) {
  const w = { db, hash: '', subs: new Set(), timer: null };
  watchers.set(prefix, w);
  const tick = async () => {
    if (w.subs.size === 0) {
      // No subscribers — stop polling and let the watcher idle out.
      clearInterval(w.timer);
      watchers.delete(prefix);
      return;
    }
    let next;
    try { next = await fingerprint(db); } catch (err) {
      // Don't kill the watcher on transient db errors.
      if (process.env.DEBUG) console.error('stream fingerprint failed:', err.message);
      return;
    }
    if (next !== w.hash) {
      w.hash = next;
      const payload = `event: tick\ndata: ${next}\n\n`;
      for (const sub of w.subs) {
        try { sub.enqueue(payload); } catch {}
      }
    }
  };
  w.timer = setInterval(tick, WATCH_MS);
  // prime hash so the first real change emits, but the initial open doesn't
  fingerprint(db).then((h) => { w.hash = h; }).catch(() => {});
  return w;
}

export function streamHandler(c) {
  const prefix = c.get('project');
  const db = c.get('db');
  if (!prefix || !db) return c.json({ error: 'no project' }, 400);

  const w = watchers.get(prefix) || startWatcher(prefix, db);

  const stream = new ReadableStream({
    start(controller) {
      const enc = new TextEncoder();
      const sub = {
        enqueue: (s) => controller.enqueue(enc.encode(s)),
        close: () => { try { controller.close(); } catch {} },
      };
      w.subs.add(sub);
      // Initial hello + heartbeat loop.
      sub.enqueue(`event: hello\ndata: ${w.hash}\n\n`);
      const hb = setInterval(() => {
        try { sub.enqueue(`: ping\n\n`); } catch {}
      }, HEARTBEAT_MS);
      // Cleanup when the client disconnects.
      const onClose = () => {
        clearInterval(hb);
        w.subs.delete(sub);
        sub.close();
      };
      c.req.raw.signal?.addEventListener('abort', onClose);
    },
  });

  return new Response(stream, {
    headers: {
      'Content-Type': 'text/event-stream; charset=utf-8',
      'Cache-Control': 'no-cache, no-transform',
      'Connection': 'keep-alive',
      'X-Accel-Buffering': 'no', // disable nginx buffering
    },
  });
}
