package main

import (
	"github.com/spf13/cobra"
)

func newReadyCmd() *cobra.Command {
	var parent string
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
			if cc.json {
				return writeJSON(out)
			}
			printIssueTable(out)
			return nil
		},
	}
	cmd.Flags().StringVar(&parent, "parent", "", "scope to descendants of this issue id (parent-child)")
	return cmd
}
