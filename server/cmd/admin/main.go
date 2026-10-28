// Command admin runs one-off operations against DATABASE_URL
// (docs/RUNBOOK.md):
//
//	go run ./cmd/admin rescore -gw 7 [-league lg_...]   rescore a gameweek from the stored stats
//	go run ./cmd/admin prune-snapshots [-keep 50]       drop old raw stat snapshots (final ones stay)
//	go run ./cmd/admin tick                             do every due job once (draft clocks, waivers, trades)
//	go run ./cmd/admin run-waivers -league lg_... -gw 9 process a gameweek's waiver claims now
//	go run ./cmd/admin session -email a@b.test          print a signed-in session (local testing; -existing refuses unknown emails)
//	go run ./cmd/admin fetch-history [-all]             fetch players' previous seasons (FPL history_past)
//	go run ./cmd/admin shadow-check -league lg_...      compare an imported league with FPL now (read-only)
//
// rescore reports matchups.updated and score.corrected through the
// realtime log, so connected clients of running API instances hear about
// corrections. Every command is idempotent.
package main

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/history"
	"github.com/aatrey56/myfantasyprem/server/internal/httpapi"
	"github.com/aatrey56/myfantasyprem/server/internal/jobs"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications/webpush"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/realtime"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := run(ctx, os.Args[1:], os.Getenv, os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

const usage = `usage: admin <command> [flags]
  rescore -gw N [-league ID]   rescore gameweek N (all in-season leagues, or one)
  prune-snapshots [-keep N]    keep the newest N raw snapshots per kind and gameweek (default 50)
  tick                         do every due job once
  run-waivers -league ID -gw N process gameweek N's waiver claims now
  session -email E [-name N] [-existing]  print a signed-in session as the app stores it (local testing)
  fetch-history [-all]         fetch previous seasons of players new this season (-all: every player)
  shadow-check -league ID      compare an imported league with FPL Draft now (read-only; stores the report)
  vapid-keys                   print a new Web Push (VAPID) key pair for the API's env`

func run(ctx context.Context, args []string, getenv func(string) string, out, errOut io.Writer) error {
	if len(args) == 0 {
		return errors.New(usage)
	}
	if args[0] == "vapid-keys" {
		pub, priv, err := webpush.GenerateVAPID()
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "VAPID_PUBLIC_KEY=%s\nVAPID_PRIVATE_KEY=%s\nVAPID_SUBJECT=mailto:you@example.com\n", pub, priv)
		return nil
	}
	url := getenv("DATABASE_URL")
	if url == "" {
		return errors.New("DATABASE_URL is not set")
	}
	log := slog.New(slog.NewTextHandler(errOut, nil))
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	fs.SetOutput(errOut)
	gw := fs.Int("gw", 0, "gameweek (1-38)")
	leagueID := fs.String("league", "", "league id (lg_...)")
	keep := fs.Int("keep", 50, "snapshots to keep per kind and gameweek")
	email := fs.String("email", "", "account email (session)")
	name := fs.String("name", "", "display name for a new account (session)")
	existing := fs.Bool("existing", false, "refuse to create an account for an unknown email (session)")
	all := fs.Bool("all", false, "refetch every player (fetch-history)")
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	pool, err := store.Open(ctx, url)
	if err != nil {
		return err
	}
	defer pool.Close()
	leagues, st := league.NewPostgres(pool), stats.NewPostgres(pool)
	// The API's renderer publishes realtime events to the shared log;
	// running instances deliver them through LISTEN/NOTIFY.
	api := httpapi.NewAPI(httpapi.Options{
		Logger: log, Auth: auth.NewPostgres(pool, time.Now), Leagues: leagues, Stats: st,
		Realtime: realtime.NewPostgres(pool, time.Now), RealtimeListen: true,
		Notifications: notifications.NewPostgres(pool, time.Now),
	})
	runner := &jobs.Runner{Leagues: leagues, Stats: st, Now: time.Now, Notify: api.Notifier, Log: log}

	switch args[0] {
	case "rescore":
		if *gw < 1 || *gw > 38 {
			return errors.New("rescore needs -gw between 1 and 38")
		}
		sc := &league.Scorer{Leagues: leagues, Stats: st, Rules: scoring.StandardLineup(), Log: log, Notify: api.Notifier}
		if *leagueID != "" {
			teams, fixes, err := sc.RescoreLeague(ctx, *leagueID, *gw)
			if err != nil {
				return err
			}
			fmt.Fprintf(out, "Rescored gameweek %d of %s: %d team scores changed, %d corrections.\n", *gw, *leagueID, len(teams), len(fixes))
			return nil
		}
		res, err := sc.Recompute(ctx, *gw, nil, true)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Rescored gameweek %d in %d leagues: %d team scores changed, %d corrections.\n",
			*gw, res.Leagues, len(res.ChangedTeams), len(res.Corrections))
		for _, c := range res.Corrections {
			fmt.Fprintf(out, "  %s %s: %d -> %d\n", c.LeagueID, c.TeamID, c.Previous, c.Points)
		}
	case "prune-snapshots":
		n, err := st.PruneSnapshots(ctx, *keep)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Deleted %d stat snapshots (kept the newest %d per kind and gameweek, and every final one).\n", n, *keep)
	case "tick":
		res, err := runner.Tick(ctx)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "Tick: %d drafts started, %d clocks settled, %d lineup locks, %d waiver runs, %d trades closed, %d errors.\n",
			res.DraftsStarted, res.ClocksSettled, res.LineupLocks, res.WaiverRuns, res.TradesClosed, res.Errors)
	case "session":
		// A signed-in session for local testing (the server Playwright
		// suite): anyone with DATABASE_URL could do this anyway.
		if *email == "" {
			return errors.New("session needs -email")
		}
		a := auth.NewPostgres(pool, time.Now)
		var u *auth.User
		var id string
		if err := pool.QueryRow(ctx, `SELECT id FROM users WHERE email = lower($1)`, *email).Scan(&id); err == nil {
			u, err = a.User(ctx, id)
			if err != nil {
				return err
			}
		} else if *existing {
			return fmt.Errorf("no account has the email %s (-existing: not creating one; check the spelling)", *email)
		} else if u, err = a.CreateUser(ctx, cmp.Or(*name, strings.Split(*email, "@")[0]), *email); err != nil {
			return err
		}
		tok, err := a.Issue(ctx, u.ID)
		if err != nil {
			return err
		}
		providers := u.Providers
		if providers == nil {
			providers = []string{}
		}
		// The app's stored-session shape (apps/app/src/auth/session.ts).
		return json.NewEncoder(out).Encode(map[string]any{
			"accessToken": tok.AccessToken, "refreshToken": tok.RefreshToken,
			"expiresAt": tok.AccessExpiresAt.UTC(), "refreshExpiresAt": tok.RefreshExpiresAt.UTC(),
			"user": map[string]any{"id": u.ID, "displayName": u.DisplayName, "email": u.Email,
				"authProviders": providers, "createdAt": u.CreatedAt.UTC()},
		})
	case "fetch-history":
		// POLLER_SOURCE=demo reads the embedded demo world; otherwise FPL,
		// one request at a time at the client's 2 a second.
		var src history.Source = fpl.New(fpl.Options{BaseURL: getenv("FPL_BASE_URL"), UserAgent: getenv("FPL_USER_AGENT"), Logger: log})
		providerName := "fpl"
		if getenv("POLLER_SOURCE") == "demo" {
			src, providerName = demo.Source(log), demo.ProviderName
		}
		f := &history.Fetcher{Source: src, Store: history.NewPostgres(pool), Now: time.Now, Log: log,
			Codes: history.CodesFrom(st, providerName)}
		since := history.SeasonStart(time.Now())
		if *all {
			since = time.Now().Add(time.Minute)
		}
		res, err := f.Run(ctx, since)
		if err != nil {
			return err
		}
		fmt.Fprint(out, historyLine(res))
	case "run-waivers":
		if *leagueID == "" || *gw < 1 || *gw > 38 {
			return errors.New("run-waivers needs -league and -gw")
		}
		ran, err := runner.Waivers().Run(ctx, *leagueID, *gw)
		if err != nil {
			return err
		}
		if !ran {
			fmt.Fprintf(out, "Gameweek %d's waivers had already run for %s (or the league is not in season).\n", *gw, *leagueID)
			return nil
		}
		fmt.Fprintf(out, "Processed gameweek %d's waivers for %s.\n", *gw, *leagueID)
	case "shadow-check":
		if *leagueID == "" {
			return errors.New("shadow-check needs -league")
		}
		// Reads FPL (FPL_BASE_URL overrides it) at the client's pace and
		// never changes the league; a mismatch notifies the commissioner.
		mir := &mirror.Service{
			Source:  fpl.New(fpl.Options{BaseURL: getenv("FPL_BASE_URL"), UserAgent: getenv("FPL_USER_AGENT"), Logger: log}),
			Leagues: leagues, Stats: st, Store: mirror.NewPostgresStore(pool), Notify: api.Notifier, Log: log,
			Alert: notifications.NewNotifier(notifications.NewPostgres(pool, time.Now)),
		}
		rep, err := mir.ShadowCheck(ctx, *leagueID, "admin")
		if err != nil {
			return err
		}
		printShadow(out, rep)
	default:
		return errors.New(usage)
	}
	return nil
}

// printShadow prints a shadow check's outcome and every item that is not ok.
func printShadow(out io.Writer, rep *mirror.ShadowReport) {
	counts := map[string]int{}
	for _, it := range rep.Items {
		counts[it.Status]++
	}
	fmt.Fprintf(out, "Shadow check (gameweek %d): %s. %d ok, %d mismatch, %d pending, %d unverified.\n", rep.Gameweek, rep.Status,
		counts[mirror.ItemOK], counts[mirror.ItemMismatch], counts[mirror.ItemPending], counts[mirror.ItemUnverified])
	for _, it := range rep.Items {
		if it.Status == mirror.ItemOK {
			continue
		}
		fmt.Fprintf(out, "  %-10s %s: ours %q, FPL %q", it.Status, it.Key, it.Ours, it.Theirs)
		if it.Note != "" {
			fmt.Fprintf(out, " (%s)", it.Note)
		}
		fmt.Fprintln(out)
	}
}

// historyLine is fetch-history's summary.
func historyLine(res history.Result) string {
	if res.Held > 0 {
		return fmt.Sprintf("History: %d players held until FPL relaunches the season (its game's first deadline is still %s); %d already current.\n",
			res.Held, res.FirstDeadline.UTC().Format("2006-01-02 15:04 UTC"), res.Skipped)
	}
	return fmt.Sprintf("History: %d players fetched, %d already current, %d with none upstream, %d failed.\n",
		res.Fetched, res.Skipped, res.Missing, res.Failed)
}
