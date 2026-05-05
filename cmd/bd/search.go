package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/store"
)

// `bd search <query>` — substring search across title + description + notes.
// Plain LIKE — adequate up to a few thousand issues. Add an FTS5 virtual
// table later if it needs to scale.
func newSearchCmd() *cobra.Command {
	var (
		limit int
	)
	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Substring search across title/description/notes",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			q := strings.ToLower(args[0])
			if q == "" {
				return fmt.Errorf("empty query")
			}
			// fetch all (filter applied client-side; LIKE in dynamic query is
			// equally valid but the listIssues path is simpler).
			all, err := cc.store.ListIssues(cc.ctx, store.ListFilter{Limit: 0})
			if err != nil {
				return err
			}
			var hits []int
			for i, it := range all {
				if strings.Contains(strings.ToLower(it.Title), q) ||
					strings.Contains(strings.ToLower(it.Description), q) ||
					strings.Contains(strings.ToLower(it.Notes), q) {
					hits = append(hits, i)
				}
			}
			if limit > 0 && len(hits) > limit {
				hits = hits[:limit]
			}
			out := make([]any, 0, len(hits))
			for _, i := range hits {
				out = append(out, all[i])
			}
			if cc.json {
				return writeJSON(out)
			}
			for _, i := range hits {
				it := all[i]
				fmt.Printf("%s  [%s] %s p%d  %s\n",
					it.ID, it.Status, it.Type, it.Priority, it.Title)
			}
			return nil
		},
	}
	cmd.Flags().IntVarP(&limit, "limit", "n", 0, "max hits")
	return cmd
}
