package mirror_test

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// bareEnvs runs test on the memory stores and, with TEST_DATABASE_URL, on
// Postgres, with the real league upstream and no stats ingested.
func bareEnvs(t *testing.T, test func(t *testing.T, e *env)) {
	run := func(t *testing.T, e *env) {
		clock := realNow
		e.clock = &clock
		e.src = fpl.New(fpl.Options{Transport: fpl.DirTransport{FS: os.DirFS(realDir)}, Logger: quiet(), MinInterval: -1})
		e.svc = &mirror.Service{Source: e.src, Leagues: e.leagues, Stats: e.stats, Store: newStore(e), Now: e.now, Log: quiet()}
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

// A server whose stats come from the demo world (POLLER_SOURCE=demo) can
// never import an FPL league: the preview says so before a team is
// picked, and the import is refused rather than queued to fail.
func TestImportOnDemoStatsIsUnavailable(t *testing.T) {
	bareEnvs(t, func(t *testing.T, e *env) {
		if _, err := demo.LoadStats(ctx, e.stats, quiet(), e.now); err != nil {
			t.Fatal(err)
		}
		owner := e.user(t, "Importer")
		p, err := e.svc.Preview(ctx, owner, realLeague)
		if err != nil {
			t.Fatal(err)
		}
		if p.Problem == nil || p.Problem.Slug != "import-unavailable" {
			t.Fatalf("preview problem = %+v, want import-unavailable", p.Problem)
		}
		if !strings.Contains(p.Problem.Detail, "isn't connected to live player data") {
			t.Fatalf("detail = %q", p.Problem.Detail)
		}
		if len(p.Seats) == 0 || p.Name == "" {
			t.Fatalf("the preview still names the league: %+v", p)
		}
		if _, _, err := e.svc.StartImport(ctx, owner, realLeague, 1); slug(err) != "import-unavailable" {
			t.Fatalf("start import err = %v, want import-unavailable", err)
		}
		// An input error still comes first.
		if _, _, err := e.svc.StartImport(ctx, owner, realLeague, 99); slug(err) != "invalid-request" {
			t.Fatalf("seat 99 err = %v, want invalid-request", err)
		}
	})
}

// On FPL's source before anything is ingested, the preview says the
// player data is still loading; an import already asked for fails saying
// the same (not a generic failure).
func TestImportBeforeStatsIngestedIsStillLoading(t *testing.T) {
	bareEnvs(t, func(t *testing.T, e *env) {
		owner := e.user(t, "Importer")
		p, err := e.svc.Preview(ctx, owner, realLeague)
		if err != nil {
			t.Fatal(err)
		}
		if p.Problem == nil || p.Problem.Slug != "stats-not-ready" {
			t.Fatalf("preview problem = %+v, want stats-not-ready", p.Problem)
		}
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
		if imp.Status != mirror.ImportFailed || imp.Error == nil || imp.Error.Slug != "stats-not-ready" ||
			imp.Error.Detail != "Player data is still loading. Try again in a few minutes." {
			t.Fatalf("import = %+v (error %+v)", imp, imp.Error)
		}
	})
}
