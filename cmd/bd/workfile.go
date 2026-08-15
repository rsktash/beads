package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/internal/config"
)

// newWorkfileCmd implements `bd workfile <id>` — writes a bead's body to disk
// instead of stdout. `bd show --full` measured ~2,113 tok/call in agent
// context because the whole body round-trips through the model; executors
// that just need the contract on disk can read the file instead of paying
// for it in context. Only the header (same as `bd show`: metadata, deps,
// section index) reaches stdout — the body text itself never does.
func newWorkfileCmd() *cobra.Command {
	var out string
	cmd := &cobra.Command{
		Use:   "workfile <id>",
		Short: "Write a bead's body to .bd/.scratch/<id>.md (--out to override) and print the header — avoids putting the body in agent context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			// Fetch first so an unknown id fails exactly like `bd show`
			// before we touch the filesystem.
			i, err := cc.store.GetIssue(cc.ctx, id)
			if err != nil {
				return err
			}

			path := out
			if path == "" {
				cfg, err := config.Resolve(flagDB)
				if err != nil {
					return err
				}
				if cfg.BeadDir == "" {
					return fmt.Errorf("no .bd directory found — run `bd init` first")
				}
				scratchDir := filepath.Join(cfg.BeadDir, ".scratch")
				if err := os.MkdirAll(scratchDir, 0o755); err != nil {
					return err
				}
				path = filepath.Join(scratchDir, id+".md")
			} else if dir := filepath.Dir(path); dir != "." {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					return err
				}
			}

			// Byte-exact with `bd get <id> body`: the raw description, with
			// a trailing newline ensured (see get.go's emitBody).
			body := i.Description
			if !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
			if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
				return err
			}

			// Header only: force the outline branch so the body never
			// prints to stdout, regardless of its length.
			if err := printShowHuman(os.Stdout, cc, id, showOpts{outline: true}); err != nil {
				return err
			}
			fmt.Fprintf(os.Stdout, "workfile: %s\n", path)
			return nil
		},
	}
	cmd.Flags().StringVar(&out, "out", "", "override the output path (default .bd/.scratch/<id>.md)")
	return cmd
}
