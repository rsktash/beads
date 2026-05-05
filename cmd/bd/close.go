package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rustamsmax/beads/internal/storage"
	"github.com/rustamsmax/beads/internal/types"
)

func newCloseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "close <id>...",
		Short: "Close one or more issues",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			closed := types.StatusClosed
			for _, id := range args {
				if _, err := cc.store.UpdateIssue(cc.ctx, id, storage.IssueUpdate{Status: &closed}); err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
				fmt.Printf("closed %s\n", id)
			}
			return nil
		},
	}
}
