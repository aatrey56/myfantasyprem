package mirror_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

func slug(err error) string {
	var le *league.Error
	if errors.As(err, &le) {
		return le.Slug
	}
	return ""
}

// claimSeat gives a new user a seat through its claim link.
func claimSeat(t *testing.T, e *env, l *league.League, i int) string {
	t.Helper()
	u := e.user(t, "Member")
	inv, ok := l.ClaimLink(l.Teams[i].ID)
	if !ok {
		t.Fatalf("seat %d has no claim link", i)
	}
	svc := league.NewService(e.leagues, e.now)
	if _, _, err := svc.AcceptInvite(ctx, u, inv.Code, ""); err != nil {
		t.Fatal(err)
	}
	return u
}

func explainAll(t *testing.T, e *env, owner, leagueID string) {
	t.Helper()
	v, err := e.svc.View(ctx, owner, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range v.Unexplained {
		if _, err := e.svc.Explain(ctx, owner, leagueID, it.Key, "Checked by hand: no live stats in the test capture."); err != nil {
			t.Fatal(err)
		}
	}
}

// Without the live stats of gameweeks 2-5 their points, subs and results
// are unverified: failing items the commissioner may explain (an
// informational report since 2026-10-14).
func TestSyncViewAndExplanations(t *testing.T) {
	realEnvsFS(t, withoutLive(2, 3, 4, 5), func(t *testing.T, e *env) {
		owner, st, _ := importReal(t, e)
		l := st.League
		member := claimSeat(t, e, l, 1)

		v, err := e.svc.View(ctx, owner, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !v.Commissioner || v.Report == nil || v.Record == nil || v.Record.LastSyncedAt == nil ||
			!v.Record.LastSyncedAt.Equal(e.now()) || v.Record.Pending != "" || v.Record.Note != "" || len(v.Unexplained) == 0 {
			t.Fatalf("commissioner view = %+v", v)
		}
		mv, err := e.svc.View(ctx, member, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if mv.Commissioner || mv.Report != nil || mv.Unexplained != nil || mv.Record == nil {
			t.Errorf("member view = %+v", mv)
		}
		if _, err := e.svc.View(ctx, e.user(t, "Stranger"), l.ID); slug(err) != "not-a-member" {
			t.Errorf("stranger: %v", err)
		}

		// Explanations: commissioner only, failing items only, a note.
		key := v.Unexplained[0].Key
		if _, err := e.svc.Explain(ctx, member, l.ID, key, "fine"); slug(err) != "not-commissioner" {
			t.Errorf("member explains: %v", err)
		}
		// (Ownership needs no stats; the waiver order, worked out from
		// standings, does and is unverified here.)
		if _, err := e.svc.Explain(ctx, owner, l.ID, "owner:gw7", "fine"); slug(err) != "not-found" {
			t.Errorf("explain an ok item: %v", err)
		}
		if _, err := e.svc.Explain(ctx, owner, l.ID, key, "  "); slug(err) != "invalid-request" {
			t.Errorf("empty note: %v", err)
		}
		v2, err := e.svc.Explain(ctx, owner, l.ID, key, "No live stats for this gameweek yet.")
		if err != nil {
			t.Fatal(err)
		}
		if len(v2.Unexplained) != len(v.Unexplained)-1 || len(v2.Explanations) != 1 || v2.Explanations[0].ExplainedBy != owner {
			t.Errorf("after explaining: %d unexplained, %+v", len(v2.Unexplained), v2.Explanations)
		}
	})
}

// An explanation holds only while the item shows the same values.
func TestExplanationHoldsForTheSameValues(t *testing.T) {
	r := &mirror.Report{Items: []mirror.Item{
		{Key: "points:gw2:tm_a", Status: mirror.ItemMismatch, Ours: "40", Theirs: "42"},
		{Key: "points:gw2:tm_b", Status: mirror.ItemOK, Ours: "50", Theirs: "50"},
	}}
	ex := []mirror.Explanation{{Key: "points:gw2:tm_a", Ours: "40", Theirs: "42"}}
	if b := r.Blocking(ex); len(b) != 0 {
		t.Errorf("explained item blocks: %v", b)
	}
	r.Items[0].Theirs = "43"
	if b := r.Blocking(ex); len(b) != 1 || b[0].Key != "points:gw2:tm_a" {
		t.Errorf("changed item: %v", b)
	}
}

// failing makes every league endpoint fail.
type failing struct{ provider.LeagueMirrorSource }

func (failing) LeagueDetails(context.Context, string) (*provider.LeagueDetails, error) {
	return nil, provider.ErrUpstream
}

// recording collects scheduled jobs.
type recording struct {
	mu   sync.Mutex
	jobs []league.JobArgs
}

func (r *recording) ScheduleTx(_ context.Context, _ any, args league.JobArgs, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.jobs = append(r.jobs, args)
	return nil
}

// An import that cannot reach FPL fails with a problem; starting it again
// is allowed. A seat out of range and an unknown league are refused at once.
func TestImportFailures(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner := e.user(t, "Importer")
		if _, _, err := e.svc.StartImport(ctx, owner, realLeague, 13); slug(err) != "invalid-request" {
			t.Errorf("seat 13: %v", err)
		}
		if _, _, err := e.svc.StartImport(ctx, owner, 99999, 1); slug(err) != "not-found" {
			t.Errorf("unknown league: %v", err)
		}
		if _, err := e.svc.Preview(ctx, owner, 99999); slug(err) != "not-found" {
			t.Errorf("preview unknown league: %v", err)
		}
		imp, _, err := e.svc.StartImport(ctx, owner, realLeague, 1)
		if err != nil {
			t.Fatal(err)
		}
		e.svc.Source = failing{e.src}
		if err := e.svc.RunImport(ctx, imp.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := e.svc.Import(ctx, owner, imp.ID)
		if got.Status != mirror.ImportFailed || got.Error == nil || got.Error.Slug != "upstream-error" {
			t.Fatalf("failed import = %+v", got)
		}
		if _, err := e.svc.Import(ctx, e.user(t, "Other"), imp.ID); slug(err) != "not-found" {
			t.Errorf("someone else's import: %v", err)
		}
		if l, _ := e.leagues.Get(ctx, imp.LeagueID); l != nil {
			t.Error("a failed import left a league")
		}
		e.svc.Source = e.src
		again, existing, err := e.svc.StartImport(ctx, owner, realLeague, 1)
		if err != nil || existing || again.ID == imp.ID {
			t.Errorf("retry = %+v %v %v", again, existing, err)
		}
		p, err := e.svc.Preview(ctx, owner, realLeague)
		if err != nil || p.Name != "Fixture Draft League" || len(p.Seats) != 12 || p.CurrentGameweek != 6 || !p.H2H ||
			!p.DraftComplete || p.Problem != nil || p.ImportedLeagueID != "" {
			t.Errorf("preview = %+v %v", p, err)
		}
	})
}
