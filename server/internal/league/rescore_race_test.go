package league

import (
	"context"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// midLoad runs hook once, after a rescore has read its stats (the last
// read of LoadWorld) and before it saves.
type midLoad struct {
	stats.Repo
	hook func()
}

func (m *midLoad) PlayerFixtures(ctx context.Context, gw int) ([]stats.PlayerFixture, error) {
	rows, err := m.Repo.PlayerFixtures(ctx, gw)
	if h := m.hook; h != nil {
		m.hook = nil
		h()
	}
	return rows, err
}

// correct gives player pid 3 more points in gameweek 1, as a stats
// correction would.
func correct(t *testing.T, statsRepo stats.Repo, pid string) {
	t.Helper()
	rows, err := statsRepo.PlayerFixtures(ctx, 1)
	if err != nil {
		t.Error(err)
		return
	}
	for i := range rows {
		if rows[i].PlayerID == pid {
			rows[i].Points += 3
		}
	}
	if err := statsRepo.Apply(ctx, stats.Batch{Live: &stats.LiveRows{Gameweek: 1, Rows: rows}}); err != nil {
		t.Error(err)
	}
}

// An admin rescore reads gameweek 1's stats; a correction lands (team A's
// goalkeeper P1 +3, so A goes 46 -> 49, see TestScoreCorrection) and the
// poller rescores it while the admin rescore is still computing. The admin
// rescore then saves what it read. The stored score must be the corrected
// 49: a rescore working from older stats must not overwrite a newer one.
// The admin rescores every league (Recompute) or one (RescoreLeague).
func TestStaleRescoreDoesNotOverwriteNewer(t *testing.T) {
	for _, one := range []bool{false, true} {
		name := "every league"
		if one {
			name = "one league"
		}
		t.Run(name, func(t *testing.T) { staleRescore(t, one) })
	}
}

func staleRescore(t *testing.T, one bool) {
	scoringImpls(t, func(t *testing.T, repo Repo, statsRepo stats.Repo) {
		l, pid := scoringFixture(t, repo, statsRepo, func(a, b string) []Matchup {
			return []Matchup{{ID: ids.New("mu"), Gameweek: 1, HomeTeamID: a, AwayTeamID: b, State: scoring.StateUpcoming}}
		})
		poller := &Scorer{Leagues: repo, Stats: statsRepo, Rules: scoring.StandardLineup()}
		if _, err := poller.Recompute(ctx, 1, nil, true); err != nil {
			t.Fatal(err)
		}
		a := l.Teams[0].ID
		score := func() int {
			g, err := repo.Game(ctx, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			s, _ := g.Score(a, 1)
			return s.Points
		}
		if got := score(); got != 46 {
			t.Fatalf("before the correction A = %d, want 46", got)
		}

		done := make(chan error, 1)
		slow := &midLoad{Repo: statsRepo, hook: func() {
			correct(t, statsRepo, pid[1])
			go func() {
				_, err := poller.Recompute(ctx, 1, []string{pid[1]}, false)
				done <- err
			}()
			// The poller's rescore finishes first unless it has to wait
			// for this one.
			select {
			case err := <-done:
				done <- err
			case <-time.After(time.Second):
			}
		}}
		admin := &Scorer{Leagues: repo, Stats: slow, Rules: scoring.StandardLineup()}
		var err error
		if one {
			_, _, err = admin.RescoreLeague(ctx, l.ID, 1)
		} else {
			_, err = admin.Recompute(ctx, 1, nil, true)
		}
		if err != nil {
			t.Fatal(err)
		}
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if got := score(); got != 49 {
			t.Fatalf("after both rescores A = %d, want the corrected 49", got)
		}
	})
}
