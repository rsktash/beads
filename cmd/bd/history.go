package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newHistoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "history <id>",
		Short: "Show event history for a bead",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			out, err := cc.store.ListEvents(cc.ctx, args[0])
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(out)
			}
			for _, e := range out {
				fmt.Printf("[%s] %s by %s: %q -> %q  %s\n",
					e.CreatedAt.Format("2006-01-02 15:04"),
					e.EventType, e.Actor, e.OldValue, e.NewValue, e.Comment)
			}
			return nil
		},
	}
}
