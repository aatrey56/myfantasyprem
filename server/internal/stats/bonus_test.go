package stats

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// Gameweek 3 of the testdata is a double for Northgate (901): 2605
// Northgate v Brookvale and 2607 Northgate v Cliffton. These tests feed it
// hand-built live data.

func intp(n int) *int { return &n }

// gw3Fixtures returns gameweek 3's fixtures with the given statuses.
func gw3Fixtures(f2605, f2606, f2607 provider.FixtureStatus) []provider.Fixture {
	at := func(h int) *time.Time { t := time.Date(2026, 8, 29, h, 0, 0, 0, time.UTC); return &t }
	return []provider.Fixture{
		{ExternalID: "2605", Gameweek: 3, HomeClubExternalID: "901", AwayClubExternalID: "903", KickoffAt: at(14), Status: f2605},
		{ExternalID: "2606", Gameweek: 3, HomeClubExternalID: "902", AwayClubExternalID: "904", KickoffAt: at(14), Status: f2606},
		{ExternalID: "2607", Gameweek: 3, HomeClubExternalID: "901", AwayClubExternalID: "904", KickoffAt: at(14), Status: f2607},
	}
}

// played is a player who played one fixture, with his BPS there.
func played(code, fixture string, minutes, bps int) provider.PlayerLive {
	return provider.PlayerLive{
		PlayerExternalID: code, Minutes: minutes, BPS: bps,
		Explain: []provider.FixtureExplain{{FixtureExternalID: fixture, BPS: intp(bps), Lines: []provider.ExplainLine{
			{Stat: provider.StatMinutes, Value: minutes, Points: map[bool]int{true: 2, false: 1}[minutes >= 60]},
		}}},
	}
}

func bonusByFixture(t *testing.T, repo Repo, gw int) map[[2]string]PlayerFixture {
	t.Helper()
	ids := playerIDs(t, repo)
	fx, err := repo.ExternalIDs(ctx, "fpl", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	code := map[string]string{}
	for c, id := range ids {
		code[id] = c
	}
	fcode := map[string]string{}
	for c, id := range fx {
		fcode[id] = c
	}
	rows, err := repo.PlayerFixtures(ctx, gw)
	if err != nil {
		t.Fatal(err)
	}
	out := map[[2]string]PlayerFixture{}
	for _, r := range rows {
		out[[2]string{code[r.PlayerID], fcode[r.FixtureID]}] = r
	}
	return out
}

// A player in a double gameweek is ranked in each fixture by his BPS in
// that fixture (FPL awards bonus per match from that match's BPS). P, a
// Northgate forward, plays both matches and tops the BPS in 2607 with 40;
// Q, R and T have 30, 20 and 10 there: P 3, Q 2, R 1, T 0. In 2605 his 5
// BPS is third behind Brookvale's 20 and 15.
func TestDoubleGameweekBonusUsesPerFixtureBPS(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, _, _ := ingestAll(t, repo)
		p := provider.PlayerLive{
			PlayerExternalID: "1004", Minutes: 150, BPS: 45,
			Explain: []provider.FixtureExplain{
				{FixtureExternalID: "2605", BPS: intp(5), Lines: []provider.ExplainLine{{Stat: provider.StatMinutes, Value: 90, Points: 2}}},
				{FixtureExternalID: "2607", BPS: intp(40), Lines: []provider.ExplainLine{{Stat: provider.StatMinutes, Value: 60, Points: 2}}},
			},
		}
		live := &provider.EventLive{
			Gameweek: 3,
			Fixtures: gw3Fixtures(provider.FixtureFinishedProvisional, provider.FixtureScheduled, provider.FixtureLive),
			Players: []provider.PlayerLive{
				p,
				played("1011", "2607", 60, 30), // Q
				played("1012", "2607", 60, 20), // R
				played("1024", "2607", 60, 10), // T
				played("1009", "2605", 90, 20),
				played("1010", "2605", 90, 15),
			},
		}
		if _, err := in.Live(ctx, live, &provider.GameweekStatus{Current: 3}); err != nil {
			t.Fatal(err)
		}
		got := bonusByFixture(t, repo, 3)
		want := map[[2]string]int{
			{"1004", "2607"}: 3, {"1011", "2607"}: 2, {"1012", "2607"}: 1, {"1024", "2607"}: 0,
			{"1009", "2605"}: 3, {"1010", "2605"}: 2, {"1004", "2605"}: 1,
		}
		for k, w := range want {
			r, ok := got[k]
			if !ok || r.Stats.Bonus != w || !r.BonusProvisional {
				t.Errorf("player %s in %s: bonus %d provisional %v (row %v), want %d provisional",
					k[0], k[1], r.Stats.Bonus, r.BonusProvisional, ok, w)
			}
		}
		if r := got[[2]string{"1004", "2607"}]; r.BPS != 40 {
			t.Errorf("P's BPS in 2607 = %d, want 40", r.BPS)
		}
	})
}

// Without a per-fixture BPS a double-gameweek player who played both
// matches has an unknown BPS in each: he is not ranked (no guess), the row
// says the BPS is unknown rather than 0, and 2607 keeps upstream's
// projection (none here) rather than a ranking that leaves him out. A
// known negative BPS ranks like any other (FPL ranks "the three best
// performing players"): in 2606, where every BPS is known, 1006 (30) gets
// 3 and 1007 (-2) gets 2.
func TestUnknownBPSIsNotRankedButNegativeIs(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, _, _ := ingestAll(t, repo)
		p := provider.PlayerLive{
			PlayerExternalID: "1004", Minutes: 150, BPS: 45,
			Explain: []provider.FixtureExplain{
				{FixtureExternalID: "2605", Lines: []provider.ExplainLine{{Stat: provider.StatMinutes, Value: 90, Points: 2}}},
				{FixtureExternalID: "2607", Lines: []provider.ExplainLine{{Stat: provider.StatMinutes, Value: 60, Points: 2}}},
			},
		}
		live := &provider.EventLive{
			Gameweek: 3,
			Fixtures: gw3Fixtures(provider.FixtureFinishedProvisional, provider.FixtureLive, provider.FixtureLive),
			Players: []provider.PlayerLive{
				p,
				played("1011", "2607", 60, 30),
				played("1012", "2607", 60, 20),
				played("1006", "2606", 60, 30),
				played("1007", "2606", 60, -2),
			},
		}
		if _, err := in.Live(ctx, live, &provider.GameweekStatus{Current: 3}); err != nil {
			t.Fatal(err)
		}
		got := bonusByFixture(t, repo, 3)
		want := map[[2]string]int{
			{"1011", "2607"}: 0, {"1012", "2607"}: 0, {"1004", "2607"}: 0, {"1004", "2605"}: 0,
			{"1006", "2606"}: 3, {"1007", "2606"}: 2,
		}
		for k, w := range want {
			if r := got[k]; r.Stats.Bonus != w {
				t.Errorf("player %s in %s: bonus %d, want %d", k[0], k[1], r.Stats.Bonus, w)
			}
		}
		for _, k := range [][2]string{{"1004", "2605"}, {"1004", "2607"}} {
			if r := got[k]; !r.BPSUnknown {
				t.Errorf("player %s in %s: BPS %d stored as known", k[0], k[1], r.BPS)
			}
		}
		if r := got[[2]string{"1007", "2606"}]; r.BPSUnknown || r.BPS != -2 {
			t.Errorf("1007's BPS = %d unknown %v, want known -2", r.BPS, r.BPSUnknown)
		}
	})
}

// gw2Rows ingests gameweek 2 (2603 Ashford 1-1 Brookvale over, 2604
// Cliffton v Northgate in play), after mutate, with st, and returns the
// rows by player code.
func gw2Rows(t *testing.T, repo Repo, st *provider.GameweekStatus, mutate func(*provider.EventLive)) map[string]PlayerFixture {
	t.Helper()
	in, src, _ := ingestAll(t, repo)
	live, err := src.EventLive(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if mutate != nil {
		mutate(live)
	}
	if _, err := in.Live(ctx, live, st); err != nil {
		t.Fatal(err)
	}
	ids := playerIDs(t, repo)
	code := map[string]string{}
	for c, id := range ids {
		code[id] = c
	}
	rows, _ := repo.PlayerFixtures(ctx, 2)
	out := map[string]PlayerFixture{}
	for _, r := range rows {
		out[code[r.PlayerID]] = r
	}
	return out
}

// From 2026-27 FPL shows projected bonus in live points and revises it
// until the bonus is confirmed (premierleague.com/en/news/2174909). A
// bonus value upstream is therefore not confirmation: until the match's
// bonus is confirmed the row's bonus is projected from BPS and marked
// provisional. 2603 is over but its day's bonus is not added; upstream
// shows FPL's projection for one of the two players tied on 30 BPS.
func TestUpstreamBonusIsNotConfirmation(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		st := &provider.GameweekStatus{Current: 2}
		rows := gw2Rows(t, repo, st, func(l *provider.EventLive) {
			for i := range l.Players {
				if l.Players[i].PlayerExternalID == "1006" {
					l.Players[i].Bonus = 3
				}
			}
		})
		for code, want := range map[string]int{"1006": 3, "1010": 3, "1005": 1, "1007": 0} {
			if r := rows[code]; r.Stats.Bonus != want || !r.BonusProvisional {
				t.Errorf("player %s: bonus %d provisional %v, want %d provisional", code, r.Stats.Bonus, r.BonusProvisional, want)
			}
		}
	})
}

// Bonus is confirmed per matchday: once event-status says a day's bonus
// is added, that day's finished matches take upstream's bonus as final.
// A match still in play that day stays projected.
func TestBonusConfirmedByMatchday(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		st := &provider.GameweekStatus{Current: 2, BonusAddedOn: map[string]bool{"2026-08-22": true}}
		rows := gw2Rows(t, repo, st, func(l *provider.EventLive) {
			for i := range l.Players {
				switch l.Players[i].PlayerExternalID {
				case "1006":
					l.Players[i].Bonus = 3
				case "1010":
					l.Players[i].Bonus = 2
				}
			}
		})
		for code, want := range map[string]int{"1006": 3, "1010": 2, "1005": 0} {
			if r := rows[code]; r.Stats.Bonus != want || r.BonusProvisional {
				t.Errorf("player %s (2603, confirmed): bonus %d provisional %v, want %d confirmed", code, r.Stats.Bonus, r.BonusProvisional, want)
			}
		}
		if r := rows["1003"]; r.Stats.Bonus != 3 || !r.BonusProvisional {
			t.Errorf("player 1003 (2604, in play): bonus %d provisional %v, want 3 provisional", r.Stats.Bonus, r.BonusProvisional)
		}
	})
}

// FPL adds projected bonus after 20 minutes of a match (2026-27), so a
// match that has not reached 20 minutes projects none yet.
func TestNoProjectedBonusBeforeTwentyMinutes(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, _, _ := ingestAll(t, repo)
		live := &provider.EventLive{
			Gameweek: 3,
			Fixtures: gw3Fixtures(provider.FixtureScheduled, provider.FixtureScheduled, provider.FixtureLive),
			Players: []provider.PlayerLive{
				played("1011", "2607", 15, 9), played("1012", "2607", 15, 6), played("1024", "2607", 15, 3),
			},
		}
		if _, err := in.Live(ctx, live, &provider.GameweekStatus{Current: 3}); err != nil {
			t.Fatal(err)
		}
		for k, r := range bonusByFixture(t, repo, 3) {
			if r.Stats.Bonus != 0 || !r.BonusProvisional {
				t.Errorf("player %s at 15 minutes: bonus %d provisional %v, want 0 provisional", k[0], r.Stats.Bonus, r.BonusProvisional)
			}
		}
		live.Players = []provider.PlayerLive{
			played("1011", "2607", 20, 9), played("1012", "2607", 20, 6), played("1024", "2607", 20, 3),
		}
		if _, err := in.Live(ctx, live, &provider.GameweekStatus{Current: 3}); err != nil {
			t.Fatal(err)
		}
		if r := bonusByFixture(t, repo, 3)[[2]string{"1011", "2607"}]; r.Stats.Bonus != 3 {
			t.Errorf("at 20 minutes the top BPS projects 3, got %d", r.Stats.Bonus)
		}
	})
}

// withBonus adds upstream's projected bonus to a one-fixture line, as the
// gameweek total and as the fixture's explain line.
func withBonus(pl provider.PlayerLive, bonus int) provider.PlayerLive {
	pl.Bonus = bonus
	pl.Explain[0].Lines = append(pl.Explain[0].Lines, provider.ExplainLine{Stat: provider.StatBonus, Value: bonus, Points: bonus})
	return pl
}

// Astra finding 1. A Northgate forward (1004) plays 90 minutes in both of
// his double-gameweek matches, neither confirmed, and upstream gives no
// per-fixture BPS, so neither match can be ranked completely. FPL's own
// projection (explain bonus 3 in 2605, 2 in 2607) is the best there is and
// stays, marked provisional: 2 (90 min) + 3 + 2 (90 min) + 2 = 9 points.
// His rivals keep upstream's projection too rather than a ranking that
// leaves him out (which would give 1009 3 and 1010 2 in 2605).
func TestIncompleteRankingKeepsUpstreamProjection(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, _, _ := ingestAll(t, repo)
		p := provider.PlayerLive{
			PlayerExternalID: "1004", Minutes: 180, BPS: 60, Bonus: 5,
			Explain: []provider.FixtureExplain{
				{FixtureExternalID: "2605", Lines: []provider.ExplainLine{
					{Stat: provider.StatMinutes, Value: 90, Points: 2}, {Stat: provider.StatBonus, Value: 3, Points: 3}}},
				{FixtureExternalID: "2607", Lines: []provider.ExplainLine{
					{Stat: provider.StatMinutes, Value: 90, Points: 2}, {Stat: provider.StatBonus, Value: 2, Points: 2}}},
			},
		}
		live := &provider.EventLive{
			Gameweek: 3,
			Fixtures: gw3Fixtures(provider.FixtureFinishedProvisional, provider.FixtureScheduled, provider.FixtureLive),
			Players: []provider.PlayerLive{
				p,
				withBonus(played("1009", "2605", 90, 20), 2),
				withBonus(played("1010", "2605", 90, 15), 1),
				withBonus(played("1011", "2607", 90, 30), 3),
				withBonus(played("1012", "2607", 90, 20), 1),
			},
		}
		if _, err := in.Live(ctx, live, &provider.GameweekStatus{Current: 3}); err != nil {
			t.Fatal(err)
		}
		got := bonusByFixture(t, repo, 3)
		want := map[[2]string]int{
			{"1004", "2605"}: 3, {"1009", "2605"}: 2, {"1010", "2605"}: 1,
			{"1004", "2607"}: 2, {"1011", "2607"}: 3, {"1012", "2607"}: 1,
		}
		for k, w := range want {
			if r := got[k]; r.Stats.Bonus != w || !r.BonusProvisional {
				t.Errorf("player %s in %s: bonus %d provisional %v, want %d provisional", k[0], k[1], r.Stats.Bonus, r.BonusProvisional, w)
			}
		}
		total := 0
		for _, f := range []string{"2605", "2607"} {
			r := got[[2]string{"1004", f}]
			total += r.Points
			if b := breakdownBonus(r); b != r.Stats.Bonus {
				t.Errorf("1004 in %s: breakdown bonus %d, stats bonus %d", f, b, r.Stats.Bonus)
			}
		}
		if total != 9 {
			t.Errorf("1004's gameweek points = %d, want 2+3+2+2 = 9", total)
		}
	})
}

// breakdownBonus is the bonus line's value in a row's points breakdown.
func breakdownBonus(r PlayerFixture) int {
	for _, l := range r.Breakdown {
		if l.Stat == "bonus" {
			return l.Value
		}
	}
	return 0
}

// Astra finding 2. A Northgate forward (1004) is booked on the bench in
// 2605 (no minutes, -1 point) and plays 90 minutes in 2607 with a BPS of
// 30 there; his gameweek BPS is 27, so the booking cost 3. Rivals in 2607
// have 29, 28 and 26. A known fixture BPS is kept, and a missing one is
// worked out from the total only when every other fixture's BPS is known
// or he did not appear in it (a card-only appearance has BPS of its own).
// He tops 2607 with 30: 3 bonus, 1011 2, 1012 1, 1024 0, and his
// gameweek is -1 + 2 + 3 = 4 points.
func TestKnownFixtureBPSIsKept(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, _, _ := ingestAll(t, repo)
		p := provider.PlayerLive{
			PlayerExternalID: "1004", Minutes: 90, YellowCards: 1, BPS: 27,
			Explain: []provider.FixtureExplain{
				{FixtureExternalID: "2605", Lines: []provider.ExplainLine{{Stat: provider.StatYellowCards, Value: 1, Points: -1}}},
				{FixtureExternalID: "2607", BPS: intp(30), Lines: []provider.ExplainLine{{Stat: provider.StatMinutes, Value: 90, Points: 2}}},
			},
		}
		live := &provider.EventLive{
			Gameweek: 3,
			Fixtures: gw3Fixtures(provider.FixtureLive, provider.FixtureScheduled, provider.FixtureLive),
			Players: []provider.PlayerLive{
				p,
				played("1011", "2607", 90, 29),
				played("1012", "2607", 90, 28),
				played("1024", "2607", 90, 26),
			},
		}
		if _, err := in.Live(ctx, live, &provider.GameweekStatus{Current: 3}); err != nil {
			t.Fatal(err)
		}
		got := bonusByFixture(t, repo, 3)
		if r := got[[2]string{"1004", "2607"}]; r.BPS != 30 || r.BPSUnknown {
			t.Errorf("1004's BPS in 2607 = %d unknown %v, want known 30", r.BPS, r.BPSUnknown)
		}
		// 27 in all, 30 in 2607: -3 in 2605.
		if r := got[[2]string{"1004", "2605"}]; r.BPS != -3 || r.BPSUnknown {
			t.Errorf("1004's BPS in 2605 = %d unknown %v, want known -3", r.BPS, r.BPSUnknown)
		}
		want := map[[2]string]int{{"1004", "2607"}: 3, {"1011", "2607"}: 2, {"1012", "2607"}: 1, {"1024", "2607"}: 0}
		for k, w := range want {
			if r := got[k]; r.Stats.Bonus != w || !r.BonusProvisional {
				t.Errorf("player %s in %s: bonus %d provisional %v, want %d provisional", k[0], k[1], r.Stats.Bonus, r.BonusProvisional, w)
			}
		}
		total := got[[2]string{"1004", "2605"}].Points + got[[2]string{"1004", "2607"}].Points
		if total != 4 {
			t.Errorf("1004's gameweek points = %d, want -1+2+3 = 4", total)
		}
	})
}

// The same booking with no per-fixture BPS at all: the card-only
// appearance in 2605 has BPS of its own, so the gameweek's 27 cannot be
// put on 2607. Both stay unknown (no guess) and 2607 keeps FPL's own
// projection, which has 1004 on top: 1004 3, 1011 2, 1012 1.
func TestCardOnlyAppearanceKeepsBPSUnknown(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		in, _, _ := ingestAll(t, repo)
		p := provider.PlayerLive{
			PlayerExternalID: "1004", Minutes: 90, YellowCards: 1, BPS: 27, Bonus: 3,
			Explain: []provider.FixtureExplain{
				{FixtureExternalID: "2605", Lines: []provider.ExplainLine{{Stat: provider.StatYellowCards, Value: 1, Points: -1}}},
				{FixtureExternalID: "2607", Lines: []provider.ExplainLine{
					{Stat: provider.StatMinutes, Value: 90, Points: 2}, {Stat: provider.StatBonus, Value: 3, Points: 3}}},
			},
		}
		live := &provider.EventLive{
			Gameweek: 3,
			Fixtures: gw3Fixtures(provider.FixtureLive, provider.FixtureScheduled, provider.FixtureLive),
			Players: []provider.PlayerLive{
				p,
				withBonus(played("1011", "2607", 90, 29), 2),
				withBonus(played("1012", "2607", 90, 28), 1),
				played("1024", "2607", 90, 26),
			},
		}
		if _, err := in.Live(ctx, live, &provider.GameweekStatus{Current: 3}); err != nil {
			t.Fatal(err)
		}
		got := bonusByFixture(t, repo, 3)
		for _, f := range []string{"2605", "2607"} {
			if r := got[[2]string{"1004", f}]; !r.BPSUnknown {
				t.Errorf("1004's BPS in %s stored as known %d", f, r.BPS)
			}
		}
		want := map[[2]string]int{{"1004", "2607"}: 3, {"1011", "2607"}: 2, {"1012", "2607"}: 1, {"1024", "2607"}: 0}
		for k, w := range want {
			if r := got[k]; r.Stats.Bonus != w || !r.BonusProvisional {
				t.Errorf("player %s in %s: bonus %d provisional %v, want %d provisional", k[0], k[1], r.Stats.Bonus, r.BonusProvisional, w)
			}
		}
	})
}
