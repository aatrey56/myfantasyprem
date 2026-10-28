package mirror_test

import (
	"context"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
)

// sent collects notifications.
type sent struct {
	mu sync.Mutex
	ns []notifications.Notification
}

func (s *sent) Send(_ context.Context, ns ...notifications.Notification) ([]notifications.Notification, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []notifications.Notification
	for _, n := range ns {
		if !slices.ContainsFunc(s.ns, func(o notifications.Notification) bool { return o.UserID == n.UserID && o.Key == n.Key }) {
			s.ns = append(s.ns, n)
			out = append(out, n)
		}
	}
	return out, nil
}

// The real league, imported: a shadow check agrees with FPL, stores its
// report and changes nothing. A stored score corrupted here is a final
// mismatch; the commissioner hears once, however often it is found.
func TestShadowCheckRealLeague(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		alerts := &sent{}
		e.svc.Alert = alerts
		owner, st, _ := importReal(t, e)
		id := st.League.ID
		before := e.state(t, id)

		rep, err := e.svc.ShadowCheck(ctx, id, mirror.ReasonManual)
		if err != nil {
			t.Fatal(err)
		}
		if ms := rep.Mismatches(); len(ms) != 0 || rep.Gameweek != 6 {
			t.Fatalf("gw %d mismatches %+v", rep.Gameweek, ms)
		}
		var players int
		for _, it := range rep.Items {
			if it.Kind == mirror.KindPlayerPoints {
				players++
			}
		}
		if players == 0 {
			t.Error("no per-player points were compared")
		}
		after := e.state(t, id)
		if !slices.Equal(rosterKeys(before), rosterKeys(after)) || !slices.Equal(lineupKeys(before), lineupKeys(after)) ||
			len(after.Trades) != len(before.Trades) || len(after.Claims) != len(before.Claims) {
			t.Error("a shadow check changed the league")
		}
		if got, _ := e.svc.Store.LatestShadow(ctx, id); got == nil || got.Reason != mirror.ReasonManual || len(got.Items) != len(rep.Items) {
			t.Errorf("stored = %+v", got)
		}
		if len(alerts.ns) != 0 {
			t.Errorf("alerts on a clean check: %+v", alerts.ns)
		}

		var m league.Matchup
		for _, mu := range st.Game.Matchups {
			if mu.Gameweek == 4 && mu.HomeTeamID != "" && mu.AwayTeamID != "" {
				m = mu
				break
			}
		}
		m.HomePoints += 7
		if _, err := e.leagues.SaveScores(ctx, id, 4, nil, []league.Matchup{m}); err != nil {
			t.Fatal(err)
		}
		for range 2 {
			if rep, err = e.svc.ShadowCheck(ctx, id, mirror.ReasonBonus); err != nil {
				t.Fatal(err)
			}
		}
		if !slices.ContainsFunc(rep.Mismatches(), func(it mirror.Item) bool { return it.Key == "result:gw4:"+m.ID }) {
			t.Errorf("mismatches = %+v", rep.Mismatches())
		}
		if len(alerts.ns) != 1 {
			t.Fatalf("alerts = %+v", alerts.ns)
		}
		n := alerts.ns[0]
		if n.UserID != owner || n.Type != notifications.FPLCheckMismatch || n.LeagueID != id || !strings.HasPrefix(n.Title, "FPL check: ") ||
			!strings.Contains(n.Body, "GW4") {
			t.Errorf("alert = %+v", n)
		}
	})
}

// The planner schedules each league's slots once; a native league gets none.
func TestPlanShadow(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		_, st, _ := importReal(t, e)
		rec := &recording{}
		e.svc.Sched = rec
		n, err := e.svc.PlanShadow(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if n == 0 || n != len(rec.jobs) {
			t.Fatalf("scheduled %d, jobs %d", n, len(rec.jobs))
		}
		for _, j := range rec.jobs {
			a, ok := j.(mirror.ShadowArgs)
			if !ok || a.LeagueID != st.League.ID || a.Reason == "" {
				t.Errorf("job = %#v", j)
			}
		}
		if again, _ := e.svc.PlanShadow(ctx); again != 0 {
			t.Errorf("planned again: %d", again)
		}
		e.svc.ShadowUntil = e.now()
		if n, _ := (&mirror.Service{Leagues: e.leagues, Stats: e.stats, Sched: rec, Now: e.now, ShadowUntil: e.now()}).PlanShadow(ctx); n != 0 {
			t.Errorf("planned after the switch-over: %d", n)
		}
	})
}
