package mirror

import (
	"cmp"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Shadow item kinds, beyond Verify's.
const (
	KindWaiver       = "waiver"        // one FPL waiver claim's outcome vs ours
	KindTrade        = "trade"         // one closed FPL trade's status vs ours
	KindLineup       = "lineup"        // a team's locked XI and bench vs its FPL picks
	KindPlayerPoints = "player_points" // a starter's gameweek points vs FPL's live points
)

// ShadowArgs is one scheduled shadow check of a league: a read-only
// comparison with FPL (docs/adr/0016). At and Reason are part of the
// arguments, so a slot shared by several fixtures runs once.
type ShadowArgs struct {
	LeagueID string    `json:"leagueId"`
	At       time.Time `json:"at"`
	Reason   string    `json:"reason"`
}

// Kind names the job.
func (ShadowArgs) Kind() string { return "fpl_shadow_check" }

// ShadowPlanArgs is the hourly job that schedules the next day's shadow
// checks from the FPL calendar.
type ShadowPlanArgs struct{}

// Kind names the job.
func (ShadowPlanArgs) Kind() string { return "fpl_shadow_plan" }

// ShadowReport is one shadow check: Verify's items plus waiver outcomes,
// trade statuses, locked lineups and per-player points, with differences
// on data FPL has not finalized yet marked pending.
type ShadowReport struct {
	CheckedAt time.Time `json:"checkedAt"`
	// Reason is what triggered it (a ShadowSlot reason, "manual" or "admin").
	Reason string `json:"reason"`
	// Gameweek is the current gameweek when it ran (0 before the season).
	Gameweek int    `json:"gameweek"`
	Status   string `json:"status"`
	Items    []Item `json:"items"`
}

// Mismatches are the items that differ on finalized data.
func (r *ShadowReport) Mismatches() []Item {
	var out []Item
	for _, it := range r.Items {
		if it.Status == ItemMismatch {
			out = append(out, it)
		}
	}
	return out
}

// ShadowInput is what Shadow compares. State must hold every claim and
// trade, decided or closed too (a loaded State has only open ones). Live
// is FPL's live points for LiveGW by our player id (nil: not fetched).
type ShadowInput struct {
	Snap   *Snapshot
	Plan   *Plan
	State  *league.State
	World  *league.World
	Live   map[string]int
	LiveGW int
	// Txns is the league's stored feed: the sync imports each FPL waiver
	// and free-agent move as one, with its outcome (RefTransaction).
	Txns   []league.Txn
	Reason string
	Now    time.Time
	// Mirroring: the league is still played on FPL (before the switch-over,
	// FPL_SHADOW_UNTIL), so its waiver claims are made there and the sync
	// imports them as transactions. Waivers then compare FPL's outcomes
	// with the imported ones, unless claims for that gameweek were made
	// here (pending or decided).
	Mirroring bool
}

// gwFinal reports whether FPL's numbers for gw are final: every fixture
// over and bonus added (or the data checked). Until then FPL lags or
// leads us by a poll, so a difference is only pending.
func gwFinal(w *league.World, gw int) bool {
	s := w.State(gw)
	return s == scoring.StateOfficial || s == scoring.StateFinal
}

// playerFinal reports whether a player's points in gw are final: each of
// his club's fixtures finished (not provisionally) and his bonus confirmed.
func playerFinal(w *league.World, playerID string, gw int) bool {
	if gwFinal(w, gw) {
		return true
	}
	for _, f := range w.ClubFixtures(w.Players[playerID].ClubID, gw) {
		if f.Status != stats.FixtureFinished {
			return false
		}
	}
	_, _, provisional := w.PlayerPoints(playerID, gw)
	return !provisional
}

// Shadow compares the league as played here with FPL without changing
// anything (docs/adr/0016). It is pure.
func Shadow(in ShadowInput) ShadowReport {
	st, w, p := in.State, in.World, in.Plan
	l, g := st.League, st.Game
	base := Verify(in.Snap, p, st, w, in.Now)
	team := Seats(st)
	name := func(id string) string {
		if pl, ok := w.Players[id]; ok {
			return pl.DisplayName
		}
		return id
	}
	cur := w.CurrentGW(in.Now)

	// Locked lineups whose stored order is still FPL's mid-gameweek picks,
	// where scoring FPL's final order gives FPL's auto-subs and points
	// (reorder): the next re-sync stores that order and rescores, so those
	// differences, and the results and table they move, are pending.
	ro := reorder(p, st, w, team)
	const resyncNote = "Our lineup is in FPL's order from during the gameweek; scoring FPL's final picks (reordered by its auto-subs) gives FPL's auto-subs and points. Re-sync stores FPL's final order and rescores."
	// Verify's differences on gameweeks FPL has not finalized are pending.
	lastFinished := 0
	for _, it := range base.Items {
		if (it.Kind == KindPoints || it.Kind == KindResult) && it.Gameweek > lastFinished {
			lastFinished = it.Gameweek
		}
	}
	items := make([]Item, 0, len(base.Items))
	for _, it := range base.Items {
		if it.Status == ItemMismatch {
			switch it.Kind {
			case KindPoints, KindResult, KindSubs:
				if !gwFinal(w, it.Gameweek) {
					it.Status = ItemPending
				}
				explained := ro.teams["gw"+strconv.Itoa(it.Gameweek)+":"+it.TeamID]
				if it.Kind == KindResult {
					text, ok := ro.results[it.Key]
					explained = ok && text == it.Theirs
				}
				if explained {
					it.Status, it.Note = ItemPending, strings.TrimSpace(it.Note+" "+resyncNote)
				}
			case KindStanding, KindTable:
				if lastFinished > 0 && !gwFinal(w, lastFinished) {
					it.Status = ItemPending
				}
				// Our table from our stored scores; with the reordered
				// scores it is FPL's.
				if it.Kind == KindStanding && ro.scores != nil && it.Status == ItemMismatch {
					for _, r := range league.StandingsThrough(l, ro.scores, w, lastFinished) {
						if r.TeamID == it.TeamID && row(r.Wins, r.Draws, r.Losses, r.PointsFor, r.PointsAgainst, r.LeaguePoints(), r.Rank) == it.Theirs {
							it.Status, it.Note = ItemPending, strings.TrimSpace(it.Note+" "+resyncNote)
						}
					}
				}
			}
		}
		items = append(items, it)
	}
	add := func(it Item) { items = append(items, it) }
	lagged := func(final bool, same bool) string {
		switch {
		case same:
			return ItemOK
		case final:
			return ItemMismatch
		}
		return ItemPending
	}

	// The current gameweek while it is played: team totals and each
	// starter's points, ours from our stats and engine, FPL's from its
	// match points and live feed.
	if cur > 0 && cur >= p.Settings.StartGameweek && cur <= p.Settings.EndGameweek && len(w.Rows[cur]) > 0 {
		final := gwFinal(w, cur)
		results, _ := league.ScoreGameweek(scoring.StandardLineup(), l, g, w, cur)
		fplPoints := map[string]int{}
		haveMatch := map[string]bool{}
		for _, m := range p.Matches {
			if m.Gameweek != cur || m.Finished {
				continue
			}
			for _, side := range []struct {
				seat string
				pts  int
			}{{m.Home, m.HomePts}, {m.Away, m.AwayPts}} {
				if side.seat != "" {
					fplPoints[side.seat], haveMatch[side.seat] = side.pts, true
				}
			}
		}
		for _, s := range p.Seats {
			id := team[s.ExternalID]
			r, ok := results[id]
			if !ok {
				continue
			}
			if haveMatch[s.ExternalID] {
				it := Item{Key: "points:gw" + strconv.Itoa(cur) + ":" + id, Kind: KindPoints, Gameweek: cur, TeamID: id,
					Ours: strconv.Itoa(r.Points), Theirs: strconv.Itoa(fplPoints[s.ExternalID])}
				it.Status = lagged(final, it.Ours == it.Theirs)
				if it.Status != ItemOK {
					it.Note = "Our XI: " + xiNote(r, name)
					if it.Status == ItemPending {
						it.Note = "FPL is still updating this gameweek. " + it.Note
					}
				}
				add(it)
			}
			if in.Live == nil || in.LiveGW != cur {
				continue
			}
			for _, sl := range r.Slots {
				theirs, ok := in.Live[sl.PlayerID]
				if !ok {
					continue
				}
				it := Item{Key: "player_points:gw" + strconv.Itoa(cur) + ":" + sl.PlayerID, Kind: KindPlayerPoints, Gameweek: cur,
					TeamID: id, Ours: strconv.Itoa(sl.Points), Theirs: strconv.Itoa(theirs)}
				it.Status = lagged(playerFinal(w, sl.PlayerID, cur), it.Ours == it.Theirs)
				if it.Status != ItemOK {
					it.Note = name(sl.PlayerID) + ": ours " + it.Ours + ", FPL " + it.Theirs + "."
				}
				add(it)
			}
		}
	}

	// Locked lineups: every gameweek whose deadline passed while the
	// league was played here (a lineup saved here), vs FPL's picks.
	for _, lu := range p.Lineups {
		id := team[lu.Seat]
		if id == "" || lu.Gameweek > cur {
			continue
		}
		ours, ok := g.LineupAt(id, lu.Gameweek)
		if !ok || len(ours.Slots) != len(lu.Slots) {
			continue
		}
		// FPL's picks are the lineup as locked (its auto-subs come apart).
		// The XI order counts: auto-subs take starters in XI order.
		it := Item{Key: "lineup:gw" + strconv.Itoa(lu.Gameweek) + ":" + id, Kind: KindLineup, Gameweek: lu.Gameweek, TeamID: id,
			Ours: lineupText(ours.Slots, name), Theirs: lineupText(lu.Slots, name)}
		it.Status = status(slices.Equal(ours.Slots, lu.Slots))
		if ro.teams["gw"+strconv.Itoa(lu.Gameweek)+":"+id] {
			it.Status, it.Note = ItemPending, resyncNote
		}
		add(it)
	}

	// Waiver outcomes: each gameweek we ran waivers for (a run marker, even
	// with no claims, or decided claims), every FPL claim vs ours (same
	// team, same players).
	ran := map[int]bool{}
	for k := range st.Runs {
		if k.Kind == league.RunWaivers {
			ran[k.Gameweek] = true
		}
	}
	type claimKey struct {
		team, in, out string
		gw            int
	}
	ourClaims := map[claimKey][]league.Claim{} // per attempt key, in processing order
	claimedHere := map[int]bool{}              // gameweeks with claims made here, decided or not
	for _, c := range st.Claims {
		if c.Status == league.ClaimPending {
			claimedHere[c.Gameweek] = true
		}
		if c.Status != league.ClaimWon && c.Status != league.ClaimLost {
			continue
		}
		ran[c.Gameweek] = true
		claimedHere[c.Gameweek] = true
		ck := claimKey{c.TeamID, c.AddPlayerID, c.DropPlayerID, c.Gameweek}
		ourClaims[ck] = append(ourClaims[ck], c)
	}
	for _, cs := range ourClaims {
		slices.SortStableFunc(cs, func(a, b league.Claim) int {
			return cmp.Or(cmp.Compare(a.Order, b.Order), a.CreatedAt.Compare(b.CreatedAt))
		})
	}
	// The same add/drop can be tried more than once in a gameweek: attempts
	// pair in order (FPL's by transaction id, ours by claim order), the
	// first keeps the plain key, later ones get an ordinal.
	waiverKey := func(k claimKey, n int) string {
		key := fmt.Sprintf("waiver:gw%d:%s:%s:%s", k.gw, k.team, k.in, k.out)
		if n > 0 {
			key += ":" + strconv.Itoa(n+1)
		}
		return key
	}
	imported := map[string]league.Txn{}
	for _, t := range in.Txns {
		imported[t.ID] = t
	}
	seen := map[claimKey]int{}
	fplTxns := slices.Clone(p.Txns)
	slices.SortStableFunc(fplTxns, func(a, b PlanTxn) int { return cmpNumeric(a.ExternalID, b.ExternalID) })
	for _, x := range fplTxns {
		if x.Kind != league.TxnWaiver || !ran[x.Gameweek] {
			continue
		}
		k := claimKey{team[x.Seat], x.In, x.Out, x.Gameweek}
		n := seen[k]
		seen[k]++
		it := Item{Key: waiverKey(k, n), Kind: KindWaiver, Gameweek: x.Gameweek,
			TeamID: k.team, Theirs: outcome(x.Approved) + " (priority " + strconv.Itoa(x.Priority) + ")", Ours: "no claim"}
		if cs := ourClaims[k]; n < len(cs) {
			c := cs[n]
			it.Ours = outcome(c.Status == league.ClaimWon) + " (order " + strconv.Itoa(c.Order) + ")"
			it.Status = status(x.Approved == (c.Status == league.ClaimWon))
		} else if in.Mirroring && !claimedHere[x.Gameweek] {
			// The claim was made on FPL: its outcome is FPL's transaction
			// history, and the sync imported that transaction (by its FPL
			// id) with the outcome it recorded. Our rosters cannot tell: a
			// later move in the gameweek changes them.
			it.Ours, it.Status = "not imported", ItemMismatch
			if id, ok := st.Ref(league.RefTransaction, Provider, x.ExternalID); ok {
				if t, ok := imported[id]; ok {
					it.Ours = outcome(t.Approved) + " (imported)"
					it.Status = status(t.Approved == x.Approved && t.Kind == league.TxnWaiver && t.TeamID == k.team &&
						t.PlayerIn == x.In && t.PlayerOut == x.Out && t.Gameweek == x.Gameweek)
				}
			}
		} else {
			it.Status = ItemMismatch
		}
		it.Note = name(x.In) + " for " + name(x.Out) + ", " + teamName(l, k.team) + "."
		add(it)
	}
	var extra []claimKey
	for k, cs := range ourClaims {
		if seen[k] < len(cs) {
			extra = append(extra, k)
		}
	}
	slices.SortFunc(extra, func(a, b claimKey) int {
		return strings.Compare(fmt.Sprint(a.gw, a.team, a.in, a.out), fmt.Sprint(b.gw, b.team, b.in, b.out))
	})
	for _, k := range extra {
		for n := seen[k]; n < len(ourClaims[k]); n++ {
			c := ourClaims[k][n]
			add(Item{Key: waiverKey(k, n), Kind: KindWaiver, Gameweek: k.gw, TeamID: k.team,
				Status: ItemMismatch, Ours: outcome(c.Status == league.ClaimWon) + " (order " + strconv.Itoa(c.Order) + ")", Theirs: "no claim",
				Note: name(k.in) + " for " + name(k.out) + ", " + teamName(l, k.team) + "."})
		}
	}

	// Closed FPL trades (Verify covers pending ones): the same trade here,
	// by import ref or else by teams and players, must have the same status.
	// Oldest first on both sides (the store loads trades newest first), so
	// a trade offered again pairs attempt by attempt.
	fplTrades := slices.Clone(p.Trades)
	slices.SortStableFunc(fplTrades, func(a, b PlanTrade) int {
		return cmp.Or(a.OfferedAt.Compare(b.OfferedAt), cmpNumeric(a.ExternalID, b.ExternalID))
	})
	usedTrade := map[string]bool{}
	for _, x := range fplTrades {
		if x.Status == league.TradeInReview {
			continue
		}
		// The importer creates only trades that happened (processed,
		// vetoed) or were pending; one closed without happening before the
		// import was never made here, so it has nothing to compare.
		if _, imported := st.Ref(league.RefTrade, Provider, x.ExternalID); !imported &&
			x.Status != league.TradeProcessed && x.Status != league.TradeVetoed {
			continue
		}
		offer, receive := team[x.Offer], team[x.Receive]
		it := Item{Key: "trade:fpl" + x.ExternalID, Kind: KindTrade, Gameweek: x.Gameweek, TeamID: offer, Theirs: x.Status, Ours: "not here",
			Note: teamName(l, offer) + " give " + playerList(x.Offered, name) + "; " + teamName(l, receive) + " give " + playerList(x.Requested, name) + "."}
		if tr := matchTrade(st, x, offer, receive, usedTrade); tr != nil {
			usedTrade[tr.ID] = true
			it.Ours = tr.Status
		}
		it.Status = status(it.Ours == it.Theirs)
		add(it)
	}

	r := ShadowReport{CheckedAt: in.Now, Reason: in.Reason, Gameweek: cur, Status: ReportClean, Items: items}
	for _, it := range items {
		switch it.Status {
		case ItemOK:
		case ItemMismatch:
			r.Status = ReportMismatch
		default:
			if r.Status == ReportClean {
				r.Status = ReportIncomplete
			}
		}
	}
	return r
}

// reordered is what scoring FPL's final lineup order explains.
type reordered struct {
	// teams: "gw9:tA" whose auto-subs and points FPL's order gives.
	teams map[string]bool
	// results: result item key -> the score FPL's order gives, for matches
	// whose stored score is the engine's on our order.
	results map[string]string
	// scores: the schedule with those scores (nil: none).
	scores *league.Game
}

// reorder rescores, without writing, each finished gameweek's lineups that
// hold FPL's final picks with the same bench in the same order (it decides
// auto-sub priority) but the XI in another order, in FPL's order. FPL
// serves its picks in the manager's order during the gameweek, and the
// sync stores them; once FPL has made its auto-subs, its picks come back
// reordered, and the plan rebuilds the order its subs imply (lineup),
// which the next re-sync stores. A team counts only when that rescore
// gives FPL's auto-subs and points: a difference the order does not
// explain stays a mismatch.
func reorder(p *Plan, st *league.State, w *league.World, team map[string]string) reordered {
	l, g := st.League, st.Game
	rules := scoring.StandardLineup()
	out := reordered{teams: map[string]bool{}, results: map[string]string{}}
	fplPts := map[string]int{} // "gw9:1" -> FPL's points
	for _, m := range p.Matches {
		for _, side := range []struct {
			seat string
			pts  int
		}{{m.Home, m.HomePts}, {m.Away, m.AwayPts}} {
			if side.seat != "" {
				fplPts["gw"+strconv.Itoa(m.Gameweek)+":"+side.seat] = side.pts
			}
		}
	}
	byGW := map[int][]PlanLineup{}
	for _, lu := range p.Lineups {
		ours, ok := g.LineupAt(team[lu.Seat], lu.Gameweek)
		if team[lu.Seat] == "" || !ok || len(lu.Subs) == 0 || !w.Gameweeks[lu.Gameweek].Finished || len(ours.Slots) != len(lu.Slots) ||
			len(ours.Slots) < 11 || slices.Equal(ours.Slots, lu.Slots) || !sameSet(ours.Slots[:11], lu.Slots[:11]) ||
			!slices.Equal(ours.Slots[11:], lu.Slots[11:]) {
			continue
		}
		byGW[lu.Gameweek] = append(byGW[lu.Gameweek], lu)
	}
	// withOrder is g with these lineups in FPL's order for their gameweek.
	withOrder := func(lus []PlanLineup) *league.Game {
		alt := cloneGame(g)
		alt.Lineups = slices.Clone(g.Lineups)
		for _, lu := range lus {
			alt.Lineups = slices.DeleteFunc(alt.Lineups, func(x league.Lineup) bool { return x.TeamID == team[lu.Seat] && x.FromGW == lu.Gameweek })
			alt.Lineups = append(alt.Lineups, league.Lineup{TeamID: team[lu.Seat], FromGW: lu.Gameweek, Slots: lu.Slots})
		}
		return alt
	}
	ident := func(id string) string { return id }
	gws := slices.Sorted(maps.Keys(byGW))
	for _, gw := range gws {
		results, _ := league.ScoreGameweek(rules, l, withOrder(byGW[gw]), w, gw)
		var explained []PlanLineup
		for _, lu := range byGW[gw] {
			r := results[team[lu.Seat]]
			var got []Sub
			for _, sb := range r.Subs {
				got = append(got, Sub{Out: r.Lineup.Slots[sb.OutSlot-1], In: r.Lineup.Slots[sb.InSlot-1]})
			}
			theirs, ok := fplPts["gw"+strconv.Itoa(gw)+":"+lu.Seat]
			if ok && r.Points == theirs && subList(got, ident) == subList(lu.Subs, ident) {
				out.teams["gw"+strconv.Itoa(gw)+":"+team[lu.Seat]] = true
				explained = append(explained, lu)
			}
		}
		if len(explained) == 0 {
			continue
		}
		// The matches: FPL's order's scores where the stored score is the
		// engine's on our order (any other difference is not the order's).
		_, now := league.ScoreGameweek(rules, l, g, w, gw)
		_, then := league.ScoreGameweek(rules, l, withOrder(explained), w, gw)
		if out.scores == nil {
			out.scores = cloneGame(g)
		}
		for _, m := range p.Matches {
			if m.Gameweek != gw {
				continue
			}
			id, _ := st.Ref(league.RefMatch, Provider, m.Key)
			i := slices.IndexFunc(g.Matchups, func(mu league.Matchup) bool { return mu.ID == id })
			j := slices.IndexFunc(now, func(mu league.Matchup) bool { return mu.ID == id })
			k := slices.IndexFunc(then, func(mu league.Matchup) bool { return mu.ID == id })
			if id == "" || i < 0 || j < 0 || k < 0 || g.Matchups[i].HomePoints != now[j].HomePoints || g.Matchups[i].AwayPoints != now[j].AwayPoints {
				continue
			}
			h, a := then[k].HomePoints, then[k].AwayPoints
			out.scores.Matchups[i].HomePoints, out.scores.Matchups[i].AwayPoints = h, a
			if m.Home == "" { // our average side is away
				h, a = a, h
			}
			out.results["result:gw"+strconv.Itoa(gw)+":"+id] = fmt.Sprintf("%d-%d", h, a)
		}
	}
	return out
}

// matchTrade is FPL trade x's trade here: the one imported from it, else
// the oldest unused one with the same teams and players (a trade offered
// again after a veto pairs attempt by attempt). Nil when there is none.
func matchTrade(st *league.State, x PlanTrade, offer, receive string, used map[string]bool) *league.Trade {
	if id, ok := st.Ref(league.RefTrade, Provider, x.ExternalID); ok {
		return st.Trade(id)
	}
	imported := map[string]bool{}
	for k, id := range st.Refs {
		if k.Entity == league.RefTrade && k.Provider == Provider {
			imported[id] = true
		}
	}
	var first *league.Trade
	for i := range st.Trades {
		tr := &st.Trades[i]
		if used[tr.ID] || imported[tr.ID] {
			continue
		}
		if tr.OfferingTeamID == offer && tr.ReceivingTeamID == receive && sameSet(tr.Offered, x.Offered) && sameSet(tr.Requested, x.Requested) &&
			(first == nil || cmp.Or(tr.CreatedAt.Compare(first.CreatedAt), strings.Compare(tr.ID, first.ID)) < 0) {
			first = tr
		}
	}
	return first
}

// cmpNumeric orders FPL's numeric ids by value, then as text.
func cmpNumeric(a, b string) int {
	return cmp.Or(cmp.Compare(len(a), len(b)), strings.Compare(a, b))
}

func outcome(won bool) string {
	if won {
		return "won"
	}
	return "lost"
}

// lineupText lists the XI and the bench in slot order.
func lineupText(slots []string, name func(string) string) string {
	if len(slots) < 11 {
		return playerList(slots, name)
	}
	return "XI " + playerList(slots[:11], name) + " | bench " + playerList(slots[11:], name)
}

func playerList(ids []string, name func(string) string) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = name(id)
	}
	return strings.Join(out, ", ")
}

func sameSet(a, b []string) bool {
	a, b = slices.Clone(a), slices.Clone(b)
	slices.Sort(a)
	slices.Sort(b)
	return slices.Equal(a, b)
}
