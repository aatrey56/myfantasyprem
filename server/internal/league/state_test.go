package league

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// stateImpls runs a test on both repositories with a few players stored.
func stateImpls(t *testing.T, test func(t *testing.T, m *Service, players []string)) {
	run := func(t *testing.T, repo Repo, st stats.Repo) {
		club := "clb_" + ids.ULID()
		var b stats.Batch
		b.Clubs = []stats.Club{{ID: club, Code: "NOR", Name: "Northgate"}}
		var players []string
		for i := range 8 {
			p := ids.New("plr")
			players = append(players, p)
			b.Players = append(b.Players, stats.Player{ID: p, DisplayName: p, Position: scoring.DEF, ClubID: club, Availability: "available", DraftRank: i + 1})
		}
		if err := st.Apply(ctx, b); err != nil {
			t.Fatal(err)
		}
		c := &clock{t: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
		test(t, NewService(repo, c.now), players)
	}
	t.Run("memory", func(t *testing.T) { run(t, NewMemory(), stats.NewMemory()) })
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		seedUsers(t, pool)
		run(t, NewPostgres(pool), stats.NewPostgres(pool))
	})
}

// comparable strips what SaveState fills in (slot ids, the snapshot).
func comparable(s *State) *State {
	c := s.clone()
	for i := range c.Game.Rosters {
		c.Game.Rosters[i].ID = 0
	}
	slices.SortFunc(c.Game.Rosters, func(a, b RosterSlot) int {
		if a.PlayerID != b.PlayerID {
			if a.PlayerID < b.PlayerID {
				return -1
			}
			return 1
		}
		return a.FromGW - b.FromGW
	})
	slices.SortFunc(c.Game.Lineups, func(a, b Lineup) int { return a.FromGW - b.FromGW })
	c.Game.Matchups, c.NewMatchups, c.NewTxns, c.Game.Scores = nil, nil, nil, nil
	if len(c.Locks) == 0 {
		c.Locks = nil
	}
	if len(c.Claims) == 0 {
		c.Claims = nil
	}
	if len(c.Trades) == 0 {
		c.Trades = nil
	}
	return c
}

// diffState fails the test with the first part of the states that differs.
func diffState(t *testing.T, what string, got, want *State) {
	t.Helper()
	parts := []struct {
		name string
		g, w any
	}{
		{"league", *got.League, *want.League}, {"rosters", got.Game.Rosters, want.Game.Rosters},
		{"lineups", got.Game.Lineups, want.Game.Lineups}, {"draft", *got.Draft, *want.Draft},
		{"claims", got.Claims, want.Claims}, {"trades", got.Trades, want.Trades}, {"locks", got.Locks, want.Locks},
		{"priority", got.Priority, want.Priority}, {"runs", got.Runs, want.Runs},
	}
	for _, p := range parts {
		if !reflect.DeepEqual(p.g, p.w) {
			t.Fatalf("%s: %s differ:\n got %+v\nwant %+v", what, p.name, p.g, p.w)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s: states differ", what)
	}
}

func TestStateRoundTrip(t *testing.T) {
	stateImpls(t, func(t *testing.T, m *Service, p []string) {
		l, err := m.Create(ctx, usrA, "State League", 4, "A", SettingsInput{})
		if err != nil {
			t.Fatal(err)
		}
		inv, _ := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		_, tb, err := m.AcceptInvite(ctx, usrB, inv.Code, "B")
		if err != nil {
			t.Fatal(err)
		}
		ta := l.Teams[0].ID
		now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
		later := now.Add(time.Hour)

		var want *State
		err = m.Repo().Tx(ctx, func(tx Tx) error {
			s, err := tx.LoadState(ctx, l.ID)
			if err != nil {
				return err
			}
			s.League.Status = StatusInSeason
			s.Draft = &Draft{
				ID: ids.New("drf"), Status: DraftComplete, PickTimerS: 60, ClockStartedAt: &now, CompletedAt: &later,
				Order: []string{ta, tb.ID}, Autopick: map[string]bool{tb.ID: true}, StayManual: map[string]bool{ta: true},
				Picks:  []Pick{{ID: ids.New("pk"), Overall: 1, TeamID: ta, PlayerID: p[0], Method: PickManual, PickedAt: now}},
				Queues: map[string][]string{ta: {p[2], p[3]}},
			}
			s.Game.Acquire(ta, p[0], 1, "draft")
			s.Game.Acquire(tb.ID, p[1], 1, "draft")
			slots := slices.Repeat([]string{p[7]}, 15)
			slots[0] = p[0]
			s.Game.SetLineup(ta, 1, slots, now)
			s.NewMatchups = RoundRobin([]string{ta, tb.ID}, 1, 2)
			s.Priority = []string{tb.ID, ta}
			s.Claims = []Claim{{ID: ids.New("wv"), TeamID: ta, AddPlayerID: p[4], DropPlayerID: p[0], Order: 1, Gameweek: 8, Status: ClaimPending, CreatedAt: now}}
			s.Trades = []Trade{{
				ID: ids.New("trd"), OfferingTeamID: ta, ReceivingTeamID: tb.ID, Offered: []string{p[0]}, Requested: []string{p[1]},
				Status: TradeInReview, Message: "fair?", CreatedAt: now, ExpiresAt: later, RespondedAt: &now, ReviewEndsAt: &later,
				ReviewMode: ReviewLeagueVote, Votes: map[string]string{ta: "allow"},
			}}
			s.SetLock(p[5], 8, LockDropped)
			s.Runs[RunKey{RunWaivers, 7}] = now
			s.NewTxns = []Txn{{ID: ids.New("txn"), Kind: TxnFreeAgent, Gameweek: 8, OccurredAt: now, TeamID: ta, PlayerIn: p[6], PlayerOut: p[5], Approved: true}}
			if err := tx.SaveState(ctx, s); err != nil {
				return err
			}
			want = comparable(s)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		got, err := m.Repo().State(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		diffState(t, "after save", comparable(got), want)
		if len(got.Game.Matchups) != 2 {
			t.Errorf("matchups = %d, want 2", len(got.Game.Matchups))
		}

		// A second change: release, swap, decide the claim and the trade.
		err = m.Repo().Tx(ctx, func(tx Tx) error {
			s, err := tx.LoadState(ctx, l.ID)
			if err != nil {
				return err
			}
			s.Game.Release(ta, p[0], 3)
			s.Game.Acquire(ta, p[4], 3, "waiver")
			s.Game.Swap(ta, p[0], p[4], 3, later)
			s.Claims[0].Status, s.Claims[0].ProcessedAt = ClaimWon, &later
			s.Trades[0].Status, s.Trades[0].ProcessedAt, s.Trades[0].EffectiveGW = TradeVetoed, &later, 3
			s.Trades[0].Votes[ta] = "veto"
			s.Draft.Picks = append(s.Draft.Picks, Pick{ID: ids.New("pk"), Overall: 2, TeamID: tb.ID, PlayerID: p[1], Method: PickRank, PickedAt: later, MissedClock: true})
			s.Draft.StayManual[tb.ID] = true
			s.Draft.Queues = map[string][]string{}
			s.Draft.Order = []string{tb.ID, ta}
			s.Priority = []string{ta, tb.ID}
			s.PruneLocks(9)
			s.NewTxns = append(s.NewTxns, Txn{ID: ids.New("txn"), Kind: TxnTrade, Gameweek: 3, OccurredAt: later, TradeID: s.Trades[0].ID,
				Status: TradeVetoed, OfferingTeamID: ta, ReceivingTeamID: tb.ID, Offered: []string{p[0]}, Requested: []string{p[1]}},
				Txn{ID: ids.New("txn"), Kind: TxnDraftAutopick, Gameweek: 1, OccurredAt: later.Add(time.Minute), TeamID: tb.ID, PlayerIn: p[1], Overall: 2, Method: PickRank})
			if err := tx.SaveState(ctx, s); err != nil {
				return err
			}
			want = comparable(s)
			// The closed claim and trade are no longer "loaded" state.
			want.Claims, want.Trades = nil, nil
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		got, _ = m.Repo().State(ctx, l.ID)
		diffState(t, "after second save", comparable(got), want)
		if r := got.Game.RosterAt(ta, 2); !slices.Equal(r, []string{p[0]}) {
			t.Errorf("roster gw2 = %v", r)
		}
		if r := got.Game.RosterAt(ta, 3); !slices.Equal(r, []string{p[4]}) {
			t.Errorf("roster gw3 = %v", r)
		}
		if lu, _ := got.Game.LineupAt(ta, 3); lu.Slots[0] != p[4] || lu.FromGW != 3 {
			t.Errorf("lineup gw3 = %v", lu.Slots)
		}

		claims, _ := m.Repo().Claims(ctx, l.ID, ta)
		if len(claims) != 1 || claims[0].Status != ClaimWon {
			t.Errorf("claims = %+v", claims)
		}
		trades, _ := m.Repo().Trades(ctx, l.ID)
		if len(trades) != 1 || trades[0].Status != TradeVetoed || trades[0].Votes[ta] != "veto" ||
			!slices.Equal(trades[0].Offered, []string{p[0]}) || trades[0].Message != "fair?" {
			t.Errorf("trades = %+v", trades)
		}
		txns, _ := m.Repo().Transactions(ctx, l.ID)
		if len(txns) != 3 || txns[0].Kind != TxnDraftAutopick || txns[0].Overall != 2 || txns[0].Method != PickRank ||
			txns[0].PlayerIn != p[1] || txns[1].Kind != TxnTrade || txns[2].PlayerIn != p[6] || !txns[2].Approved {
			t.Errorf("txns = %+v", txns)
		}
		ins, _ := m.Repo().LeaguesWithStatus(ctx, StatusInSeason)
		if !slices.Contains(ins, l.ID) {
			t.Errorf("in season = %v", ins)
		}
	})
}

// A slot that only started at the release gameweek is removed, not ended.
func TestReleaseRemovesSlotThatNeverStarted(t *testing.T) {
	g := &Game{}
	g.Acquire("tm_a", "plr_x", 5, "free_agent")
	g.Release("tm_a", "plr_x", 5)
	if len(g.Rosters) != 0 {
		t.Fatalf("rosters = %+v", g.Rosters)
	}
	g.Acquire("tm_a", "plr_y", 1, "draft")
	g.Release("tm_a", "plr_y", 5)
	if g.Rosters[0].ToGW != 5 || !g.Rosters[0].OnRosterAt(4) || g.Rosters[0].OnRosterAt(5) {
		t.Fatalf("rosters = %+v", g.Rosters)
	}
}

// A carried-over lineup that no longer matches the roster is repaired in
// place: each departed player's slot goes to the arrival of his position,
// and every other slot stays as the manager set it.
func TestEffectiveLineupRepairsDepartures(t *testing.T) {
	w := &World{Players: map[string]stats.Player{}}
	g := &Game{}
	var slots []string
	add := func(pos scoring.Position, n int, prefix string) []string {
		var out []string
		for i := range n {
			id := "plr_" + prefix + string(rune('a'+i))
			w.Players[id] = stats.Player{ID: id, Position: pos, DraftRank: len(w.Players) + 1}
			out = append(out, id)
		}
		return out
	}
	gk, def, mid, fwd := add(scoring.GKP, 2, "g"), add(scoring.DEF, 5, "d"), add(scoring.MID, 5, "m"), add(scoring.FWD, 3, "f")
	// A 3-5-2, not the default 4-4-2.
	slots = slices.Concat(gk[:1], def[:3], mid, fwd[:2], gk[1:], def[3:], fwd[2:])
	for _, id := range slots {
		g.Acquire("tm_a", id, 1, "draft")
	}
	g.SetLineup("tm_a", 1, slots, time.Now())

	// Gameweek 2: the third midfielder leaves and a new one arrives, with
	// no lineup saved for it.
	arrival := add(scoring.MID, 1, "n")[0]
	g.Release("tm_a", mid[2], 2)
	g.Acquire("tm_a", arrival, 2, "trade")

	want := slices.Clone(slots)
	want[slices.Index(want, mid[2])] = arrival
	if got := EffectiveLineup(g, w, "tm_a", 2); !slices.Equal(got.Slots, want) {
		t.Fatalf("gameweek 2 = %v, want %v", got.Slots, want)
	}
	if got := EffectiveLineup(g, w, "tm_a", 1); !slices.Equal(got.Slots, slots) {
		t.Fatalf("gameweek 1 = %v", got.Slots)
	}
}
