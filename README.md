# beads (`bd`)

Graph-based issue tracker for AI agents — same shape as
[gastownhall/beads](https://github.com/gastownhall/beads), but with
**SQLite or Postgres** instead of Dolt as the backing store. Single static Go
binary, sqlc-typed queries.

## Why

Agents lose context when their plan lives in a markdown file. Beads gives them
a structured, dependency-aware graph so they can ask *"what can I work on
next?"* without re-reading the world.

## Install

```sh
go install github.com/rsktash/beads/cmd/bd@latest
```

Or from a clone:

```sh
git clone https://github.com/rsktash/beads.git
cd beads
go build -o bd ./cmd/bd
```

> SQLite uses `mattn/go-sqlite3` (CGO). Default `go build` works on macOS/Linux.

## Quickstart

```sh
bd init --prefix yuklar                 # creates .beads/beads.db, ids look like yuklar-a1b2
bd create "Set up CI" -p 0              # priorities 0..4 (0=highest)
bd create "Add login endpoint"
ID=$(bd --json create "Wire login UI" -p 1 | jq -r .id)
bd dep add $ID bd-xxxx                  # `bd dep add <issue> <depends-on>`
bd ready                                # tasks with no open blockers
bd show $ID                             # bead + labels, deps, comments, history
bd update $ID --claim                   # set in_progress + assign you
bd close $ID -r "shipped"
```

Postgres instead of SQLite:

```sh
bd init --db postgres://user:pw@localhost/beads?sslmode=disable
# or
export BEADS_DB=postgres://user:pw@localhost/beads?sslmode=disable
```

Bead types beyond `task`:

```sh
bd create "Hello team"          -t message --sender alice --ephemeral
bd create "Closed v1"           -t epic   -p 0
bd create "User signup"         -t feature
bd create "Reproduce 500"       -t bug
bd create "Reviewer subagent"   -t role   --owner ops
bd create "user.signed_in"      -t event
```

Defer/due:

```sh
bd create "kickoff retro" --due 2026-05-20T15:00:00Z
bd create "later thing"   --defer 2026-06-01T00:00:00Z   # excluded from `ready` until then
```

## File-based updates

`bd edit <id>` opens the bead in `$EDITOR` as YAML and applies the diff on
save (title, description, design, acceptance, notes, type, status, priority,
assignee, owner, labels, due, defer, ephemeral). Non-listed fields are
read-only.

`bd batch [-f file]` runs a line-oriented script of ops. Reads stdin by
default. Lines starting with `#` are comments. Grammar:

```
# bootstrap
create epic 0 "Auth rewrite"  assignee=alice
create task 1 "Login endpoint"
update bd-XXXX priority=0 status=in_progress
label add bd-XXXX infra
comment bd-XXXX "owner is alice"
dep add bd-YYYY bd-XXXX blocks
close bd-ZZZZ wontfix later
```

Keys for `create`/`update`: `title, desc, design, accept, notes, status,
priority, type, assignee, owner, due, defer, ephemeral`. Strings with spaces
go in `"double quotes"`. v0.1 runs sequentially: first error aborts; previously
applied ops in the same batch are NOT rolled back. Atomic transactional mode
is on the roadmap.

## Migrate from upstream Dolt-backed beads

`bd migrate` reads from a running upstream Dolt sql-server (Dolt speaks the
MySQL wire protocol) and copies issues, dependencies, labels, comments, and
events into the active store.

```sh
# 1) start dolt sql-server on the upstream beads repo
cd /path/to/upstream/.beads/embeddeddolt
dolt sql-server -P 3306 -u root --no-auto-commit

# 2) point bd at it
bd migrate --from "root@tcp(127.0.0.1:3306)/beads"
# add --force to overwrite existing rows
```

## ID format

`<prefix>-<base36>` — e.g. `yuklar-a1b2`. Matches upstream:

- `prefix` is per-project, stored in `.beads/config`. `bd init --prefix foo`
  sets it explicitly; otherwise it's auto-derived from the DSN's database name
  (postgres `.../yuklar` → `yuklar`, mysql `.../yuklar` → `yuklar`,
  `auth.db` → `auth`), falling back to `bd`.
- The hash is the rightmost N base36 digits of `SHA-256(title|description|
  created_by|nonce)`. On collision the nonce increments and the hash recomputes
  — so newly created issues with identical content get distinct ids
  deterministically.
- Default length is 4 chars (`36⁴ ≈ 1.7M` ids per prefix); `idgen` accepts
  3–8.
- Hierarchical ids (`yuklar-a1b2.1`, `yuklar-a1b2.2`, …) come from a
  `child_counters` table with `INSERT … ON CONFLICT DO UPDATE … RETURNING` so
  concurrent allocation stays correct.

## DSN resolution

In order:
1. `--db <dsn>` flag.
2. `$BEADS_DB`.
3. `db=` line in `.beads/config` (walked up from cwd).
4. Default: `.beads/beads.db` (SQLite) under the nearest `.beads/`.

Override the search root with `$BEADS_DIR`.

## Bead types

`task`, `bug`, `epic`, `feature`, `message`, `wisp`, `molecule`, `role`,
`event`. The `issues` table is polymorphic — every bead is a row, with
type-specific columns (`sender`, `event_kind`/`actor`/`target`/`payload`,
`mol_type`, `role_type`, etc.) populated as needed. DB `CHECK` constraints
enforce the allowed values for `status`, `issue_type`, `dependency.type`, and
`priority` — invalid values fail at insert/update time.

## Dependency types

`blocks` (default), `related`, `duplicates`, `supersedes`, `replies-to`,
`parent-child`, `discovered-by`. PK is `(issue_id, depends_on_id)` — one pair
carries one type. Cycles are rejected for `blocks` (direct + transitive).

## Ready semantics

A bead is *ready* iff:
- `status = 'open'`,
- `ephemeral = 0` and `is_template = 0`,
- `defer_until IS NULL` or `defer_until <= now()`,
- no `blocks` dependency points at it from a non-`closed`, non-`pinned` issue.

## JSON output

Every command accepts `-j` / `--json`:

```sh
bd ready -j
bd show bd-a1b2 -j
bd --json create "ship it" -p 0
```

## Storage layout

Five tables: `issues`, `dependencies`, `labels`, `comments`, `events`. Schema
files live in `sql/`; queries are written in [sqlc](https://sqlc.dev) format
in `sql/queries.sql` and generated for both engines under
`internal/db/sqlitedb/` and `internal/db/pgdb/`. Re-generate after editing
queries:

```sh
sqlc generate
```

## Tests

```sh
go test ./...
```

Covers ready-detection (with pinned-blocker transparency, ephemeral, defer),
cycle rejection (direct + transitive), labels, comments, filtered listing,
and DB-level CHECK enforcement.

## Differences vs upstream

- **Storage**: SQLite/Postgres via `database/sql` + sqlc, not Dolt. No
  cell-level merge or native branching. Use the host DB's tooling for backups.
- **Out of scope (v0.1)**: federation, compaction, await/hook/agent
  subsystems, third-party integrations (GitHub/GitLab/Jira/Linear/Notion),
  daemon mode. Their schema columns are dropped (not persisted).
- **Same surface** for the day-to-day flow: `init`, `create`, `list`, `show`,
  `update --claim`, `close`, `ready`, `dep add|rm|list`, `delete`,
  `label add|rm|list`, `comment add|list`, `history`. Plus `migrate`.

## License

MIT.
