package mirror_test

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// One suite on both stores (ADR 0009).
func TestStore(t *testing.T) {
	run := func(t *testing.T, s mirror.Store, a auth.Service, repo league.Repo) {
		u, err := a.CreateUser(ctx, "Importer", "importer@example.com")
		if err != nil {
			t.Fatal(err)
		}
		lsvc := league.NewService(repo, time.Now)
		l, err := lsvc.Create(ctx, u.ID, "Some League", 4, "Mine", league.SettingsInput{})
		if err != nil {
			t.Fatal(err)
		}
		now := time.Date(2026, 10, 14, 12, 0, 0, 0, time.UTC)
		imp := mirror.Import{ID: ids.New("imp"), UserID: u.ID, FPLLeagueID: 90001, Seat: 2, Status: mirror.ImportQueued,
			LeagueID: ids.New("lg"), CreatedAt: now, UpdatedAt: now}
		rec := &recording{}
		got, err := s.CreateImport(ctx, imp, rec, mirror.ImportArgs{ImportID: imp.ID})
		if err != nil || got.ID != imp.ID || len(rec.jobs) != 1 {
			t.Fatalf("create = %+v %v, jobs %v", got, err, rec.jobs)
		}
		// A second active import of the same league returns the first.
		dup := imp
		dup.ID, dup.CreatedAt = ids.New("imp"), now.Add(time.Second)
		got, err = s.CreateImport(ctx, dup, rec, mirror.ImportArgs{ImportID: dup.ID})
		if !errors.Is(err, mirror.ErrImportExists) || got.ID != imp.ID || len(rec.jobs) != 1 {
			t.Fatalf("duplicate = %+v %v", got, err)
		}
		// A failed one does not block a new one.
		imp.Status, imp.Done, imp.Total = mirror.ImportFailed, 3, 90
		imp.Error = &mirror.Problem{Slug: "upstream-error", Title: "FPL unavailable", Detail: "Try again."}
		if err := s.SaveImport(ctx, imp); err != nil {
			t.Fatal(err)
		}
		back, err := s.Import(ctx, imp.ID)
		if err != nil || back == nil || back.Status != mirror.ImportFailed || back.Error == nil || back.Error.Slug != "upstream-error" ||
			back.Done != 3 || back.Total != 90 || back.Seat != 2 || !back.CreatedAt.Equal(now) {
			t.Fatalf("import = %+v %v", back, err)
		}
		if _, err := s.CreateImport(ctx, dup, nil, nil); err != nil {
			t.Fatalf("retry after failure: %v", err)
		}
		if latest, _ := s.LatestImport(ctx, u.ID, 90001); latest == nil || latest.ID != dup.ID {
			t.Errorf("latest = %+v", latest)
		}
		if none, _ := s.Import(ctx, ids.New("imp")); none != nil {
			t.Error("unknown import found")
		}

		pts := 41
		p := &provider.EntryPicks{EntryID: "900001", Gameweek: 6, Points: &pts,
			Picks:    []provider.Pick{{Slot: 1, PlayerExternalID: "101"}},
			AutoSubs: []provider.AutoSub{{OutExternalID: "101", InExternalID: "102"}}}
		if err := s.SavePicks(ctx, "fpl", p); err != nil {
			t.Fatal(err)
		}
		cached, err := s.Picks(ctx, "fpl", "900001", 6)
		if err != nil || cached == nil || cached.Picks[0].PlayerExternalID != "101" || cached.AutoSubs[0].InExternalID != "102" || *cached.Points != 41 {
			t.Fatalf("picks = %+v %v", cached, err)
		}
		if none, _ := s.Picks(ctx, "fpl", "900001", 7); none != nil {
			t.Error("uncached gameweek found")
		}

		// One pending re-sync or check at a time; a stale one gives way.
		if ok, err := s.Request(ctx, l.ID, mirror.PendingCheck, now, now.Add(-30*time.Minute)); err != nil || !ok {
			t.Fatalf("first request: %v %v", ok, err)
		}
		if ok, _ := s.Request(ctx, l.ID, mirror.PendingResync, now.Add(time.Minute), now.Add(-29*time.Minute)); ok {
			t.Error("second request while one is pending")
		}
		if b, _ := s.Sync(ctx, l.ID); b == nil || b.Pending != mirror.PendingCheck || !b.RequestedAt.Equal(now) || b.LastSyncedAt != nil {
			t.Errorf("pending = %+v", b)
		}
		if ok, _ := s.Request(ctx, l.ID, mirror.PendingResync, now.Add(31*time.Minute), now.Add(time.Minute)); !ok {
			t.Error("a stale request still blocks")
		}

		synced := now.Add(-time.Minute)
		r := mirror.SyncRecord{LeagueID: l.ID, LastAttemptAt: &synced, LastSyncedAt: &synced, LastError: "FPL down",
			Note: "Re-synced from FPL. 1 open trade was cancelled."}
		if err := s.SaveSync(ctx, r); err != nil {
			t.Fatal(err)
		}
		if b, _ := s.Sync(ctx, l.ID); b == nil || b.LastError != "FPL down" || b.Note != r.Note || b.Pending != "" ||
			b.RequestedAt != nil || !b.LastSyncedAt.Equal(synced) || !b.LastAttemptAt.Equal(synced) {
			t.Errorf("sync = %+v", b)
		}
		if ok, _ := s.Request(ctx, l.ID, mirror.PendingResync, now, now.Add(-30*time.Minute)); !ok {
			t.Error("request after the last one finished")
		}

		for i := range 52 {
			rep := mirror.Report{CheckedAt: now.Add(time.Duration(i) * time.Minute), Status: mirror.ReportIncomplete,
				Items: []mirror.Item{{Key: "points:gw2:x", Kind: mirror.KindPoints, Gameweek: 2, Status: mirror.ItemUnverified, Ours: "?", Theirs: "40"}}}
			if err := s.SaveReport(ctx, l.ID, rep); err != nil {
				t.Fatal(err)
			}
		}
		latest, err := s.LatestReport(ctx, l.ID)
		if err != nil || latest == nil || !latest.CheckedAt.Equal(now.Add(51*time.Minute)) || len(latest.Items) != 1 ||
			latest.Items[0].Theirs != "40" {
			t.Fatalf("latest report = %+v %v", latest, err)
		}

		e := mirror.Explanation{Key: "points:gw2:x", Ours: "?", Theirs: "40", Note: "no stats", ExplainedBy: u.ID, ExplainedAt: now}
		if err := s.SaveExplanation(ctx, l.ID, e); err != nil {
			t.Fatal(err)
		}
		e.Note = "checked by hand"
		if err := s.SaveExplanation(ctx, l.ID, e); err != nil {
			t.Fatal(err)
		}
		ex, err := s.Explanations(ctx, l.ID)
		if err != nil || len(ex) != 1 || ex[0] != e {
			t.Errorf("explanations = %+v %v", ex, err)
		}

		// Shadow checks: the newest is returned; old ones are pruned.
		if none, err := s.LatestShadow(ctx, l.ID); err != nil || none != nil {
			t.Fatalf("shadow before any = %+v %v", none, err)
		}
		for i := range 3 {
			sr := mirror.ShadowReport{CheckedAt: now.Add(time.Duration(i) * time.Hour), Reason: mirror.ReasonHalfTime, Gameweek: 9,
				Status: mirror.ReportMismatch, Items: []mirror.Item{{Key: "player_points:gw9:plr_x", Kind: mirror.KindPlayerPoints,
					Gameweek: 9, Status: mirror.ItemMismatch, Ours: strconv.Itoa(i), Theirs: "7", Note: "Saka: ours 6, FPL 7."}}}
			if err := s.SaveShadow(ctx, l.ID, sr); err != nil {
				t.Fatal(err)
			}
		}
		sh, err := s.LatestShadow(ctx, l.ID)
		if err != nil || sh == nil || !sh.CheckedAt.Equal(now.Add(2*time.Hour)) || sh.Reason != mirror.ReasonHalfTime || sh.Gameweek != 9 ||
			sh.Status != mirror.ReportMismatch || len(sh.Items) != 1 || sh.Items[0].Ours != "2" || sh.Items[0].Note != "Saka: ours 6, FPL 7." {
			t.Fatalf("latest shadow = %+v %v", sh, err)
		}
	}
	t.Run("memory", func(t *testing.T) {
		run(t, mirror.NewMemoryStore(), auth.NewMemory(time.Now), league.NewMemory())
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		run(t, mirror.NewPostgresStore(pool), auth.NewPostgres(pool, time.Now), league.NewPostgres(pool))
	})
}
