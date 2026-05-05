package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "Show an issue and its dependencies",
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
			deps, err := cc.store.ListDependencies(cc.ctx, id)
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(struct {
					Issue any `json:"issue"`
					Deps  any `json:"dependencies"`
				}{i, deps})
			}
			fmt.Printf("%s  [%s] p%d %s\n", i.ID, i.Status, i.Priority, i.Title)
			fmt.Printf("type:     %s\n", i.Type)
			if i.Assignee != "" {
				fmt.Printf("assignee: %s\n", i.Assignee)
			}
			if len(i.Labels) > 0 {
				fmt.Printf("labels:   %s\n", strings.Join(i.Labels, ", "))
			}
			if i.ParentID != "" {
				fmt.Printf("parent:   %s\n", i.ParentID)
			}
			fmt.Printf("created:  %s\n", i.CreatedAt.Format("2006-01-02 15:04:05"))
			fmt.Printf("updated:  %s\n", i.UpdatedAt.Format("2006-01-02 15:04:05"))
			if i.ClosedAt != nil {
				fmt.Printf("closed:   %s\n", i.ClosedAt.Format("2006-01-02 15:04:05"))
			}
			if i.Description != "" {
				fmt.Println("\n" + i.Description)
			}
			if len(deps) > 0 {
				fmt.Println("\ndependencies:")
				for _, d := range deps {
					arrow := "->"
					if d.ToID == id {
						arrow = "<-"
					}
					other := d.ToID
					if d.ToID == id {
						other = d.FromID
					}
					fmt.Printf("  %s %s %s\n", arrow, d.Type, other)
				}
			}
			return nil
		},
	}
}
