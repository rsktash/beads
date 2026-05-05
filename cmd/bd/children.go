package main

import (
	"context"
	"fmt"
	"sort"

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

func walkChildren(ctx context.Context, st *store.Store, parentID string, recursive bool) ([]indentedIssue, error) {
	out := []indentedIssue{}
	visited := map[string]bool{parentID: true}
	var walk func(pid string, depth int) error
	walk = func(pid string, depth int) error {
		if depth > 8 { // hard cap
			return nil
		}
		deps, err := st.ListDependencies(ctx, pid)
		if err != nil {
			return err
		}
		// children of pid: rows where depends_on_id=pid AND type=parent-child
		var kids []beads.Dependency
		for _, d := range deps {
			if d.Type == beads.DepParentChild && d.DependsOnID == pid {
				kids = append(kids, d)
			}
		}
		// stable order: by child issue's priority then created_at — but we
		// don't have those without an extra GetIssue. Compromise: sort by id.
		sort.Slice(kids, func(i, j int) bool { return kids[i].IssueID < kids[j].IssueID })
		for _, k := range kids {
			if visited[k.IssueID] {
				continue
			}
			visited[k.IssueID] = true
			child, err := st.GetIssue(ctx, k.IssueID)
			if err != nil {
				continue
			}
			out = append(out, indentedIssue{depth: depth, issue: *child})
			if recursive {
				if err := walk(k.IssueID, depth+1); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(parentID, 0); err != nil {
		return nil, err
	}
	return out, nil
}
