package mirror

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// Item statuses.
const (
	ItemOK         = "ok"
	ItemMismatch   = "mismatch"
	ItemUnverified = "unverified" // could not be checked (no stats yet)
	// ItemPending: an accepted FPL trade still waiting for FPL, imported
	// here as the same pending trade. Not failing: the next re-sync
	// applies what FPL decides (docs/adr/0012).
	ItemPending = "pending"
)

// Report statuses.
const (
	ReportClean      = "clean"
	ReportMismatch   = "mismatch"
	ReportIncomplete = "incomplete"
)

// Item kinds.
const (
	KindPoints       = "points"        // a team's gameweek points vs FPL's match points
	KindSubs         = "subs"          // our auto-subs vs FPL's
	KindResult       = "result"        // a head-to-head match's score
	KindStanding     = "standing"      // our table, from our scores
	KindTable        = "table"         // our table logic fed FPL's match points
	KindSquad        = "squad"         // a team's 15 in a gameweek vs its FPL picks
	KindOwner        = "owner"         // every player's current owner vs element-status
	KindWaiverOrder  = "waiver_order"  // our waiver order vs FPL's waiver_pick
	KindPendingTrade = "pending_trade" // an accepted FPL trade still waiting for FPL
	KindSetting      = "setting"       // an FPL setting we could not map
)

// Item is one check of a verification report. Key is stable across
// reports (kind, gameweek, our team or match id).
type Item struct {
	Key      string `json:"key"`
	Kind     string `json:"kind"`
	Gameweek int    `json:"gameweek,omitempty"`
	TeamID   string `json:"teamId,omitempty"`
	Status   string `json:"status"`
	Ours     string `json:"ours"`
	Theirs   string `json:"theirs"`
	Note     string `json:"note,omitempty"`
}

// Failing reports whether the item is a mismatch or unverified (any
// status but ok and pending: enums are open).
func (it Item) Failing() bool { return it.Status != ItemOK && it.Status != ItemPending }

// Report is one verification: every item, and the overall status.
type Report struct {
	CheckedAt time.Time `json:"checkedAt"`
	Status    string    `json:"status"`
	Items     []Item    `json:"items"`
}

// Blocking returns the failing items no explanation covers (an
// explanation covers an item while the values are the same). They are
// informational: nothing waits on them.
func (r *Report) Blocking(ex []Explanation) []Item {
	byKey := map[string]Explanation{}
	for _, e := range ex {
		byKey[e.Key] = e
	}
	var out []Item
	for _, it := range r.Items {
		if !it.Failing() {
			continue
		}
		if e, ok := byKey[it.Key]; ok && e.Ours == it.Ours && e.Theirs == it.Theirs {
			continue
		}
		out = append(out, it)
	}
	return out
}

// Pending returns the trades waiting for FPL (ItemPending): not failing;
// the next re-sync applies what FPL decides.
func (r *Report) Pending() []Item {
	var out []Item
	for _, it := range r.Items {
		if it.Status == ItemPending {
			out = append(out, it)
		}
	}
	return out
}

// Verify recomputes the league from the imported data with our engines
// (league.ScoreGameweek, StandingsThrough and the stored stats) and diffs
// it against what FPL says (docs/adr/0012). It is pure.
func Verify(snap *Snapshot, p *Plan, st *league.State, w *league.World, now time.Time) Report {
	rules := scoring.StandardLineup()
	l, g := st.League, st.Game
	team := Seats(st)
	seatOf := map[string]string{}
	for s, id := range team {
		seatOf[id] = s
	}
	var items []Item
	add := func(it Item) { items = append(items, it) }
	name := func(id string) string {
		if pl, ok := w.Players[id]; ok {
			return pl.DisplayName
		}
		return id
	}

	// The finished gameweeks, by FPL's matches.
	matchesIn := map[int][]PlanMatch{}
	for _, m := range p.Matches {
		matchesIn[m.Gameweek] = append(matchesIn[m.Gameweek], m)
	}
	var finished []int
	for gw := p.Settings.StartGameweek; gw <= p.Settings.EndGameweek; gw++ {
		ms := matchesIn[gw]
		if len(ms) > 0 && !slices.ContainsFunc(ms, func(m PlanMatch) bool { return !m.Finished }) {
			finished = append(finished, gw)
		}
	}
	lineupOf := map[string]map[int]PlanLineup{}
	for _, lu := range p.Lineups {
		if lineupOf[lu.Seat] == nil {
			lineupOf[lu.Seat] = map[int]PlanLineup{}
		}
		lineupOf[lu.Seat][lu.Gameweek] = lu
	}
	matchID := func(m PlanMatch) string {
		id, _ := st.Ref(league.RefMatch, Provider, m.Key)
		return id
	}

	// Ours: every finished gameweek scored from scratch, and the scores
	// stored for its matches; FPL's: its match points. Both go into copies
	// of the schedule for the two tables.
	storedMatchup := map[string]league.Matchup{}
	for _, mu := range g.Matchups {
		storedMatchup[mu.ID] = mu
	}
	ours := cloneGame(g)
	theirs := cloneGame(g)
	scored := true
	for _, gw := range finished {
		haveStats := len(w.Rows[gw]) > 0 && w.Gameweeks[gw].Finished
		scored = scored && haveStats
		var results map[string]league.TeamResult
		var computed []league.Matchup
		if haveStats {
			results, computed = league.ScoreGameweek(rules, l, g, w, gw)
		}
		noStats := "No stats for gameweek " + strconv.Itoa(gw) + " yet."
		fplPoints := map[string]int{}
		for _, m := range matchesIn[gw] {
			if m.Home != "" {
				fplPoints[m.Home] = m.HomePts
			}
			if m.Away != "" {
				fplPoints[m.Away] = m.AwayPts
			}
			id := matchID(m)
			setPoints(theirs, id, m.HomePts, m.AwayPts, m.Home == "")
			it := Item{Key: "result:gw" + strconv.Itoa(gw) + ":" + id, Kind: KindResult, Gameweek: gw, TeamID: team[m.Home],
				Theirs: fmt.Sprintf("%d-%d", m.HomePts, m.AwayPts)}
			if !haveStats {
				it.Status, it.Ours, it.Note = ItemUnverified, "?", noStats
				add(it)
				continue
			}
			// Ours is the score as stored and shown (what the rescore
			// wrote), so a score that was not saved, or saved wrong, is a
			// mismatch even when the engine is right.
			it.Ours = "no score"
			for _, mu := range computed {
				stored, ok := storedMatchup[id]
				if mu.ID != id || !ok {
					continue
				}
				h, a := stored.HomePoints, stored.AwayPoints
				ch, ca := mu.HomePoints, mu.AwayPoints
				if m.Home == "" { // our average side is away
					h, a, ch, ca = a, h, ca, ch
				}
				it.Ours = fmt.Sprintf("%d-%d", h, a)
				if h != ch || a != ca {
					it.Note = fmt.Sprintf("Stored %d-%d, recomputed %d-%d.", h, a, ch, ca)
				}
				setPoints(ours, id, h, a, m.Home == "")
			}
			it.Status = status(it.Ours == it.Theirs)
			add(it)
		}
		for _, s := range p.Seats {
			id := team[s.ExternalID]
			key := "gw" + strconv.Itoa(gw) + ":" + id
			pts := Item{Key: "points:" + key, Kind: KindPoints, Gameweek: gw, TeamID: id, Theirs: strconv.Itoa(fplPoints[s.ExternalID])}
			subs := Item{Key: "subs:" + key, Kind: KindSubs, Gameweek: gw, TeamID: id}
			lu, haveLineup := lineupOf[s.ExternalID][gw]
			if haveLineup {
				subs.Theirs = subList(lu.Subs, name)
			}
			if !haveStats {
				pts.Status, pts.Ours, pts.Note = ItemUnverified, "?", noStats
				subs.Status, subs.Ours, subs.Note = ItemUnverified, "?", noStats
				add(pts)
				add(subs)
				continue
			}
			r := results[id]
			pts.Ours = strconv.Itoa(r.Points)
			pts.Status = status(pts.Ours == pts.Theirs)
			if pts.Status != ItemOK {
				pts.Note = "Our XI: " + xiNote(r, name)
			}
			add(pts)
			var got []Sub
			for _, sb := range r.Subs {
				got = append(got, Sub{Out: r.Lineup.Slots[sb.OutSlot-1], In: r.Lineup.Slots[sb.InSlot-1]})
			}
			subs.Ours = subList(got, name)
			switch {
			case !haveLineup:
				subs.Status, subs.Note = ItemUnverified, "FPL has no picks for this gameweek."
			default:
				subs.Status = status(subs.Ours == subs.Theirs)
			}
			add(subs)
		}
	}

	// The tables, through the last finished gameweek.
	last := 0
	if len(finished) > 0 {
		last = finished[len(finished)-1]
	}
	fplRow := map[string]string{}
	for _, s := range snap.Details.Standings {
		fplRow[s.LeagueEntry] = row(s.Wins, s.Draws, s.Losses, s.PointsFor, s.PointsAgainst, s.Total, s.Rank)
	}
	table := func(kind string, gm *league.Game, ok bool) {
		rows := map[string]league.StandingRow{}
		for _, r := range league.StandingsThrough(l, gm, w, last) {
			rows[r.TeamID] = r
		}
		for _, s := range p.Seats {
			id := team[s.ExternalID]
			it := Item{Key: kind + ":" + id, Kind: kind, TeamID: id, Theirs: fplRow[s.ExternalID]}
			if !ok {
				it.Status, it.Ours, it.Note = ItemUnverified, "?", "Some finished gameweeks have no stats yet."
				add(it)
				continue
			}
			r := rows[id]
			it.Ours = row(r.Wins, r.Draws, r.Losses, r.PointsFor, r.PointsAgainst, r.LeaguePoints(), r.Rank)
			it.Status = status(it.Ours == it.Theirs)
			add(it)
		}
	}
	finishedHere := true
	for _, gw := range finished {
		finishedHere = finishedHere && w.Gameweeks[gw].Finished
	}
	table(KindStanding, ours, scored)
	table(KindTable, theirs, finishedHere)

	// Squads, every gameweek FPL shows picks for.
	for _, s := range p.Seats {
		id := team[s.ExternalID]
		gws := make([]int, 0, len(lineupOf[s.ExternalID]))
		for gw := range lineupOf[s.ExternalID] {
			gws = append(gws, gw)
		}
		slices.Sort(gws)
		for _, gw := range gws {
			want := slices.Clone(lineupOf[s.ExternalID][gw].Slots)
			got := g.RosterAt(id, gw)
			slices.Sort(want)
			slices.Sort(got)
			it := Item{Key: "squad:gw" + strconv.Itoa(gw) + ":" + id, Kind: KindSquad, Gameweek: gw, TeamID: id,
				Ours: strings.Join(got, ","), Theirs: strings.Join(want, ",")}
			it.Status = status(slices.Equal(got, want))
			if it.Status != ItemOK {
				it.Note = setDiff(got, want, name)
			}
			add(it)
		}
	}

	// Who owns whom now, and pending FPL trades.
	upcoming := w.UpcomingGW(now)
	owners := g.OwnerAt(upcoming)
	flagged := map[string]string{} // in an accepted FPL trade -> his FPL owner
	var diffs []string
	for _, es := range snap.Status {
		pid := players(p)[es.PlayerExternalID]
		want := ""
		if es.OwnerEntry != nil {
			want = team[*es.OwnerEntry]
		}
		if pid == "" {
			if want != "" {
				diffs = append(diffs, "player "+es.PlayerExternalID+" is unknown here")
			}
			continue
		}
		if got := owners[pid]; got != want {
			diffs = append(diffs, fmt.Sprintf("%s: ours %s, FPL %s", name(pid), teamName(l, got), teamName(l, want)))
		}
		if es.InAcceptedTrade {
			flagged[pid] = want
		}
	}
	add(Item{Key: "owner:gw" + strconv.Itoa(upcoming), Kind: KindOwner, Gameweek: upcoming, Status: status(len(diffs) == 0),
		Ours: strconv.Itoa(len(diffs)) + " different", Theirs: "0 different", Note: strings.Join(diffs, "; ")})
	items = append(items, pendingTrades(p, st, flagged, team, name)...)

	var want []string
	for _, s := range p.Priority {
		want = append(want, team[s])
	}
	// Ours is the order our rule gives the next run: with reverse
	// standings, worked out from our standings and the draft, so FPL's
	// waiver_pick checks the rule, not a copy of itself.
	next, _ := league.NextWaiverRun(st, w, now)
	order := league.WaiverOrder(st, w, next)
	wo := Item{Key: "waiver_order", Kind: KindWaiverOrder, Status: status(slices.Equal(order, want)),
		Ours: names(l, order), Theirs: names(l, want)}
	if l.Settings.WaiverRule() == league.WaiverReverseStandings && !scored {
		wo.Status, wo.Ours, wo.Note = ItemUnverified, "?", "Some finished gameweeks have no stats yet."
	}
	add(wo)

	items = append(items, p.Problems...)
	r := Report{CheckedAt: now, Status: ReportClean, Items: items}
	for _, it := range items {
		switch it.Status {
		case ItemMismatch:
			r.Status = ReportMismatch
		case ItemUnverified:
			if r.Status == ReportClean {
				r.Status = ReportIncomplete
			}
		}
	}
	return r
}

// pendingTrades checks the trades FPL accepted but has not processed or
// vetoed yet. Each must be imported as the same pending trade (same teams,
// same players, in review), and FPL must agree with itself: element-status
// flags exactly those players (in_accepted_trade), each still on the team
// that gives him. Then the trade is pending, not a mismatch. A flagged
// player in no such trade, or a pending trade here that FPL no longer
// shows as waiting, is a mismatch.
func pendingTrades(p *Plan, st *league.State, flagged map[string]string, team map[string]string, name func(string) string) []Item {
	var items []Item
	l := st.League
	line := func(offer, receive string, offered, requested []string) string {
		pl := func(ids []string) string {
			out := make([]string, len(ids))
			for i, id := range ids {
				out[i] = name(id)
			}
			return strings.Join(out, ", ")
		}
		return teamName(l, offer) + " give " + pl(offered) + "; " + teamName(l, receive) + " give " + pl(requested)
	}
	imported := map[string]bool{} // our trade ids imported from FPL
	for k, id := range st.Refs {
		if k.Entity == league.RefTrade && k.Provider == Provider {
			imported[id] = true
		}
	}
	covered, seen := map[string]bool{}, map[string]bool{}
	for _, x := range p.Trades {
		if x.Status != league.TradeInReview {
			continue
		}
		offer, receive := team[x.Offer], team[x.Receive]
		it := Item{Key: "pending_trade:fpl" + x.ExternalID, Kind: KindPendingTrade, Gameweek: x.Gameweek, TeamID: offer,
			Ours: "not imported", Theirs: line(offer, receive, x.Offered, x.Requested),
			Note: "Wait for FPL to process or veto the pending trade between " + teamName(l, offer) + " and " + teamName(l, receive) + "."}
		ok := false
		if id, found := st.Ref(league.RefTrade, Provider, x.ExternalID); found {
			if tr := st.Trade(id); tr != nil {
				seen[id] = true
				it.Ours = line(tr.OfferingTeamID, tr.ReceivingTeamID, tr.Offered, tr.Requested)
				ok = tr.Status == league.TradeInReview && it.Ours == it.Theirs
				if tr.Status != league.TradeInReview {
					it.Ours += " (" + tr.Status + ")"
				}
			}
		}
		var odd []string
		for i := range x.Offered {
			for _, side := range []struct{ player, from string }{{x.Offered[i], offer}, {x.Requested[i], receive}} {
				covered[side.player] = true
				if owner, in := flagged[side.player]; !in {
					odd = append(odd, name(side.player)+" is not marked in an accepted trade")
				} else if owner != side.from {
					odd = append(odd, name(side.player)+" is on "+teamName(l, owner)+" in FPL")
				}
			}
		}
		if len(odd) > 0 {
			ok = false
			it.Note += " FPL: " + strings.Join(odd, "; ") + "."
		}
		it.Status = ItemPending
		if !ok {
			it.Status = ItemMismatch
		}
		items = append(items, it)
	}
	pids := make([]string, 0, len(flagged))
	for pid := range flagged {
		if !covered[pid] {
			pids = append(pids, pid)
		}
	}
	slices.Sort(pids)
	for _, pid := range pids {
		items = append(items, Item{Key: "pending_trade:" + pid, Kind: KindPendingTrade, TeamID: flagged[pid], Status: ItemMismatch,
			Ours: "in no pending trade", Theirs: "in an accepted trade",
			Note: name(pid) + " is in an accepted FPL trade that its trades feed does not show."})
	}
	for _, tr := range st.Trades {
		if tr.Status != league.TradeInReview || !imported[tr.ID] || seen[tr.ID] {
			continue
		}
		items = append(items, Item{Key: "pending_trade:" + tr.ID, Kind: KindPendingTrade, Gameweek: tr.EffectiveGW, TeamID: tr.OfferingTeamID,
			Status: ItemMismatch, Ours: line(tr.OfferingTeamID, tr.ReceivingTeamID, tr.Offered, tr.Requested), Theirs: "not pending",
			Note: "Pending here, but FPL no longer shows this trade as waiting."})
	}
	return items
}

// players is the upstream-to-our player mapping the plan was built with
// (element-status keeps upstream ids).
func players(p *Plan) map[string]string { return p.players }

func status(ok bool) string {
	if ok {
		return ItemOK
	}
	return ItemMismatch
}

func row(w, d, l, pf, pa, pts, rank int) string {
	return fmt.Sprintf("W%d D%d L%d PF%d PA%d Pts%d #%d", w, d, l, pf, pa, pts, rank)
}

func subList(subs []Sub, name func(string) string) string {
	if len(subs) == 0 {
		return "none"
	}
	out := make([]string, 0, len(subs))
	for _, s := range subs {
		out = append(out, name(s.In)+" for "+name(s.Out))
	}
	slices.Sort(out)
	return strings.Join(out, "; ")
}

func xiNote(r league.TeamResult, name func(string) string) string {
	var out []string
	for _, s := range r.Slots {
		out = append(out, fmt.Sprintf("%s %d", name(s.PlayerID), s.Points))
	}
	return strings.Join(out, ", ")
}

func setDiff(got, want []string, name func(string) string) string {
	var only, missing []string
	for _, id := range got {
		if !slices.Contains(want, id) {
			only = append(only, name(id))
		}
	}
	for _, id := range want {
		if !slices.Contains(got, id) {
			missing = append(missing, name(id))
		}
	}
	return "only here: " + strings.Join(only, ", ") + "; only in FPL: " + strings.Join(missing, ", ")
}

func teamName(l *league.League, id string) string {
	if id == "" {
		return "nobody"
	}
	if t, ok := l.Team(id); ok {
		return t.Name
	}
	return id
}

func names(l *league.League, teamIDs []string) string {
	out := make([]string, len(teamIDs))
	for i, id := range teamIDs {
		out[i] = teamName(l, id)
	}
	return strings.Join(out, ", ")
}

func cloneGame(g *league.Game) *league.Game {
	return &league.Game{Rosters: g.Rosters, Lineups: g.Lineups, Matchups: slices.Clone(g.Matchups)}
}

// setPoints sets a matchup's points in a schedule copy; homeIsAverage
// flips FPL's sides to ours (our average side is away).
func setPoints(g *league.Game, id string, home, away int, homeIsAverage bool) {
	if homeIsAverage {
		home, away = away, home
	}
	for i := range g.Matchups {
		if g.Matchups[i].ID == id {
			g.Matchups[i].HomePoints, g.Matchups[i].AwayPoints = home, away
		}
	}
}
