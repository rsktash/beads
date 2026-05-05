package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rustamsmax/beads/internal/types"
)

func newDepCmd() *cobra.Command {
	dep := &cobra.Command{
		Use:   "dep",
		Short: "Manage dependencies between issues",
	}
	dep.AddCommand(newDepAddCmd(), newDepRmCmd(), newDepListCmd())
	return dep
}

func newDepAddCmd() *cobra.Command {
	var typeStr string
	cmd := &cobra.Command{
		Use:   "add <from> <to>",
		Short: "Add a dependency. Default type is `blocks` (from blocks to).",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			dt, err := types.ParseDependencyType(typeStr)
			if err != nil {
				return err
			}
			if err := cc.store.AddDependency(cc.ctx, args[0], args[1], dt); err != nil {
				return err
			}
			fmt.Printf("%s -%s-> %s\n", args[0], dt, args[1])
			return nil
		},
	}
	cmd.Flags().StringVarP(&typeStr, "type", "t", string(types.DepBlocks), "dependency type (blocks|relates_to|duplicates|supersedes|replies_to|parent_of)")
	return cmd
}

func newDepRmCmd() *cobra.Command {
	var typeStr string
	cmd := &cobra.Command{
		Use:   "rm <from> <to>",
		Short: "Remove a dependency",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cc, err := openStore(cmd)
			if err != nil {
				return err
			}
			defer cc.store.Close()
			dt, err := types.ParseDependencyType(typeStr)
			if err != nil {
				return err
			}
			return cc.store.RemoveDependency(cc.ctx, args[0], args[1], dt)
		},
	}
	cmd.Flags().StringVarP(&typeStr, "type", "t", string(types.DepBlocks), "dependency type")
	return cmd
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
				fmt.Printf("%s -%s-> %s\n", d.FromID, d.Type, d.ToID)
			}
			return nil
		},
	}
}
