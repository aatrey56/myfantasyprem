// Command migrate applies or inspects the database migrations
// (server/internal/store/migrations) on DATABASE_URL.
//
//	go run ./cmd/migrate up             # apply every pending migration
//	go run ./cmd/migrate status         # list migrations and whether they ran
//	go run ./cmd/migrate down -to 0     # roll back to a version (0 = empty)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "migrate:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is not set")
	}
	cmd := "up"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "up":
		if err := store.Migrate(ctx, url); err != nil {
			return err
		}
		return store.MigrationStatus(ctx, url, os.Stdout)
	case "status":
		return store.MigrationStatus(ctx, url, os.Stdout)
	case "down":
		fs := flag.NewFlagSet("down", flag.ContinueOnError)
		to := fs.Int64("to", -1, "version to roll back to (0 = empty database)")
		if err := fs.Parse(args); err != nil {
			return err
		}
		if *to < 0 {
			return errors.New("down needs -to <version>, for example -to 0")
		}
		if err := store.MigrateDown(ctx, url, *to); err != nil {
			return err
		}
		return store.MigrationStatus(ctx, url, os.Stdout)
	}
	return fmt.Errorf("unknown command %q (want up, status or down)", cmd)
}
