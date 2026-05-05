package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show a bead with its labels, dependencies, comments, and history",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			id := args[0]
			i, err := cc.store.GetIssue(cc.ctx, id)
			if err != nil {
				return err
			}
			labels, err := cc.store.ListLabels(cc.ctx, id)
			if err != nil {
				return err
			}
			i.Labels = labels
			deps, err := cc.store.ListDependencies(cc.ctx, id)
			if err != nil {
				return err
			}
			comments, err := cc.store.ListComments(cc.ctx, id)
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(struct {
					Issue        any `json:"issue"`
					Dependencies any `json:"dependencies"`
					Comments     any `json:"comments"`
				}{i, deps, comments})
			}
			fmt.Printf("%s  [%s] %s p%d %s\n", i.ID, i.Status, i.Type, i.Priority, i.Title)
			if i.Assignee != "" {
				fmt.Printf("assignee: %s\n", i.Assignee)
			}
			if len(labels) > 0 {
				fmt.Printf("labels:   %s\n", strings.Join(labels, ", "))
			}
			fmt.Printf("created:  %s\n", i.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("updated:  %s\n", i.UpdatedAt.Format("2006-01-02 15:04:05"))
			if i.DueAt != nil {
				fmt.Printf("due:      %s\n", i.DueAt.Format("2006-01-02 15:04:05"))
			}
			if i.DeferUntil != nil {
				fmt.Printf("defer:    %s\n", i.DeferUntil.Format("2006-01-02 15:04:05"))
			}
			if i.ClosedAt != nil {
				fmt.Printf("closed:   %s (%s)\n", i.ClosedAt.Format("2006-01-02 15:04:05"), i.CloseReason)
			}
			if i.Description != "" {
				fmt.Println("\n" + i.Description)
			}
			if len(deps) > 0 {
				fmt.Println("\ndependencies:")
				for _, d := range deps {
					arrow, other := "->", d.DependsOnID
					if d.DependsOnID == id {
						arrow, other = "<-", d.IssueID
					}
					fmt.Printf("  %s %s %s\n", arrow, d.Type, other)
				}
			}
			if len(comments) > 0 {
				fmt.Println("\ncomments:")
				for _, c := range comments {
					fmt.Printf("  [%s] %s: %s\n", c.CreatedAt.Format("2006-01-02 15:04"), c.Author, c.Text)
				}
			}
			return nil
		},
	}
}
