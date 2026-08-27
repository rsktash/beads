package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/store"
)

func newSettledCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "settled <keywords>",
		Short: "Search statement and untyped comment text (case-insensitive substring)",
		Long: `Search statement text and untyped comment text together, case-insensitively, via plain substring scan (no FTS index).

Machine contract:
- No matches: prints "settled: no matches" on stdout, exit 0.
- Matches: one line per hit on stdout (kind and id), exit 0. The no-matches line never appears alongside hits.
- Any failure (empty query, unopenable database) writes error to stderr and exits non-zero, never printing the no-matches line.`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.TrimSpace(strings.Join(args, " "))
			if query == "" {
				return fmt.Errorf("empty query")
			}
			qLower := strings.ToLower(query)

			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			stmts, err := cc.store.ListStatements(cc.ctx, store.StatementFilter{})
			if err != nil {
				return err
			}
			comments, err := cc.store.ListAllComments(cc.ctx)
			if err != nil {
				return err
			}

			type hit struct {
				ID    string
				Kind  string
				Issue string
				Text  string
				Date  string
			}
			var hits []hit
			for _, s := range stmts {
				if strings.Contains(strings.ToLower(s.Text), qLower) {
					issueStr := "project"
					if s.IssueID != nil && *s.IssueID != "" {
						issueStr = *s.IssueID
					}
					hits = append(hits, hit{
						ID:    s.ID,
						Kind:  s.Kind,
						Issue: issueStr,
						Text:  s.Text,
						Date:  s.CreatedAt.Format("2006-01-02"),
					})
				}
			}
			for _, c := range comments {
				if strings.Contains(strings.ToLower(c.Text), qLower) {
					hits = append(hits, hit{
						ID:    c.ID,
						Kind:  "comment",
						Issue: c.IssueID,
						Text:  c.Text,
						Date:  c.CreatedAt.Format("2006-01-02"),
					})
				}
			}

			if len(hits) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "settled: no matches")
				return nil
			}

			for _, h := range hits {
				fmt.Fprintf(cmd.OutOrStdout(), "%s  %s  %s  %s  %s\n", h.ID, h.Kind, h.Date, h.Issue, h.Text)
			}
			return nil
		},
	}
	return cmd
}
