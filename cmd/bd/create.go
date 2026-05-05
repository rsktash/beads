package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/rustamsmax/beads/internal/types"
)

func newCreateCmd() *cobra.Command {
	var (
		desc     string
		typeStr  string
		priority int
		assignee string
		labels   []string
		parent   string
	)
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()

			t, err := types.ParseType(typeStr)
			if err != nil {
				return err
			}
			i := &types.Issue{
				Title:       strings.TrimSpace(args[0]),
				Description: desc,
				Type:        t,
				Status:      types.StatusOpen,
				Priority:    priority,
				Assignee:    assignee,
				Labels:      labels,
				ParentID:    parent,
			}
			if err := cc.store.CreateIssue(cc.ctx, i); err != nil {
				return err
			}
			if cc.json {
				return writeJSON(i)
			}
			fmt.Printf("%s  %s\n", i.ID, i.Title)
			return nil
		},
	}
	cmd.Flags().StringVarP(&desc, "desc", "d", "", "description body")
	cmd.Flags().StringVarP(&typeStr, "type", "t", "task", "issue type (task|bug|epic|feature|message)")
	cmd.Flags().IntVarP(&priority, "priority", "p", 2, "priority (0=highest, 3=lowest)")
	cmd.Flags().StringVarP(&assignee, "assignee", "a", "", "assignee identifier")
	cmd.Flags().StringSliceVarP(&labels, "label", "l", nil, "label (repeatable)")
	cmd.Flags().StringVar(&parent, "parent", "", "parent issue id")
	return cmd
}
