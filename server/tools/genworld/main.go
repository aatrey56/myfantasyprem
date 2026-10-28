// Command genworld writes the demo world: a fictional season in FPL
// Draft's JSON shapes (bootstrap-static, game, pl/event-status and
// event/{gw}/live for the played gameweeks), used by cmd/seed and the
// demo poller source so the app runs against a real server without
// touching FPL. Everything is invented (clubs, players, results) and
// deterministic: the same files on every run.
//
//	go run ./tools/genworld            # writes internal/demo/data
//
// The season: gameweeks 1-6 are final, gameweek 7 is live (seven matches
// final, two over with bonus pending, one in play), gameweek 8 has a
// double for two clubs and a blank for two others. The explain breakdown
// is computed with the scoring package's FPL 2026-27 rules.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

const (
	currentGW = 7
	lastGW    = 38
)

var clubs = []struct{ short, name string }{
	{"ASH", "Ashford"}, {"BRK", "Brookvale"}, {"CLF", "Cliffton"}, {"DAL", "Dalehurst"},
	{"EAS", "Eastmoor"}, {"FEN", "Fenwick"}, {"GRA", "Granton"}, {"HAR", "Harlow Town"},
	{"IRO", "Ironbridge"}, {"KEL", "Kelham"}, {"LAK", "Lakeside"}, {"MER", "Merrow"},
	{"NOR", "Northgate"}, {"OAK", "Oakley"}, {"PRI", "Priory Park"}, {"QUA", "Quayside"},
	{"RIV", "Riverton"}, {"STO", "Stonebridge"}, {"TAL", "Talbot"}, {"WES", "Westbrook"},
}

var surnames = []string{
	"Calder", "Marlow", "Okafor", "Brandt", "Vasquez", "Lindqvist", "Hale", "Moreau", "Dunmore", "Ketteridge",
	"Abara", "Santoro", "Whitlock", "Duarte", "Penhallow", "Ivanek", "Rourke", "Tanaka", "Fairbairn", "Osei",
	"Castellan", "Thorne", "Verhoeven", "Almeida", "Kowalczyk", "Draper", "Nakamura", "Ellery", "Sorensen", "Bellamy",
	"Quinlan", "Faulkner", "Rinaldi", "Harrow", "Delacroix", "Pemberton", "Szabo", "Lamont", "Adeyemi", "Voss",
	"Gallagher", "Mercer", "Ibarra", "Tolliver", "Brightwell", "Haverford", "Ostrander", "Mazur", "Cavendish", "Rask",
	"Yilmaz", "Prentice", "Aldous", "Bernardi", "Kovacs", "Sutcliffe", "Mwangi", "Ferreira", "Lockhart", "Abernethy",
}

var firstNames = []string{"Aiden", "Bruno", "Callum", "Dario", "Emeka"}

// squad is the per-club shape: 2 GKP, 5 DEF, 5 MID, 3 FWD.
var squad = []int{1, 1, 2, 2, 2, 2, 2, 3, 3, 3, 3, 3, 4, 4, 4}

type element struct {
	ID          int    `json:"id"`
	Code        int    `json:"code"`
	FirstName   string `json:"first_name"`
	SecondName  string `json:"second_name"`
	WebName     string `json:"web_name"`
	Team        int    `json:"team"`
	ElementType int    `json:"element_type"`
	Status      string `json:"status"`
	News        string `json:"news"`
	DraftRank   int    `json:"draft_rank"`

	quality float64
	depth   int // 0 = first choice at the position
}

type team struct {
	ID        int    `json:"id"`
	Code      int    `json:"code"`
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
}

type event struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	DeadlineTime string `json:"deadline_time"`
	WaiversTime  string `json:"waivers_time"`
	TradesTime   string `json:"trades_time"`
	Finished     bool   `json:"finished"`
	DataChecked  bool   `json:"data_checked"`
}

type fixture struct {
	ID                  int     `json:"id"`
	Code                int     `json:"code"`
	Event               *int    `json:"event"`
	TeamH               int     `json:"team_h"`
	TeamA               int     `json:"team_a"`
	KickoffTime         *string `json:"kickoff_time"`
	Started             bool    `json:"started"`
	FinishedProvisional bool    `json:"finished_provisional"`
	Finished            bool    `json:"finished"`
	TeamHScore          *int    `json:"team_h_score"`
	TeamAScore          *int    `json:"team_a_score"`
	Minutes             int     `json:"minutes"`
	// Stats are per-player match stats, written only in event/{gw}/live.
	Stats []fixtureStat `json:"stats,omitempty"`
}

// fixtureStat is one identifier of a fixture's stats, as FPL lists them:
// home and away players, best first.
type fixtureStat struct {
	Identifier string        `json:"identifier"`
	H          []statElement `json:"h"`
	A          []statElement `json:"a"`
}

type statElement struct {
	Value   int `json:"value"`
	Element int `json:"element"`
}

type liveStats struct {
	Minutes                       int `json:"minutes"`
	GoalsScored                   int `json:"goals_scored"`
	Assists                       int `json:"assists"`
	CleanSheets                   int `json:"clean_sheets"`
	GoalsConceded                 int `json:"goals_conceded"`
	OwnGoals                      int `json:"own_goals"`
	PenaltiesSaved                int `json:"penalties_saved"`
	PenaltiesMissed               int `json:"penalties_missed"`
	YellowCards                   int `json:"yellow_cards"`
	RedCards                      int `json:"red_cards"`
	Saves                         int `json:"saves"`
	Bonus                         int `json:"bonus"`
	BPS                           int `json:"bps"`
	ClearancesBlocksInterceptions int `json:"clearances_blocks_interceptions"`
	Recoveries                    int `json:"recoveries"`
	Tackles                       int `json:"tackles"`
	DefensiveContribution         int `json:"defensive_contribution"`
	TotalPoints                   int `json:"total_points"`
	// FPL's decimal strings: the ICT index and its parts, expected stats.
	Influence                string `json:"influence"`
	Creativity               string `json:"creativity"`
	Threat                   string `json:"threat"`
	ICTIndex                 string `json:"ict_index"`
	ExpectedGoals            string `json:"expected_goals"`
	ExpectedAssists          string `json:"expected_assists"`
	ExpectedGoalInvolvements string `json:"expected_goal_involvements"`
	ExpectedGoalsConceded    string `json:"expected_goals_conceded"`
}

// unused is the line of a player who did not play, as FPL lists him.
func unused() liveStats {
	return liveStats{Influence: "0.0", Creativity: "0.0", Threat: "0.0", ICTIndex: "0.0", ExpectedGoals: "0.00",
		ExpectedAssists: "0.00", ExpectedGoalInvolvements: "0.00", ExpectedGoalsConceded: "0.00"}
}

// underlying invents ICT and expected stats for one player in one match,
// consistent with his goals, assists and saves. It draws from its own
// stream (seeded by player and fixture), so adding it left every other
// number in the world unchanged.
func underlying(e *element, f *fixture, s scoring.FixtureStats, ls *liveStats) {
	r := rand.New(rand.NewPCG(uint64(e.ID), uint64(f.ID)))
	pos := positions[e.ElementType]
	share := float64(s.Minutes) / 90
	attack := map[scoring.Position]float64{scoring.GKP: 0, scoring.DEF: 0.25, scoring.MID: 0.6, scoring.FWD: 1}[pos] * (0.4 + e.quality)
	cs := 0.0
	if s.CleanSheet && pos != scoring.FWD {
		cs = 1
	}
	xg := round(float64(s.Goals)*0.3+r.Float64()*0.35*attack*share, 2)
	xa := round(float64(s.Assists)*0.25+r.Float64()*0.25*attack*share, 2)
	influence := round(4*share+float64(s.Goals)*20+float64(s.Assists)*12+float64(s.Saves)*4+cs*8+r.Float64()*18, 1)
	creativity := round(float64(s.Assists)*10+r.Float64()*map[scoring.Position]float64{
		scoring.GKP: 3, scoring.DEF: 28, scoring.MID: 42, scoring.FWD: 22}[pos]*share, 1)
	threat := math.Round(float64(s.Goals)*14 + r.Float64()*map[scoring.Position]float64{
		scoring.GKP: 0, scoring.DEF: 12, scoring.MID: 30, scoring.FWD: 45}[pos]*share)
	xgc := round(float64(s.GoalsConceded)*0.6+r.Float64()*0.9*share, 2)
	if pos == scoring.FWD {
		xgc = round(r.Float64()*1.5*share, 2)
	}
	dec := func(x float64, places int) string { return strconv.FormatFloat(x, 'f', places, 64) }
	ls.Influence, ls.Creativity, ls.Threat = dec(influence, 1), dec(creativity, 1), dec(threat, 1)
	ls.ICTIndex = dec(round((influence+creativity+threat)/10, 1), 1)
	ls.ExpectedGoals, ls.ExpectedAssists = dec(xg, 2), dec(xa, 2)
	ls.ExpectedGoalInvolvements, ls.ExpectedGoalsConceded = dec(round(xg+xa, 2), 2), dec(xgc, 2)
}

func round(x float64, places int) float64 {
	p := math.Pow(10, float64(places))
	return math.Round(x*p) / p
}

type explainLine struct {
	Stat   string `json:"stat"`
	Points int    `json:"points"`
	Value  int    `json:"value"`
}

type liveElement struct {
	Stats   liveStats `json:"stats"`
	Explain [][2]any  `json:"explain"`
}

var positions = []scoring.Position{"", scoring.GKP, scoring.DEF, scoring.MID, scoring.FWD}

func main() {
	out := flag.String("out", "internal/demo/data", "output directory")
	flag.Parse()
	r := rand.New(rand.NewPCG(2026, 1009))

	var teams []team
	for i, c := range clubs {
		teams = append(teams, team{ID: i + 1, Code: 9100 + i + 1, Name: c.name, ShortName: c.short})
	}
	strength := map[int]float64{}
	for _, t := range teams {
		strength[t.ID] = 0.7 + 0.6*r.Float64()
	}

	// Players.
	var elements []*element
	byClub := map[int][]*element{}
	for ci := range clubs {
		depth := map[int]int{}
		for _, typ := range squad {
			n := len(elements)
			e := &element{
				ID: n + 1, Code: 9_000_001 + n,
				FirstName: firstNames[(n/len(surnames))%len(firstNames)], SecondName: surnames[n%len(surnames)],
				Team: ci + 1, ElementType: typ, Status: "a",
				depth: depth[typ],
			}
			depth[typ]++
			e.WebName = e.FirstName[:1] + ". " + e.SecondName
			e.quality = strength[e.Team]*(1.2-0.25*float64(e.depth)) + 0.3*r.Float64()
			elements = append(elements, e)
			byClub[e.Team] = append(byClub[e.Team], e)
		}
	}
	// A few availability flags, as FPL shows them.
	flags := []struct{ status, news string }{
		{"d", "Knock - 75% chance of playing"}, {"i", "Hamstring injury - expected back in 3 weeks"},
		{"s", "Suspended until the next gameweek"}, {"d", "Illness - 50% chance of playing"}, {"u", "Left the club"},
	}
	for i, f := range flags {
		e := elements[(i*61+17)%len(elements)]
		e.Status, e.News = f.status, f.news
	}
	ranked := slices.Clone(elements)
	sort.SliceStable(ranked, func(i, j int) bool {
		wi, wj := posWeight(ranked[i]), posWeight(ranked[j])
		return ranked[i].quality*wi > ranked[j].quality*wj
	})
	for i, e := range ranked {
		e.DraftRank = i + 1
	}

	// Gameweeks: Friday deadlines from 14 August 2026, skipping the
	// international breaks.
	var events []event
	day := time.Date(2026, 8, 14, 17, 30, 0, 0, time.UTC)
	breaks := map[string]bool{"2026-09-04": true, "2026-10-02": true, "2026-11-13": true, "2027-03-26": true}
	var deadlines []time.Time
	for gw := 1; gw <= lastGW; gw++ {
		for breaks[day.Format("2006-01-02")] {
			day = day.AddDate(0, 0, 7)
		}
		deadlines = append(deadlines, day)
		events = append(events, event{
			ID: gw, Name: "Gameweek " + strconv.Itoa(gw),
			DeadlineTime: ts(day), WaiversTime: ts(day.Add(-24 * time.Hour)), TradesTime: ts(day.Add(-48 * time.Hour)),
			Finished: gw < currentGW, DataChecked: gw < currentGW,
		})
		day = day.AddDate(0, 0, 7)
	}

	// Fixtures: a double round robin, ten matches a gameweek.
	var fixtures []*fixture
	ids := make([]int, len(teams))
	for i := range ids {
		ids[i] = i + 1
	}
	for gw := 1; gw <= lastGW; gw++ {
		n := len(ids)
		round := (gw - 1) % (n - 1)
		second := gw > n-1
		rot := make([]int, n)
		rot[0] = ids[0]
		for i := 1; i < n; i++ {
			rot[i] = ids[1+(i-1+round)%(n-1)]
		}
		// The deadline is the first kickoff minus 90 minutes (Friday 19:00).
		friday := deadlines[gw-1].Add(90 * time.Minute)
		sat := friday.Add(19 * time.Hour) // Saturday 14:00
		for i := 0; i < n/2; i++ {
			h, a := rot[i], rot[n-1-i]
			if (round+i)%2 == 1 != second {
				h, a = a, h
			}
			var k time.Time
			switch {
			case i == 0:
				k = friday
			case i >= 8:
				k = sat.Add(54 * time.Hour) // Monday 20:00
			case i >= 6:
				k = sat.Add(24 * time.Hour) // Sunday 14:00
			case i >= 4:
				k = sat.Add(150 * time.Minute) // Saturday 16:30
			default:
				k = sat
			}
			g := gw
			ks := ts(k)
			fixtures = append(fixtures, &fixture{ID: len(fixtures) + 1, Event: &g, TeamH: h, TeamA: a, KickoffTime: &ks})
		}
	}
	for _, f := range fixtures {
		f.Code = 2_700_000 + f.ID
	}
	// Gameweek 8: one match postponed (a blank for both clubs) and an
	// earlier postponed match replayed (a double for two others).
	var gw8 []*fixture
	for _, f := range fixtures {
		if *f.Event == 8 {
			gw8 = append(gw8, f)
		}
	}
	gw8[0].Event, gw8[0].KickoffTime = nil, nil
	for _, f := range fixtures {
		if f.Event != nil && *f.Event == 20 && f.TeamH != gw8[0].TeamH && f.TeamA != gw8[0].TeamH && f.TeamH != gw8[0].TeamA && f.TeamA != gw8[0].TeamA {
			g := 8
			k := ts(deadlines[7].Add(4*24*time.Hour + 2*time.Hour)) // Tuesday 19:30
			f.Event, f.KickoffTime = &g, &k
			break
		}
	}

	// Results and per-player stats for the played gameweeks.
	live := map[int]map[string]*liveElement{}
	matchStats := map[int][]fixtureStat{}
	for gw := 1; gw <= currentGW; gw++ {
		live[gw] = map[string]*liveElement{}
		var gwFixtures []*fixture
		for _, f := range fixtures {
			if f.Event != nil && *f.Event == gw {
				gwFixtures = append(gwFixtures, f)
			}
		}
		for i, f := range gwFixtures {
			minute := 90
			switch {
			case gw < currentGW || i < 7:
				f.Started, f.FinishedProvisional, f.Finished = true, true, true
			case i < 9:
				f.Started, f.FinishedProvisional = true, true
			default:
				f.Started, minute = true, 65
			}
			f.Minutes = minute
			bonusIn := f.Finished
			matchStats[f.ID] = playMatch(r, f, byClub, minute, bonusIn, live[gw])
		}
	}

	// Output.
	type fixtureList []*fixture
	byGW := map[string]fixtureList{}
	for _, f := range fixtures {
		k := "0"
		if f.Event != nil {
			k = strconv.Itoa(*f.Event)
		}
		byGW[k] = append(byGW[k], f)
	}
	write(*out, "bootstrap-static.json", map[string]any{
		"elements": elements,
		"element_types": []map[string]any{
			{"id": 1, "singular_name_short": "GKP", "plural_name_short": "GKP", "squad_select": 2},
			{"id": 2, "singular_name_short": "DEF", "plural_name_short": "DEF", "squad_select": 5},
			{"id": 3, "singular_name_short": "MID", "plural_name_short": "MID", "squad_select": 5},
			{"id": 4, "singular_name_short": "FWD", "plural_name_short": "FWD", "squad_select": 3},
		},
		"teams":    teams,
		"events":   map[string]any{"current": currentGW, "next": currentGW + 1, "data": events},
		"fixtures": byGW,
		"settings": map[string]any{"squad": map[string]int{"squad_size": 15, "starting_size": 11}},
	})
	for gw := 1; gw <= currentGW; gw++ {
		// Every element appears, as in FPL's payload.
		all := map[string]*liveElement{}
		for _, e := range elements {
			k := strconv.Itoa(e.ID)
			if le, ok := live[gw][k]; ok {
				all[k] = le
			} else {
				all[k] = &liveElement{Stats: unused(), Explain: [][2]any{}}
			}
		}
		var fx []*fixture
		for _, f := range fixtures {
			if f.Event != nil && *f.Event == gw {
				c := *f
				c.Stats = matchStats[f.ID]
				fx = append(fx, &c)
			}
		}
		write(*out, filepath.Join("event", strconv.Itoa(gw), "live.json"), map[string]any{"elements": all, "fixtures": fx})
	}
	next := currentGW + 1
	write(*out, "game.json", map[string]any{
		"current_event": currentGW, "current_event_finished": false, "next_event": next,
		"processing_status": "n", "waivers_processed": true, "trades_time_for_approval": false,
	})
	fri := deadlines[currentGW-1]
	write(*out, filepath.Join("pl", "event-status.json"), map[string]any{
		"status": []map[string]any{
			{"bonus_added": true, "date": fri.Format("2006-01-02"), "event": currentGW, "points": "r"},
			{"bonus_added": true, "date": fri.AddDate(0, 0, 1).Format("2006-01-02"), "event": currentGW, "points": "r"},
			{"bonus_added": false, "date": fri.AddDate(0, 0, 2).Format("2006-01-02"), "event": currentGW, "points": "l"},
			{"bonus_added": false, "date": fri.AddDate(0, 0, 3).Format("2006-01-02"), "event": currentGW, "points": "l"},
		},
		"leagues": "",
	})
	log.Printf("wrote %d clubs, %d players, %d fixtures, live gameweeks 1-%d to %s", len(teams), len(elements), len(fixtures), currentGW, *out)
}

func posWeight(e *element) float64 {
	return map[int]float64{1: 0.55, 2: 0.8, 3: 1.0, 4: 0.95}[e.ElementType]
}

func ts(t time.Time) string { return t.UTC().Format(time.RFC3339) }

func write(dir, name string, v any) {
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		log.Fatal(err)
	}
	b, err := json.Marshal(v)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(p, append(b, '\n'), 0o644); err != nil {
		log.Fatal(err)
	}
}

// playMatch invents a match, writes every participant's stats and returns
// the match's own bonus and bps lists.
func playMatch(r *rand.Rand, f *fixture, byClub map[int][]*element, minute int, bonusIn bool, out map[string]*liveElement) []fixtureStat {
	goals := func() int {
		n := 0
		for range 6 {
			if r.Float64() < 0.22 {
				n++
			}
		}
		return n
	}
	hs, as := goals(), goals()
	if minute < 90 {
		hs, as = hs*minute/90, as*minute/90
	}
	f.TeamHScore, f.TeamAScore = &hs, &as

	type line struct {
		e     *element
		s     scoring.FixtureStats
		bps   int
		start bool
	}
	var lines []*line
	side := func(club, scored, conceded int) {
		var starters, subs []*element
		for _, e := range byClub[club] {
			first := map[int]int{1: 1, 2: 4, 3: 4, 4: 2}[e.ElementType]
			if e.depth < first && e.Status != "i" && e.Status != "s" && e.Status != "u" {
				starters = append(starters, e)
			} else if e.ElementType != 1 {
				subs = append(subs, e)
			}
		}
		var ls []*line
		for _, e := range starters {
			m := minute
			if e.ElementType != 1 && r.Float64() < 0.25 && minute == 90 {
				m = 60 + r.IntN(25)
			}
			ls = append(ls, &line{e: e, s: scoring.FixtureStats{Minutes: m}, start: true})
		}
		if minute == 90 {
			for _, e := range subs[:min(3, len(subs))] {
				if r.Float64() < 0.6 && e.Status == "a" {
					ls = append(ls, &line{e: e, s: scoring.FixtureStats{Minutes: 5 + r.IntN(25)}})
				}
			}
		}
		// Goals and assists, weighted by position.
		pick := func(weights map[int]float64) *line {
			total := 0.0
			for _, l := range ls {
				total += weights[l.e.ElementType] * l.e.quality
			}
			x := r.Float64() * total
			for _, l := range ls {
				x -= weights[l.e.ElementType] * l.e.quality
				if x <= 0 {
					return l
				}
			}
			return ls[len(ls)-1]
		}
		for range scored {
			pick(map[int]float64{2: 0.4, 3: 1.6, 4: 3}).s.Goals++
			if r.Float64() < 0.75 {
				pick(map[int]float64{2: 0.6, 3: 2.2, 4: 1}).s.Assists++
			}
		}
		for _, l := range ls {
			s := &l.s
			pos := positions[l.e.ElementType]
			if l.start {
				s.GoalsConceded = conceded
			}
			s.CleanSheet = conceded == 0 && s.Minutes >= 60
			switch pos {
			case scoring.GKP:
				s.Saves = r.IntN(7)
				if r.Float64() < 0.04 {
					s.PenaltiesSaved = 1
				}
			case scoring.DEF:
				s.ClearancesBlocksInterceptions, s.Tackles, s.Recoveries = r.IntN(9), r.IntN(5), r.IntN(6)
			case scoring.MID:
				s.ClearancesBlocksInterceptions, s.Tackles, s.Recoveries = r.IntN(4), r.IntN(5), r.IntN(9)
			default:
				s.ClearancesBlocksInterceptions, s.Tackles, s.Recoveries = r.IntN(2), r.IntN(2), r.IntN(5)
			}
			if r.Float64() < 0.12 {
				s.YellowCards = 1
			}
			if r.Float64() < 0.01 {
				s.RedCards = 1
			}
			if pos == scoring.FWD && r.Float64() < 0.02 {
				s.PenaltiesMissed = 1
			}
			if r.Float64() < 0.008 {
				s.OwnGoals = 1
			}
			// A rough model of FPL's 2026-27 BPS: 2 per save, 7 per
			// penalty save, 1 per 3 clearances, blocks and interceptions,
			// no deduction for being tackled.
			goalBPS := map[scoring.Position]int{scoring.GKP: 12, scoring.DEF: 12, scoring.MID: 18, scoring.FWD: 24}[pos]
			l.bps = s.Goals*goalBPS + s.Assists*9 + s.Saves*2 + 7*s.PenaltiesSaved + s.ClearancesBlocksInterceptions/3 +
				s.Tackles/2 + s.Recoveries/3 + r.IntN(8) - 3*s.YellowCards - 9*s.RedCards - 6*s.OwnGoals
			if s.Minutes >= 60 {
				l.bps += 6
			} else {
				l.bps += 3
			}
			if s.CleanSheet && (pos == scoring.GKP || pos == scoring.DEF) {
				l.bps += 12
			}
		}
		lines = append(lines, ls...)
	}
	side(f.TeamH, hs, as)
	side(f.TeamA, as, hs)

	if bonusIn {
		var scores []scoring.BPS
		for _, l := range lines {
			scores = append(scores, scoring.BPS{PlayerID: strconv.Itoa(l.e.ID), Score: l.bps})
		}
		bonus := scoring.BonusFromBPS(scores)
		for _, l := range lines {
			l.s.Bonus = bonus[strconv.Itoa(l.e.ID)]
		}
	}
	rules := scoring.StandardRules()
	for _, l := range lines {
		pos := positions[l.e.ElementType]
		total, breakdown := rules.Points(pos, l.s)
		var expl []explainLine
		for _, b := range breakdown {
			expl = append(expl, explainLine{Stat: string(b.Stat), Points: b.Points, Value: b.Value})
		}
		dc := l.s.ClearancesBlocksInterceptions + l.s.Tackles
		if pos != scoring.DEF {
			dc += l.s.Recoveries
		}
		cs := 0
		if l.s.CleanSheet {
			cs = 1
		}
		ls := liveStats{
			Minutes: l.s.Minutes, GoalsScored: l.s.Goals, Assists: l.s.Assists, CleanSheets: cs,
			GoalsConceded: l.s.GoalsConceded, OwnGoals: l.s.OwnGoals, PenaltiesSaved: l.s.PenaltiesSaved,
			PenaltiesMissed: l.s.PenaltiesMissed, YellowCards: l.s.YellowCards, RedCards: l.s.RedCards,
			Saves: l.s.Saves, Bonus: l.s.Bonus, BPS: l.bps,
			ClearancesBlocksInterceptions: l.s.ClearancesBlocksInterceptions, Recoveries: l.s.Recoveries,
			Tackles: l.s.Tackles, DefensiveContribution: dc, TotalPoints: total,
		}
		underlying(l.e, f, l.s, &ls)
		out[strconv.Itoa(l.e.ID)] = &liveElement{Stats: ls, Explain: [][2]any{{expl, f.ID}}}
	}

	bps := fixtureStat{Identifier: "bps", H: []statElement{}, A: []statElement{}}
	bonus := fixtureStat{Identifier: "bonus", H: []statElement{}, A: []statElement{}}
	for _, l := range lines {
		add := func(st *fixtureStat, v int) {
			if l.e.Team == f.TeamH {
				st.H = append(st.H, statElement{Value: v, Element: l.e.ID})
			} else {
				st.A = append(st.A, statElement{Value: v, Element: l.e.ID})
			}
		}
		add(&bps, l.bps)
		if l.s.Bonus > 0 {
			add(&bonus, l.s.Bonus)
		}
	}
	for _, st := range []*fixtureStat{&bps, &bonus} {
		for _, side := range [][]statElement{st.H, st.A} {
			slices.SortStableFunc(side, func(a, b statElement) int { return b.Value - a.Value })
		}
	}
	return []fixtureStat{bonus, bps}
}
