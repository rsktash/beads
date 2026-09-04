package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/rsktash/beads"
	"github.com/rsktash/beads/store"
	"github.com/spf13/cobra"
)

func newReadyCmd() *cobra.Command {
	var (
		parent string
		full   bool
		limit  int
	)
	cmd := &cobra.Command{
		Use:   "ready",
		Short: "List beads with no open blockers, deferred, ephemeral, or open questions. --parent <id> scopes to descendants of that issue.",
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
			if parent != "" {
				// Walk the descendant tree of parent and intersect with
				// ready. Used by the executing-plans loop to drive work
				// against a single epic.
				descendants, err := walkChildren(cc.ctx, cc.store, parent, true)
				if err != nil {
					return err
				}
				inTree := make(map[string]bool, len(descendants))
				for _, d := range descendants {
					inTree[d.issue.ID] = true
				}
				filtered := out[:0]
				for _, i := range out {
					if inTree[i.ID] {
						filtered = append(filtered, i)
					}
				}
				out = filtered
			}
			planID, queue, err := orderReadyIssues(cc.ctx, cc.store, out)
			if err != nil {
				return err
			}
			if limit > 0 && len(out) > limit {
				out = out[:limit]
			}
			if cc.json {
				if full {
					return writeJSON(out)
				}
				rows, err := slimIssues(cc.ctx, cc.store, out)
				if err != nil {
					return err
				}
				return writeJSON(rows)
			}
			if planID != "" {
				printReadyPlanTable(out, queue)
			} else {
				printIssueTable(out)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "scope to descendants of this issue id (parent-child)")
	cmd.Flags().BoolVar(&full, "full", false, "emit full Issue rows in --json (default: id/title/status/priority/type/assignee)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "cap returned rows (0 = unlimited)")
	return cmd
}

func orderReadyIssues(ctx context.Context, st *store.Store, issues []beads.Issue) (string, map[string]store.QueueSlot, error) {
	planID, queue, err := st.ActivePlanQueue(ctx)
	if err != nil {
		var two *store.ErrTwoActivePlans
		if errors.As(err, &two) {
			return "", nil, fmt.Errorf(
				"bd ready: plans %s and %s are both active; end one with bd plan handoff/close before ordering",
				two.A, two.B)
		}
		return "", nil, err
	}

	cleared := map[string]bool(nil)
	lastHandoff, err := st.NewestHandoffAt(ctx)
	if err != nil {
		return "", nil, err
	}
	if lastHandoff != nil {
		cleared, err = st.BlockerClearedSince(ctx, *lastHandoff)
		if err != nil {
			return "", nil, err
		}
	}

	sort.SliceStable(issues, func(i, j int) bool {
		a, b := issues[i], issues[j]
		aSlot, aQueued := queue[a.ID]
		bSlot, bQueued := queue[b.ID]
		if aQueued != bQueued {
			return aQueued
		}
		if aQueued {
			if aSlot.Lane != bSlot.Lane {
				return aSlot.Lane < bSlot.Lane
			}
			return aSlot.Index < bSlot.Index
		}
		if a.Priority != b.Priority {
			return a.Priority < b.Priority
		}
		if lastHandoff != nil && cleared[a.ID] != cleared[b.ID] {
			return cleared[a.ID]
		}
		return a.UpdatedAt.After(b.UpdatedAt)
	})
	return planID, queue, nil
}

func printReadyPlanTable(issues []beads.Issue, queue map[string]store.QueueSlot) {
	if len(issues) == 0 {
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, "LANE\tID\tP\tSTATUS\tTYPE\tASSIGNEE\tTITLE")
	for _, issue := range issues {
		lane := ""
		if slot, ok := queue[issue.ID]; ok {
			lane = fmt.Sprintf("%s%d", slot.Lane, slot.Index)
		}
		assignee := issue.Assignee
		if assignee == "" {
			assignee = "-"
		}
		title := issue.Title
		if len(title) > 64 {
			title = title[:61] + "..."
		}
		fmt.Fprintf(w, "%s\t%s\tp%d\t%s\t%s\t%s\t%s\n",
			lane, issue.ID, issue.Priority, issue.Status, issue.Type, assignee, strings.ReplaceAll(title, "\n", " "))
	}
	w.Flush()
}
