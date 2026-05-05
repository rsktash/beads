package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newCloseCmd() *cobra.Command {
	var reason string
	cmd := &cobra.Command{
		Use:   "close <id>...",
		Short: "Close one or more beads",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			closed := beads.StatusClosed
			for _, id := range args {
				u := store.IssueUpdate{Status: &closed}
				if reason != "" {
					u.CloseReason = &reason
				}
				if _, err := cc.store.UpdateIssue(cc.ctx, id, u); err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
				fmt.Printf("closed %s\n", id)
			}
			return nil
		},
	}
	cmd.Flags().StringVarP(&reason, "reason", "r", "", "close reason")
	return cmd
}
