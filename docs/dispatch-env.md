# Dispatch environment — beads repo

Static facts every dispatched implementer and reviewer needs. Task-specific
facts live in the bead; this file never restates a contract.

## Repo

- Root: `/Users/rustam/Projects/beads`, module `github.com/rsktash/beads`, Go (see `go.mod`).
- `cmd/bd/` is the CLI, `store/` the storage layer (hand-written SQL via `s.rebind`), `store/migrations/` the embedded `NNNN_<name>.{sqlite,postgres}.sql` pairs, `internal/db/` sqlc-generated code (do not regenerate — `sqlc generate` cannot run here), `web/` a separate npm package out of scope for Go tasks.
- No CI runs Go tests; the local gate is the only gate.

## bd

- Run `bd` from the repo root `/Users/rustam/Projects/beads` (it reads `.bd/config`; the tracker is a remote Postgres over an ssh tunnel on port 5482 — "connection refused" means the tunnel is down, never start a local DB).
- Executors: `export BD_ACTOR=executor` before any bd write. `bd workfile <id>` writes the body to `.bd/.scratch/<id>.md` and prints the header (rulings, findings, sections).
- Never pipe a bd read into `head`, `tail`, `cut` or `grep`; read the whole block, `--section <slug>`, or `--json`.
- Scratch files go in `.bd/.scratch/` only.
- Smoke-testing a built `bd` binary: ALWAYS pass an explicit throwaway DSN — `BD_DB=/tmp/<task>.sqlite ./bd …` or `--db /tmp/<task>.sqlite` (the variable is `BD_DB`, see `internal/config/config.go`). A bare run resolves the DSN from the cwd's `.bd/config` and writes into a real shared tracker; on 2026-09-04 one such run left junk beads in the zanjir tracker.

## Tests and gate

- Targeted: `go test ./cmd/bd/ -run '<Pattern>' -v` and `go test ./store/ -run '<Pattern>' -v`.
- Repo gate (coordinator only, never an implementer): `go build ./... && go vet ./... && go test ./...`.
- `gofmt -l` on files you touched must print nothing (some untouched files are not gofmt-clean; leave them).

## Worktrees and commits

- Each task works in its own worktree under `.worktrees/<bead-id>` on branch `task/<short-id>`; never edit the main checkout or another task's worktree. Never `git stash`.
- One commit per feature, message `feat(bd): feature N — <headline>`. Commit only inside your worktree branch. Never push.
- Files under `~/.claude/` are the owner's corpus: never edit them; put the proposed diff in your report.
