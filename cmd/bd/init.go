package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/rsktash/beads/internal/config"
	"github.com/rsktash/beads/store"
)

func newInitCmd() *cobra.Command {
	var dsn, prefix, idMode string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Initialize a bd project (writes .bd/config + seeds DB config)",
		Long: `Creates .bd/config (which holds only the DSN) and connects to the database
to ensure the schema exists, then writes project settings into the DB config
table:

  issue_prefix    — required; the per-project bead-id prefix (e.g. "bd", "yuklar")
  issue_id_mode   — "hash" (default) or "counter"

The DSN may point at a local sqlite file or a remote postgres server.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if prefix == "" {
				prefix = os.Getenv(config.EnvPrefix)
			}
			if prefix == "" {
				return fmt.Errorf("--prefix is required (e.g. `bd init --prefix yuklar`)")
			}
			cfg, err := config.Init(dsn)
			if err != nil {
				return err
			}
			st, err := store.Open(context.Background(), cfg.DSN)
			if err != nil {
				return fmt.Errorf("open db: %w", err)
			}
			defer st.Close()
			if err := st.SetConfig(cmd.Context(), store.CfgIssuePrefix, prefix); err != nil {
				return fmt.Errorf("write issue_prefix: %w", err)
			}
			if idMode != "" {
				if idMode != store.IDModeHash && idMode != store.IDModeCounter {
					return fmt.Errorf("--id-mode must be hash|counter, got %q", idMode)
				}
				if err := st.SetConfig(cmd.Context(), store.CfgIssueIDMode, idMode); err != nil {
					return fmt.Errorf("write issue_id_mode: %w", err)
				}
			}
			fmt.Printf("initialized %s (driver=%s)\n", cfg.DSN, st.Driver())
			fmt.Printf("config:    %s/config\n", cfg.BeadDir)
			fmt.Printf("prefix:    %s\n", prefix)
			if idMode != "" {
				fmt.Printf("id_mode:   %s\n", idMode)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&dsn, "db", "", "DSN. Default: sqlite at .bd/bd.db. Examples:\n  --db postgres://user:pw@localhost/yuklar?sslmode=disable\n  --db sqlite:/tmp/test.db")
	cmd.Flags().StringVar(&prefix, "prefix", "", "id prefix (REQUIRED, also via $BD_PREFIX)")
	cmd.Flags().StringVar(&idMode, "id-mode", "", "id allocation mode: hash (default) or counter")
	return cmd
}
