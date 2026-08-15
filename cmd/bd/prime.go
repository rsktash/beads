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

- ` + "`bd ready`" + ` — what's available to work on (no open blockers).
- ` + "`bd show <id>`" + ` — header + deps. Long descriptions print an
  outline; pass ` + "`--full`" + ` for the body or ` + "`--section <slug>`" + ` for one heading.
- ` + "`bd update <id> --claim`" + ` — assign yourself + set in_progress.
- ` + "`bd close <id> --reason \"...\"`" + ` — when done.

## Capture

- ` + "`bd q \"title\"`" + ` — quick capture; prints id only.
- ` + "`bd create \"title\" -p 0 -t bug`" + ` — full create with priority/type.
- ` + "`bd update <id> --body-file path.md`" + ` — set description from file.
- ` + "`bd dep add <issue> <depends-on>`" + ` — link a dependency.

## Reading (token-aware)

- ` + "`bd get <id> <field>`" + ` — single field, raw, no jq. ` + "`bd get <id> fields`" + ` lists names.
- ` + "`bd show <id> --full`" + ` — full description body (also ` + "`--head N`" + `, ` + "`--tail N`" + `, ` + "`--lines START-END`" + `).
- ` + "`bd show id1 id2 id3 --full`" + ` — batch read several beads in one call.
- Executing a bead? ` + "`bd workfile <id>`" + ` writes your contract to ` + "`.bd/.scratch/<id>.md`" + `; read that file. Avoid ` + "`--full`" + ` (prints the body into your context).
- ` + "`bd show <id> --include comments`" + ` — comment bodies (` + "`--json`" + ` returns ` + "`comments_count`" + ` only by default).
- ` + "`bd children <id>`" + ` — direct children. Add ` + "`-r`" + ` for full tree.
- ` + "`bd search 'query'`" + ` — substring across title/description/notes.

## Lists

- ` + "`bd list -s open -t bug`" + ` — filter by status/type.
- ` + "`bd ready --json`" + ` / ` + "`bd list --json`" + ` — slim DTOs (id/title/status/priority/type/assignee).
  Add ` + "`--full`" + ` for full Issue rows. ` + "`bd ready --limit N`" + ` caps results.

## Comments + memory

- ` + "`bd comment add <id> \"text\"`" + ` — issue-scoped discussion.
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
					return fmt.Errorf("no .bd directory found — run `bd init` first")
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
