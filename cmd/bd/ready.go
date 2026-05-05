package main

import (
	"github.com/spf13/cobra"
)

func newReadyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "ready",
		Short: "List beads with no open blockers (and not deferred/ephemeral)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			out, err := cc.store.Ready(cc.ctx)
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(out)
			}
			printIssueTable(out)
			return nil
		},
	}
}
