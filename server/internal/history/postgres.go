package history

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the Store on Postgres (migration 00017).
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Store = (*Postgres)(nil)

// NewPostgres returns a Store on pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

// statColumns are player_seasons' stat columns, in fields() order.
var statColumns = []string{
	"total_points", "minutes", "goals_scored", "assists", "clean_sheets", "goals_conceded",
	"own_goals", "penalties_saved", "penalties_missed", "yellow_cards", "red_cards", "saves",
	"bonus", "bps", "start_cost", "end_cost", "influence", "creativity", "threat", "ict_index",
	"starts", "expected_goals", "expected_assists", "expected_goal_involvements",
	"expected_goals_conceded", "clearances_blocks_interceptions", "recoveries", "tackles",
	"defensive_contribution",
}

// fields are pointers to s's stats, in statColumns order.
func fields(s *Season) []any {
	return []any{
		&s.TotalPoints, &s.Minutes, &s.GoalsScored, &s.Assists, &s.CleanSheets, &s.GoalsConceded,
		&s.OwnGoals, &s.PenaltiesSaved, &s.PenaltiesMissed, &s.YellowCards, &s.RedCards, &s.Saves,
		&s.Bonus, &s.BPS, &s.StartCost, &s.EndCost, &s.Influence, &s.Creativity, &s.Threat, &s.ICTIndex,
		&s.Starts, &s.ExpectedGoals, &s.ExpectedAssists, &s.ExpectedGoalInvolvements,
		&s.ExpectedGoalsConceded, &s.ClearancesBlocksInterceptions, &s.Recoveries, &s.Tackles,
		&s.DefensiveContribution,
	}
}

var upsertSQL = func() string {
	cols := strings.Join(statColumns, ", ")
	ph := make([]string, len(statColumns))
	set := make([]string, len(statColumns))
	for i, c := range statColumns {
		ph[i] = "$" + strconv.Itoa(i+4)
		set[i] = c + " = EXCLUDED." + c
	}
	return `INSERT INTO player_seasons (code, season, fetched_at, ` + cols + `)
		VALUES ($1, $2, $3, ` + strings.Join(ph, ", ") + `)
		ON CONFLICT (code, season) DO UPDATE SET fetched_at = EXCLUDED.fetched_at, ` + strings.Join(set, ", ")
}()

func (p *Postgres) Save(ctx context.Context, code string, seasons []Season, at time.Time) error {
	at = at.UTC().Truncate(time.Microsecond)
	return pgx.BeginFunc(ctx, p.pool, func(tx pgx.Tx) error {
		for _, s := range seasons {
			args := append([]any{code, s.Season, at}, deref(fields(&s))...)
			if _, err := tx.Exec(ctx, upsertSQL, args...); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, `INSERT INTO player_history_fetches (code, fetched_at) VALUES ($1, $2)
			ON CONFLICT (code) DO UPDATE SET fetched_at = EXCLUDED.fetched_at`, code, at)
		return err
	})
}

// deref turns field pointers (**int, **float64) into values for Exec.
func deref(ps []any) []any {
	out := make([]any, len(ps))
	for i, p := range ps {
		switch v := p.(type) {
		case **int:
			out[i] = *v
		case **float64:
			out[i] = *v
		}
	}
	return out
}

func (p *Postgres) Seasons(ctx context.Context, code string) ([]Season, error) {
	rows, err := p.pool.Query(ctx, `SELECT season, `+strings.Join(statColumns, ", ")+`
		FROM player_seasons WHERE code = $1 ORDER BY season DESC`, code)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Season{}
	for rows.Next() {
		s := Season{PlayerExternalID: code}
		if err := rows.Scan(append([]any{&s.Season}, fields(&s)...)...); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (p *Postgres) Fetched(ctx context.Context) (map[string]time.Time, error) {
	rows, err := p.pool.Query(ctx, `SELECT code, fetched_at FROM player_history_fetches`)
	if err != nil {
		return nil, err
	}
	out := map[string]time.Time{}
	var code string
	var at time.Time
	_, err = pgx.ForEachRow(rows, []any{&code, &at}, func() error {
		out[code] = at.UTC()
		return nil
	})
	return out, err
}
