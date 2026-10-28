// Package midseason seeds a demo league drafted mid-season, after
// gameweek 7's deadline, so it starts scoring at gameweek 8 (ADR 0010):
// the case where a league's views must not show gameweeks before it
// existed. cmd/seed and the database-less dev server add it next to the
// demo league (package demo); it drafts through the real draft service,
// with two managers who let their pick clock run out.
package midseason

import (
	"cmp"
	"context"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/draft"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// LeagueName is the seeded league.
const LeagueName = "Late Kickoff"

// Managers are its teams, the demo account's first (commissioner), by
// index into demo.Managers.
var Managers = []struct {
	Manager int
	Team    string
}{
	{0, "Better Late Than Never"},
	{1, "Transfer Window Shoppers"},
	{3, "International Break FC"},
	{5, "The January Signings"},
	{6, "Second Half Specialists"},
	{8, "Injury Time Athletic"},
}

// Seed creates the league for the demo managers (userIDs in demo.Managers
// order, as demo.Seed returns them) unless the demo account already has
// it, and returns its id.
func Seed(ctx context.Context, d demo.Deps, userIDs []string) (string, error) {
	commish := userIDs[Managers[0].Manager]
	mine, err := d.Leagues.ForUser(ctx, commish)
	if err != nil {
		return "", err
	}
	if i := slices.IndexFunc(mine, func(l *league.League) bool {
		return l.Name == LeagueName && l.CommissionerID == commish
	}); i >= 0 {
		return mine[i].ID, nil
	}
	return create(ctx, d, userIDs)
}

// midSeasonDraftAt is when the mid-season league drafts: Saturday of
// gameweek 7, after its deadline, so its first gameweek is 8.
var midSeasonDraftAt = time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)

// create creates and fills the league drafted mid-season
// and drafts it through the draft service on a clock of its own: most
// managers pick the best player they still need within the clock, two let
// it run out (autopick, announced in the feed), the rest of their picks are
// on autopick. Its rosters and schedule start at gameweek 8 (ADR 0010).
func create(ctx context.Context, d demo.Deps, userIDs []string) (string, error) {
	at := midSeasonDraftAt.Add(-26 * time.Hour)
	clock := func() time.Time { return at }
	leagues := league.NewService(d.Leagues.Repo(), clock)
	commish := userIDs[Managers[0].Manager]
	l, err := leagues.Create(ctx, commish, LeagueName, len(Managers), Managers[0].Team,
		league.SettingsInput{PickTimerSeconds: ptr(90)})
	if err != nil {
		return "", err
	}
	inv, err := leagues.CreateInvite(ctx, commish, l.ID, nil, nil)
	if err != nil {
		return "", err
	}
	owner := map[string]string{l.Teams[0].ID: commish}
	for _, m := range Managers[1:] {
		at = at.Add(3 * time.Hour)
		if l, _, err = leagues.AcceptInvite(ctx, userIDs[m.Manager], inv.Code, m.Team); err != nil {
			return "", err
		}
		t, _ := l.TeamOf(userIDs[m.Manager])
		owner[t.ID] = userIDs[m.Manager]
	}
	at = midSeasonDraftAt
	ds := draft.New(d.Leagues.Repo(), d.Stats, clock, nil)
	v, err := ds.Start(ctx, commish, l.ID)
	if err != nil {
		return "", err
	}
	// Two managers miss one clock each, early in the draft.
	missOn := map[int]bool{4: true, 9: true}
	for n := 1; v.Draft.Status == league.DraftInProgress; n++ {
		c := v.Current()
		if missOn[n] {
			at = at.Add(time.Duration(v.Draft.PickTimerS+1) * time.Second)
			if _, err := ds.Expire(ctx, l.ID); err != nil {
				return "", err
			}
		} else {
			at = at.Add(time.Duration(20+n%40) * time.Second)
			if _, _, err := ds.Pick(ctx, owner[c.TeamID], l.ID, bestNeeded(v, c.TeamID)); err != nil {
				return "", err
			}
		}
		if v, err = ds.Get(ctx, commish, l.ID); err != nil {
			return "", err
		}
	}
	return l.ID, nil
}

func ptr[T any](v T) *T { return &v }

// bestNeeded is the best-ranked player left at a position the team still
// needs, as a manager drafting by the rankings would pick.
func bestNeeded(v *draft.View, teamID string) string {
	need := scoring.Counts{}
	for pos, n := range scoring.StandardLineup().Squad {
		need[pos] = n
	}
	taken := map[string]bool{}
	for _, p := range v.Draft.Picks {
		taken[p.PlayerID] = true
		if p.TeamID == teamID {
			need[v.World.Players[p.PlayerID].Position]--
		}
	}
	best := ""
	for id, p := range v.World.Players {
		if taken[id] || need[p.Position] <= 0 {
			continue
		}
		if b, ok := v.World.Players[best]; !ok || cmp.Or(cmp.Compare(p.DraftRank, b.DraftRank), cmp.Compare(id, best)) < 0 {
			best = id
		}
	}
	return best
}
