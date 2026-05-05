# @rsktash/bd-web

Web UI for [bd](https://github.com/rsktash/beads). Same workspace, runs
locally or on a server, no Go runtime needed.

```sh
npm install -g @rsktash/bd-web
cd /path/to/your/bd-workspace   # contains .bd/config
bd-web start
# → http://127.0.0.1:3333
```

## Stack

- Server: Hono + `@hono/node-server`, `pg`, `better-sqlite3`
- Client: Vite + React 19 + TanStack Router + TanStack Query + Tailwind v4

## Environment

| var | purpose |
|---|---|
| `BD_DB`             | override DSN (otherwise read from `.bd/config`) |
| `BD_DB_PASSWORD`    | postgres password (or put it in `.bd/.env`) |
| `BD_WEB_AUTH_FILE`  | enable optional auth — JSON `{users:[{username,password,role}]}` |
| `HOST`, `PORT`      | bind (default `127.0.0.1:3333`) |
| `DEBUG`             | extra logging |

## Development

```sh
cd web/
npm install
npm run dev   # vite at 5173 + hono at 3333; vite proxies /api -> hono
```

`npm run build` writes to `web/dist/`. `npm start` runs the Hono server, which
serves both `/api/*` and the built client at `/`.

## Routes

- `/` — board (kanban by status; ready issues at top)
- `/list` — table view with filters
- `/issue/<id>` — detail with markdown sections, deps, comments
- `/projects` — postgres-only; lists schemas containing a `config` table

The header always shows the project prefix, current user, and backend driver.
