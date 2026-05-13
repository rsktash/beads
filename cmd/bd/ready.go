package main

import (
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
		Short: "List beads with no open blockers (and not deferred/ephemeral). --parent <id> scopes to descendants of that issue.",
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
			if limit > 0 && len(out) > limit {
				out = out[:limit]
			}
			if cc.json {
				if full {
					return writeJSON(out)
				}
				return writeJSON(slimIssues(out))
			}
			printIssueTable(out)
			return nil
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "scope to descendants of this issue id (parent-child)")
	cmd.Flags().BoolVar(&full, "full", false, "emit full Issue rows in --json (default: id/title/status/priority/type/assignee)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "cap returned rows (0 = unlimited)")
	return cmd
}
