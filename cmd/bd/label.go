package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newLabelCmd() *cobra.Command {
	root := &cobra.Command{Use: "label", Short: "Manage labels on a bead"}
	root.AddCommand(
		&cobra.Command{
			Use: "add <id> <label>...", Short: "Attach labels", Args: cobra.MinimumNArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				cc, err := openStore(cmd)
				if err != nil {
					return err
				}
				defer cc.store.Close()
				for _, l := range args[1:] {
					if err := cc.store.AddLabel(cc.ctx, args[0], l); err != nil {
						return err
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "rm <id> <label>...", Short: "Detach labels", Args: cobra.MinimumNArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				cc, err := openStore(cmd)
				if err != nil {
					return err
				}
				defer cc.store.Close()
				for _, l := range args[1:] {
					if err := cc.store.RemoveLabel(cc.ctx, args[0], l); err != nil {
						return fmt.Errorf("%s: %w", l, err)
					}
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "list <id>", Short: "List labels on a bead", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cc, err := openStore(cmd)
				if err != nil {
					return err
				}
				defer cc.store.Close()
				ls, err := cc.store.ListLabels(cc.ctx, args[0])
				if err != nil {
					return err
				}
				if cc.json {
					return writeJSON(ls)
				}
				for _, l := range ls {
					fmt.Println(l)
				}
				return nil
			},
		},
	)
	return root
}
