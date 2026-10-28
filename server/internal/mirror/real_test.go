package mirror_test

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/poller"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// The real league, captured once on 14 October 2026 (gameweek 6
// finished, 7 next) and anonymised (provider/fpl testdata/real-gw6), is
// imported and verified with our engines against FPL's own numbers. The
// capture has live stats for every finished gameweek (1-6), so every
// item is checked: zero mismatches and nothing unverified.

const realDir = "../provider/fpl/testdata/real-gw6"

const realLeague = 90001

// Wednesday 14 October 2026, 12:00 UTC.
var realNow = time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)

var ctx = context.Background()

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

type env struct {
	name    string
	clock   *time.Time
	stats   stats.Repo
	leagues league.Repo
	auth    auth.Service
	svc     *mirror.Service
	src     *fpl.Client
	pool    *pgxpool.Pool
}

func newStore(e *env) mirror.Store {
	if e.pool == nil {
		return mirror.NewMemoryStore()
	}
	return mirror.NewPostgresStore(e.pool)
}

func (e *env) now() time.Time { return *e.clock }

// noRescore: no league exists while the poller ingests.
type noRescore struct{}

func (noRescore) Recompute(context.Context, int, []string, bool) (league.RecomputeResult, error) {
	return league.RecomputeResult{}, nil
}

func (e *env) user(t *testing.T, name string) string {
	t.Helper()
	u, err := e.auth.CreateUser(ctx, name, fmt.Sprintf("%s-%d@example.com", name, time.Now().UnixNano()))
	if err != nil {
		t.Fatal(err)
	}
	return u.ID
}

// realGameweeks are the finished gameweeks of the capture, each with its
// live stats.
const realGameweeks = 6

// realEnvs runs test on the memory stores and, with TEST_DATABASE_URL, on
// Postgres, each with the real bootstrap and the live stats of gameweeks
// 1-6 ingested by the poller, as in production.
func realEnvs(t *testing.T, test func(t *testing.T, e *env)) {
	realEnvsFS(t, os.DirFS(realDir), test)
}

// realEnvsFS is realEnvs with the capture served from fsys (a patched
// capture).
func realEnvsFS(t *testing.T, fsys fs.FS, test func(t *testing.T, e *env)) {
	run := func(t *testing.T, e *env) {
		clock := realNow
		e.clock = &clock
		e.src = fpl.New(fpl.Options{Transport: fpl.DirTransport{FS: fsys}, Logger: quiet(), MinInterval: -1})
		p := poller.New(e.src, stats.NewIngester(e.stats, "fpl", e.now), e.stats, noRescore{}, poller.Config{}, quiet())
		p.Now = e.now
		p.Sleep = func(context.Context, time.Duration) error { return nil }
		for range 3 {
			p.Tick(ctx)
		}
		for gw := 1; gw <= realGameweeks; gw++ {
			_, hidden := fs.Stat(fsys, "event/"+strconv.Itoa(gw)+"/live.json")
			if rows, _ := e.stats.PlayerFixtures(ctx, gw); len(rows) == 0 && hidden == nil {
				t.Fatalf("gameweek %d not ingested", gw)
			}
		}
		e.svc = &mirror.Service{
			Source: e.src, Leagues: e.leagues, Stats: e.stats, Store: newStore(e), Now: e.now, Log: quiet(),
		}
		test(t, e)
	}
	t.Run("memory", func(t *testing.T) {
		run(t, &env{name: "memory", stats: stats.NewMemory(), leagues: league.NewMemory(), auth: auth.NewMemory(time.Now)})
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		run(t, &env{name: "postgres", stats: stats.NewPostgres(pool), leagues: league.NewPostgres(pool),
			auth: auth.NewPostgres(pool, time.Now), pool: pool})
	})
}

// importReal imports the real league with the importer in seat 1.
func importReal(t *testing.T, e *env) (string, *league.State, *mirror.Report) {
	t.Helper()
	owner := e.user(t, "Importer")
	imp, existing, err := e.svc.StartImport(ctx, owner, realLeague, 1)
	if err != nil || existing {
		t.Fatalf("start import: %v (existing %v)", err, existing)
	}
	if err := e.svc.RunImport(ctx, imp.ID); err != nil {
		t.Fatal(err)
	}
	imp, err = e.svc.Import(ctx, owner, imp.ID)
	if err != nil {
		t.Fatal(err)
	}
	if imp.Status != mirror.ImportSucceeded || imp.LeagueID == "" || imp.Done != imp.Total || imp.Total == 0 {
		t.Fatalf("import = %+v", imp)
	}
	st, err := e.leagues.State(ctx, imp.LeagueID)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := e.svc.Report(ctx, imp.LeagueID)
	if err != nil || rep == nil {
		t.Fatalf("report: %v %v", rep, err)
	}
	return owner, st, rep
}

func TestRealCaptureVerifiesWithZeroDiffs(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner, st, rep := importReal(t, e)
		l := st.League
		if l.Source != league.SourceFPL || l.FPLLeagueID != realLeague ||
			l.Status != league.StatusInSeason || l.CommissionerID != owner || l.Name != "Fixture Draft League" || len(l.Teams) != 12 {
			t.Fatalf("league = %+v", l)
		}
		if s := l.Settings; s.TeamCount != 12 || s.TradeReview != league.ReviewCommissioner || s.PickTimerSeconds != 120 ||
			s.StartGameweek != 1 || s.EndGameweek != 38 {
			t.Errorf("settings = %+v", s)
		}

		byKind := map[string]map[string]int{}
		for _, it := range rep.Items {
			if byKind[it.Kind] == nil {
				byKind[it.Kind] = map[string]int{}
			}
			byKind[it.Kind][it.Status]++
			if it.Status != mirror.ItemOK {
				t.Errorf("%s %s: ours %q, FPL %q %s", strings.ToUpper(it.Status), it.Key, it.Ours, it.Theirs, it.Note)
			}
		}
		for _, kind := range slices.Sorted(maps.Keys(byKind)) {
			t.Logf("%s: %-12s ok %3d  mismatch %d  unverified %d", e.name, kind,
				byKind[kind][mirror.ItemOK], byKind[kind][mirror.ItemMismatch], byKind[kind][mirror.ItemUnverified])
		}
		// Every finished gameweek checked, per kind and per gameweek.
		want := map[string]int{
			mirror.KindPoints: 12 * realGameweeks, mirror.KindSubs: 12 * realGameweeks, mirror.KindResult: 6 * realGameweeks,
			mirror.KindStanding: 12, mirror.KindTable: 12, mirror.KindSquad: 12 * realGameweeks,
			mirror.KindOwner: 1, mirror.KindWaiverOrder: 1,
		}
		for kind, n := range want {
			if got := byKind[kind][mirror.ItemOK]; got != n {
				t.Errorf("%s ok = %d, want %d", kind, got, n)
			}
		}
		perGW := map[int]int{}
		for _, it := range rep.Items {
			if it.Kind == mirror.KindPoints || it.Kind == mirror.KindSubs || it.Kind == mirror.KindResult {
				perGW[it.Gameweek]++
			}
		}
		for gw := 1; gw <= realGameweeks; gw++ {
			if perGW[gw] != 12+12+6 {
				t.Errorf("gameweek %d: %d points, subs and result items, want 30", gw, perGW[gw])
			}
		}
		if rep.Status != mirror.ReportClean {
			t.Errorf("report status %s, want clean", rep.Status)
		}

		// The imported data itself.
		if st.Draft == nil || st.Draft.Status != league.DraftComplete || len(st.Draft.Picks) != 180 || len(st.Draft.Order) != 12 {
			t.Fatalf("draft = %+v", st.Draft)
		}
		txns, err := e.leagues.Transactions(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		kinds := map[string]int{}
		for _, x := range txns {
			kinds[x.Kind]++
		}
		if kinds[league.TxnWaiver] != 177 || kinds[league.TxnFreeAgent] != 22 || kinds[league.TxnTrade] != 3 {
			t.Errorf("feed = %v", kinds)
		}
		trades, _ := e.leagues.Trades(ctx, l.ID)
		statuses := map[string]int{}
		for _, tr := range trades {
			statuses[tr.Status]++
		}
		if statuses[league.TradeProcessed] != 2 || statuses[league.TradeVetoed] != 1 {
			t.Errorf("trades = %v", statuses)
		}
		if len(st.Priority) != 12 {
			t.Errorf("priority = %v", st.Priority)
		}
		// Every seat but the importer's has a claim link.
		links := 0
		for _, tm := range l.Teams {
			if _, ok := l.ClaimLink(tm.ID); ok {
				links++
			} else if tm.OwnerID != owner {
				t.Errorf("seat %s has no claim link", tm.Name)
			}
		}
		if links != 11 {
			t.Errorf("%d claim links", links)
		}
	})
}

// A re-sync of the same FPL data changes nothing: no new rows, refs or
// feed entries, the same rosters and lineups.
func TestRealResyncIsIdempotent(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		_, before, _ := importReal(t, e)
		txBefore, _ := e.leagues.Transactions(ctx, before.League.ID)
		*e.clock = e.clock.Add(20 * time.Minute)
		if err := e.svc.Resync(ctx, before.League.ID); err != nil {
			t.Fatal(err)
		}
		after, err := e.leagues.State(ctx, before.League.ID)
		if err != nil {
			t.Fatal(err)
		}
		txAfter, _ := e.leagues.Transactions(ctx, before.League.ID)
		if len(txAfter) != len(txBefore) || len(after.Refs) != len(before.Refs) || len(after.Game.Matchups) != len(before.Game.Matchups) {
			t.Fatalf("feed %d -> %d, refs %d -> %d", len(txBefore), len(txAfter), len(before.Refs), len(after.Refs))
		}
		if !slices.EqualFunc(after.Game.Rosters, before.Game.Rosters, func(a, b league.RosterSlot) bool { return a == b }) {
			t.Error("rosters changed")
		}
		if len(after.Game.Lineups) != len(before.Game.Lineups) {
			t.Errorf("lineups %d -> %d", len(before.Game.Lineups), len(after.Game.Lineups))
		}
		for _, lu := range before.Game.Lineups {
			got, _ := after.Game.LineupAt(lu.TeamID, lu.FromGW)
			if !slices.Equal(got.Slots, lu.Slots) || !got.UpdatedAt.Equal(lu.UpdatedAt) {
				t.Errorf("lineup %s gw%d changed", lu.TeamID, lu.FromGW)
			}
		}
		if !slices.Equal(after.Priority, before.Priority) || after.League.Name != before.League.Name {
			t.Error("priority or name changed")
		}
		// The same user importing the same league again gets that import.
		owner := before.League.CommissionerID
		imp, existing, err := e.svc.StartImport(ctx, owner, realLeague, 1)
		if err != nil || !existing || imp.LeagueID != before.League.ID {
			t.Errorf("second import = %+v existing %v err %v", imp, existing, err)
		}
	})
}

// patchedFS serves the capture with some files changed or hidden.
type patchedFS struct {
	base  fs.FS
	patch map[string]func([]byte) []byte // nil: the file does not exist
}

func (p patchedFS) Open(name string) (fs.File, error) {
	if f, ok := p.patch[name]; ok && f == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return p.base.Open(name)
}

func (p patchedFS) ReadFile(name string) ([]byte, error) {
	f, ok := p.patch[name]
	if ok && f == nil {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	b, err := fs.ReadFile(p.base, name)
	if err != nil || !ok {
		return b, err
	}
	return f(b), nil
}

// withoutLive is the capture without the live stats of gameweeks gws, as
// if the poller had not ingested them yet.
func withoutLive(gws ...int) fs.FS {
	p := patchedFS{base: os.DirFS(realDir), patch: map[string]func([]byte) []byte{}}
	for _, gw := range gws {
		p.patch["event/"+strconv.Itoa(gw)+"/live.json"] = nil
	}
	return p
}

var _ provider.LeagueMirrorSource = (*fpl.Client)(nil)
