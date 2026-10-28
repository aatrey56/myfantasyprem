package mirror_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
)

// The verifier must catch a wrong import, not only bless a right one.
// Each case corrupts one value of the real import, verifies, and expects
// a mismatch of that kind that the commissioner's report shows.

// mismatches returns the report's mismatch items of a kind.
func mismatches(rep *mirror.Report, kind string) []mirror.Item {
	var out []mirror.Item
	for _, it := range rep.Items {
		if it.Kind == kind && it.Status == mirror.ItemMismatch {
			out = append(out, it)
		}
	}
	return out
}

// mustBlock checks the commissioner's report shows unexplained items
// (informational since 2026-10-14: nothing waits on them).
func mustBlock(t *testing.T, e *env, owner, leagueID string) {
	t.Helper()
	if v, err := e.svc.View(ctx, owner, leagueID); err != nil || len(v.Unexplained) == 0 {
		t.Errorf("report of a corrupted import: %v unexplained, %v", len(v.Unexplained), err)
	}
}

// verifyStored re-verifies the stored league and logs every mismatch.
func verifyStored(t *testing.T, e *env, leagueID string) *mirror.Report {
	t.Helper()
	rep, err := e.svc.VerifyStored(ctx, leagueID)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range rep.Items {
		if it.Status == mirror.ItemMismatch {
			t.Logf("%s: mismatch %s: ours %q, FPL %q", e.name, it.Key, it.Ours, it.Theirs)
		}
	}
	return rep
}

// healed re-syncs: FPL's state is applied again and our scores are
// recomputed, so the report is clean with nothing unexplained.
func healed(t *testing.T, e *env, owner, leagueID string) {
	t.Helper()
	*e.clock = e.clock.Add(20 * time.Minute)
	if err := e.svc.Resync(ctx, leagueID); err != nil {
		t.Fatal(err)
	}
	rep, err := e.svc.Report(ctx, leagueID)
	if err != nil || rep.Status != mirror.ReportClean {
		t.Fatalf("after a re-sync: %v %v", rep, err)
	}
	if v, err := e.svc.View(ctx, owner, leagueID); err != nil || len(v.Unexplained) != 0 {
		t.Errorf("after the re-sync healed it: %+v %v", v, err)
	}
}

// corrupt changes the stored league in one transaction.
func corrupt(t *testing.T, e *env, leagueID string, fn func(st *league.State)) {
	t.Helper()
	err := e.leagues.Tx(ctx, func(tx league.Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		fn(st)
		return tx.SaveState(ctx, st)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// A seat of the capture and the starter it plays in goal in gameweek 3.
const (
	corruptEntry = 900005
	corruptGW    = 3
)

// zeroMinutes is the gameweek's live stats with one player's minutes set
// to 0 (his total and per-fixture lines).
func zeroMinutes(t *testing.T, element string) func([]byte) []byte {
	return func(b []byte) []byte {
		var live map[string]any
		if err := json.Unmarshal(b, &live); err != nil {
			t.Fatal(err)
		}
		el := live["elements"].(map[string]any)[element].(map[string]any)
		el["stats"].(map[string]any)["minutes"] = 0
		for _, fx := range el["explain"].([]any) {
			for _, line := range fx.([]any)[0].([]any) {
				if l := line.(map[string]any); l["stat"] == "minutes" {
					l["value"], l["points"] = 0, 0
				}
			}
		}
		out, err := json.Marshal(live)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
}

func TestVerifierCatchesWrongMinutes(t *testing.T) {
	raw, err := os.ReadFile(realDir + "/entry/" + strconv.Itoa(corruptEntry) + "/event/" + strconv.Itoa(corruptGW) + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var picks struct {
		Picks []struct {
			Element  int `json:"element"`
			Position int `json:"position"`
		} `json:"picks"`
	}
	if err := json.Unmarshal(raw, &picks); err != nil || picks.Picks[0].Position != 1 {
		t.Fatalf("picks: %v", err)
	}
	keeper := strconv.Itoa(picks.Picks[0].Element)
	live := "event/" + strconv.Itoa(corruptGW) + "/live.json"
	fsys := patchedFS{base: os.DirFS(realDir), patch: map[string]func([]byte) []byte{live: zeroMinutes(t, keeper)}}

	realEnvsFS(t, fsys, func(t *testing.T, e *env) {
		owner, st, rep := importReal(t, e)
		var team string
		for _, tm := range st.League.Teams {
			if tm.FPLEntryID == corruptEntry {
				team = tm.ID
			}
		}
		for _, it := range rep.Items {
			if it.Status == mirror.ItemMismatch {
				t.Logf("%s: mismatch %s: ours %q, FPL %q", e.name, it.Key, it.Ours, it.Theirs)
			}
		}
		if rep.Status != mirror.ReportMismatch {
			t.Errorf("report %s, want mismatch", rep.Status)
		}
		pts := mismatches(rep, mirror.KindPoints)
		if len(pts) != 1 || pts[0].Key != "points:gw"+strconv.Itoa(corruptGW)+":"+team {
			t.Errorf("points mismatches = %+v, want only %s in gameweek %d", pts, team, corruptGW)
		}
		res := mismatches(rep, mirror.KindResult)
		if len(res) != 1 || res[0].Gameweek != corruptGW {
			t.Errorf("result mismatches = %+v", res)
		}
		if len(mismatches(rep, mirror.KindStanding)) == 0 {
			t.Error("our table agrees with FPL's despite a wrong score")
		}
		// FPL's own points through our table logic still agree: the table
		// check isolates the scores from the schedule and tie-breaks.
		if tb := mismatches(rep, mirror.KindTable); len(tb) != 0 {
			t.Errorf("table mismatches = %+v", tb)
		}
		mustBlock(t, e, owner, st.League.ID)
	})
}

func TestVerifierCatchesWrongMatchScore(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner, st, _ := importReal(t, e)
		if rep := verifyStored(t, e, st.League.ID); rep.Status != mirror.ReportClean {
			t.Fatalf("control: %s", rep.Status)
		}
		var m league.Matchup
		for _, mu := range st.Game.Matchups {
			if mu.Gameweek == 4 && mu.HomeTeamID != "" && mu.AwayTeamID != "" {
				m = mu
				break
			}
		}
		m.HomePoints += 7 // the score as stored and shown, not FPL's
		if _, err := e.leagues.SaveScores(ctx, st.League.ID, 4, nil, []league.Matchup{m}); err != nil {
			t.Fatal(err)
		}
		rep := verifyStored(t, e, st.League.ID)
		res := mismatches(rep, mirror.KindResult)
		if len(res) != 1 || res[0].Key != "result:gw4:"+m.ID {
			t.Errorf("result mismatches = %+v, want %s", res, m.ID)
		}
		if len(mismatches(rep, mirror.KindStanding)) == 0 {
			t.Error("our table agrees with FPL's despite a wrong stored score")
		}
		if p := mismatches(rep, mirror.KindPoints); len(p) != 0 {
			t.Errorf("points mismatches = %+v: the engine's scores are right", p)
		}
		mustBlock(t, e, owner, st.League.ID)
		healed(t, e, owner, st.League.ID)
	})
}

func TestVerifierCatchesWrongSquad(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner, st, _ := importReal(t, e)
		// Swap two players between two teams in gameweek 5.
		var a, b = -1, -1
		for i, r := range st.Game.Rosters {
			if r.FromGW != 5 && r.OnRosterAt(5) {
				switch {
				case a < 0:
					a = i
				case b < 0 && r.TeamID != st.Game.Rosters[a].TeamID:
					b = i
				}
			}
		}
		if a < 0 || b < 0 {
			t.Fatal("no slots to swap")
		}
		teamA, teamB := st.Game.Rosters[a].TeamID, st.Game.Rosters[b].TeamID
		// A slot's team is fixed once stored, so the swapped slots are new
		// rows (ID 0) replacing the old ones.
		corrupt(t, e, st.League.ID, func(s *league.State) {
			rs := s.Game.Rosters
			for i := range rs {
				switch rs[i].ID {
				case st.Game.Rosters[a].ID:
					rs[i].TeamID, rs[i].ID = teamB, 0
				case st.Game.Rosters[b].ID:
					rs[i].TeamID, rs[i].ID = teamA, 0
				}
			}
		})
		rep := verifyStored(t, e, st.League.ID)
		got := map[string]bool{}
		for _, it := range mismatches(rep, mirror.KindSquad) {
			if it.Gameweek == 5 {
				got[it.TeamID] = true
			}
		}
		if !got[teamA] || !got[teamB] || len(got) != 2 {
			t.Errorf("gameweek 5 squad mismatches for %v, want %s and %s", got, teamA, teamB)
		}
		mustBlock(t, e, owner, st.League.ID)
		healed(t, e, owner, st.League.ID)
	})
}

// A league whose commissioner chose rolling keeps FPL's order as its own:
// a stored order that differs from FPL's is caught.
func TestVerifierCatchesWrongWaiverOrder(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner, st, _ := importReal(t, e)
		corrupt(t, e, st.League.ID, func(s *league.State) {
			s.League.Settings.WaiverOrder = league.WaiverRolling
			s.Priority[0], s.Priority[1] = s.Priority[1], s.Priority[0]
		})
		rep := verifyStored(t, e, st.League.ID)
		if w := mismatches(rep, mirror.KindWaiverOrder); len(w) != 1 {
			t.Errorf("waiver order mismatches = %+v", w)
		}
		mustBlock(t, e, owner, st.League.ID)
		healed(t, e, owner, st.League.ID)
	})
}

// With reverse standings the order is worked out from our standings: a
// stored gameweek 6 result flipped (the loser of its most one-sided match
// wins it) changes our table, and the order no longer matches FPL's
// waiver_pick.
func TestVerifierCatchesWrongReverseStandings(t *testing.T) {
	realEnvs(t, func(t *testing.T, e *env) {
		owner, st, _ := importReal(t, e)
		var m league.Matchup
		for _, mu := range st.Game.Matchups {
			if mu.Gameweek == 6 && mu.AwayTeamID != "" && abs(mu.HomePoints-mu.AwayPoints) > abs(m.HomePoints-m.AwayPoints) {
				m = mu
			}
		}
		m.HomePoints, m.AwayPoints = m.AwayPoints, m.HomePoints
		if _, err := e.leagues.SaveScores(ctx, st.League.ID, 6, nil, []league.Matchup{m}); err != nil {
			t.Fatal(err)
		}
		rep := verifyStored(t, e, st.League.ID)
		if w := mismatches(rep, mirror.KindWaiverOrder); len(w) != 1 {
			t.Errorf("waiver order mismatches = %+v", w)
		}
		mustBlock(t, e, owner, st.League.ID)
		healed(t, e, owner, st.League.ID)
	})
}

func abs(n int) int { return max(n, -n) }

// FPL Draft reports the phase as transaction_mode: "free-agency" between
// the waiver run and the deadline. The same capture in that phase still
// verifies with nothing unverified.
func TestFreeAgencyPhaseVerifies(t *testing.T) {
	details := "league/" + strconv.Itoa(realLeague) + "/details.json"
	fsys := patchedFS{base: os.DirFS(realDir), patch: map[string]func([]byte) []byte{details: func(b []byte) []byte {
		out := bytes.Replace(b, []byte(`"transaction_mode":"waivers"`), []byte(`"transaction_mode":"free-agency"`), 1)
		if bytes.Equal(out, b) {
			t.Fatal("no transaction_mode in the capture")
		}
		return out
	}}}
	realEnvsFS(t, fsys, func(t *testing.T, e *env) {
		_, _, rep := importReal(t, e)
		for _, it := range rep.Items {
			if it.Status != mirror.ItemOK {
				t.Errorf("%s %s: ours %q, FPL %q %s", it.Status, it.Key, it.Ours, it.Theirs, it.Note)
			}
		}
	})
}
