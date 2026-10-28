package fpl

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// History is the per-season career of current players: element-summary's
// history_past, one row per finished Premier League season back to
// 2006/07. It has no club field.
//
// FPL sends 0, not null, for a stat it did not track that season. The
// first season FPL tracked each group (checked against the owner's
// reference export of history_past, 2026-10-17: every earlier row is 0):
//
//   - influence, creativity, threat, ICT index: 2016/17;
//   - starts and the expected stats (xG, xA, xGI, xGC): 2022/23;
//   - defensive_contribution: 2024/25 (FPL back-filled the season before
//     the stat started scoring);
//   - clearances_blocks_interceptions, recoveries, tackles: 2025/26.
//
// Before those seasons the stat is nil, so the card shows "—", not 0.
const (
	firstICTSeason        = 2016
	firstExpectedSeason   = 2022
	firstDefContribSeason = 2024
	firstDefensiveSeason  = 2025
)

type rawElementSummary struct {
	HistoryPast []rawPastSeason `json:"history_past"`
}

type rawPastSeason struct {
	SeasonName      string `json:"season_name"`
	ElementCode     int64  `json:"element_code"`
	StartCost       *int   `json:"start_cost"`
	EndCost         *int   `json:"end_cost"`
	TotalPoints     *int   `json:"total_points"`
	Minutes         *int   `json:"minutes"`
	GoalsScored     *int   `json:"goals_scored"`
	Assists         *int   `json:"assists"`
	CleanSheets     *int   `json:"clean_sheets"`
	GoalsConceded   *int   `json:"goals_conceded"`
	OwnGoals        *int   `json:"own_goals"`
	PenaltiesSaved  *int   `json:"penalties_saved"`
	PenaltiesMissed *int   `json:"penalties_missed"`
	YellowCards     *int   `json:"yellow_cards"`
	RedCards        *int   `json:"red_cards"`
	Saves           *int   `json:"saves"`
	Bonus           *int   `json:"bonus"`
	BPS             *int   `json:"bps"`

	Influence  *decimal `json:"influence"`
	Creativity *decimal `json:"creativity"`
	Threat     *decimal `json:"threat"`
	ICTIndex   *decimal `json:"ict_index"`

	Starts                   *int     `json:"starts"`
	ExpectedGoals            *decimal `json:"expected_goals"`
	ExpectedAssists          *decimal `json:"expected_assists"`
	ExpectedGoalInvolvements *decimal `json:"expected_goal_involvements"`
	ExpectedGoalsConceded    *decimal `json:"expected_goals_conceded"`

	ClearancesBlocksInterceptions *int `json:"clearances_blocks_interceptions"`
	Recoveries                    *int `json:"recoveries"`
	Tackles                       *int `json:"tackles"`
	DefensiveContribution         *int `json:"defensive_contribution"`
}

var errWrongPlayer = errors.New("another player's history")

var seasonLabel = regexp.MustCompile(`^(\d{4})/(\d{2})$`)

// seasonStart is the first year of a "2021/22" label, or an error when the
// label is malformed or the two years are not consecutive.
func seasonStart(label string) (int, error) {
	m := seasonLabel.FindStringSubmatch(label)
	if m == nil {
		return 0, fmt.Errorf("season %q: not YYYY/YY", label)
	}
	y, _ := strconv.Atoi(m[1])
	end, _ := strconv.Atoi(m[2])
	if (y+1)%100 != end {
		return 0, fmt.Errorf("season %q: years not consecutive", label)
	}
	return y, nil
}

// since keeps v from season `first` on; earlier it is untracked (nil).
func since[T any](start, first int, v *T) *T {
	if start < first {
		return nil
	}
	return v
}

// decodeHistoryPast maps element-summary's history_past for the player
// with the given code. A malformed season label, a repeated season or a row
// for another player (element_code) fails the whole response.
func decodeHistoryPast(body []byte, playerCode string) ([]provider.PlayerSeason, error) {
	var raw rawElementSummary
	if err := decodeStrict(body, &raw); err != nil {
		return nil, err
	}
	out := make([]provider.PlayerSeason, 0, len(raw.HistoryPast))
	seen := map[string]bool{}
	for _, r := range raw.HistoryPast {
		y, err := seasonStart(r.SeasonName)
		if err != nil {
			return nil, err
		}
		if seen[r.SeasonName] {
			return nil, fmt.Errorf("season %s twice", r.SeasonName)
		}
		seen[r.SeasonName] = true
		// Element ids are reassigned at FPL's season rollover, so a stale
		// index can fetch someone else's summary: never save it as ours.
		if r.ElementCode != 0 && strconv.FormatInt(r.ElementCode, 10) != playerCode {
			return nil, fmt.Errorf("%w: summary is for code %d, not %s", errWrongPlayer, r.ElementCode, playerCode)
		}
		out = append(out, provider.PlayerSeason{
			PlayerExternalID: playerCode,
			Season:           r.SeasonName,
			TotalPoints:      r.TotalPoints,
			Minutes:          r.Minutes,
			GoalsScored:      r.GoalsScored,
			Assists:          r.Assists,
			CleanSheets:      r.CleanSheets,
			GoalsConceded:    r.GoalsConceded,
			OwnGoals:         r.OwnGoals,
			PenaltiesSaved:   r.PenaltiesSaved,
			PenaltiesMissed:  r.PenaltiesMissed,
			YellowCards:      r.YellowCards,
			RedCards:         r.RedCards,
			Saves:            r.Saves,
			Bonus:            r.Bonus,
			BPS:              r.BPS,
			StartCost:        r.StartCost,
			EndCost:          r.EndCost,

			Influence:  since(y, firstICTSeason, r.Influence.value()),
			Creativity: since(y, firstICTSeason, r.Creativity.value()),
			Threat:     since(y, firstICTSeason, r.Threat.value()),
			ICTIndex:   since(y, firstICTSeason, r.ICTIndex.value()),

			Starts:                   since(y, firstExpectedSeason, r.Starts),
			ExpectedGoals:            since(y, firstExpectedSeason, r.ExpectedGoals.value()),
			ExpectedAssists:          since(y, firstExpectedSeason, r.ExpectedAssists.value()),
			ExpectedGoalInvolvements: since(y, firstExpectedSeason, r.ExpectedGoalInvolvements.value()),
			ExpectedGoalsConceded:    since(y, firstExpectedSeason, r.ExpectedGoalsConceded.value()),

			DefensiveContribution:         since(y, firstDefContribSeason, r.DefensiveContribution),
			ClearancesBlocksInterceptions: since(y, firstDefensiveSeason, r.ClearancesBlocksInterceptions),
			Recoveries:                    since(y, firstDefensiveSeason, r.Recoveries),
			Tackles:                       since(y, firstDefensiveSeason, r.Tackles),
		})
	}
	return out, nil
}

// SeasonHistory returns the finished seasons of the current player with
// external id playerCode (FPL code), oldest first as FPL sends them
// (GET /element-summary/{element id}). The element id comes from the last
// bootstrap (RefreshIndex re-reads it); an unknown code is
// provider.ErrNotFound. Calls go through the
// client's pacing (MinInterval), so a caller looping over every player
// stays polite.
func (c *Client) SeasonHistory(ctx context.Context, playerCode string) ([]provider.PlayerSeason, error) {
	idx, err := c.index(ctx)
	if err != nil {
		return nil, err
	}
	// No bootstrap refetch for an unknown code: a code gone from FPL would
	// refetch it on every call. The caller refreshes per batch (RefreshIndex).
	id, ok := idx.elementID(playerCode)
	if !ok {
		return nil, fmt.Errorf("fpl: player %s: %w", playerCode, provider.ErrNotFound)
	}
	path := "/element-summary/" + strconv.FormatInt(id, 10)
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	// The body is only needed once; never answer a later call from a 304.
	c.forget(path)
	seasons, err := decodeHistoryPast(body, playerCode)
	if errors.Is(err, errWrongPlayer) {
		return nil, fmt.Errorf("fpl: element %d: %w", id, err)
	}
	if err != nil {
		return nil, invalid("element-summary", err)
	}
	return seasons, nil
}

// RefreshIndex re-reads bootstrap, so element ids reassigned at FPL's
// season rollover are picked up. history.Fetcher calls it before each batch.
func (c *Client) RefreshIndex(ctx context.Context) error {
	_, _, err := c.bootstrap(ctx)
	return err
}

// SeasonLive reports whether bootstrap is the game for the season starting
// at since: its first gameweek's deadline (also returned) is on or after
// since. FPL relaunches in mid/late July; until then history_past lacks
// the season just finished.
func (c *Client) SeasonLive(ctx context.Context, since time.Time) (bool, time.Time, error) {
	b, _, err := c.bootstrap(ctx)
	if err != nil {
		return false, time.Time{}, err
	}
	if len(b.Gameweeks) == 0 {
		return false, time.Time{}, nil
	}
	first := b.Gameweeks[0]
	for _, g := range b.Gameweeks {
		if g.Number < first.Number {
			first = g
		}
	}
	return !first.DeadlineAt.Before(since), first.DeadlineAt, nil
}

func (idx *bootstrapIndex) elementID(playerCode string) (int64, bool) {
	for id, c := range idx.elementCode {
		if c == playerCode {
			return id, true
		}
	}
	return 0, false
}
