package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/internal/config"
	"github.com/rsktash/beads/store"
)

func newInitCmd() *cobra.Command {
	var dsn string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a beads database in the current directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Init(dsn)
			if err != nil {
				return err
			}
			st, err := store.Open(context.Background(), cfg.DSN)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer st.Close()
			fmt.Printf("initialized %s (driver=%s)\n", cfg.DSN, st.Driver())
			fmt.Printf("config:    %s/config\n", cfg.BeadDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "db", "", "DSN to use; default is sqlite at .beads/beads.db. Examples:\n  --db postgres://user:pw@localhost/beads?sslmode=disable\n  --db sqlite:/tmp/test.db")
	return cmd
}
