package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func newRulingRetireCmd() *cobra.Command {
	var note string
	cmd := &cobra.Command{
		Use:   "retire <ruling-id>",
		Short: "Retire a legacy ruling in place (owner only: interactive terminal)",
		Long: `Retire a ruling: its status flips to retired in place, no new row is minted.
A retired ruling stops rendering in the contract and in bd rulings, same as a
superseded one, but stays readable via bd statements list --status retired
and bd get.

--note is required and records why the ruling no longer applies.

Runs only from an interactive terminal: an agent's shell has no terminal on stdin,
so an agent that finds a stale ruling files a finding instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !stdinIsTerminal() {
				return errors.New("ruling retire runs only from the owner's interactive terminal; an agent files a finding naming the stale ruling")
			}
			identity, isExecutor := resolveActor()
			if isExecutor {
				return executorRefusal(identity)
			}

			noteVal := strings.TrimSpace(note)
			if noteVal == "" {
				return fmt.Errorf("--note is required")
			}

			rulingID := strings.TrimSpace(args[0])

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			target, err := cc.store.GetStatement(cc.ctx, rulingID)
			if err != nil {
				return err
			}
			if target.Kind != "ruling" {
				return fmt.Errorf("%s is a %s, not a ruling", rulingID, target.Kind)
			}
			if target.Status == "retired" || target.Status == "superseded" {
				return fmt.Errorf("%s is already %s", rulingID, target.Status)
			}

			if err := cc.store.RetireRuling(cc.ctx, rulingID, noteVal); err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), rulingID)
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "why the ruling is being retired (required)")
	return cmd
}

// stdinIsTerminal reports whether stdin is an interactive terminal. Tests
// replace it to stand in for the owner's terminal.
var stdinIsTerminal = func() bool { return isInteractiveTerminal(os.Stdin) }
