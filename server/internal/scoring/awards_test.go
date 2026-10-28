package scoring

import (
	"encoding/json"
	"os"
	"slices"
	"strconv"
	"testing"
)

// The cases in testdata/awards_cases.json are the app's awards tests
// (apps/app/src/lib/awards.test.ts) as data. The app runs the same file
// (awards.cases.test.ts), so a rule change on one side fails the other.

type caseEntry struct {
	AwardCandidate
	Line string `json:"line"`
	N    int    `json:"n"`
	Top  int    `json:"top"`
}

type awardCases struct {
	PlayerOfTheWeek []struct {
		Name string      `json:"name"`
		Pool []caseEntry `json:"pool"`
		Want *string     `json:"want"`
	} `json:"playerOfTheWeek"`
	Compare []struct {
		Name string    `json:"name"`
		A    caseEntry `json:"a"`
		B    caseEntry `json:"b"`
		Want int       `json:"want"`
	} `json:"compare"`
	TeamOfTheWeek []struct {
		Name      string      `json:"name"`
		Pool      []caseEntry `json:"pool"`
		Want      []string    `json:"want"`
		Formation string      `json:"formation"`
	} `json:"teamOfTheWeek"`
}

func expand(t *testing.T, entries []caseEntry) []AwardCandidate {
	t.Helper()
	var out []AwardCandidate
	for _, e := range entries {
		if e.Line == "" {
			out = append(out, e.AwardCandidate)
			continue
		}
		for i := range e.N {
			out = append(out, AwardCandidate{
				ID: e.Line + "-" + strconv.Itoa(i), Position: Position(e.Line),
				Stats: AwardStats{Points: e.Top - i, Minutes: 90},
			})
		}
	}
	return out
}

// UnmarshalJSON defaults minutes to 90 when a candidate leaves it out.
func (e *caseEntry) UnmarshalJSON(b []byte) error {
	type plain caseEntry
	p := plain{AwardCandidate: AwardCandidate{Stats: AwardStats{Minutes: 90}}}
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*e = caseEntry(p)
	return nil
}

func loadCases(t *testing.T) awardCases {
	t.Helper()
	b, err := os.ReadFile("testdata/awards_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var c awardCases
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.PlayerOfTheWeek) == 0 || len(c.TeamOfTheWeek) == 0 || len(c.Compare) == 0 {
		t.Fatal("no cases")
	}
	return c
}

func reversed(pool []AwardCandidate) []AwardCandidate {
	r := slices.Clone(pool)
	slices.Reverse(r)
	return r
}

func TestPlayerOfTheWeekCases(t *testing.T) {
	for _, tc := range loadCases(t).PlayerOfTheWeek {
		t.Run(tc.Name, func(t *testing.T) {
			pool := expand(t, tc.Pool)
			for _, p := range [][]AwardCandidate{pool, reversed(pool)} {
				got, ok := PlayerOfTheWeek(p)
				switch {
				case tc.Want == nil && ok:
					t.Errorf("got %s, want none", got.ID)
				case tc.Want != nil && (!ok || got.ID != *tc.Want):
					t.Errorf("got %q (ok %v), want %s", got.ID, ok, *tc.Want)
				}
			}
		})
	}
}

func TestCompareForAwardCases(t *testing.T) {
	for _, tc := range loadCases(t).Compare {
		t.Run(tc.Name, func(t *testing.T) {
			sign := func(n int) int { return min(max(n, -1), 1) }
			if got := sign(CompareForAward(tc.A.AwardCandidate, tc.B.AwardCandidate)); got != tc.Want {
				t.Errorf("compare(a, b) = %d, want %d", got, tc.Want)
			}
			if got := sign(CompareForAward(tc.B.AwardCandidate, tc.A.AwardCandidate)); got != -tc.Want {
				t.Errorf("compare(b, a) = %d, want %d", got, -tc.Want)
			}
		})
	}
}

func TestTeamOfTheWeekCases(t *testing.T) {
	for _, tc := range loadCases(t).TeamOfTheWeek {
		t.Run(tc.Name, func(t *testing.T) {
			pool := expand(t, tc.Pool)
			for _, p := range [][]AwardCandidate{pool, reversed(pool)} {
				team := TeamOfTheWeek(p)
				var ids []string
				for _, c := range team {
					ids = append(ids, c.ID)
				}
				if !slices.Equal(ids, tc.Want) && !(len(ids) == 0 && len(tc.Want) == 0) {
					t.Errorf("team = %v\nwant   %v", ids, tc.Want)
				}
				if f := TOTWFormation(team); f != tc.Formation {
					t.Errorf("formation = %s, want %s", f, tc.Formation)
				}
			}
		})
	}
}

// Every legal pool gives a team within the shape, whatever the scores.
func TestTeamOfTheWeekShape(t *testing.T) {
	for seed := range 200 {
		var pool []AwardCandidate
		x := uint32(seed*2654435761 + 1)
		next := func(n int) int {
			x ^= x << 13
			x ^= x >> 17
			x ^= x << 5
			return int(x % uint32(n))
		}
		for i := range 60 {
			pos := TOTWOrder[next(4)]
			pool = append(pool, AwardCandidate{
				ID: "p" + strconv.Itoa(i), Position: pos,
				Stats: AwardStats{Points: next(20) - 2, Bonus: next(4), BPS: next(50), Minutes: next(91)},
			})
		}
		team := TeamOfTheWeek(pool)
		n := map[Position]int{}
		for _, c := range team {
			n[c.Position]++
		}
		for pos, lim := range TOTWLimits {
			if n[pos] < lim.Min || n[pos] > lim.Max {
				t.Fatalf("seed %d: %d %s, want %d-%d", seed, n[pos], pos, lim.Min, lim.Max)
			}
		}
		if len(team) != TOTWSize {
			t.Fatalf("seed %d: %d players", seed, len(team))
		}
		// Nobody left out beats a picked outfielder whose place he could take.
		best, _ := PlayerOfTheWeek(pool)
		if best.Position.Valid() && !slices.ContainsFunc(team, func(c AwardCandidate) bool { return c.ID == best.ID }) {
			t.Fatalf("seed %d: Player of the Week %s not in the team", seed, best.ID)
		}
	}
}
