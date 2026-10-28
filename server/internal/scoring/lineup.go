package scoring

import (
	"fmt"
	"slices"
	"strconv"
)

// Counts is a number per position.
type Counts map[Position]int

// LineupRules describe a legal squad and starting XI. StandardLineup holds
// the v1 rules: a 15-man squad of 2 GKP, 5 DEF, 5 MID, 3 FWD; an XI of
// exactly 1 GKP, 3-5 DEF, 2-5 MID, 1-3 FWD; the first bench slot (12) must be
// the other goalkeeper.
type LineupRules struct {
	Squad                Counts
	Starters             int
	MinStarters          Counts
	MaxStarters          Counts
	BenchGoalkeeperFirst bool
}

// StandardLineup returns the v1 squad and formation rules.
func StandardLineup() LineupRules {
	return LineupRules{
		Squad:                Counts{GKP: 2, DEF: 5, MID: 5, FWD: 3},
		Starters:             11,
		MinStarters:          Counts{GKP: 1, DEF: 3, MID: 2, FWD: 1},
		MaxStarters:          Counts{GKP: 1, DEF: 5, MID: 5, FWD: 3},
		BenchGoalkeeperFirst: true,
	}
}

// SquadSize is the total of Squad.
func (r LineupRules) SquadSize() int {
	n := 0
	for _, c := range r.Squad {
		n += c
	}
	return n
}

// Violation codes. They match LineupViolationCode in api/openapi.yaml.
const (
	ViolationStarterCount    = "wrong_starter_count"
	ViolationBenchCount      = "wrong_bench_count"
	ViolationTooFew          = "too_few_" // + position plural, see positionWord
	ViolationTooMany         = "too_many_"
	ViolationBenchGKNotFirst = "bench_goalkeeper_not_first"
	ViolationNotOnRoster     = "player_not_on_roster"
	ViolationDuplicate       = "duplicate_player"
	ViolationMissing         = "player_missing"
	ViolationSquadShape      = "invalid_squad"
)

// Violation is one reason a lineup is illegal.
type Violation struct {
	Code     string
	Message  string
	PlayerID string
}

var positionWord = map[Position]string{GKP: "goalkeepers", DEF: "defenders", MID: "midfielders", FWD: "forwards"}

// Slotted is a player in a lineup.
type Slotted struct {
	PlayerID string
	Position Position
}

// ValidateLineup checks a proposed XI and bench against the roster (player id
// to position). starters are slots 1-11 in order, bench slots 12-15 in
// priority order. It returns every violation; none means the lineup is legal.
func (r LineupRules) ValidateLineup(roster map[string]Position, starters, bench []string) []Violation {
	var out []Violation
	if len(starters) != r.Starters {
		out = append(out, Violation{Code: ViolationStarterCount,
			Message: fmt.Sprintf("Pick exactly %d starters.", r.Starters)})
	}
	if want := r.SquadSize() - r.Starters; len(bench) != want {
		out = append(out, Violation{Code: ViolationBenchCount,
			Message: fmt.Sprintf("Put exactly %d players on the bench.", want)})
	}
	seen := map[string]bool{}
	for _, id := range append(append([]string(nil), starters...), bench...) {
		if seen[id] {
			out = append(out, Violation{Code: ViolationDuplicate, PlayerID: id, Message: "A player appears twice."})
			continue
		}
		seen[id] = true
		if _, ok := roster[id]; !ok {
			out = append(out, Violation{Code: ViolationNotOnRoster, PlayerID: id, Message: "That player is not on this roster."})
		}
	}
	var missing []string
	for id := range roster {
		if !seen[id] {
			missing = append(missing, id)
		}
	}
	slices.Sort(missing)
	for _, id := range missing {
		out = append(out, Violation{Code: ViolationMissing, PlayerID: id, Message: "Every roster player must be in the XI or on the bench."})
	}

	counts := Counts{}
	for _, id := range starters {
		if pos, ok := roster[id]; ok {
			counts[pos]++
		}
	}
	for _, pos := range []Position{GKP, DEF, MID, FWD} {
		if counts[pos] < r.MinStarters[pos] {
			out = append(out, Violation{Code: ViolationTooFew + positionWord[pos],
				Message: "Start at least " + strconv.Itoa(r.MinStarters[pos]) + " " + positionWord[pos] + "."})
		}
		if counts[pos] > r.MaxStarters[pos] {
			out = append(out, Violation{Code: ViolationTooMany + positionWord[pos],
				Message: "Start at most " + strconv.Itoa(r.MaxStarters[pos]) + " " + positionWord[pos] + "."})
		}
	}
	if r.BenchGoalkeeperFirst && len(bench) > 0 {
		if pos, ok := roster[bench[0]]; ok && pos != GKP {
			out = append(out, Violation{Code: ViolationBenchGKNotFirst, PlayerID: bench[0],
				Message: "The first bench slot must be your backup goalkeeper."})
		}
	}
	return out
}

// ValidateSquad checks that a roster has exactly the squad shape (2/5/5/3).
func (r LineupRules) ValidateSquad(roster map[string]Position) []Violation {
	counts := Counts{}
	for _, pos := range roster {
		counts[pos]++
	}
	for _, pos := range []Position{GKP, DEF, MID, FWD} {
		if counts[pos] != r.Squad[pos] {
			return []Violation{{Code: ViolationSquadShape,
				Message: fmt.Sprintf("A squad needs %d GKP, %d DEF, %d MID and %d FWD.",
					r.Squad[GKP], r.Squad[DEF], r.Squad[MID], r.Squad[FWD])}}
		}
	}
	return nil
}

// FormationLegal reports whether starter counts are within the min and max.
func (r LineupRules) FormationLegal(counts Counts) bool {
	for _, pos := range []Position{GKP, DEF, MID, FWD} {
		if counts[pos] < r.MinStarters[pos] || counts[pos] > r.MaxStarters[pos] {
			return false
		}
	}
	return true
}

// Formation renders starter counts as DEF-MID-FWD, for example "4-4-2".
func Formation(counts Counts) string {
	return strconv.Itoa(counts[DEF]) + "-" + strconv.Itoa(counts[MID]) + "-" + strconv.Itoa(counts[FWD])
}
