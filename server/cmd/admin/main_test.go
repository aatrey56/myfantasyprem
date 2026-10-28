package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/history"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

func TestAdminNeedsACommandAndADatabase(t *testing.T) {
	if err := run(context.Background(), nil, func(string) string { return "" }, io.Discard, io.Discard); err == nil {
		t.Fatal("no command accepted")
	}
	if err := run(context.Background(), []string{"tick"}, func(string) string { return "" }, io.Discard, io.Discard); err == nil {
		t.Fatal("no DATABASE_URL accepted")
	}
}

func TestAdminCommands(t *testing.T) {
	pool, url := storetest.NewWithURL(t)
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	seeded, err := demo.Seed(ctx, demo.Deps{
		Auth: auth.NewPostgres(pool, time.Now), Leagues: league.NewService(league.NewPostgres(pool), time.Now),
		Stats: stats.NewPostgres(pool), Log: log,
	})
	if err != nil {
		t.Fatal(err)
	}
	env := func(k string) string {
		if k == "DATABASE_URL" {
			return url
		}
		return ""
	}
	admin := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := run(ctx, args, env, &out, io.Discard); err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		return out.String()
	}
	if out := admin("rescore", "-gw", "3"); !strings.Contains(out, "Rescored gameweek 3 in 1 leagues: 0 team scores changed, 0 corrections") {
		t.Fatalf("rescore = %q", out)
	}
	if out := admin("rescore", "-gw", "3", "-league", seeded.LeagueID); !strings.Contains(out, "0 team scores changed") {
		t.Fatalf("rescore one league = %q", out)
	}
	if out := admin("prune-snapshots", "-keep", "1"); !strings.Contains(out, "Deleted") {
		t.Fatalf("prune = %q", out)
	}
	if out := admin("tick"); !strings.HasPrefix(out, "Tick: ") || !strings.Contains(out, "0 errors") {
		t.Fatalf("tick = %q", out)
	}
	if out := admin("run-waivers", "-league", seeded.LeagueID, "-gw", "9"); !strings.Contains(out, "Processed gameweek 9") {
		t.Fatalf("run-waivers = %q", out)
	}
	if out := admin("run-waivers", "-league", seeded.LeagueID, "-gw", "9"); !strings.Contains(out, "already run") {
		t.Fatalf("run-waivers twice = %q", out)
	}
	out := admin("session", "-email", demo.DemoEmail)
	if !strings.Contains(out, `"accessToken":"`) || !strings.Contains(out, `"displayName":"Sam Parker"`) {
		t.Fatalf("session = %q", out)
	}
	if out := admin("session", "-email", "new@example.com", "-name", "New"); !strings.Contains(out, `"displayName":"New"`) {
		t.Fatalf("new session = %q", out)
	}
	// -existing never creates an account for a mistyped email.
	var sink bytes.Buffer
	if err := run(ctx, []string{"session", "-email", "typo@example.com", "-existing"}, env, &sink, io.Discard); err == nil || sink.Len() != 0 {
		t.Fatalf("-existing created or signed in an unknown email: %v %q", err, sink.String())
	}
	if out := admin("session", "-email", demo.DemoEmail, "-existing"); !strings.Contains(out, `"accessToken":"`) {
		t.Fatalf("existing session = %q", out)
	}
	// fetch-history on the demo world: 300 players, 3 with recorded seasons
	// and 9000068 with none (demo.HistoryCodes); the other 296 have no
	// element-summary file (404). A code FPL no longer has counts as checked
	// for the season, so a second run skips all 300.
	demoEnv := func(k string) string {
		if k == "POLLER_SOURCE" {
			return "demo"
		}
		return env(k)
	}
	fetch := func(args ...string) string {
		t.Helper()
		var out bytes.Buffer
		if err := run(ctx, append([]string{"fetch-history"}, args...), demoEnv, &out, io.Discard); err != nil {
			t.Fatal(err)
		}
		return out.String()
	}
	if out := fetch(); !strings.Contains(out, "4 players fetched, 0 already current, 296 with none upstream, 0 failed") {
		t.Fatalf("fetch-history = %q", out)
	}
	if out := fetch(); !strings.Contains(out, "0 players fetched, 300 already current, 0 with none upstream, 0 failed") {
		t.Fatalf("fetch-history again = %q", out)
	}
	if out := fetch("-all"); !strings.Contains(out, "4 players fetched, 0 already current") {
		t.Fatalf("fetch-history -all = %q", out)
	}
	var seasons int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM player_seasons`).Scan(&seasons); err != nil || seasons != 13+6+3 {
		// 113: 2013/14..2025/26 = 13; 8: 2019/20..2025/26 minus 2021/22 = 6; 73: 3.
		t.Fatalf("player_seasons = %d, %v", seasons, err)
	}
	if err := run(ctx, []string{"rescore"}, env, io.Discard, io.Discard); err == nil {
		t.Fatal("rescore without -gw accepted")
	}
	// shadow-check refuses a league made here before reaching FPL.
	if err := run(ctx, []string{"shadow-check"}, env, io.Discard, io.Discard); err == nil {
		t.Fatal("shadow-check without -league accepted")
	}
	if err := run(ctx, []string{"shadow-check", "-league", seeded.LeagueID}, env, io.Discard, io.Discard); err == nil ||
		!strings.Contains(err.Error(), "not imported") && !strings.Contains(err.Error(), "created here") {
		t.Fatalf("shadow-check of a native league: %v", err)
	}
}

func TestPrintShadow(t *testing.T) {
	var out bytes.Buffer
	printShadow(&out, &mirror.ShadowReport{Gameweek: 9, Status: mirror.ReportMismatch, Items: []mirror.Item{
		{Key: "points:gw9:tm_a", Status: mirror.ItemOK},
		{Key: "player_points:gw9:plr_a", Status: mirror.ItemMismatch, Ours: "2", Theirs: "3", Note: "Saka: ours 2, FPL 3."},
		{Key: "points:gw9:tm_b", Status: mirror.ItemPending, Ours: "40", Theirs: "38"},
	}})
	want := "Shadow check (gameweek 9): mismatch. 1 ok, 1 mismatch, 1 pending, 0 unverified.\n" +
		"  mismatch   player_points:gw9:plr_a: ours \"2\", FPL \"3\" (Saka: ours 2, FPL 3.)\n" +
		"  pending    points:gw9:tm_b: ours \"40\", FPL \"38\"\n"
	if out.String() != want {
		t.Errorf("got\n%s\nwant\n%s", out.String(), want)
	}
}

func TestVAPIDKeys(t *testing.T) {
	var out bytes.Buffer
	if err := run(context.Background(), []string{"vapid-keys"}, func(string) string { return "" }, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "VAPID_PUBLIC_KEY=B") || !strings.HasPrefix(lines[1], "VAPID_PRIVATE_KEY=") {
		t.Fatalf("output %q", out.String())
	}
}

// A batch held for FPL's relaunch says so, not "0 fetched, N current".
func TestHistoryLine(t *testing.T) {
	held := history.Result{Skipped: 1, Held: 299, FirstDeadline: time.Date(2026, 8, 14, 17, 30, 0, 0, time.UTC)}
	want := "History: 299 players held until FPL relaunches the season (its game's first deadline is still 2026-08-14 17:30 UTC); 1 already current.\n"
	if got := historyLine(held); got != want {
		t.Errorf("held = %q, want %q", got, want)
	}
	done := history.Result{Fetched: 2, Skipped: 3, Missing: 1}
	if got := historyLine(done); got != "History: 2 players fetched, 3 already current, 1 with none upstream, 0 failed.\n" {
		t.Errorf("done = %q", got)
	}
}
