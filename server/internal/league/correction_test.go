package league

import (
	"context"
	"slices"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// notes records reported changes.
type notes struct{ changes []Change }

func (n *notes) Notify(_ context.Context, cs ...Change) { n.changes = append(n.changes, cs...) }

// A stats correction to a final gameweek is reported as score.corrected,
// with the settled points before and the points now.
func TestScoreCorrection(t *testing.T) {
	scoringImpls(t, func(t *testing.T, repo Repo, statsRepo stats.Repo) {
		l, pid := scoringFixture(t, repo, statsRepo, func(a, b string) []Matchup {
			return []Matchup{{ID: ids.New("mu"), Gameweek: 1, HomeTeamID: a, AwayTeamID: b, State: scoring.StateUpcoming}}
		})
		n := &notes{}
		sc := &Scorer{Leagues: repo, Stats: statsRepo, Rules: scoring.StandardLineup(), Notify: n}
		if res, err := sc.Recompute(ctx, 1, nil, true); err != nil || len(res.Corrections) != 0 {
			t.Fatalf("first score: %+v %v", res, err)
		}
		if len(n.changes) != 1 || n.changes[0].Kind != ChangeMatchups {
			t.Fatalf("changes = %+v", n.changes)
		}
		// Nothing moved: nothing reported.
		n.changes = nil
		if _, err := sc.Recompute(ctx, 1, nil, true); err != nil || len(n.changes) != 0 {
			t.Fatalf("idempotent recompute reported %+v (%v)", n.changes, err)
		}
		// A late correction: team A's goalkeeper (P1) gets 3 more points.
		rows, err := statsRepo.PlayerFixtures(ctx, 1)
		if err != nil {
			t.Fatal(err)
		}
		for i := range rows {
			if rows[i].PlayerID == pid[1] {
				rows[i].Points += 3
			}
		}
		if err := statsRepo.Apply(ctx, stats.Batch{Live: &stats.LiveRows{Gameweek: 1, Rows: rows}}); err != nil {
			t.Fatal(err)
		}
		res, err := sc.Recompute(ctx, 1, []string{pid[1]}, false)
		if err != nil {
			t.Fatal(err)
		}
		a := l.Teams[0].ID
		if len(res.Corrections) != 1 || res.Corrections[0].TeamID != a || res.Corrections[0].Previous != 46 || res.Corrections[0].Points != 49 {
			t.Fatalf("corrections = %+v", res.Corrections)
		}
		var kinds []string
		for _, c := range n.changes {
			kinds = append(kinds, c.Kind)
		}
		if !slices.Equal(kinds, []string{ChangeMatchups, ChangeScoreCorrected}) || n.changes[1].Previous != 46 || n.changes[1].Points != 49 {
			t.Fatalf("changes = %+v", n.changes)
		}
	})
}
