package main

import (
	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

func newListCmd() *cobra.Command {
	var (
		statusStr string
		typeStr   string
		assignee  string
		priority  int
		limit     int
		all       bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List beads (defaults to open; --all to include closed/all statuses)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			f := store.ListFilter{Assignee: assignee, Limit: limit}
			// Default behaviour: only show open. --all disables the filter,
			// --status takes precedence over both.
			if statusStr != "" {
				st, err := beads.ParseStatus(statusStr)
				if err != nil {
					return err
				}
				f.Status = &st
			} else if !all {
				st := beads.StatusOpen
				f.Status = &st
			}
			if typeStr != "" {
				t, err := beads.ParseType(typeStr)
				if err != nil {
					return err
				}
				f.Type = &t
			}
			if cmd.Flags().Changed("priority") {
				f.Priority = &priority
			}
			out, err := cc.store.ListIssues(cc.ctx, f)
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
	cmd.Flags().StringVarP(&statusStr, "status", "s", "", "filter by status (overrides default)")
	cmd.Flags().StringVarP(&typeStr, "type", "t", "", "filter by type")
	cmd.Flags().StringVarP(&assignee, "assignee", "a", "", "filter by assignee")
	cmd.Flags().IntVarP(&priority, "priority", "p", 0, "filter by priority")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max rows")
	cmd.Flags().BoolVar(&all, "all", false, "include all statuses, not just open")
	return cmd
}
