# beads (`bd`)

A graph-based issue tracker for AI agents — same shape as
[gastownhall/beads](https://github.com/gastownhall/beads) but with **SQLite or
Postgres** as the backing store instead of Dolt. Single static Go binary.

## Why

Agents lose context when their plan lives in a markdown file. Beads gives them
a structured, dependency-aware graph so they can answer *"what can I work on
next?"* without re-reading the world.

## Install

```sh
go install github.com/rustamsmax/beads/cmd/bd@latest
```

Or from a clone:

```sh
git clone https://github.com/rsktash/beads.git
cd beads
go build -o bd ./cmd/bd
```

> SQLite support uses `mattn/go-sqlite3`, which requires CGO. The default
> `go build` invocation works on macOS/Linux out of the box.

## Quickstart

```sh
bd init                          # creates .beads/beads.db (SQLite)
bd create "Set up CI" -p 0       # priorities: 0 highest .. 3 lowest
bd create "Add login endpoint"
bd create "Wire login UI"
bd dep add <ui> <endpoint>       # ui blocks endpoint? no — ui DEPENDS on endpoint:
                                 # form is `bd dep add <blocker> <blocked>`
bd ready                         # tasks with no open blockers
bd show <id>
bd update <id> --claim           # set in_progress + assign to you
bd close <id>
```

Use Postgres instead:

```sh
bd init --db postgres://user:pw@localhost/beads?sslmode=disable
# or
export BEADS_DB=postgres://user:pw@localhost/beads?sslmode=disable
bd ready
```

## DSN resolution

In order:
1. `--db <dsn>` flag.
2. `$BEADS_DB`.
3. `db=` line in `.beads/config` (walked up from cwd).
4. Default: `.beads/beads.db` (SQLite) under the nearest `.beads/`.

Override the search root with `$BEADS_DIR`.

## Issue model

| field        | notes                                            |
|--------------|--------------------------------------------------|
| `id`         | hash-style, e.g. `bd-a1b2`. Random 16-bit hex.   |
| `title`      | short summary                                    |
| `description`| body                                             |
| `type`       | `task` \| `bug` \| `epic` \| `feature` \| `message` |
| `status`     | `open` \| `in_progress` \| `blocked` \| `closed` |
| `priority`   | int, 0=highest                                   |
| `assignee`   | free-form string                                 |
| `labels`     | string list                                      |
| `parent_id`  | optional parent issue id                         |

## Dependency types

`blocks` (default), `relates_to`, `duplicates`, `supersedes`, `replies_to`,
`parent_of`. Cycles are rejected for `blocks` (direct and transitive).

## JSON output

Every command accepts `-j` / `--json`:

```sh
bd ready -j
bd show bd-a1b2 -j
bd --json create "ship it" -p 0
```

## Storage

The whole schema is two tables: `issues` and `dependencies` (edge list, FK to
issues with `ON DELETE CASCADE`). The same DDL works on SQLite and Postgres
with minor type swaps (`TIMESTAMP` ↔ `TIMESTAMPTZ`). See
[`internal/storage/storage.go`](internal/storage/storage.go).

## Tests

```sh
go test ./...
```

The tests cover the ready-detection query and cycle rejection on top of an
in-tempdir SQLite database. Postgres is wire-compatible — point the same code
at a Postgres DSN to validate manually.

## Differences vs upstream

This is a from-scratch reimplementation focused on the core model:

- **Database**: SQLite/Postgres via `database/sql` + `sqlx`, not Dolt. No
  cell-level merge or native branching; rely on the host DB and ordinary
  backups.
- **No git/sync hooks**, no integrations (GitHub/GitLab/Jira/Linear/Notion),
  no daemon, no compaction yet.
- **Same surface** for the core flow: `init`, `create`, `list`, `show`,
  `update --claim`, `close`, `ready`, `dep add|rm|list`, `delete`. JSON output
  and hash IDs match.

## License

MIT.
