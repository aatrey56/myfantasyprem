package notifications

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Audiences returns who hears about playerID in league l in gameweek gw:
// the user owning the team that holds him that gameweek, and the user
// owning that team's opponent in the gameweek's matchup. A player on no
// roster has none; an open seat or the league-average side is "".
func Audiences(l *league.League, g *league.Game, gw int, playerID string) []Audience {
	team, ok := g.OwnerAt(gw)[playerID]
	if !ok {
		return nil
	}
	owner := func(teamID string) string {
		for _, t := range l.Teams {
			if t.ID == teamID {
				return t.OwnerID
			}
		}
		return ""
	}
	a := Audience{LeagueID: l.ID, OwnerID: owner(team)}
	for _, m := range g.MatchupsIn(gw) {
		switch team {
		case m.HomeTeamID:
			a.OpponentID = owner(m.AwayTeamID)
		case m.AwayTeamID:
			a.OpponentID = owner(m.HomeTeamID)
		}
	}
	return []Audience{a}
}

// OffTracker guesses when a player has left the pitch. The stats feed
// gives minutes played, not substitutions, so a player whose minutes have
// not moved for Still while his match is live, and who has fewer than 85
// (minutes stop at 90 through stoppage time, and at 45 through half time),
// has gone off. A sent-off player is not "off" here: FPL keeps counting
// goals conceded against him, so his clean sheet waits for full time.
type OffTracker struct {
	Still time.Duration
	mu    sync.Mutex
	seen  map[[2]string]offSeen
}

type offSeen struct {
	minutes int
	since   time.Time
}

// Observe records a poll of the player's minutes and reports whether he is off.
func (o *OffTracker) Observe(playerID, fixtureID string, minutes, reds int, live bool, now time.Time) bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.seen == nil {
		o.seen = map[[2]string]offSeen{}
	}
	k := [2]string{playerID, fixtureID}
	s, ok := o.seen[k]
	if !ok || s.minutes != minutes {
		s = offSeen{minutes: minutes, since: now}
		o.seen[k] = s
	}
	if !live {
		delete(o.seen, k)
		return false
	}
	return reds == 0 && minutes > 0 && minutes < 85 && now.Sub(s.since) >= o.Still
}

// Live turns rescored live stats into notifications. It implements
// league.LiveHook: the scorer calls it for each league it rescored.
type Live struct {
	Notifier *Notifier
	Off      *OffTracker
	Now      func() time.Time
	Log      *slog.Logger
	// Window bounds how long after kickoff a fixture still notifies, so a
	// backfill or a rescore of an old gameweek stays quiet.
	Window time.Duration
}

// NewLive returns a Live with the default heuristics.
func NewLive(n *Notifier, log *slog.Logger) *Live {
	return &Live{Notifier: n, Off: &OffTracker{Still: 5 * time.Minute}, Now: time.Now, Log: log, Window: 6 * time.Hour}
}

var _ league.LiveHook = (*Live)(nil)

func (lv *Live) LiveScored(ctx context.Context, l *league.League, g *league.Game, w *league.World, gw int) {
	now := lv.Now()
	fixtures := map[string]stats.Fixture{}
	for _, f := range w.FixturesIn(gw) {
		fixtures[f.ID] = f
	}
	owned := g.OwnerAt(gw)
	var out []Notification
	for playerID, rows := range w.Rows[gw] {
		if _, ok := owned[playerID]; !ok {
			continue
		}
		aud := Audiences(l, g, gw, playerID)
		if len(aud) == 0 {
			continue
		}
		p := w.Players[playerID]
		for _, r := range rows {
			f, ok := fixtures[r.FixtureID]
			if !ok || !f.Started() || f.KickoffAt == nil || now.Sub(*f.KickoffAt) > lv.Window {
				continue
			}
			out = append(out, LiveEvents(Match(p, r, f, lv.Off, now), aud)...)
		}
	}
	if _, err := lv.Notifier.Send(ctx, out...); err != nil && lv.Log != nil {
		lv.Log.ErrorContext(ctx, "live notifications", "league", l.ID, "gameweek", gw, "err", err)
	}
}

// Match builds a PlayerMatch from a stored row.
func Match(p stats.Player, r stats.PlayerFixture, f stats.Fixture, off *OffTracker, now time.Time) PlayerMatch {
	s := r.Stats
	m := PlayerMatch{
		PlayerID: r.PlayerID, FixtureID: r.FixtureID, Name: p.DisplayName, Position: string(p.Position),
		Goals: s.Goals, Assists: s.Assists, OwnGoals: s.OwnGoals, RedCards: s.RedCards, Minutes: s.Minutes,
		GoalsConceded: s.GoalsConceded, Finished: f.Over(),
	}
	if m.Name == "" {
		m.Name = p.LastName
	}
	for _, line := range r.Breakdown {
		if line.Stat == scoring.StatDefensiveContribution {
			m.DCPoints += line.Points
		}
	}
	if off != nil {
		m.Off = off.Observe(r.PlayerID, r.FixtureID, s.Minutes, s.RedCards, f.Status == stats.FixtureLive, now)
	}
	return m
}
