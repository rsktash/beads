package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <id>...",
		Short: "Permanently delete issues",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			for _, id := range args {
				if err := cc.store.DeleteIssue(cc.ctx, id); err != nil {
					return fmt.Errorf("%s: %w", id, err)
				}
				fmt.Printf("deleted %s\n", id)
			}
			return nil
		},
	}
}
