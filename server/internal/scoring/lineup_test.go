package scoring

import (
	"fmt"
	"reflect"
	"testing"
)

// squad returns a legal 15-man roster: g1 g2, d1-d5, m1-m5, f1-f3.
func squad() map[string]Position {
	r := map[string]Position{}
	for pos, n := range map[Position]int{GKP: 2, DEF: 5, MID: 5, FWD: 3} {
		for i := 1; i <= n; i++ {
			r[fmt.Sprintf("%c%d", []rune(string(pos))[0]|0x20, i)] = pos
		}
	}
	return r
}

func codes(vs []Violation) []string {
	out := []string{}
	for _, v := range vs {
		out = append(out, v.Code)
	}
	return out
}

func TestSquadHelper(t *testing.T) {
	if got := StandardLineup().ValidateSquad(squad()); got != nil {
		t.Fatalf("squad() is not legal: %v", got)
	}
}

func TestValidateLineup(t *testing.T) {
	rules := StandardLineup()
	tests := []struct {
		name     string
		starters []string
		bench    []string
		want     []string
	}{
		{"4-4-2", []string{"g1", "d1", "d2", "d3", "d4", "m1", "m2", "m3", "m4", "f1", "f2"}, []string{"g2", "d5", "m5", "f3"}, []string{}},
		{"3-5-2", []string{"g1", "d1", "d2", "d3", "m1", "m2", "m3", "m4", "m5", "f1", "f2"}, []string{"g2", "d4", "d5", "f3"}, []string{}},
		{"5-4-1", []string{"g1", "d1", "d2", "d3", "d4", "d5", "m1", "m2", "m3", "m4", "f1"}, []string{"g2", "m5", "f2", "f3"}, []string{}},
		{"5-2-3", []string{"g1", "d1", "d2", "d3", "d4", "d5", "m1", "m2", "f1", "f2", "f3"}, []string{"g2", "m3", "m4", "m5"}, []string{}},
		{"two defenders", []string{"g1", "d1", "d2", "m1", "m2", "m3", "m4", "m5", "f1", "f2", "f3"}, []string{"g2", "d3", "d4", "d5"}, []string{"too_few_defenders"}},
		{"one midfielder", []string{"g1", "d1", "d2", "d3", "d4", "d5", "m1", "f1", "f2", "f3", "g2"}, []string{"m2", "m3", "m4", "m5"}, []string{"too_many_goalkeepers", "too_few_midfielders", "bench_goalkeeper_not_first"}},
		{"no forward", []string{"g1", "d1", "d2", "d3", "d4", "d5", "m1", "m2", "m3", "m4", "m5"}, []string{"g2", "f1", "f2", "f3"}, []string{"too_few_forwards"}},
		{"two keepers", []string{"g1", "g2", "d1", "d2", "d3", "d4", "m1", "m2", "m3", "m4", "f1"}, []string{"d5", "m5", "f2", "f3"}, []string{"too_many_goalkeepers", "bench_goalkeeper_not_first"}},
		{"no keeper", []string{"d1", "d2", "d3", "d4", "d5", "m1", "m2", "m3", "m4", "f1", "f2"}, []string{"g1", "g2", "m5", "f3"}, []string{"too_few_goalkeepers"}},
		{"bench keeper not first", []string{"g1", "d1", "d2", "d3", "d4", "m1", "m2", "m3", "m4", "f1", "f2"}, []string{"d5", "g2", "m5", "f3"}, []string{"bench_goalkeeper_not_first"}},
		{"ten starters", []string{"g1", "d1", "d2", "d3", "d4", "m1", "m2", "m3", "m4", "f1"}, []string{"g2", "d5", "m5", "f3", "f2"}, []string{"wrong_starter_count", "wrong_bench_count"}},
		{"stranger", []string{"g1", "d1", "d2", "d3", "d4", "m1", "m2", "m3", "m4", "f1", "x9"}, []string{"g2", "d5", "m5", "f3"}, []string{"player_not_on_roster", "player_missing"}},
		{"duplicate", []string{"g1", "d1", "d1", "d3", "d4", "m1", "m2", "m3", "m4", "f1", "f2"}, []string{"g2", "d5", "m5", "f3"}, []string{"duplicate_player", "player_missing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := codes(rules.ValidateLineup(squad(), tt.starters, tt.bench))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("violations = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestValidateSquad(t *testing.T) {
	s := squad()
	delete(s, "f3")
	s["m6"] = MID
	if got := codes(StandardLineup().ValidateSquad(s)); !reflect.DeepEqual(got, []string{"invalid_squad"}) {
		t.Errorf("got %v", got)
	}
}

func TestFormation(t *testing.T) {
	if got := Formation(Counts{GKP: 1, DEF: 3, MID: 5, FWD: 2}); got != "3-5-2" {
		t.Errorf("got %q", got)
	}
}
