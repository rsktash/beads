package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/internal/config"
)

// `bd prime` — emits a markdown cheat-sheet for an LLM agent on session
// start / post-compact. Per-project override: drop `.bd/PRIME.md` next to
// the workspace and `bd prime` will print that verbatim. `bd prime --export`
// writes the default body to `.bd/PRIME.md` so you can edit-and-keep.

const defaultPrime = `# bd quick reference

You're working in a beads project (` + "`bd`" + `). Use the CLI for issue
state, not free-form notes.

## Daily flow

- ` + "`bd authority <bead-id | words…>`" + ` — the authority brief: rulings, topics, questions, findings, doctrine and links in one capped read. Run it first on any bead or owner question; a words query needs ` + "`--concern <name>`" + `.
- ` + "`bd ready`" + ` — what's available to work on (no open blockers, deferred, ephemeral, or open questions).
- ` + "`bd plan show <plan>`" + ` — the execution plan: every lane with its cursor, holder, last handoff and the graph-computed readiness of the next item.
- ` + "`bd show <id>`" + ` — execution contract for the bead (see Reading). Long descriptions print an
  outline; pass ` + "`--full`" + ` for the body or ` + "`--section <slug>`" + ` for one heading.
- ` + "`bd update <id> --status=in_progress --assignee \"<name> / <model>\"`" + ` — claim with attribution (never ` + "`--claim`" + `, which drops the model).
- ` + "`bd close <id> --reason \"...\"`" + ` — when done.

## Statements — typed authority (rulings/questions/findings)

- Filing: ` + "`bd ruling add [<id>] \"text\" --topic <slug> [--supersedes R-x] [--answers Q-x] [--scope self] [--defer <RFC3339>|--park|--close]`" + ` (--topic is required unless --answers supplies it), ` + "`bd question add <id> \"text\" --topic <slug>`" + `, ` + "`bd question answer Q-x --ruling R-y|--finding F-y`" + `, ` + "`bd question close Q-x --reason moot|duplicate|superseded --note \"why\" [--of Q-z]`" + `, ` + "`bd finding add <id> \"text\" --topic <slug> [--evidence \"refs\"]`" + `. A topic slug is [a-z0-9-]{2,48} and is never invented for you: filing without one prints the catalogue for the bead's areas and refuses. --park defers the bead to far future (defer_until=9999-12-31) and adds label 'parked' (deferred+parked, excluded from ready); --defer sets defer_until; --close closes.
- Query: ` + "`bd topics [--concern <c>] [--workspace <w>] [--all]`" + ` (the catalogue: slug, status, question, ruling; dormant hidden without --all; ` + "`bd topics rename <old> <new>`" + ` and ` + "`bd topics merge <from> <into>`" + ` maintain it), ` + "`bd rulings`" + `, ` + "`bd rulings --scope project`" + `, ` + "`bd rulings --grep <kw>`" + `, ` + "`bd ruling retire R-x --note \"…\"`" + `, ` + "`bd question list [<id>] [--status answered|retracted|superseded]`" + ` (the blocked frontier, read-only, open to executors), ` + "`bd settled <keywords>`" + `, ` + "`bd statements backfill`" + `, ` + "`bd source R-x`" + ` (the owner message behind a statement or comment; ` + "`bd annotate <id> --session <sid> --msg <uuid> --tool <tool_use_id>`" + ` writes that pointer and is open to executors).
- Actor gating: An executor context cannot file a ruling, and cannot answer or close a question; it may file questions and findings. Closing a blocker is an owner or coordinator act.
- Consequences: An active ruling is binding and must not be re-litigated. An open question removes the bead from ` + "`bd ready`" + ` until it is answered or closed; a closed question is never deleted and keeps rendering under CLOSED QUESTIONS — NO LONGER BLOCKING. A parked bead is deferred with label 'parked' and absent from ` + "`bd ready`" + `; un-park via ` + "`bd update <id> --defer \"\"`" + ` (clear) and ` + "`bd label rm <id> parked`" + `.
- Tiers: statements are the authority tier, untyped comments are the advisory tier — use a statement where a ruling, question, or finding is the right record; do not use an untyped comment where a statement is the right record.

## Capture

- ` + "`bd q \"title\"`" + ` — quick capture; prints id only.
- ` + "`bd create \"title\" -p 0 -t bug`" + ` — full create with priority/type.
- ` + "`bd update <id> --body-file path.md`" + ` — set description from file.
- ` + "`bd dep add <issue> <depends-on>`" + ` — link a dependency.

## Reading (token-aware)

- ` + "`bd show <id>`" + ` renders the execution contract in this order: ` + "`CONTRACT`" + `, ` + "`ACTIVE RULINGS — MUST OBEY`" + `, ` + "`OPEN QUESTIONS — EXECUTION BLOCKERS`" + `, ` + "`CLOSED QUESTIONS — NO LONGER BLOCKING`" + `, ` + "`FINDINGS`" + `, ` + "`BASE TEXT`" + `, ` + "`DEPENDENCIES`" + `, ` + "`NOTES / UNTYPED HISTORY`" + `.
  Long descriptions print an outline; pass ` + "`--full`" + ` for the body or ` + "`--section <slug>`" + ` for one heading.
- ` + "`bd get <id> <field>`" + ` — single field, raw, no jq. ` + "`bd get <id> fields`" + ` lists names.
- ` + "`bd show <id> --full`" + ` — full description body (also ` + "`--head N`" + `, ` + "`--tail N`" + `, ` + "`--lines START-END`" + `).
- ` + "`bd show id1 id2 id3 --full`" + ` — batch read several beads in one call.
- Executing a bead? ` + "`bd workfile <id>`" + ` writes your contract to ` + "`.bd/.scratch/<id>.md`" + `; read that file. Avoid ` + "`--full`" + ` (prints the body into your context).
- ` + "`bd show <id> --include comments`" + ` — comment bodies (` + "`--json`" + ` returns ` + "`comments_count`" + ` only by default).
- ` + "`bd show <id> --json --include statements`" + ` — typed statements (` + "`--include all`" + ` includes both).
- ` + "`bd children <id>`" + ` — direct children. Add ` + "`-r`" + ` for full tree.
- ` + "`bd search 'query'`" + ` — substring across title/description/notes.

## Lists

- ` + "`bd list -s open -t bug`" + ` — filter by status/type.
- ` + "`bd ready --json`" + ` / ` + "`bd list --json`" + ` — slim DTOs (id/title/status/priority/type/assignee).
  Add ` + "`--full`" + ` for full Issue rows. ` + "`bd ready --limit N`" + ` caps results.

## Comments + memory

- ` + "`bd comment add <id> \"text\"`" + ` — issue-scoped discussion (advisory tier). For binding authority use the statement commands above.
- Addressing a comment? Start it with ` + "`[reviewer]`, `[next-phase]`, `[orchestrator]`, or `[all]`" + ` — readers filter with ` + "`bd comment list <id> --tag <t>`" + `.
- ` + "`bd remember \"text\"`" + ` — save a project-wide memory note.
- ` + "`bd memories list`" + ` — read all memories back.

## Bulk

- ` + "`bd batch -f script.bd`" + ` — apply many ops in one run (create / update
  / close / dep / label / comment).

Ids look like ` + "`<prefix>-<base36>`" + ` (e.g. ` + "`yuklar-a3f8`" + `). The prefix is
configured per-project in the DB ` + "`config`" + ` table; see ` + "`bd config list`" + `.
`

func newPrimeCmd() *cobra.Command {
	var export bool
	cmd := &cobra.Command{
		Use:   "prime",
		Short: "Print a cheat-sheet for LLM agents on session start",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Resolve(flagDB)
			if err != nil {
				return err
			}
			path := filepath.Join(cfg.BeadDir, "PRIME.md")

			if export {
				if cfg.BeadDir == "" {
					return config.NoBeadDirError()
				}
				if err := os.WriteFile(path, []byte(defaultPrime), 0o644); err != nil {
					return err
				}
				fmt.Fprintf(os.Stderr, "wrote %s\n", path)
				return nil
			}

			if cfg.BeadDir != "" {
				if body, err := os.ReadFile(path); err == nil {
					fmt.Print(string(body))
					return nil
				}
			}
			fmt.Print(defaultPrime)
			return nil
		},
	}
	cmd.Flags().BoolVar(&export, "export", false, "write the default body to .bd/PRIME.md so you can edit it")
	return cmd
}
