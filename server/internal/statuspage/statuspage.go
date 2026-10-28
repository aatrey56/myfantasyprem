// Package statuspage computes the league Status page (GET
// /v1/leagues/{leagueId}/status): what needs the caller's attention, the
// change in their league position, the best available players by form,
// and every flagged player. It mirrors the MSW mock
// (apps/app/src/mocks/status.ts) and is pure: no I/O, no clocks. httpapi
// gathers the inputs and maps the results to the contract.
package statuspage

import (
	"cmp"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// UpcomingWeeks is how many gameweeks of fixtures a top available player
// shows.
const UpcomingWeeks = 3

// TopPerPosition is how many available players each position lists.
const TopPerPosition = 10

// FormWeeks is how many completed gameweeks form averages.
const FormWeeks = 2

// FormGameweek is the gameweek form is for: the season's current gameweek
// (FPL's, the one whose deadline passed last) while it is being played,
// the next one once it has finished. Before the season it is gameweek 1;
// after gameweek 38 has finished, 39. Form averages the gameweeks before
// it, so with gameweek 7 live or up next it is gameweeks 5 and 6.
func FormGameweek(current int, currentFinished bool) int {
	if currentFinished {
		return current + 1
	}
	return max(current, 1)
}

// MatchPoints is a player's points in one match of a gameweek.
type MatchPoints struct {
	Gameweek int
	Points   int
}

// Form is the contract's form (PlayerListItem.form): the mean of a
// player's points over the FormWeeks completed gameweeks before gameweek
// formGW (FormGameweek), rounded to one decimal (halves up, as
// JavaScript's Math.round). A gameweek's points are the sum of its matches,
// so a double gameweek counts both; a gameweek without a match or without
// minutes counts as 0, and the divisor is always FormWeeks, or fewer only
// early in the season, when fewer gameweeks have been completed. ok is
// false when none has (gameweek 1): there is no form yet.
func Form(matches []MatchPoints, formGW int) (form float64, ok bool) {
	from := max(formGW-FormWeeks, 1)
	n := formGW - from
	if n <= 0 {
		return 0, false
	}
	sum := 0
	for _, m := range matches {
		if m.Gameweek >= from && m.Gameweek < formGW {
			sum += m.Points
		}
	}
	return math.Floor(float64(sum)/float64(n)*10+0.5) / 10, true
}

// RankChange is how many places a team moved: positive is up. before and
// after are its ranks (1 = top) before and after the last completed
// gameweek.
func RankChange(before, after int) int { return before - after }

// Attention item kinds and severities (StatusAttentionItem).
const (
	KindStartersWithoutFixture = "starters_without_fixture"
	KindInjuredStarters        = "injured_starters"
	KindTradeOffers            = "trade_offers"
	KindPendingWaivers         = "pending_waivers"

	SeverityCritical = "critical"
	SeverityWarning  = "warning"
	SeverityInfo     = "info"
)

// Starter is one of the caller's starters for the next gameweek.
type Starter struct {
	// Flagged: injured, suspended, doubtful or otherwise not available.
	Flagged bool
	// Fixtures is how many matches his club plays in the gameweek.
	Fixtures int
}

// AttentionInput is what the attention strip looks at.
type AttentionInput struct {
	// Gameweek is the next gameweek, the one lineups are set for.
	Gameweek int
	Starters []Starter
	// TradeOffers counts offers the caller can accept.
	TradeOffers int
	// PendingClaims counts the caller's waiver claims waiting for a run.
	PendingClaims int
}

// AttentionItem is one entry of the strip. Gameweek is 0 when the item is
// not about one.
type AttentionItem struct {
	Kind     string
	Severity string
	Message  string
	Count    int
	Gameweek int
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}

// Attention lists what needs the caller, most urgent first: starters with
// no game, injured or doubtful starters, trade offers, pending claims.
// Kinds with nothing to report are left out.
func Attention(in AttentionInput) []AttentionItem {
	blank, flagged := 0, 0
	for _, s := range in.Starters {
		if s.Fixtures == 0 {
			blank++
		}
		if s.Flagged {
			flagged++
		}
	}
	gw := strconv.Itoa(in.Gameweek)
	out := []AttentionItem{}
	if blank > 0 {
		out = append(out, AttentionItem{
			Kind: KindStartersWithoutFixture, Severity: SeverityCritical, Count: blank, Gameweek: in.Gameweek,
			Message: strconv.Itoa(blank) + plural(blank, " starter has", " starters have") + " no game in GW " + gw,
		})
	}
	if flagged > 0 {
		out = append(out, AttentionItem{
			Kind: KindInjuredStarters, Severity: SeverityWarning, Count: flagged, Gameweek: in.Gameweek,
			Message: strconv.Itoa(flagged) + plural(flagged, " starter is", " starters are") + " injured or doubtful",
		})
	}
	if n := in.TradeOffers; n > 0 {
		out = append(out, AttentionItem{
			Kind: KindTradeOffers, Severity: SeverityWarning, Count: n,
			Message: strconv.Itoa(n) + plural(n, " trade offer", " trade offers") + " waiting for you",
		})
	}
	if n := in.PendingClaims; n > 0 {
		out = append(out, AttentionItem{
			Kind: KindPendingWaivers, Severity: SeverityInfo, Count: n, Gameweek: in.Gameweek,
			Message: strconv.Itoa(n) + plural(n, " waiver claim", " waiver claims") + " pending",
		})
	}
	return out
}

// Available is a player who is not on a team in the league.
type Available struct {
	ID          string
	Position    string
	Form        float64
	TotalPoints int
}

// TopAvailable ranks available players by form, then season points, then
// id, and keeps the first TopPerPosition of each position, in that overall
// order: the first ten are the overall top ten.
func TopAvailable(players []Available) []Available {
	ranked := slices.Clone(players)
	slices.SortFunc(ranked, func(a, b Available) int {
		return cmp.Or(cmp.Compare(b.Form, a.Form), cmp.Compare(b.TotalPoints, a.TotalPoints), cmp.Compare(a.ID, b.ID))
	})
	seen := map[string]int{}
	out := []Available{}
	for _, p := range ranked {
		if seen[p.Position] < TopPerPosition {
			out = append(out, p)
		}
		seen[p.Position]++
	}
	return out
}

// Upcoming is the gameweeks a top available player's fixtures are listed
// for: the next UpcomingWeeks from next, never past gameweek 38.
func Upcoming(next int) []int {
	var out []int
	for gw := max(next, 1); gw <= 38 && len(out) < UpcomingWeeks; gw++ {
		out = append(out, gw)
	}
	return out
}

// Availability values (PlayerAvailability).
const (
	AvailabilityAvailable = "available"
	AvailabilityDoubtful  = "doubtful"
)

// Flagged is a player whose availability is not "available".
type Flagged struct {
	ID           string
	Name         string
	Availability string
}

// SortInjuries orders flagged players out (injured, suspended,
// unavailable) before doubtful, then by name (case-insensitive), then id.
func SortInjuries(players []Flagged) {
	slices.SortStableFunc(players, func(a, b Flagged) int {
		da, db := a.Availability == AvailabilityDoubtful, b.Availability == AvailabilityDoubtful
		if da != db {
			if da {
				return 1
			}
			return -1
		}
		return cmp.Or(cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name)),
			cmp.Compare(a.Name, b.Name), cmp.Compare(a.ID, b.ID))
	})
}

var percent = regexp.MustCompile(`(\d{1,3})%`)

// ChanceOfPlaying is the percent chance of playing next: for a doubtful
// player the figure in his news ("Knock - 75% chance of playing", as FPL
// writes it), nil when it has none; 0 for anyone out; nil for an available
// player.
func ChanceOfPlaying(availability, news string) *int {
	switch availability {
	case AvailabilityAvailable:
		return nil
	case AvailabilityDoubtful:
		m := percent.FindStringSubmatch(news)
		if m == nil {
			return nil
		}
		n, _ := strconv.Atoi(m[1])
		if n > 100 {
			return nil
		}
		return &n
	}
	zero := 0
	return &zero
}
