package httpapi

import (
	"context"
	"slices"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/statuspage"
	"github.com/aatrey56/myfantasyprem/server/internal/trades"
)

// The Status tab's page (GET /v1/leagues/{leagueId}/status). The rules
// live in package statuspage; this gathers their inputs from the league
// and the world and maps the results, as apps/app/src/mocks/status.ts
// does for the mocks.

func (s *Server) GetLeagueStatus(ctx context.Context, r GetLeagueStatusRequestObject) (GetLeagueStatusResponseObject, error) {
	if d := lg(r.LeagueId); d != "" {
		return fail[GetLeagueStatusdefaultApplicationProblemPlusJSONResponse](Problem(badRequest(ctx, d))), nil
	}
	v, p, err := s.loadView(ctx, r.LeagueId)
	if p != nil || err != nil {
		return fail[GetLeagueStatusdefaultApplicationProblemPlusJSONResponse](deref(p)), err
	}
	if v.l.Status != league.StatusInSeason && v.l.Status != league.StatusComplete {
		return failLeague[GetLeagueStatusdefaultApplicationProblemPlusJSONResponse](ctx, league.ErrSeasonNotStarted)
	}
	// The gameweek lineups and roster moves are for now.
	next := v.w.UpcomingGW(v.now)
	totals := seasonTotals(v.w)
	own := owners(v)
	myTeam, _ := v.l.TeamOf(userFrom(ctx))

	page := LeagueStatusPage{
		Gameweek:         v.leagueGW(),
		TransactionPhase: v.transactionPhase(),
		RankChange:       rankChange(v, myTeam.ID),
		Attention:        []StatusAttentionItem{},
		TopAvailable:     []StatusAvailablePlayer{},
		Injuries:         []StatusFlaggedPlayer{},
	}
	if myTeam.ID != "" {
		for _, a := range statuspage.Attention(attentionInput(v, myTeam.ID, userFrom(ctx), next)) {
			item := StatusAttentionItem{
				Kind: StatusAttentionItemKind(a.Kind), Severity: StatusAttentionItemSeverity(a.Severity),
				Message: a.Message, Count: a.Count,
			}
			if a.Gameweek != 0 {
				item.Gameweek = ptr(a.Gameweek)
			}
			page.Attention = append(page.Attention, item)
		}
	}

	var available []statuspage.Available
	var flagged []statuspage.Flagged
	for id, pl := range v.w.Players {
		if availability(pl.Availability) != PlayerAvailabilityAvailable {
			flagged = append(flagged, statuspage.Flagged{ID: id, Name: pl.DisplayName, Availability: string(availability(pl.Availability))})
		}
		if _, owned := own[id]; owned {
			continue
		}
		t := totals[id]
		available = append(available, statuspage.Available{ID: id, Position: string(pl.Position), Form: t.form, TotalPoints: t.points})
	}
	weeks := statuspage.Upcoming(next)
	for _, a := range statuspage.TopAvailable(available) {
		rs, clears, owner := rosterStatus(v, own, a.ID)
		if owner != nil {
			continue
		}
		club := v.w.Players[a.ID].ClubID
		row := StatusAvailablePlayer{
			Player: playerRef(v.w, a.ID), RosterStatus: rs, WaiverClearsAt: clears, Form: totals[a.ID].contractForm(),
			Upcoming: []StatusFixtureWeek{},
		}
		for _, gw := range weeks {
			fixtures := upcomingFixtures(v.w, club, v.w.ClubFixtures(club, gw), 3)
			for i := range fixtures {
				fixtures[i].Status = nil // as the mocks: these have not started
			}
			row.Upcoming = append(row.Upcoming, StatusFixtureWeek{Gameweek: gw, Fixtures: fixtures})
		}
		page.TopAvailable = append(page.TopAvailable, row)
	}
	statuspage.SortInjuries(flagged)
	for _, f := range flagged {
		pl := v.w.Players[f.ID]
		row := StatusFlaggedPlayer{
			Player:          playerRef(v.w, f.ID),
			ChanceOfPlaying: statuspage.ChanceOfPlaying(f.Availability, pl.News),
		}
		if team, ok := own[f.ID]; ok {
			row.Owner = ptr(v.team(team))
		}
		page.Injuries = append(page.Injuries, row)
	}

	state := v.w.State(v.leagueGW())
	meta, err := s.statsMeta(ctx, &state)
	if err != nil {
		return nil, err
	}
	return GetLeagueStatus200JSONResponse{Data: page, Meta: meta}, nil
}

// attentionInput is the caller's starters for gameweek next, the trade
// offers they can accept and their pending waiver claims.
func attentionInput(v *view, teamID, userID string, next int) statuspage.AttentionInput {
	in := statuspage.AttentionInput{Gameweek: next}
	if next <= 38 && len(v.g.RosterAt(teamID, next)) > 0 {
		slots := league.EffectiveLineup(v.g, v.w, teamID, next).Slots
		for _, id := range slots[:min(11, len(slots))] {
			pl := v.w.Players[id]
			in.Starters = append(in.Starters, statuspage.Starter{
				Flagged:  availability(pl.Availability) != PlayerAvailabilityAvailable,
				Fixtures: len(v.w.ClubFixtures(pl.ClubID, next)),
			})
		}
	}
	for _, t := range v.st.Trades {
		if slices.Contains(trades.Actions(t, v.l, userID, teamID), trades.ActAccept) {
			in.TradeOffers++
		}
	}
	for _, c := range v.st.Claims {
		if c.TeamID == teamID && c.Status == league.ClaimPending {
			in.PendingClaims++
		}
	}
	return in
}

// rankChange is how many places the team moved with the league's last
// completed gameweek; 0 before it has two.
func rankChange(v *view, teamID string) int {
	var played []int
	for gw := 38; gw >= 1 && len(played) < 2; gw-- {
		if v.w.Completed(gw) && len(v.g.MatchupsIn(gw)) > 0 {
			played = append(played, gw)
		}
	}
	if teamID == "" || len(played) < 2 {
		return 0
	}
	rank := func(last int) int {
		for _, r := range league.StandingsThrough(v.l, v.g, v.w, last) {
			if r.TeamID == teamID {
				return r.Rank
			}
		}
		return 0
	}
	return statuspage.RankChange(rank(played[1]), rank(played[0]))
}
