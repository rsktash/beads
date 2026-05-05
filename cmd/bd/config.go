package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newConfigCmd manages the in-DB `config` key/value table. Local-only DSN
// settings are NOT exposed here — those live in .bd/config and are edited
// directly.
func newConfigCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "config",
		Short: "Get/set project settings stored in the DB config table",
	}
	root.AddCommand(
		&cobra.Command{
			Use: "get <key>", Short: "Print a config value", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				cc, err := openStore(cmd)
				if err != nil {
					return err
				}
				defer cc.store.Close()
				v, err := cc.store.GetConfig(cc.ctx, args[0])
				if err != nil {
					return err
				}
				fmt.Println(v)
				return nil
			},
		},
		&cobra.Command{
			Use: "set <key> <value>", Short: "Upsert a config value", Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				cc, err := openStore(cmd)
				if err != nil {
					return err
				}
				defer cc.store.Close()
				return cc.store.SetConfig(cc.ctx, args[0], args[1])
			},
		},
		&cobra.Command{
			Use: "list", Short: "List all config keys",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cc, err := openStore(cmd)
				if err != nil {
					return err
				}
				defer cc.store.Close()
				m, err := cc.store.ListConfig(cc.ctx)
				if err != nil {
					return err
				}
				if cc.json {
					return writeJSON(m)
				}
				for k, v := range m {
					fmt.Printf("%s=%s\n", k, v)
				}
				return nil
			},
		},
	)
	return root
}
