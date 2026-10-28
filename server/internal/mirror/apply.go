package mirror

import (
	"cmp"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Provider names upstream in league_refs and the picks cache.
const Provider = "fpl"

// Seats maps the plan's seats to the league's teams through the refs.
func Seats(st *league.State) map[string]string {
	out := map[string]string{}
	for k, id := range st.Refs {
		if k.Entity == league.RefTeam && k.Provider == Provider {
			out[k.ExternalID] = id
		}
	}
	return out
}

// Apply makes the State match the plan (docs/adr/0012): an idempotent
// upsert by refs. FPL wins on everything it owns (names, settings, rosters,
// the locked lineups, the schedule, the feed, trades, the waiver order);
// owners, invites and scores stay ours. Applying the same plan twice
// changes nothing. now stamps teams that have no FPL join time.
func Apply(st *league.State, p *Plan, w *league.World, now time.Time) {
	l := st.League
	// The waiver order rule is the commissioner's to change: a re-sync
	// keeps it (a new import takes FPL's, reverse standings).
	order := l.Settings.WaiverOrder
	l.Name, l.Settings = p.Name, p.Settings
	if order != "" {
		l.Settings.WaiverOrder = order
	}
	if w.Gameweeks[p.Settings.EndGameweek].Finished {
		l.Status = league.StatusComplete
	} else {
		l.Status = league.StatusInSeason
	}

	// Teams, by seat.
	team := Seats(st)
	for _, s := range p.Seats {
		id, ok := team[s.ExternalID]
		if !ok {
			id = ids.New("tm")
			joined := now
			if s.JoinedAt != nil {
				joined = *s.JoinedAt
			}
			l.Teams = append(l.Teams, league.Team{ID: id, JoinedAt: joined})
			st.SetRef(league.RefTeam, Provider, s.ExternalID, id)
			team[s.ExternalID] = id
		}
		for i := range l.Teams {
			if l.Teams[i].ID == id {
				l.Teams[i].Name, l.Teams[i].ManagerName, l.Teams[i].FPLEntryID = s.TeamName, s.ManagerName, s.EntryID
			}
		}
	}
	slices.SortStableFunc(l.Teams, func(a, b league.Team) int { return cmp.Or(a.JoinedAt.Compare(b.JoinedAt), cmp.Compare(a.ID, b.ID)) })

	// The draft, once: it never changes after it completed.
	if st.Draft == nil {
		d := &league.Draft{
			ID: ids.New("drf"), Status: league.DraftComplete, PickTimerS: p.Draft.PickTimerS, CompletedAt: p.Draft.CompletedAt,
			Autopick: map[string]bool{}, StayManual: map[string]bool{}, Queues: map[string][]string{},
		}
		for _, s := range p.Draft.Order {
			d.Order = append(d.Order, team[s])
		}
		for _, pk := range p.Draft.Picks {
			method := league.PickManual
			if pk.Auto {
				method = league.PickRank
			}
			d.Picks = append(d.Picks, league.Pick{ID: ids.New("pk"), Overall: pk.Overall, TeamID: team[pk.Seat],
				PlayerID: pk.PlayerID, Method: method, PickedAt: pk.At, MissedClock: pk.Auto})
		}
		if d.CompletedAt == nil && len(d.Picks) > 0 {
			last := d.Picks[len(d.Picks)-1].PickedAt
			d.CompletedAt = &last
		}
		st.Draft = d
	}

	// Rosters: exactly the replay, keeping the ids of unchanged slots.
	type slotKey struct {
		team, player string
		from         int
		via          string
	}
	have := map[slotKey][]league.RosterSlot{}
	for _, r := range st.Game.Rosters {
		k := slotKey{r.TeamID, r.PlayerID, r.FromGW, r.AcquiredVia}
		have[k] = append(have[k], r)
	}
	var rosters []league.RosterSlot
	for _, s := range p.Rosters {
		r := league.RosterSlot{TeamID: team[s.Seat], PlayerID: s.PlayerID, FromGW: s.FromGW, ToGW: s.ToGW, AcquiredVia: s.Via}
		k := slotKey{r.TeamID, r.PlayerID, r.FromGW, r.AcquiredVia}
		if old := have[k]; len(old) > 0 {
			r.ID = old[0].ID
			have[k] = old[1:]
		}
		rosters = append(rosters, r)
	}
	st.Game.Rosters = rosters

	// Lineups of locked gameweeks, stamped with the deadline.
	for _, lu := range p.Lineups {
		id := team[lu.Seat]
		at := w.Gameweeks[lu.Gameweek].DeadlineAt
		if cur, ok := st.Game.LineupAt(id, lu.Gameweek); ok && cur.FromGW == lu.Gameweek &&
			slices.Equal(cur.Slots, lu.Slots) && cur.UpdatedAt.Equal(at) {
			continue
		}
		st.Game.SetLineup(id, lu.Gameweek, lu.Slots, at)
	}

	// The schedule, once per match.
	for _, m := range p.Matches {
		if _, ok := st.Ref(league.RefMatch, Provider, m.Key); ok {
			continue
		}
		mu := league.Matchup{ID: ids.New("mu"), Gameweek: m.Gameweek, HomeTeamID: team[m.Home], AwayTeamID: team[m.Away],
			State: scoring.StateUpcoming}
		if m.Home == "" { // the average side is listed away
			mu.HomeTeamID, mu.AwayTeamID = mu.AwayTeamID, ""
		}
		st.NewMatchups = append(st.NewMatchups, mu)
		st.SetRef(league.RefMatch, Provider, m.Key, mu.ID)
	}

	st.Priority = st.Priority[:0]
	for _, s := range p.Priority {
		st.Priority = append(st.Priority, team[s])
	}

	// The feed and trades, once each.
	for _, x := range p.Txns {
		if _, ok := st.Ref(league.RefTransaction, Provider, x.ExternalID); ok {
			continue
		}
		t := league.Txn{ID: ids.New("txn"), Kind: x.Kind, Gameweek: x.Gameweek, OccurredAt: x.At, TeamID: team[x.Seat],
			PlayerIn: x.In, PlayerOut: x.Out, Approved: x.Approved, Reason: x.Reason, Priority: x.Priority, SourceState: x.SourceState}
		st.NewTxns = append(st.NewTxns, t)
		st.SetRef(league.RefTransaction, Provider, x.ExternalID, t.ID)
	}
	// A trade pending on FPL is in review here, without a review window
	// (FPL decides it, never our engines); a later sync closes it the way
	// FPL did. The feed shows a trade once it is processed or vetoed.
	for _, x := range p.Trades {
		if id, ok := st.Ref(league.RefTrade, Provider, x.ExternalID); ok {
			if tr := st.Trade(id); tr != nil && tr.Status == league.TradeInReview && x.Status != league.TradeInReview {
				closeTrade(st, tr, x)
			}
			continue
		}
		switch x.Status {
		case league.TradeProcessed, league.TradeVetoed, league.TradeInReview:
		default:
			continue // closed without happening: nothing to show
		}
		tr := league.Trade{
			ID: ids.New("trd"), OfferingTeamID: team[x.Offer], ReceivingTeamID: team[x.Receive],
			Offered: slices.Clone(x.Offered), Requested: slices.Clone(x.Requested), Status: league.TradeInReview,
			CreatedAt: x.OfferedAt, ExpiresAt: tradeAt(x), RespondedAt: x.ResolvedAt, ReviewMode: league.ReviewCommissioner,
			EffectiveGW: x.Gameweek, Votes: map[string]string{},
		}
		st.SetRef(league.RefTrade, Provider, x.ExternalID, tr.ID)
		st.Trades = append(st.Trades, tr)
		if x.Status != league.TradeInReview {
			closeTrade(st, &st.Trades[len(st.Trades)-1], x)
		}
	}
}

// tradeAt is when FPL resolved a trade (its response time; its offer
// time when it has none).
func tradeAt(x PlanTrade) time.Time {
	if x.ResolvedAt != nil {
		return *x.ResolvedAt
	}
	return x.OfferedAt
}

// closeTrade gives a trade FPL's outcome and, when it was processed or
// vetoed, its feed entry. The players move by the replay (Rosters).
func closeTrade(st *league.State, tr *league.Trade, x PlanTrade) {
	at := tradeAt(x)
	tr.Status, tr.RespondedAt = x.Status, x.ResolvedAt
	switch x.Status {
	case league.TradeProcessed, league.TradeVetoed:
		tr.ProcessedAt = &at
		st.NewTxns = append(st.NewTxns, league.Txn{
			ID: ids.New("txn"), Kind: league.TxnTrade, Gameweek: x.Gameweek, OccurredAt: at, TradeID: tr.ID, Status: x.Status,
			OfferingTeamID: tr.OfferingTeamID, ReceivingTeamID: tr.ReceivingTeamID, Offered: tr.Offered, Requested: tr.Requested,
			SourceState: x.SourceState,
		})
	case league.TradeFailed:
		tr.FailureReason = league.TradeFailNotOnRoster
	}
}
