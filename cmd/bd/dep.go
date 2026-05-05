package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads"
)

func newDepCmd() *cobra.Command {
	dep := &cobra.Command{Use: "dep", Short: "Manage dependencies"}
	dep.AddCommand(newDepAddCmd(), newDepRmCmd(), newDepListCmd())
	return dep
}

func newDepAddCmd() *cobra.Command {
	var typeStr, threadID string
	cmd := &cobra.Command{
		Use:   "add <issue> <depends-on>",
		Short: "Add dependency: <issue> --type--> <depends-on>",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			dt, err := beads.ParseDependencyType(typeStr)
			if err != nil {
				return err
			}
			d := beads.Dependency{
				IssueID: args[0], DependsOnID: args[1], Type: dt, ThreadID: threadID,
				CreatedBy: assigneeFromEnv(),
			}
			if err := cc.store.AddDependency(cc.ctx, d); err != nil {
				return err
			}
			fmt.Printf("%s -%s-> %s\n", args[0], dt, args[1])
			return nil
		},
	}
	cmd.Flags().StringVarP(&typeStr, "type", "t", string(beads.DepBlocks), "dependency type (blocks|related|duplicates|supersedes|replies-to|parent-child|discovered-by)")
	cmd.Flags().StringVar(&threadID, "thread", "", "thread id (for message threads)")
	return cmd
}

func newDepRmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <issue> <depends-on>",
		Short: "Remove a dependency",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			return cc.store.RemoveDependency(cc.ctx, args[0], args[1])
		},
	}
}

func newDepListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list <id>",
		Short: "List dependencies touching an issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			deps, err := cc.store.ListDependencies(cc.ctx, args[0])
			if err != nil {
				return err
			}
			if cc.json {
				return writeJSON(deps)
			}
			for _, d := range deps {
				fmt.Printf("%s -%s-> %s\n", d.IssueID, d.Type, d.DependsOnID)
			}
			return nil
		},
	}
}
