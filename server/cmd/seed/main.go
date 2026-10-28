// Command seed loads the demo world (internal/demo: fictional clubs,
// players and gameweeks 1-7 in FPL's JSON shapes) into DATABASE_URL and
// creates the demo league, so the app runs against the real server
// without touching FPL. It applies pending migrations first and is safe
// to run again.
//
//	DATABASE_URL=postgres://... go run ./cmd/seed
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/demo/midseason"
	"github.com/aatrey56/myfantasyprem/server/internal/history"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "seed:", err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is not set")
	}
	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := store.Migrate(ctx, url); err != nil {
		return err
	}
	pool, err := store.Open(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	deps := demo.Deps{
		Auth:    auth.NewPostgres(pool, time.Now),
		Leagues: league.NewService(league.NewPostgres(pool), time.Now),
		Stats:   stats.NewPostgres(pool),
		History: history.NewPostgres(pool),
		Log:     log,
	}
	res, err := demo.Seed(ctx, deps)
	if err != nil {
		return err
	}
	late, err := midseason.Seed(ctx, deps, res.UserIDs)
	if err != nil {
		return err
	}
	fmt.Printf("Seeded %q (%s) at gameweek %d with %d managers.\n", demo.LeagueName, res.LeagueID, res.Gameweek, len(res.UserIDs))
	fmt.Printf("Seeded %q (%s), drafted mid-season: it starts at gameweek 8.\n", midseason.LeagueName, late)
	fmt.Printf("Sign in as %s: request a magic link in the app; with AUTH_DEV_MAGIC_LINKS=true the server logs it.\n", demo.DemoEmail)
	return nil
}
