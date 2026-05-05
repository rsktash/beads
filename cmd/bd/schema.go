package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// `bd schema list` shows applied + pending migrations. `bd schema apply`
// re-runs migrate() against the current connection — useful after pulling
// a new bd binary that adds tables.
func newSchemaCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "schema",
		Short: "Inspect and apply database schema migrations",
	}
	root.AddCommand(
		&cobra.Command{
			Use: "list", Short: "List applied + pending migrations",
			RunE: func(cmd *cobra.Command, _ []string) error {
				cc, err := openStore(cmd)
				if err != nil {
					return err
				}
				defer cc.store.Close()
				st, err := cc.store.MigrationStatus(cc.ctx)
				if err != nil {
					return err
				}
				if cc.json {
					return writeJSON(st)
				}
				for _, m := range st {
					mark := "pending"
					when := ""
					if m.Applied {
						mark = "applied"
						if m.AppliedAt != nil {
							when = "  " + m.AppliedAt.Format("2006-01-02 15:04:05")
						}
					}
					fmt.Printf("%04d  %-9s  %s%s\n", m.Version, mark, m.Name, when)
				}
				return nil
			},
		},
		&cobra.Command{
			Use: "apply", Short: "Apply any pending migrations",
			RunE: func(cmd *cobra.Command, _ []string) error {
				// Open() runs cached migrate(); ForceMigrate bypasses the
				// cache so this command always re-verifies against the DB.
				cc, err := openStore(cmd)
				if err != nil {
					return err
				}
				defer cc.store.Close()
				if err := cc.store.ForceMigrate(cc.ctx); err != nil {
					return err
				}
				st, err := cc.store.MigrationStatus(cc.ctx)
				if err != nil {
					return err
				}
				applied := 0
				for _, m := range st {
					if m.Applied {
						applied++
					}
				}
				fmt.Printf("schema up to date: %d/%d migrations applied\n", applied, len(st))
				return nil
			},
		},
	)
	return root
}
