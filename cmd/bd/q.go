package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

// `bd q <title> [-p N] [-t type]` — quick capture. Prints just the id on
// success, suitable for shell piping (`id=$(bd q "fix bug")`). Replaces the
// upstream `bd q` flow that bd-defaults relies on.
func newQCmd() *cobra.Command {
	var (
		typeStr  string
		priority int
		assignee string
		labels   []string
	)
	cmd := &cobra.Command{
		Use:   "q <title>",
		Short: "Quick-capture an issue; prints id only",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			t, err := beads.ParseType(typeStr)
			if err != nil {
				return err
			}
			i := &beads.Issue{
				Title:    strings.TrimSpace(args[0]),
				Type:     t,
				Status:   beads.StatusOpen,
				Priority: priority,
				Assignee: assignee,
				Labels:   labels,
			}
			if err := cc.store.CreateIssue(cc.ctx, i); err != nil {
				return err
			}
			for _, l := range labels {
				_ = cc.store.AddLabel(cc.ctx, i.ID, l)
			}
			fmt.Println(i.ID)
			return nil
		},
	}
	cmd.Flags().StringVarP(&typeStr, "type", "t", "task", "issue type")
	cmd.Flags().IntVarP(&priority, "priority", "p", 2, "priority")
	cmd.Flags().StringVarP(&assignee, "assignee", "a", "", "assignee")
	cmd.Flags().StringSliceVarP(&labels, "label", "l", nil, "label")
	return cmd
}
