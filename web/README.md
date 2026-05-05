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

## Docker

The repo ships a multi-stage [Dockerfile](Dockerfile). It builds the client,
keeps only production deps, and runs `node server/index.js` as PID 1.

For the **full stack** (Postgres + bd-web together), use the root-level
[docker-compose.yml](../docker-compose.yml):

```sh
# from repo root
cp .env.example .env             # set POSTGRES_PASSWORD and BD_PREFIX
docker compose --profile full up -d --build
# → http://127.0.0.1:3333
```

For an existing database, just run the bd-web container directly:

```sh
docker run --rm -p 3333:3333 \
  -e BD_DB="postgres://bd@host.docker.internal:5432/tracker?sslmode=disable&search_path=myproject" \
  -e BD_DB_PASSWORD=mypassword \
  -e HOST=0.0.0.0 \
  $(docker build -q ./web)
```

To enable auth, mount a users JSON file and point at it:

```sh
docker run --rm -p 3333:3333 \
  -v $(pwd)/users.json:/etc/bd/users.json:ro \
  -e BD_DB=... -e BD_DB_PASSWORD=... \
  -e BD_WEB_AUTH_FILE=/etc/bd/users.json \
  $(docker build -q ./web)
```

`users.json` shape:

```json
{
  "users": [
    { "username": "alice", "password": "secret", "role": "Developer" },
    { "username": "bob",   "password": "guest",  "role": "Viewer" }
  ]
}
```

## CLI setup (Go)

The companion CLI `bd` lives in this repo's [parent directory](..). Install
and initialise from the project README:

```sh
go install github.com/rsktash/beads/cmd/bd@latest
bd init --prefix myproject \
  --db "postgres://bd@127.0.0.1:5432/tracker?sslmode=disable"
export BD_DB_PASSWORD=...
bd create "first issue" -p 0
bd-web start                 # uses the same .bd/config
```
