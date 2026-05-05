package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/rustamsmax/beads/internal/config"
	"github.com/rustamsmax/beads/internal/storage"
)

var (
	flagDB   string
	flagJSON bool
)

type cmdCtx struct {
	ctx   context.Context
	store *storage.Store
	json  bool
}

func newRoot() *cobra.Command {
	root := &cobra.Command{
		Use:           "bd",
		Short:         "Beads — graph-based issue tracker for AI agents",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.PersistentFlags().StringVar(&flagDB, "db", "", "DSN (sqlite path or postgres://...). Defaults to .beads/beads.db or $BEADS_DB.")
	root.PersistentFlags().BoolVarP(&flagJSON, "json", "j", false, "JSON output")

	root.AddCommand(
		newInitCmd(),
		newCreateCmd(),
		newListCmd(),
		newShowCmd(),
		newUpdateCmd(),
		newCloseCmd(),
		newReadyCmd(),
		newDepCmd(),
		newDeleteCmd(),
	)
	return root
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	root := newRoot()
	root.SetContext(ctx)
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// openStore is the shared command setup. Init does NOT call it.
func openStore(cmd *cobra.Command) (*cmdCtx, error) {
	cfg, err := config.Resolve(flagDB)
	if err != nil {
		return nil, err
	}
	st, err := storage.Open(cmd.Context(), cfg.DSN)
	if err != nil {
		return nil, err
	}
	return &cmdCtx{ctx: cmd.Context(), store: st, json: flagJSON}, nil
}
