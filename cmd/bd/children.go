package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
)

// newChildrenCmd lists the parent-child descendants of an issue. Replaces
// the upstream `bd children` workflow that bd-defaults skills lean on.
//
// Default: one level (direct children).
// --recursive: full tree, indented by depth.
// --status filters to the given status.
func newChildrenCmd() *cobra.Command {
	var (
		recursive bool
		statStr   string
		closed    bool
	)
	cmd := &cobra.Command{
		Use:   "children <id>",
		Short: "List parent-child descendants of an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			var statusFilter *beads.Status
			if statStr != "" {
				s, err := beads.ParseStatus(statStr)
				if err != nil {
					return err
				}
				statusFilter = &s
			} else if !closed {
				// default: hide closed unless --closed or --status=closed
				open := beads.StatusOpen
				_ = open
			}

			rows, err := walkChildren(cc.ctx, cc.store, args[0], recursive)
			if err != nil {
				return err
			}
			if statusFilter != nil {
				filtered := rows[:0]
				for _, r := range rows {
					if r.issue.Status == *statusFilter {
						filtered = append(filtered, r)
					}
				}
				rows = filtered
			} else if !closed {
				filtered := rows[:0]
				for _, r := range rows {
					if r.issue.Status != beads.StatusClosed {
						filtered = append(filtered, r)
					}
				}
				rows = filtered
			}

			if cc.json {
				out := make([]beads.Issue, 0, len(rows))
				for _, r := range rows {
					out = append(out, r.issue)
				}
				return writeJSON(out)
			}
			for _, r := range rows {
				indent := ""
				for i := 0; i < r.depth; i++ {
					indent += "  "
				}
				fmt.Printf("%s%s  [%s] p%d %s\n", indent, r.issue.ID, r.issue.Status, r.issue.Priority, r.issue.Title)
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&recursive, "recursive", "r", false, "walk full descendant tree")
	cmd.Flags().StringVarP(&statStr, "status", "s", "", "filter by status")
	cmd.Flags().BoolVar(&closed, "closed", false, "include closed children (default: open + non-closed only)")
	return cmd
}

type indentedIssue struct {
	depth int
	issue beads.Issue
}

// walkChildren returns the parent-child descendants of parentID.
//
// Backed by Store.Descendants, which executes a recursive CTE in one
// round trip (plus an IN-list fetch for the rows) — replacing the prior
// 1+N+N×depth round-trip walk that made `bd children` and
// `bd ready --parent <epic>` 6-12s on remote Postgres.
func walkChildren(ctx context.Context, st *store.Store, parentID string, recursive bool) ([]indentedIssue, error) {
	rows, err := st.Descendants(ctx, parentID, recursive, 8)
	if err != nil {
		return nil, err
	}
	out := make([]indentedIssue, 0, len(rows))
	for _, r := range rows {
		out = append(out, indentedIssue{depth: r.Depth - 1, issue: r.Issue})
	}
	return out, nil
}
