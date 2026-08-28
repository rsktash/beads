package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/store"
)

func newStatementsPromoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "promote <statement-id>",
		Short: "Promote a candidate statement to active (actor-gated: BD_ACTOR=executor is refused)",
		Long: `Promote a backfilled candidate statement to active.

Actor gating: BD_ACTOR=executor cannot promote candidates; promotion is the
owner's confirmation. BD_ACTOR=coordinator or unset (owner) is allowed.

The statement must exist and be in status 'candidate'. Any other status errors
without mutating. On success the row flips to 'active' and thereafter appears in
bd rulings and the contract.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			identity, isExecutor := resolveActor()
			if isExecutor {
				word := identity
				if idx := strings.IndexByte(word, ':'); idx >= 0 {
					word = word[:idx]
				}
				if word == "" {
					word = "executor"
				}
				return fmt.Errorf("BD_ACTOR=%s: executors cannot promote candidates; promotion is the owner's confirmation", word)
			}

			id := strings.TrimSpace(args[0])
			if id == "" {
				return fmt.Errorf("statement id is required")
			}

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			st, err := cc.store.GetStatement(cc.ctx, id)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					return fmt.Errorf("statement %s not found", id)
				}
				return err
			}
			if st.Status != "candidate" {
				return fmt.Errorf("statement %s is %s, not candidate; only candidates can be promoted", id, st.Status)
			}

			if err := cc.store.UpdateStatementStatus(cc.ctx, id, "active"); err != nil {
				return err
			}
			st.Status = "active"

			if cc.json || flagJSON {
				return writeJSONTo(cmd.OutOrStdout(), st)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "promoted %s to active\n", id)
			return nil
		},
	}
	return cmd
}
