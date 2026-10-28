package jobs

import (
	"context"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/history"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// A pick clock scheduled by one process runs in another after a
// "restart": the job is a row in Postgres, inserted with the draft start.
func TestRiverClockSurvivesRestart(t *testing.T) {
	pool := storetest.New(t)
	e := leaguetest.New(t, pool)
	r := runner(e, nil)
	r.Now = time.Now // River schedules on the real clock
	first, err := NewRiver(pool, r, RiverConfig{TickEvery: time.Hour}, e.Log)
	if err != nil {
		t.Fatal(err)
	}
	e.Repo.(*league.Postgres).SetScheduler(first)

	l, users := e.League(t, 4, league.SettingsInput{})
	// A one-second clock, below what the API accepts (15 s), so the test
	// waits on real time only briefly: set it in the store directly.
	if err := e.Repo.Tx(e.Ctx, func(tx league.Tx) error {
		stored, err := tx.Load(e.Ctx, l.ID)
		if err != nil {
			return err
		}
		stored.Settings.PickTimerSeconds = 1
		return tx.Save(e.Ctx, stored)
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Draft().Start(e.Ctx, users[0], l.ID); err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := pool.QueryRow(e.Ctx, `SELECT count(*) FROM river_job WHERE kind = 'draft_clock' AND state = 'scheduled'`).Scan(&queued); err != nil {
		t.Fatal(err)
	}
	if queued != 1 {
		t.Fatalf("scheduled clock jobs = %d, want 1", queued)
	}

	// The first process never starts; a second one does.
	second, err := NewRiver(pool, r, RiverConfig{TickEvery: time.Hour}, e.Log)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := second.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 10*time.Second)
		defer done()
		_ = second.Stop(stop)
	}()
	deadline := time.Now().Add(20 * time.Second)
	for {
		st := e.State(t, l.ID)
		if len(st.Draft.Picks) > 0 {
			if !st.Draft.Autopick[st.Draft.Picks[0].TeamID] {
				t.Fatal("the missed clock did not turn autopick on")
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the clock job did not run after the restart")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// The hourly periodic jobs (shadow plan, history) must insert once an
// hour. River keeps completed jobs for 24 hours, so uniqueness by args
// alone rejected every insert until the last run was pruned.
func TestRiverHourlyJobsInsertEveryHour(t *testing.T) {
	pool := storetest.New(t)
	e := leaguetest.New(t, pool)
	r := runner(e, nil)
	r.History = &history.Fetcher{} // registers the fetch_history worker; never run
	riv, err := NewRiver(pool, r, RiverConfig{TickEvery: time.Hour}, e.Log)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	insert := func(at time.Time) bool {
		t.Helper()
		opts := hourlyOpts()
		opts.ScheduledAt = at
		res, err := riv.client.Insert(e.Ctx, HistoryArgs{}, opts)
		if err != nil {
			t.Fatal(err)
		}
		return !res.UniqueSkippedAsDuplicate
	}
	if !insert(now) {
		t.Fatal("first insert skipped")
	}
	if _, err := pool.Exec(e.Ctx, `UPDATE river_job SET state = 'completed', finalized_at = now() WHERE kind = 'fetch_history'`); err != nil {
		t.Fatal(err)
	}
	// The same hour: one run is enough.
	if insert(now) {
		t.Error("second insert in the same hour was not skipped")
	}
	// The next hour runs again although the first run is still retained.
	if !insert(now.Add(time.Hour)) {
		t.Error("next hour's insert skipped as a duplicate of a completed run")
	}
}
