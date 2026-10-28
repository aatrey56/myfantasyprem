package history

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

var ctx = context.Background()

func ptr[T any](v T) *T { return &v }

// impls runs a test against Memory and, with TEST_DATABASE_URL, Postgres.
func impls(t *testing.T, test func(t *testing.T, s Store)) {
	t.Run("memory", func(t *testing.T) { test(t, NewMemory()) })
	t.Run("postgres", func(t *testing.T) { test(t, NewPostgres(storetest.New(t))) })
}

var t0 = time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

func TestSaveKeepsNullsAndOrdersNewestFirst(t *testing.T) {
	impls(t, func(t *testing.T, s Store) {
		old := Season{Season: "2015/16", TotalPoints: ptr(57), Minutes: ptr(1290), Assists: ptr(3)}
		recent := Season{Season: "2022/23", TotalPoints: ptr(69), Starts: ptr(23),
			ExpectedGoals: ptr(1.35), ICTIndex: ptr(103.0), DefensiveContribution: nil}
		if err := s.Save(ctx, "56979", []Season{old, recent}, t0); err != nil {
			t.Fatal(err)
		}
		got, err := s.Seasons(ctx, "56979")
		if err != nil || len(got) != 2 {
			t.Fatalf("got %+v, %v", got, err)
		}
		if got[0].Season != "2022/23" || got[1].Season != "2015/16" {
			t.Errorf("order %s, %s; want newest first", got[0].Season, got[1].Season)
		}
		if got[1].ICTIndex != nil || got[1].Starts != nil || got[1].ExpectedGoals != nil {
			t.Error("2015/16 untracked stats came back non-nil")
		}
		if *got[1].Assists != 3 || *got[0].Starts != 23 || *got[0].ExpectedGoals != 1.35 || got[0].DefensiveContribution != nil {
			t.Errorf("values %+v", got)
		}
		if got[0].PlayerExternalID != "56979" {
			t.Errorf("code %q", got[0].PlayerExternalID)
		}
		none, err := s.Seasons(ctx, "1")
		if err != nil || none == nil || len(none) != 0 {
			t.Errorf("unknown code: %v %v, want empty", none, err)
		}
	})
}

func TestSaveIsIdempotentAndUpdates(t *testing.T) {
	impls(t, func(t *testing.T, s Store) {
		a := Season{Season: "2025/26", TotalPoints: ptr(70)}
		for range 2 {
			if err := s.Save(ctx, "7", []Season{a}, t0); err != nil {
				t.Fatal(err)
			}
		}
		// FPL corrects a total: the row is replaced, not duplicated.
		a.TotalPoints = ptr(76)
		if err := s.Save(ctx, "7", []Season{a}, t0.Add(time.Hour)); err != nil {
			t.Fatal(err)
		}
		got, _ := s.Seasons(ctx, "7")
		if len(got) != 1 || *got[0].TotalPoints != 76 {
			t.Fatalf("got %+v", got)
		}
		// A player with no past seasons is still recorded as fetched.
		if err := s.Save(ctx, "8", nil, t0); err != nil {
			t.Fatal(err)
		}
		f, err := s.Fetched(ctx)
		if err != nil || len(f) != 2 || !f["7"].Equal(t0.Add(time.Hour)) || !f["8"].Equal(t0) {
			t.Fatalf("fetched %v %v", f, err)
		}
	})
}

type countingSource struct {
	inner Source
	calls []string
}

func (c *countingSource) SeasonHistory(ctx context.Context, code string) ([]provider.PlayerSeason, error) {
	c.calls = append(c.calls, code)
	return c.inner.SeasonHistory(ctx, code)
}

func TestFetcherFetchesNewPlayersAndOncePerSeason(t *testing.T) {
	client := fpl.New(fpl.Options{Transport: fpl.DirTransport{FS: os.DirFS("../provider/fpl/testdata")}, MinInterval: -1,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	src := &countingSource{inner: client}
	store := NewMemory()
	now := t0
	codes := []string{"1001", "1003"}
	f := &Fetcher{Source: src, Store: store, Now: func() time.Time { return now },
		Codes: func(context.Context) ([]string, error) { return codes, nil },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil))}

	since := SeasonStart(now) // 2026-07-01
	res, err := f.Run(ctx, since)
	if err != nil || res != (Result{Fetched: 2}) {
		t.Fatalf("first run %+v %v", res, err)
	}
	// Raya (1001) 5 seasons, Henderson (1003) 17 (testdata/element-summary).
	if r, _ := store.Seasons(ctx, "1001"); len(r) != 5 || r[0].Season != "2025/26" {
		t.Fatalf("1001: %d seasons", len(r))
	}
	if h, _ := store.Seasons(ctx, "1003"); len(h) != 17 {
		t.Fatalf("1003: %d seasons", len(h))
	}

	// Same season, a new player appears (1002 has no recorded summary: a
	// 404, counted as missing); the two known ones are skipped.
	codes = append(codes, "1002")
	now = t0.Add(time.Hour)
	res, err = f.Run(ctx, SeasonStart(now))
	if err != nil || res != (Result{Skipped: 2, Missing: 1}) {
		t.Fatalf("second run %+v %v", res, err)
	}
	// Next season (2027-07-01 onwards): every player again.
	src.calls = nil
	now = time.Date(2027, 7, 2, 0, 0, 0, 0, time.UTC)
	res, _ = f.Run(ctx, SeasonStart(now))
	if res != (Result{Fetched: 2, Missing: 1}) || len(src.calls) != 3 {
		t.Fatalf("new season %+v, calls %v", res, src.calls)
	}
}

func TestSeasonStart(t *testing.T) {
	for in, want := range map[time.Time]time.Time{
		time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC): time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 6, 30, 23, 0, 0, 0, time.UTC): time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC):   time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC),
	} {
		if got := SeasonStart(in); !got.Equal(want) {
			t.Errorf("SeasonStart(%v) = %v, want %v", in, got, want)
		}
	}
}

func TestFetcherStopsWhenContextEnds(t *testing.T) {
	c, cancel := context.WithCancel(ctx)
	cancel()
	f := &Fetcher{Source: errSource{}, Store: NewMemory(), Now: time.Now,
		Codes: func(context.Context) ([]string, error) { return []string{"1", "2"}, nil }}
	if _, err := f.Run(c, t0); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

type errSource struct{}

func (errSource) SeasonHistory(ctx context.Context, _ string) ([]provider.PlayerSeason, error) {
	return nil, ctx.Err()
}

type failSource struct{}

func (failSource) SeasonHistory(context.Context, string) ([]provider.PlayerSeason, error) {
	return nil, errors.New("boom")
}

func TestFetcherCountsFailuresAndGoesOn(t *testing.T) {
	store := NewMemory()
	f := &Fetcher{Source: failSource{}, Store: store, Now: time.Now, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Codes: func(context.Context) ([]string, error) { return []string{"1", "2"}, nil }}
	res, err := f.Run(ctx, t0)
	if err != nil || res != (Result{Failed: 2}) {
		t.Fatalf("%+v %v", res, err)
	}
	if done, _ := store.Fetched(ctx); len(done) != 0 {
		t.Fatalf("failed fetches recorded: %v", done)
	}
}

// refreshingSource records the order of RefreshIndex and SeasonHistory calls.
type refreshingSource struct{ calls []string }

func (r *refreshingSource) RefreshIndex(context.Context) error {
	r.calls = append(r.calls, "refresh")
	return nil
}

func (r *refreshingSource) SeasonHistory(_ context.Context, code string) ([]provider.PlayerSeason, error) {
	r.calls = append(r.calls, code)
	return nil, nil
}

// Element ids change at FPL's rollover: each batch starts from a fresh
// bootstrap index, and a run with nothing to fetch does not touch FPL.
func TestFetcherRefreshesIndexBeforeEachBatch(t *testing.T) {
	src := &refreshingSource{}
	now := t0
	f := &Fetcher{Source: src, Store: NewMemory(), Now: func() time.Time { return now },
		Codes: func(context.Context) ([]string, error) { return []string{"1", "2"}, nil },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil))}
	if _, err := f.Run(ctx, SeasonStart(now)); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(src.calls, []string{"refresh", "1", "2"}) {
		t.Fatalf("calls %v", src.calls)
	}
	src.calls = nil
	if _, err := f.Run(ctx, SeasonStart(now)); err != nil || len(src.calls) != 0 {
		t.Fatalf("second run calls %v, %v", src.calls, err)
	}
}

// A player gone from FPL's (just refreshed) bootstrap is recorded as
// fetched with no seasons, so it is not looked up again every run.
func TestFetcherRecordsPlayersGoneFromFPL(t *testing.T) {
	client := fpl.New(fpl.Options{Transport: fpl.DirTransport{FS: os.DirFS("../provider/fpl/testdata")}, MinInterval: -1,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	now := t0
	f := &Fetcher{Source: client, Store: NewMemory(), Now: func() time.Time { return now },
		Codes: func(context.Context) ([]string, error) { return []string{"999999"}, nil },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil))}
	if res, err := f.Run(ctx, SeasonStart(now)); err != nil || res != (Result{Missing: 1}) {
		t.Fatalf("first run %+v %v", res, err)
	}
	now = now.Add(time.Hour)
	if res, err := f.Run(ctx, SeasonStart(now)); err != nil || res != (Result{Skipped: 1}) {
		t.Fatalf("second run %+v %v", res, err)
	}
}

// gatedSource is a source whose new season goes live when live is set.
type gatedSource struct {
	refreshingSource
	live bool
}

// lastSeason is the first deadline of the game FPL serves before its
// relaunch: last season's.
var lastSeason = time.Date(2026, 8, 14, 17, 30, 0, 0, time.UTC)

func (g *gatedSource) SeasonLive(context.Context, time.Time) (bool, time.Time, error) {
	return g.live, lastSeason, nil
}

// On 2 July FPL still serves last season's game, so history_past lacks the
// season just finished: fetching then would save it incomplete and skip
// the player for a year. The yearly batch waits for the relaunch.
func TestFetcherWaitsForFPLsRelaunch(t *testing.T) {
	src := &gatedSource{}
	store := NewMemory()
	now := time.Date(2027, 7, 2, 0, 0, 0, 0, time.UTC)
	f := &Fetcher{Source: src, Store: store, Now: func() time.Time { return now },
		Codes: func(context.Context) ([]string, error) { return []string{"1", "2"}, nil },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil))}
	res, err := f.Run(ctx, SeasonStart(now))
	// Held, not "already current": both players wait for the relaunch.
	if err != nil || res != (Result{Held: 2, FirstDeadline: lastSeason}) || slices.Contains(src.calls, "1") {
		t.Fatalf("before relaunch %+v %v, calls %v", res, err, src.calls)
	}
	// 20 July: the new game is live; every player is fetched.
	src.live, src.calls = true, nil
	now = time.Date(2027, 7, 20, 0, 0, 0, 0, time.UTC)
	res, err = f.Run(ctx, SeasonStart(now))
	if err != nil || res != (Result{Fetched: 2}) {
		t.Fatalf("after relaunch %+v %v, calls %v", res, err, src.calls)
	}
}

// deadlineSource is live for any season starting on or before its first
// deadline, as fpl.Client.SeasonLive is.
type deadlineSource struct {
	*refreshingSource
	first time.Time
}

func (d deadlineSource) SeasonLive(_ context.Context, since time.Time) (bool, time.Time, error) {
	return !d.first.Before(since), d.first, nil
}

// A refetch of everyone (`fetch-history -all` asks for anything saved
// before a moment just ahead of now) is gated on the current season being
// live, not on a season starting at that moment: mid-season, it refetches.
func TestFetcherRefetchAllMidSeason(t *testing.T) {
	now := time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)
	src := deadlineSource{&refreshingSource{}, time.Date(2026, 8, 14, 17, 30, 0, 0, time.UTC)}
	store := NewMemory()
	f := &Fetcher{Source: src, Store: store, Now: func() time.Time { return now },
		Codes: func(context.Context) ([]string, error) { return []string{"1", "2"}, nil },
		Log:   slog.New(slog.NewTextHandler(io.Discard, nil))}
	if res, err := f.Run(ctx, SeasonStart(now)); err != nil || res != (Result{Fetched: 2}) {
		t.Fatalf("first run %+v %v", res, err)
	}
	if res, err := f.Run(ctx, now.Add(time.Minute)); err != nil || res != (Result{Fetched: 2}) {
		t.Fatalf("refetch all %+v %v", res, err)
	}
}
