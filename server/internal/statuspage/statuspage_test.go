package statuspage

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"testing"
)

// The cases in testdata/form_cases.json are also the app's form tests
// (apps/app/src/lib/form.cases.test.ts), so the server and the mocks
// cannot drift.
type formCases struct {
	Gameweek []struct {
		Name            string
		Current         int
		CurrentFinished bool
		Want            int
	}
	Form []struct {
		Name         string
		FormGameweek int
		Matches      []MatchPoints
		Want         *float64
	}
}

func loadFormCases(t *testing.T) formCases {
	t.Helper()
	b, err := os.ReadFile("testdata/form_cases.json")
	if err != nil {
		t.Fatal(err)
	}
	var c formCases
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Gameweek) == 0 || len(c.Form) == 0 {
		t.Fatal("no cases")
	}
	return c
}

func TestFormGameweekCases(t *testing.T) {
	for _, tc := range loadFormCases(t).Gameweek {
		t.Run(tc.Name, func(t *testing.T) {
			if got := FormGameweek(tc.Current, tc.CurrentFinished); got != tc.Want {
				t.Errorf("FormGameweek(%d, %v) = %d, want %d", tc.Current, tc.CurrentFinished, got, tc.Want)
			}
		})
	}
}

func TestFormCases(t *testing.T) {
	for _, tc := range loadFormCases(t).Form {
		t.Run(tc.Name, func(t *testing.T) {
			got, ok := Form(tc.Matches, tc.FormGameweek)
			switch {
			case tc.Want == nil && ok:
				t.Errorf("Form = %v, want none", got)
			case tc.Want != nil && (!ok || got != *tc.Want):
				t.Errorf("Form = %v (ok %v), want %v", got, ok, *tc.Want)
			}
		})
	}
}

func TestRankChange(t *testing.T) {
	if RankChange(5, 2) != 3 || RankChange(1, 4) != -3 || RankChange(3, 3) != 0 {
		t.Fatal("rank change is before minus after")
	}
}

func TestAttention(t *testing.T) {
	none := Attention(AttentionInput{Gameweek: 8, Starters: []Starter{{Fixtures: 1}, {Fixtures: 2}}})
	if len(none) != 0 || none == nil {
		t.Fatalf("nothing to report = %#v, want an empty list", none)
	}

	got := Attention(AttentionInput{
		Gameweek:    8,
		Starters:    []Starter{{Fixtures: 0}, {Fixtures: 0, Flagged: true}, {Fixtures: 1, Flagged: true}, {Fixtures: 2}},
		TradeOffers: 1, PendingClaims: 3,
	})
	want := []AttentionItem{
		{Kind: KindStartersWithoutFixture, Severity: SeverityCritical, Message: "2 starters have no game in GW 8", Count: 2, Gameweek: 8},
		{Kind: KindInjuredStarters, Severity: SeverityWarning, Message: "2 starters are injured or doubtful", Count: 2, Gameweek: 8},
		{Kind: KindTradeOffers, Severity: SeverityWarning, Message: "1 trade offer waiting for you", Count: 1},
		{Kind: KindPendingWaivers, Severity: SeverityInfo, Message: "3 waiver claims pending", Count: 3, Gameweek: 8},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got  %#v\nwant %#v", got, want)
	}

	one := Attention(AttentionInput{Gameweek: 9, Starters: []Starter{{Fixtures: 0, Flagged: true}}, TradeOffers: 2, PendingClaims: 1})
	var msgs []string
	for _, a := range one {
		msgs = append(msgs, a.Message)
	}
	wantMsgs := []string{"1 starter has no game in GW 9", "1 starter is injured or doubtful",
		"2 trade offers waiting for you", "1 waiver claim pending"}
	if !slices.Equal(msgs, wantMsgs) {
		t.Fatalf("messages = %q, want %q", msgs, wantMsgs)
	}
}

func TestTopAvailable(t *testing.T) {
	var pool []Available
	// 12 of each position, form descending by index, interleaved.
	for i := range 12 {
		for j, pos := range []string{"GKP", "DEF", "MID", "FWD"} {
			pool = append(pool, Available{
				ID: fmt.Sprintf("plr_%s%02d", pos, i), Position: pos, Form: float64(40-i*4-j) / 4, TotalPoints: i,
			})
		}
	}
	slices.Reverse(pool)
	top := TopAvailable(pool)
	if len(top) != 4*TopPerPosition {
		t.Fatalf("%d players, want %d", len(top), 4*TopPerPosition)
	}
	for i := 1; i < len(top); i++ {
		if top[i].Form > top[i-1].Form {
			t.Fatalf("not best first at %d: %v after %v", i, top[i].Form, top[i-1].Form)
		}
	}
	n := map[string]int{}
	for _, p := range top {
		n[p.Position]++
	}
	for pos, c := range n {
		if c != TopPerPosition {
			t.Errorf("%s: %d players", pos, c)
		}
	}
	// The first ten are the overall top ten.
	if top[0].ID != "plr_GKP00" || top[9].ID != "plr_DEF02" {
		t.Errorf("top ten runs %s..%s", top[0].ID, top[9].ID)
	}

	ties := TopAvailable([]Available{
		{ID: "plr_c", Position: "MID", Form: 5, TotalPoints: 30},
		{ID: "plr_b", Position: "MID", Form: 5, TotalPoints: 40},
		{ID: "plr_a", Position: "MID", Form: 5, TotalPoints: 30},
		{ID: "plr_d", Position: "MID", Form: 6, TotalPoints: 1},
	})
	var ids []string
	for _, p := range ties {
		ids = append(ids, p.ID)
	}
	if want := []string{"plr_d", "plr_b", "plr_a", "plr_c"}; !slices.Equal(ids, want) {
		t.Errorf("ties = %v, want %v (form, then season points, then id)", ids, want)
	}
	if got := TopAvailable(nil); got == nil || len(got) != 0 {
		t.Errorf("empty pool = %#v", got)
	}
}

func TestUpcoming(t *testing.T) {
	for _, tc := range []struct {
		next int
		want []int
	}{{8, []int{8, 9, 10}}, {37, []int{37, 38}}, {38, []int{38}}, {39, nil}} {
		if got := Upcoming(tc.next); !slices.Equal(got, tc.want) {
			t.Errorf("Upcoming(%d) = %v, want %v", tc.next, got, tc.want)
		}
	}
}

func TestSortInjuries(t *testing.T) {
	ps := []Flagged{
		{ID: "1", Name: "Zed", Availability: "doubtful"},
		{ID: "2", Name: "bell", Availability: "injured"},
		{ID: "3", Name: "Abe", Availability: "doubtful"},
		{ID: "4", Name: "Cole", Availability: "suspended"},
		{ID: "5", Name: "Adams", Availability: "unavailable"},
	}
	SortInjuries(ps)
	var ids []string
	for _, p := range ps {
		ids = append(ids, p.ID)
	}
	if want := []string{"5", "2", "4", "3", "1"}; !slices.Equal(ids, want) {
		t.Fatalf("order = %v, want %v (out before doubtful, then name ignoring case)", ids, want)
	}
}

func TestChanceOfPlaying(t *testing.T) {
	num := func(n int) *int { return &n }
	for _, tc := range []struct {
		availability, news string
		want               *int
	}{
		{"available", "", nil},
		{"available", "75% chance of playing", nil},
		{"doubtful", "Knock - 75% chance of playing", num(75)},
		{"doubtful", "Illness - 50% chance of playing", num(50)},
		{"doubtful", "Knock - 25% chance", num(25)},
		{"doubtful", "Knock", nil},
		{"doubtful", "150% sure", nil}, // never above 100
		{"injured", "Hamstring injury - expected back in 3 weeks", num(0)},
		{"suspended", "Suspended until the next gameweek", num(0)},
		{"unavailable", "Left the club", num(0)},
	} {
		got := ChanceOfPlaying(tc.availability, tc.news)
		if (got == nil) != (tc.want == nil) || got != nil && *got != *tc.want {
			t.Errorf("ChanceOfPlaying(%q, %q) = %v, want %v", tc.availability, tc.news, deref(got), deref(tc.want))
		}
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}
