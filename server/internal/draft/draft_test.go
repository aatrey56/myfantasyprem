package draft

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/leaguetest"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

func slug(err error) string {
	var e *league.Error
	if errors.As(err, &e) {
		return e.Slug
	}
	if err != nil {
		return "error: " + err.Error()
	}
	return ""
}

// jobs records scheduled jobs.
type jobs struct {
	mu   sync.Mutex
	args []league.JobArgs
}

func (j *jobs) ScheduleTx(_ context.Context, _ any, a league.JobArgs, _ time.Time) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.args = append(j.args, a)
	return nil
}

type scheduling interface{ SetScheduler(league.Scheduler) }

func TestSlotSnake(t *testing.T) {
	order := []string{"a", "b", "c"}
	var got []string
	for n := 1; n <= 7; n++ {
		team, _, _ := Slot(order, n)
		got = append(got, team)
	}
	if want := []string{"a", "b", "c", "c", "b", "a", "a"}; !slices.Equal(got, want) {
		t.Fatalf("snake = %v, want %v", got, want)
	}
	if _, round, in := Slot(order, 5); round != 2 || in != 2 {
		t.Fatalf("pick 5: round %d pick %d", round, in)
	}
}

// bestAt returns the best-ranked undrafted player at pos.
func bestAt(t *testing.T, v *View, pos scoring.Position, skip ...string) string {
	t.Helper()
	taken := map[string]bool{}
	for _, p := range v.Draft.Picks {
		taken[p.PlayerID] = true
	}
	best := ""
	for id, p := range v.World.Players {
		if p.Position != pos || taken[id] || slices.Contains(skip, id) {
			continue
		}
		if best == "" || p.DraftRank < v.World.Players[best].DraftRank {
			best = id
		}
	}
	return best
}

func TestDraftLifecycle(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		notes := &leaguetest.Notes{}
		sched := &jobs{}
		e.Repo.(scheduling).SetScheduler(sched)
		s := New(e.Repo, e.Stats, e.Clock.Now, notes)
		l, users := e.League(t, 4, league.SettingsInput{})
		ctx := e.Ctx

		v, err := s.Get(ctx, users[1], l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if v.Draft.Status != league.DraftScheduled || v.Draft.ID == "" || v.Current() != nil {
			t.Fatalf("new draft = %+v", v.Draft)
		}
		again, _ := s.Get(ctx, users[0], l.ID)
		if again.Draft.ID != v.Draft.ID {
			t.Fatal("draft id changed between reads")
		}
		if _, err := s.Get(ctx, e.User(t), l.ID); slug(err) != "not-a-member" {
			t.Fatalf("outsider: %v", err)
		}

		// Commissioner only, one of order or randomise, every team once.
		if _, err := s.UpdateDraft(ctx, users[1], l.ID, Update{Randomize: true}); slug(err) != "not-commissioner" {
			t.Fatalf("member update: %v", err)
		}
		order := []string{l.Teams[3].ID, l.Teams[2].ID, l.Teams[1].ID, l.Teams[0].ID}
		if _, err := s.UpdateDraft(ctx, users[0], l.ID, Update{Order: order[:3]}); slug(err) != "invalid-request" {
			t.Fatalf("short order: %v", err)
		}
		if _, err := s.UpdateDraft(ctx, users[0], l.ID, Update{Order: order, Randomize: true}); slug(err) != "invalid-request" {
			t.Fatalf("order and randomise: %v", err)
		}
		timer := 90
		v, err = s.UpdateDraft(ctx, users[0], l.ID, Update{Order: order, PickTimer: &timer})
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(v.Draft.Order, order) || v.League.Settings.PickTimerSeconds != 90 {
			t.Fatalf("after update: order %v timer %d", v.Draft.Order, v.League.Settings.PickTimerSeconds)
		}

		// Start; the first pick belongs to team 4 (users[3]).
		if _, err := s.Pause(ctx, users[0], l.ID); slug(err) != "draft-not-live" {
			t.Fatalf("pause before start: %v", err)
		}
		v, err = s.Start(ctx, users[0], l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if v.Draft.Status != league.DraftInProgress || v.League.Status != league.StatusDrafting {
			t.Fatalf("started: %s / %s", v.Draft.Status, v.League.Status)
		}
		c := v.Current()
		if c == nil || c.TeamID != order[0] || c.Overall != 1 || c.DeadlineAt == nil || !c.DeadlineAt.Equal(e.Clock.Now().Add(90*time.Second)) {
			t.Fatalf("clock = %+v", c)
		}
		if _, err := s.Start(ctx, users[0], l.ID); slug(err) != "draft-started" {
			t.Fatalf("start twice: %v", err)
		}
		if _, err := s.UpdateDraft(ctx, users[0], l.ID, Update{Randomize: true}); slug(err) != "draft-started" {
			t.Fatalf("reorder after start: %v", err)
		}
		if len(sched.args) == 0 {
			t.Fatal("no clock job scheduled")
		}

		gk := bestAt(t, v, scoring.GKP)
		if _, _, err := s.Pick(ctx, users[1], l.ID, gk); slug(err) != "not-your-turn" {
			t.Fatalf("out of turn: %v", err)
		}
		_, pk, err := s.Pick(ctx, users[3], l.ID, gk)
		if err != nil {
			t.Fatal(err)
		}
		if pk.Overall != 1 || pk.Method != league.PickManual || pk.TeamID != order[0] {
			t.Fatalf("pick = %+v", pk)
		}
		if _, _, err := s.Pick(ctx, users[2], l.ID, gk); slug(err) != "player-taken" {
			t.Fatalf("taken: %v", err)
		}
		// The commissioner may pick for the team on the clock.
		_, pk, err = s.Pick(ctx, users[0], l.ID, bestAt(t, v, scoring.GKP, gk))
		if err != nil || pk.Method != league.PickCommissioner || pk.TeamID != order[1] {
			t.Fatalf("commissioner pick = %+v, %v", pk, err)
		}

		// Pause stops the clock; resume restarts it in full.
		v, err = s.Pause(ctx, users[0], l.ID)
		if err != nil || v.Draft.Status != league.DraftPaused || v.Current().DeadlineAt != nil {
			t.Fatalf("paused: %+v %v", v.Draft, err)
		}
		if _, _, err := s.Pick(ctx, users[1], l.ID, bestAt(t, v, scoring.MID)); slug(err) != "draft-not-live" {
			t.Fatalf("pick while paused: %v", err)
		}
		e.Clock.Advance(time.Hour)
		if changed, err := s.Expire(ctx, l.ID); err != nil || changed {
			t.Fatalf("expire while paused: %v %v", changed, err)
		}
		if _, err := s.Resume(ctx, users[1], l.ID); slug(err) != "not-commissioner" {
			t.Fatalf("member resume: %v", err)
		}
		v, err = s.Resume(ctx, users[0], l.ID)
		if err != nil || v.Current().TeamID != order[2] || !v.Current().DeadlineAt.Equal(e.Clock.Now().Add(90*time.Second)) {
			t.Fatalf("resumed: %+v %v", v.Current(), err)
		}

		// A missed clock autopicks (queue first) and turns autopick on;
		// a fresh Service (a restart) does it from the stored deadline.
		q, err := s.SetQueue(ctx, users[1], l.ID, []string{gk, bestAt(t, v, scoring.FWD)})
		if err != nil {
			t.Fatal(err)
		}
		if len(q.Players) != 1 {
			t.Fatalf("queue kept a drafted player: %v", q.Players)
		}
		if _, err := s.SetQueue(ctx, users[1], l.ID, []string{"plr_01k7m2xq8v00000000000000a1"}); slug(err) != "invalid-request" {
			t.Fatalf("unknown queued player: %v", err)
		}
		e.Clock.Advance(91 * time.Second)
		restarted := New(e.Repo, e.Stats, e.Clock.Now, notes)
		if changed, err := restarted.Expire(ctx, l.ID); err != nil || !changed {
			t.Fatalf("expire: %v %v", changed, err)
		}
		v, _ = s.Get(ctx, users[0], l.ID)
		if got := v.Draft.Picks[2]; got.Method != league.PickQueue || got.TeamID != order[2] || got.PlayerID != q.Players[0] {
			t.Fatalf("missed pick = %+v", got)
		}
		if !v.Draft.Autopick[order[2]] {
			t.Fatal("missed clock did not turn autopick on")
		}
		// Picks 4 and 5 are team order[3] (users[0]) at the snake's turn;
		// pick 6 is order[2], on autopick, which picks at once by rank.
		if v.Current().TeamID != order[3] {
			t.Fatalf("on the clock: %s", v.Current().TeamID)
		}
		for range 2 {
			if _, _, err := s.Pick(ctx, users[0], l.ID, bestAt(t, v, scoring.DEF)); err != nil {
				t.Fatal(err)
			}
			v, _ = s.Get(ctx, users[0], l.ID)
		}
		if len(v.Draft.Picks) != 6 || v.Draft.Picks[5].TeamID != order[2] || v.Draft.Picks[5].Method != league.PickRank ||
			v.Current().TeamID != order[1] {
			t.Fatalf("after cascade: %d picks, on clock %v", len(v.Draft.Picks), v.Current())
		}
		// RESEARCH.md §7: with no usable queue, autopick takes the highest
		// draft_rank still available at a position the squad needs. Team
		// order[2] has one keeper and one queued pick; every position has
		// room, so it is the best-ranked undrafted player overall, ranked
		// here by sorting (not the code's scan).
		{
			taken := map[string]bool{}
			for _, p := range v.Draft.Picks[:5] {
				taken[p.PlayerID] = true
			}
			var best []string
			for _, pos := range []scoring.Position{scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD} {
				best = append(best, byRank(v.World, pos, taken)[0])
			}
			slices.SortFunc(best, func(a, b string) int {
				return cmp.Or(cmp.Compare(v.World.Players[a].DraftRank, v.World.Players[b].DraftRank), cmp.Compare(a, b))
			})
			if got := v.Draft.Picks[5].PlayerID; got != best[0] {
				t.Fatalf("rank autopick took %s (rank %d), want %s (rank %d)", got, v.World.Players[got].DraftRank, best[0], v.World.Players[best[0]].DraftRank)
			}
		}
		// Turning autopick on while on the clock picks at once.
		if _, err := s.SetAutopick(ctx, users[2], l.ID, true); err != nil {
			t.Fatal(err)
		}
		v, _ = s.Get(ctx, users[0], l.ID)
		if got := v.Draft.Picks[6]; got.Method != league.PickRank || got.TeamID != order[1] {
			t.Fatalf("autopick on the clock = %+v", got)
		}
		// Team order[0] (users[3]) has one goalkeeper: a second is fine,
		// a third is position-full.
		if _, _, err := s.Pick(ctx, users[3], l.ID, bestAt(t, v, scoring.GKP)); err != nil {
			t.Fatal(err)
		}
		v, _ = s.Get(ctx, users[0], l.ID)
		if _, _, err := s.Pick(ctx, users[3], l.ID, bestAt(t, v, scoring.GKP)); slug(err) != "position-full" {
			t.Fatalf("third goalkeeper: %v", err)
		}
		// Everyone on autopick finishes the draft.
		for _, u := range users {
			if _, err := s.SetAutopick(ctx, u, l.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		v, _ = s.Get(ctx, users[0], l.ID)
		if v.Draft.Status != league.DraftComplete || v.League.Status != league.StatusInSeason || len(v.Draft.Picks) != 60 {
			t.Fatalf("complete: %s %s %d picks", v.Draft.Status, v.League.Status, len(v.Draft.Picks))
		}

		st := e.State(t, l.ID)
		w := e.World(t)
		for _, team := range order {
			roster := st.Game.RosterAt(team, 8)
			counts := scoring.Counts{}
			pos := map[string]scoring.Position{}
			for _, id := range roster {
				counts[w.Players[id].Position]++
				pos[id] = w.Players[id].Position
			}
			if len(roster) != 15 || counts[scoring.GKP] != 2 || counts[scoring.DEF] != 5 || counts[scoring.MID] != 5 || counts[scoring.FWD] != 3 {
				t.Fatalf("team %s roster %v", team, counts)
			}
			if len(st.Game.RosterAt(team, 7)) != 0 {
				t.Fatal("rosters start before the upcoming gameweek")
			}
			lu, ok := st.Game.LineupAt(team, 8)
			if !ok || len(scoring.StandardLineup().ValidateLineup(pos, lu.Slots[:11], lu.Slots[11:])) != 0 {
				t.Fatalf("lineup = %+v", lu)
			}
		}
		if n := len(st.Game.MatchupsIn(8)); n != 2 {
			t.Fatalf("gameweek 8 matchups = %d", n)
		}
		if n := len(st.Game.MatchupsIn(7)); n != 0 {
			t.Fatalf("gameweek 7 matchups = %d", n)
		}
		rev := slices.Clone(order)
		slices.Reverse(rev)
		if !slices.Equal(st.Priority, rev) {
			t.Fatalf("priority %v, want %v", st.Priority, rev)
		}
		if _, _, err := s.Pick(ctx, users[0], l.ID, gk); slug(err) != "draft-complete" {
			t.Fatalf("pick after completion: %v", err)
		}
		// Reading the queue after the draft is not an error: it is empty.
		if q, err := s.GetQueue(ctx, users[0], l.ID); err != nil || len(q.Players) != 0 || q.TeamID != l.Teams[0].ID {
			t.Fatalf("queue after completion: %+v, %v", q, err)
		}
		if _, err := s.SetQueue(ctx, users[0], l.ID, nil); slug(err) != "draft-complete" {
			t.Fatalf("queue change after completion: %v", err)
		}
		kinds := notes.Kinds()
		for _, k := range []string{league.ChangePick, league.ChangeDraft, league.ChangeLeague} {
			if !slices.Contains(kinds, k) {
				t.Errorf("no %s change reported", k)
			}
		}
	})
}

// A draft needs at least 2 teams with a manager; the problem says so.
func TestStartNeedsTwoManagers(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		u := e.User(t)
		l, err := e.Leagues.Create(e.Ctx, u, "Half Empty", 4, "Mine", league.SettingsInput{})
		if err != nil {
			t.Fatal(err)
		}
		_, err = s.Start(e.Ctx, u, l.ID)
		if slug(err) != "not-enough-managers" {
			t.Fatalf("start: %v", err)
		}
		var le *league.Error
		if !errors.As(err, &le) || le.Kind != league.KindConflict ||
			!strings.Contains(le.Detail, "1 of 4 teams") || !strings.Contains(le.Detail, "at least 2") {
			t.Fatalf("detail = %q", le.Detail)
		}
		if st := e.State(t, l.ID); st.Draft.Status != league.DraftScheduled || st.League.Status != league.StatusPreDraft {
			t.Fatalf("draft %s, league %s", st.Draft.Status, st.League.Status)
		}
	})
}

// With 2 of 12 managers the draft starts: the 10 empty places become open
// seats ("Open seat N", no manager) that autopick every pick at once, so
// the draft runs to completion on the two managers' picks alone (here
// both on autopick too). Someone who joins afterwards takes over an open
// seat with its drafted squad.
func TestDraftStartsWithTwoManagersAndOpenSeatsAutopick(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.League(t, 2, league.SettingsInput{})
		twelve := 12
		if _, err := e.Leagues.Update(e.Ctx, users[0], l.ID, nil, league.SettingsInput{TeamCount: &twelve}); err != nil {
			t.Fatal(err)
		}
		v, err := s.Start(e.Ctx, users[0], l.ID)
		if err != nil {
			t.Fatalf("start with 2 of 12 managers: %v", err)
		}
		if len(v.League.Teams) != 12 || v.League.MemberCount() != 2 || len(v.Draft.Order) != 12 {
			t.Fatalf("%d teams, %d managers, order %d", len(v.League.Teams), v.League.MemberCount(), len(v.Draft.Order))
		}
		for i, tm := range v.League.Teams[2:] {
			if want := fmt.Sprintf("Open seat %d", i+3); tm.OwnerID != "" || tm.Name != want {
				t.Errorf("team %d = %+v, want %q without a manager", i+3, tm, want)
			}
		}
		// Open seats picked at once: the clock waits on the first manager.
		team, _, _ := Slot(v.Draft.Order, len(v.Draft.Picks)+1)
		if tm, _ := v.League.Team(team); tm.OwnerID == "" {
			t.Fatalf("on the clock: open seat %s", tm.Name)
		}
		for _, pk := range v.Draft.Picks {
			if tm, _ := v.League.Team(pk.TeamID); tm.OwnerID != "" {
				t.Errorf("pick %d by a manager before the clock ran", pk.Overall)
			}
			if pk.MissedClock {
				t.Errorf("open seat's pick %d marked as a missed clock", pk.Overall)
			}
		}
		for _, u := range users {
			if _, err := s.SetAutopick(e.Ctx, u, l.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		st := e.State(t, l.ID)
		if st.Draft.Status != league.DraftComplete || st.League.Status != league.StatusInSeason || len(st.Draft.Picks) != 12*s.Rounds() {
			t.Fatalf("draft %s (%d picks), league %s", st.Draft.Status, len(st.Draft.Picks), st.League.Status)
		}
		first := st.League.Settings.StartGameweek
		for _, tm := range st.League.Teams {
			if n := len(st.Game.RosterAt(tm.ID, first)); n != 15 {
				t.Errorf("%s has %d players", tm.Name, n)
			}
		}
		// 12 teams: 6 matches a gameweek, no average side.
		if n := len(st.Game.MatchupsIn(first)); n != 6 {
			t.Errorf("%d matches in gameweek %d", n, first)
		}

		// A newcomer takes over the first open seat, renamed, squad kept.
		inv, err := e.Leagues.CreateInvite(e.Ctx, users[0], l.ID, nil, nil)
		if err != nil {
			t.Fatalf("invite after the draft: %v", err)
		}
		seat := st.League.Teams[2]
		newcomer := e.User(t)
		joined, team3, err := e.Leagues.AcceptInvite(e.Ctx, newcomer, inv.Code, "Late Arrivals")
		if err != nil {
			t.Fatalf("join after the draft: %v", err)
		}
		if team3.ID != seat.ID || team3.OwnerID != newcomer || team3.Name != "Late Arrivals" || len(joined.Teams) != 12 ||
			joined.MemberCount() != 3 {
			t.Errorf("taken over = %+v (%d teams, %d managers)", team3, len(joined.Teams), joined.MemberCount())
		}
		if got := e.State(t, l.ID).Game.RosterAt(seat.ID, first); !slices.Equal(got, st.Game.RosterAt(seat.ID, first)) {
			t.Error("the open seat's squad changed hands")
		}
	})
}

// A scheduled start time starts the draft from the tick once it passes.
func TestStartIfDue(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		at := e.Clock.Now().Add(time.Hour)
		l, _ := e.League(t, 4, league.SettingsInput{DraftStartsAt: &at})
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		if ok, err := s.StartIfDue(e.Ctx, l.ID); ok || err != nil {
			t.Fatalf("early: %v %v", ok, err)
		}
		e.Clock.Advance(time.Hour)
		if ok, err := s.StartIfDue(e.Ctx, l.ID); !ok || err != nil {
			t.Fatalf("due: %v %v", ok, err)
		}
		if ok, _ := s.StartIfDue(e.Ctx, l.ID); ok {
			t.Fatal("started twice")
		}
	})
}

// hookRepo runs before once, just before the next transaction: a change
// another request commits between a job's unlocked read and its locked one.
type hookRepo struct {
	league.Repo
	before func()
}

func (r *hookRepo) Tx(ctx context.Context, fn func(tx league.Tx) error) error {
	if b := r.before; b != nil {
		r.before = nil
		b()
	}
	return r.Repo.Tx(ctx, fn)
}

// A postponement committed after the tick read the league but before it
// locked it wins: the draft does not start.
func TestStartIfDueRechecksUnderLock(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		at := e.Clock.Now().Add(time.Hour)
		l, users := e.League(t, 4, league.SettingsInput{DraftStartsAt: &at})
		direct := New(e.Repo, e.Stats, e.Clock.Now, nil)
		if _, err := direct.Get(e.Ctx, users[0], l.ID); err != nil {
			t.Fatal(err)
		}
		e.Clock.Advance(time.Hour)
		later := e.Clock.Now().Add(24 * time.Hour)
		hr := &hookRepo{Repo: e.Repo, before: func() {
			if _, err := direct.UpdateDraft(e.Ctx, users[0], l.ID, Update{StartsAt: &later}); err != nil {
				t.Error(err)
			}
		}}
		tick := New(hr, e.Stats, e.Clock.Now, nil)
		if ok, err := tick.StartIfDue(e.Ctx, l.ID); ok || err != nil {
			t.Fatalf("started despite the postponement: %v %v", ok, err)
		}
		st := e.State(t, l.ID)
		if st.Draft.Status != league.DraftScheduled || st.League.Status != league.StatusPreDraft {
			t.Fatalf("draft %s, league %s", st.Draft.Status, st.League.Status)
		}
	})
}

// Many simultaneous picks for one slot: exactly one wins.
func TestConcurrentPicks(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.League(t, 4, league.SettingsInput{})
		order := []string{l.Teams[0].ID, l.Teams[1].ID, l.Teams[2].ID, l.Teams[3].ID}
		if _, err := s.UpdateDraft(e.Ctx, users[0], l.ID, Update{Order: order}); err != nil {
			t.Fatal(err)
		}
		v, err := s.Start(e.Ctx, users[0], l.ID)
		if err != nil {
			t.Fatal(err)
		}
		var candidates []string
		for _, pos := range []scoring.Position{scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD} {
			candidates = append(candidates, bestAt(t, v, pos))
		}
		var (
			wg   sync.WaitGroup
			mu   sync.Mutex
			wins int
			errs []string
		)
		for i := 0; i < 12; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				// The team on the clock and the commissioner race, with
				// different and identical players.
				_, _, err := s.Pick(e.Ctx, users[0], l.ID, candidates[i%len(candidates)])
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					wins++
				} else {
					errs = append(errs, slug(err))
				}
			}()
		}
		wg.Wait()
		// users[0] is both on the clock (team 1, pick 1) and commissioner:
		// after the first pick it may still pick for team 2 as
		// commissioner, so count picks per slot instead.
		v, _ = s.Get(e.Ctx, users[0], l.ID)
		seen := map[string]bool{}
		for _, p := range v.Draft.Picks {
			if seen[p.PlayerID] {
				t.Fatalf("player %s drafted twice", p.PlayerID)
			}
			seen[p.PlayerID] = true
		}
		if wins != len(v.Draft.Picks) || wins == 0 {
			t.Fatalf("wins %d, picks %d, errors %v", wins, len(v.Draft.Picks), errs)
		}
		for i, p := range v.Draft.Picks {
			if p.Overall != i+1 {
				t.Fatalf("pick %d has overall %d", i, p.Overall)
			}
		}
		for _, s := range errs {
			if s != "player-taken" && s != "position-full" {
				t.Fatalf("unexpected error %q", s)
			}
		}
	})
}

// Two different managers racing for the same slot: only the one on the
// clock can win, exactly once.
func TestConcurrentPicksOneSlot(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		l, users := e.League(t, 4, league.SettingsInput{})
		order := []string{l.Teams[1].ID, l.Teams[2].ID, l.Teams[3].ID, l.Teams[0].ID}
		if _, err := s.UpdateDraft(e.Ctx, users[0], l.ID, Update{Order: order}); err != nil {
			t.Fatal(err)
		}
		v, err := s.Start(e.Ctx, users[0], l.ID)
		if err != nil {
			t.Fatal(err)
		}
		mid := bestAt(t, v, scoring.MID)
		var wg sync.WaitGroup
		results := make([]error, 8)
		for i := range results {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _, results[i] = s.Pick(e.Ctx, users[1], l.ID, mid)
			}()
		}
		wg.Wait()
		wins := 0
		for _, err := range results {
			switch slug(err) {
			case "":
				wins++
			case "not-your-turn", "player-taken":
			default:
				t.Fatalf("unexpected: %v", err)
			}
		}
		if wins != 1 {
			t.Fatalf("wins = %d, want 1", wins)
		}
	})
}

// With no gameweek left to play (ADR 0010: the draft's first gameweek is
// the first whose deadline has not passed), a draft does not start; one
// that started in time and finishes after the last deadline completes with
// rosters after the season, no lineups or matchups, and the league complete.
func TestNoGameweekLeft(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		w := e.World(t)
		last := w.Gameweeks[38].DeadlineAt
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)

		// Starting after the last deadline.
		l, users := e.League(t, 4, league.SettingsInput{})
		e.Clock.Set(last.Add(time.Minute))
		if _, err := s.Start(e.Ctx, users[0], l.ID); slug(err) != "season-over" {
			t.Fatalf("start after the last deadline: %v", err)
		}
		if st := e.State(t, l.ID); st.Draft.Status != league.DraftScheduled || st.League.Status != league.StatusPreDraft {
			t.Fatalf("draft %s, league %s", st.Draft.Status, st.League.Status)
		}
		at := last.Add(-time.Hour)
		if _, err := e.Leagues.Update(e.Ctx, users[0], l.ID, nil, league.SettingsInput{DraftStartsAt: &at}); err != nil {
			t.Fatal(err)
		}
		if ok, err := s.StartIfDue(e.Ctx, l.ID); ok || err != nil {
			t.Fatalf("tick started a draft with no gameweek left: %v %v", ok, err)
		}

		// Started in time, completing after the last deadline.
		e.Clock.Set(last.Add(-time.Hour))
		l2, users2 := e.League(t, 4, league.SettingsInput{})
		for _, u := range users2[1:] {
			if _, err := s.SetAutopick(e.Ctx, u, l2.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := s.Start(e.Ctx, users2[0], l2.ID); err != nil {
			t.Fatal(err)
		}
		e.Clock.Set(last.Add(time.Minute))
		if _, err := s.SetAutopick(e.Ctx, users2[0], l2.ID, true); err != nil {
			t.Fatalf("completion after the last deadline: %v", err)
		}
		st := e.State(t, l2.ID)
		if st.Draft.Status != league.DraftComplete || st.League.Status != league.StatusComplete || len(st.Game.Rosters) != 60 {
			t.Fatalf("draft %s, league %s, %d roster slots", st.Draft.Status, st.League.Status, len(st.Game.Rosters))
		}
		for _, r := range st.Game.Rosters {
			if r.FromGW != 39 {
				t.Fatalf("roster slot from gameweek %d", r.FromGW)
			}
		}
		if len(st.Game.Lineups) != 0 || len(st.Game.Matchups) != 0 {
			t.Fatalf("lineups %d, matchups %d", len(st.Game.Lineups), len(st.Game.Matchups))
		}
		if _, _, err := s.Pick(e.Ctx, users2[0], l2.ID, st.Draft.Picks[0].PlayerID); slug(err) != "draft-complete" {
			t.Fatalf("pick after completion: %v", err)
		}
	})
}

// A league drafted mid-season (ADR 0010) scores from the first gameweek
// whose deadline is after the draft completes, and records it as its
// startGameweek: no roster, lineup or matchup before it, so no view offers
// a gameweek from before the league existed.
func TestMidSeasonDraftStartsAtFirstOpenGameweek(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		w := e.World(t)
		first := w.UpcomingGW(e.Clock.Now())
		if first <= 1 || w.Status.Current >= first {
			t.Fatalf("test needs a mid-season clock: upcoming %d, current %d", first, w.Status.Current)
		}
		l, users := e.League(t, 4, league.SettingsInput{})
		if l.Settings.StartGameweek != 1 {
			t.Fatalf("new league starts at %d", l.Settings.StartGameweek)
		}
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		if _, err := s.Start(e.Ctx, users[0], l.ID); err != nil {
			t.Fatal(err)
		}
		for _, u := range users {
			if _, err := s.SetAutopick(e.Ctx, u, l.ID, true); err != nil {
				t.Fatal(err)
			}
		}
		st := e.State(t, l.ID)
		if st.Draft.Status != league.DraftComplete {
			t.Fatalf("draft %s", st.Draft.Status)
		}
		if got := st.League.Settings.StartGameweek; got != first {
			t.Errorf("startGameweek = %d, want %d (the first gameweek after the draft)", got, first)
		}
		if len(st.Game.Matchups) == 0 {
			t.Fatal("no matchups")
		}
		for _, m := range st.Game.Matchups {
			if m.Gameweek < first {
				t.Fatalf("matchup in gameweek %d, before the league's first (%d)", m.Gameweek, first)
			}
		}
		for _, r := range st.Game.Rosters {
			if r.FromGW != first {
				t.Fatalf("roster slot from gameweek %d, want %d", r.FromGW, first)
			}
		}
	})
}

// A missed pick clock is never silent: the pick is marked missedClock and
// joins the activity feed with the team, player and pick. With "autopick
// when my clock runs out" on (the default) autopick then stays on; with it
// off the pick is still made, so the draft never stalls, but the team gets
// a full clock for its next pick.
func TestMissedClockIsAnnounced(t *testing.T) {
	leaguetest.Each(t, func(t *testing.T, e *leaguetest.Env) {
		l, users := e.League(t, 4, league.SettingsInput{})
		s := New(e.Repo, e.Stats, e.Clock.Now, nil)
		q, err := s.GetQueue(e.Ctx, users[1], l.ID)
		if err != nil || !q.OnTimeout {
			t.Fatalf("default autopickOnTimeout = %+v, %v", q, err)
		}
		if q, err = s.SetAutopickOnTimeout(e.Ctx, users[1], l.ID, false); err != nil || q.OnTimeout || q.Autopick {
			t.Fatalf("switch off: %+v, %v", q, err)
		}
		v, err := s.Start(e.Ctx, users[0], l.ID)
		if err != nil {
			t.Fatal(err)
		}
		order := v.Draft.Order
		if order[0] != l.Teams[0].ID || order[1] != l.Teams[1].ID {
			t.Fatalf("order %v", order)
		}
		timer := time.Duration(v.Draft.PickTimerS) * time.Second

		// Team 1 (switch on) misses its clock.
		e.Clock.Advance(timer + time.Second)
		if changed, err := s.Expire(e.Ctx, l.ID); err != nil || !changed {
			t.Fatalf("expire 1: %v %v", changed, err)
		}
		// Team 2 (switch off) misses its clock too.
		e.Clock.Advance(timer + time.Second)
		if changed, err := s.Expire(e.Ctx, l.ID); err != nil || !changed {
			t.Fatalf("expire 2: %v %v", changed, err)
		}
		st := e.State(t, l.ID)
		if len(st.Draft.Picks) != 2 {
			t.Fatalf("%d picks", len(st.Draft.Picks))
		}
		for i, p := range st.Draft.Picks {
			if !p.MissedClock || (p.Method != league.PickQueue && p.Method != league.PickRank) {
				t.Errorf("pick %d = %+v, want a missed-clock autopick", i+1, p)
			}
		}
		if !st.Draft.Autopick[order[0]] {
			t.Error("switch on: autopick should stay on after a missed clock")
		}
		if st.Draft.Autopick[order[1]] {
			t.Error("switch off: autopick turned on after a missed clock")
		}
		if c := ClockAt(st.Draft, s.Rounds(), 3); c == nil || c.TeamID != order[2] {
			t.Fatalf("clock after two misses: %+v", c)
		}

		feed, err := e.Repo.Transactions(e.Ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		var got []league.Txn
		for _, x := range feed {
			if x.Kind == league.TxnDraftAutopick {
				got = append(got, x)
			}
		}
		if len(got) != 2 {
			t.Fatalf("feed has %d draft autopicks, want 2: %+v", len(got), feed)
		}
		for _, x := range got {
			p := st.Draft.Picks[x.Overall-1]
			if x.TeamID != p.TeamID || x.PlayerIn != p.PlayerID || x.Method != p.Method ||
				x.Gameweek != e.World(t).UpcomingGW(e.Clock.Now()) || !x.OccurredAt.Equal(p.PickedAt) {
				t.Errorf("feed entry %+v for pick %+v", x, p)
			}
		}

		// A manual pick and an autopick by choice are not announced.
		e.Clock.Advance(time.Second)
		if _, _, err := s.Pick(e.Ctx, users[2], l.ID, bestAt(t, &View{Draft: st.Draft, World: e.World(t)}, scoring.MID)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SetAutopick(e.Ctx, users[3], l.ID, true); err != nil {
			t.Fatal(err)
		}
		st = e.State(t, l.ID)
		for _, p := range st.Draft.Picks[2:] {
			if p.MissedClock {
				t.Errorf("pick %d marked missedClock: %+v", p.Overall, p)
			}
		}
		if feed, _ := e.Repo.Transactions(e.Ctx, l.ID); len(feed) != 2 {
			t.Errorf("feed has %d entries after a manual pick and an autopick, want 2", len(feed))
		}
	})
}
