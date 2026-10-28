package httpapi

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/aatrey56/myfantasyprem/server/internal/draft"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/realtime"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
)

// Notify files the notifications committed league changes call for
// (internal/notifications), then renders the changes into the contract's
// Realtime* events and publishes them (docs/realtime.md). It is the league.Notifier
// of the rule services, the jobs, the poller's scorer and cmd/admin.
// Rendering reads the state after the change, which is what REST would
// answer. Failures are logged: realtime is best effort, REST is the truth.
func (s *Server) Notify(ctx context.Context, changes ...league.Change) {
	if len(changes) > 0 {
		s.notifyChanges(context.WithoutCancel(ctx), changes...)
	}
	if s.publisher == nil || len(changes) == 0 {
		return
	}
	ctx = context.WithoutCancel(ctx)
	changes = coalescePicks(changes)
	seen := map[league.Change]bool{}
	// The changes of one call come from one commit: read each league's
	// state, names and the season's players once (a draft cascade reports
	// dozens of picks).
	rc := &renderCache{states: map[string]*league.State{}, names: map[string]names{}}
	for _, c := range changes {
		if seen[c] {
			continue
		}
		seen[c] = true
		ev, err := s.renderEvent(ctx, rc, c)
		if err != nil {
			s.log.ErrorContext(ctx, "realtime render failed", "kind", c.Kind, "league", c.LeagueID, "err", err)
			continue
		}
		if ev == nil {
			continue
		}
		if _, err := s.publisher.Publish(ctx, *ev); err != nil {
			s.log.ErrorContext(ctx, "realtime publish failed", "kind", c.Kind, "league", c.LeagueID, "err", err)
		}
	}
}

// coalescePicks replaces the picks of one commit with a single
// draft.updated (the whole board) when there are several of a league: an
// autopick cascade, up to a whole draft at once, would otherwise send a
// frame per pick and overflow a client's send buffer (realtime.SendBuffer),
// dropping every connected client at the end of a draft.
func coalescePicks(changes []league.Change) []league.Change {
	picks := map[string]int{}
	for _, c := range changes {
		if c.Kind == league.ChangePick {
			picks[c.LeagueID]++
		}
	}
	out := changes[:0:0]
	for _, c := range changes {
		if c.Kind == league.ChangePick && picks[c.LeagueID] > 1 {
			c = league.Change{LeagueID: c.LeagueID, Kind: league.ChangeDraft}
		}
		out = append(out, c)
	}
	return out
}

func marshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return json.RawMessage(b), err
}

// renderCache holds what one Notify call reads.
type renderCache struct {
	states map[string]*league.State
	names  map[string]names
	world  *league.World
}

func (s *Server) cachedState(ctx context.Context, rc *renderCache, leagueID string) (*league.State, names, error) {
	if st, ok := rc.states[leagueID]; ok {
		return st, rc.names[leagueID], nil
	}
	st, err := s.leagues.Repo().State(ctx, leagueID)
	if err != nil || st == nil {
		return nil, nil, err
	}
	n, err := s.namesFor(ctx, st.League)
	if err != nil {
		return nil, nil, err
	}
	rc.states[leagueID], rc.names[leagueID] = st, n
	return st, n, nil
}

func (s *Server) cachedWorld(ctx context.Context, rc *renderCache) (*league.World, error) {
	if rc.world == nil {
		w, err := league.SeasonWorld(ctx, s.stats)
		if err != nil {
			return nil, err
		}
		rc.world = w
	}
	return rc.world, nil
}

// renderEvent builds the event for one change, or nil when there is
// nothing to send (the league or record is gone).
func (s *Server) renderEvent(ctx context.Context, rc *renderCache, c league.Change) (*realtime.Event, error) {
	st, n, err := s.cachedState(ctx, rc, c.LeagueID)
	if err != nil || st == nil {
		return nil, err
	}
	ev := &realtime.Event{LeagueID: c.LeagueID, Type: c.Kind}
	switch c.Kind {
	case league.ChangeLeague, league.ChangeTradeBlock:
		ev.Body = json.RawMessage(`{}`)
	case league.ChangeWaivers, league.ChangeLineupLock:
		ev.Body, err = marshal(struct {
			Gameweek int `json:"gameweek"`
		}{c.Gameweek})
	case league.ChangeDraft, league.ChangePick:
		if st.Draft == nil {
			return nil, nil
		}
		w, err := s.cachedWorld(ctx, rc)
		if err != nil {
			return nil, err
		}
		v := &draft.View{League: st.League, Draft: st.Draft, World: w, Rounds: scoring.StandardLineup().SquadSize()}
		if c.Kind == league.ChangeDraft {
			ev.Body, err = marshal(struct {
				Draft Draft `json:"draft"`
			}{draftBody(v, n)})
			return ev, err
		}
		if c.Overall < 1 || c.Overall > len(st.Draft.Picks) {
			return nil, nil
		}
		ev.Body, err = marshal(struct {
			Pick        DraftPick   `json:"pick"`
			CurrentPick *DraftClock `json:"currentPick"`
		}{pickBody(v, n, st.Draft.Picks[c.Overall-1]), clockBody(v, n, draft.ClockAt(st.Draft, v.Rounds, c.Overall+1))})
	case league.ChangeMatchups:
		w, err := league.LoadWorld(ctx, s.stats, c.Gameweek)
		if err != nil {
			return nil, err
		}
		v := &view{l: st.League, g: st.Game, st: st, w: w, names: n, now: s.now()}
		ms, err := v.summaries(c.Gameweek)
		if err != nil {
			return nil, err
		}
		ev.Body, err = marshal(struct {
			Gameweek   int              `json:"gameweek"`
			ScoreState ScoreState       `json:"scoreState"`
			Matchups   []MatchupSummary `json:"matchups"`
		}{c.Gameweek, ScoreState(w.State(c.Gameweek)), ms})
	case league.ChangeScoreCorrected:
		t, ok := st.League.Team(c.TeamID)
		if !ok {
			return nil, nil
		}
		ev.Body, err = marshal(struct {
			Gameweek       int     `json:"gameweek"`
			Team           TeamRef `json:"team"`
			PreviousPoints int     `json:"previousPoints"`
			Points         int     `json:"points"`
		}{c.Gameweek, teamRef(t, n), c.Previous, c.Points})
	case league.ChangeTrade:
		all, err := s.leagues.Repo().Trades(ctx, c.LeagueID)
		if err != nil {
			return nil, err
		}
		i := slices.IndexFunc(all, func(t league.Trade) bool { return t.ID == c.TradeID })
		if i < 0 {
			return nil, nil
		}
		w, err := s.cachedWorld(ctx, rc)
		if err != nil {
			return nil, err
		}
		tr := all[i]
		type body struct {
			Trade Trade `json:"trade"`
		}
		if ev.Body, err = marshal(body{tradeBody(st.League, w, n, tr, "", "")}); err != nil {
			return nil, err
		}
		ev.PerTeam = map[string]json.RawMessage{}
		for _, t := range st.League.Teams {
			if ev.PerTeam[t.ID], err = marshal(body{tradeBody(st.League, w, n, tr, t.OwnerID, t.ID)}); err != nil {
				return nil, err
			}
		}
		if !trades.EverAccepted(tr) {
			ev.Audience = []string{tr.OfferingTeamID, tr.ReceivingTeamID}
		}
	default:
		return nil, nil
	}
	return ev, err
}
