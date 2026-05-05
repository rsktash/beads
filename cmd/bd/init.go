package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/internal/config"
	"github.com/rsktash/beads/store"
)

func newInitCmd() *cobra.Command {
	var dsn, prefix string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a beads database in the current directory",
		RunE: func(cmd *cobra.Command, _ []string) error {
			cfg, err := config.Init(dsn, prefix)
			if err != nil {
				return err
			}
			st, err := store.OpenWithPrefix(context.Background(), cfg.DSN, cfg.Prefix)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer st.Close()
			fmt.Printf("initialized %s (driver=%s, prefix=%s)\n", cfg.DSN, st.Driver(), cfg.Prefix)
			fmt.Printf("config:    %s/config\n", cfg.BeadDir)
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "db", "", "DSN; default sqlite at .beads/beads.db. Examples:\n  --db postgres://user:pw@localhost/yuklar?sslmode=disable\n  --db sqlite:/tmp/test.db")
	cmd.Flags().StringVar(&prefix, "prefix", "", "id prefix (default: derived from DSN database name, falls back to 'bd')")
	return cmd
}
