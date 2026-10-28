package store_test

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"

	"github.com/aatrey56/myfantasyprem/server/internal/store"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// The migration applies, rolls back to empty and applies again, and the
// database enforces the rules the architecture puts on it.
func TestMigrationsRoundTripAndConstraints(t *testing.T) {
	pool, url := storetest.NewWithURL(t)
	ctx := context.Background()

	if err := store.MigrateDown(ctx, url, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}

	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	mustExec(`INSERT INTO seasons (id) VALUES ('2026-27')`)
	mustExec(`INSERT INTO clubs (id, season, code, name) VALUES ('clb_1', '2026-27', 'NOR', 'Northgate')`)
	mustExec(`INSERT INTO players (id, display_name, position, club_id, availability, draft_rank)
	          VALUES ('plr_01k7m2xq8v00000000000000a1', 'R. Calder', 'MID', 'clb_1', 'available', 1)`)
	mustExec(`INSERT INTO leagues (id, name, season, status) VALUES ('lg_01k7m2xq8rfv3c9a4t6wbn5hdy', 'L', '2026-27', 'in_season')`)
	for _, tm := range []string{"tm_01k7m2xq8s0000000000000001", "tm_01k7m2xq8s0000000000000002"} {
		mustExec(`INSERT INTO teams (id, league_id, name, joined_at) VALUES ($1, 'lg_01k7m2xq8rfv3c9a4t6wbn5hdy', 'T', now())`, tm)
	}

	// One owner per player per gameweek: [1, 5) and [5, ∞) are fine...
	mustExec(`INSERT INTO roster_slots (league_id, team_id, player_id, from_gw, to_gw, acquired_via)
	          VALUES ('lg_01k7m2xq8rfv3c9a4t6wbn5hdy', 'tm_01k7m2xq8s0000000000000001', 'plr_01k7m2xq8v00000000000000a1', 1, 5, 'draft')`)
	mustExec(`INSERT INTO roster_slots (league_id, team_id, player_id, from_gw, acquired_via)
	          VALUES ('lg_01k7m2xq8rfv3c9a4t6wbn5hdy', 'tm_01k7m2xq8s0000000000000002', 'plr_01k7m2xq8v00000000000000a1', 5, 'trade')`)
	// ...but a second open-ended slot overlaps.
	_, err := pool.Exec(ctx, `INSERT INTO roster_slots (league_id, team_id, player_id, from_gw, acquired_via)
	          VALUES ('lg_01k7m2xq8rfv3c9a4t6wbn5hdy', 'tm_01k7m2xq8s0000000000000001', 'plr_01k7m2xq8v00000000000000a1', 7, 'waiver')`)
	if !store.IsExclusionViolation(err) {
		t.Fatalf("overlapping roster slot: err = %v, want exclusion violation", err)
	}

	// ADR 0006: a NULL season does not let the same external id map twice.
	mustExec(`INSERT INTO player_external_id (player_id, provider, external_id, match_method, confidence)
	          VALUES ('plr_01k7m2xq8v00000000000000a1', 'fpl', '223094', 'exact_key', 1)`)
	_, err = pool.Exec(ctx, `INSERT INTO player_external_id (player_id, provider, external_id, match_method, confidence)
	          VALUES ('plr_01k7m2xq8v00000000000000a1', 'fpl', '223094', 'exact_key', 1)`)
	if !store.IsUniqueViolation(err) {
		t.Fatalf("duplicate external id: err = %v, want unique violation", err)
	}

	// Public id patterns are enforced.
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, display_name) VALUES ('usr_1', 'x')`); err == nil {
		t.Fatal("malformed user id accepted")
	}
}

// ADR 0009: cmd/migrate, cmd/seed and cmd/api may all migrate at once.
// Concurrent migrators on an empty database must serialise on a lock, so
// every one succeeds and the schema is applied once. The race is timing
// dependent, so several fresh databases are raced.
func TestConcurrentMigrationsSerialise(t *testing.T) {
	admin := storetest.URL(t)
	ctx := context.Background()
	exec := func(sql string) {
		t.Helper()
		conn, err := pgx.Connect(ctx, admin)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, sql); err != nil {
			t.Fatal(err)
		}
	}
	const rounds, n = 10, 8
	for round := range rounds {
		name := "mfp_mig_" + strings.ToLower(ids.ULID())
		exec("CREATE DATABASE " + name)
		t.Cleanup(func() { exec("DROP DATABASE IF EXISTS " + name + " WITH (FORCE)") })
		u, err := url.Parse(admin)
		if err != nil {
			t.Fatal(err)
		}
		u.Path = "/" + name

		errs := make(chan error, n)
		start := make(chan struct{})
		for range n {
			go func() {
				<-start
				errs <- store.Migrate(ctx, u.String())
			}()
		}
		close(start)
		for range n {
			if err := <-errs; err != nil {
				t.Errorf("round %d: concurrent migrate: %v", round, err)
			}
		}
		var buf bytes.Buffer
		if err := store.MigrationStatus(ctx, u.String(), &buf); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(buf.String(), "pending") {
			t.Errorf("round %d: migrations left pending:\n%s", round, buf.String())
		}
	}
}

// Migration 8 gives a league drafted mid-season before the draft recorded
// its first gameweek a startGameweek of its first matchup's gameweek, and
// leaves a league that started at gameweek 1 alone.
func TestMigrationRecordsLeagueFirstGameweek(t *testing.T) {
	pool, url := storetest.NewWithURL(t)
	ctx := context.Background()
	if err := store.MigrateDown(ctx, url, 7); err != nil {
		t.Fatal(err)
	}
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	mustExec(`INSERT INTO seasons (id) VALUES ('2026-27')`)
	leagues := map[string]int{"lg_01k7m2xq8rfv3c9a4t6wbn5hd1": 8, "lg_01k7m2xq8rfv3c9a4t6wbn5hd2": 1}
	n := 0
	for id, first := range leagues {
		n++
		team := fmt.Sprintf("tm_01k7m2xq8s000000000000000%d", n)
		mustExec(`INSERT INTO leagues (id, name, season, status) VALUES ($1, 'L', '2026-27', 'in_season')`, id)
		mustExec(`INSERT INTO teams (id, league_id, name, joined_at) VALUES ($1, $2, 'T', now())`, team, id)
		// (waiverOrder set, so migration 13 adds no version of its own.)
		mustExec(`INSERT INTO league_settings (league_id, version, settings) VALUES ($1, 1, '{"teamCount": 4, "startGameweek": 1, "endGameweek": 38, "waiverOrder": "rolling"}')`, id)
		for gw := first; gw <= first+1; gw++ {
			mustExec(`INSERT INTO matchups (id, league_id, gameweek, home_team_id) VALUES ($1, $2, $3, $4)`,
				fmt.Sprintf("mu_01k7m2xq8s00000000000000%d%d", n, gw%10), id, gw, team)
		}
	}
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	for id, first := range leagues {
		var start, count int
		if err := pool.QueryRow(ctx, `SELECT (settings ->> 'startGameweek')::int FROM league_settings
		    WHERE league_id = $1 ORDER BY version DESC LIMIT 1`, id).Scan(&start); err != nil {
			t.Fatal(err)
		}
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM league_settings WHERE league_id = $1`, id).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if start != first {
			t.Errorf("%s: startGameweek %d, want %d", id, start, first)
		}
		if want := map[bool]int{true: 2, false: 1}[first > 1]; count != want {
			t.Errorf("%s: %d settings versions, want %d", id, count, want)
		}
	}
}

// Migration 13 gives existing imported leagues FPL's reverse-standings
// waiver order and existing native leagues the rolling order they were
// made with, each in a new settings version; a league that already has
// the setting is left alone.
func TestMigrationSetsWaiverOrder(t *testing.T) {
	pool, url := storetest.NewWithURL(t)
	ctx := context.Background()
	if err := store.MigrateDown(ctx, url, 12); err != nil {
		t.Fatal(err)
	}
	mustExec := func(sql string, args ...any) {
		t.Helper()
		if _, err := pool.Exec(ctx, sql, args...); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	mustExec(`INSERT INTO seasons (id) VALUES ('2026-27')`)
	type want struct {
		order    string
		versions int
	}
	leagues := map[string]struct {
		source, settings string
		want             want
	}{
		"lg_01k7m2xq8rfv3c9a4t6wbn5hd1": {"native", `{"teamCount": 4}`, want{"rolling", 2}},
		"lg_01k7m2xq8rfv3c9a4t6wbn5hd2": {"fpl_import", `{"teamCount": 12}`, want{"reverse_standings", 2}},
		"lg_01k7m2xq8rfv3c9a4t6wbn5hd3": {"native", `{"teamCount": 4, "waiverOrder": "reverse_standings"}`, want{"reverse_standings", 1}},
	}
	for id, l := range leagues {
		mustExec(`INSERT INTO leagues (id, name, season, status, source) VALUES ($1, 'L', '2026-27', 'in_season', $2)`, id, l.source)
		mustExec(`INSERT INTO league_settings (league_id, version, settings) VALUES ($1, 1, $2)`, id, l.settings)
	}
	if err := store.Migrate(ctx, url); err != nil {
		t.Fatal(err)
	}
	for id, l := range leagues {
		var got want
		if err := pool.QueryRow(ctx, `SELECT settings ->> 'waiverOrder', (SELECT count(*) FROM league_settings WHERE league_id = $1)
		    FROM league_settings WHERE league_id = $1 ORDER BY version DESC LIMIT 1`, id).Scan(&got.order, &got.versions); err != nil {
			t.Fatal(err)
		}
		if got != l.want {
			t.Errorf("%s league: %+v, want %+v", l.source, got, l.want)
		}
	}
}
