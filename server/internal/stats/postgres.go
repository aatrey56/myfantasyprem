package stats

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// Postgres is the Repo on Postgres.
type Postgres struct {
	pool *pgxpool.Pool
}

var _ Repo = (*Postgres)(nil)

// NewPostgres returns a Repo on pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func (p *Postgres) Clubs(ctx context.Context) ([]Club, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, code, name FROM clubs WHERE season = $1 ORDER BY name`, Season)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Club, error) {
		var c Club
		err := r.Scan(&c.ID, &c.Code, &c.Name)
		return c, err
	})
}

func (p *Postgres) Players(ctx context.Context) ([]Player, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, first_name, last_name, display_name, position, club_id, availability, news, news_at, chance_next, draft_rank
		 FROM players ORDER BY id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Player, error) {
		var pl Player
		var pos string
		var chance *int16
		err := r.Scan(&pl.ID, &pl.FirstName, &pl.LastName, &pl.DisplayName, &pos, &pl.ClubID, &pl.Availability, &pl.News,
			&pl.NewsAt, &chance, &pl.DraftRank)
		pl.Position = scoring.Position(pos)
		pl.NewsAt = utc(pl.NewsAt)
		if chance != nil {
			c := int(*chance)
			pl.ChanceNext = &c
		}
		return pl, err
	})
}

func (p *Postgres) Gameweeks(ctx context.Context) ([]Gameweek, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT number, name, deadline_at, waiver_deadline_at, trade_deadline_at, finished, bonus_added, data_checked
		 FROM gameweeks WHERE season = $1 ORDER BY number`, Season)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Gameweek, error) {
		var g Gameweek
		err := r.Scan(&g.Number, &g.Name, &g.DeadlineAt, &g.WaiverDeadlineAt, &g.TradeDeadlineAt, &g.Finished, &g.BonusAdded, &g.DataChecked)
		g.DeadlineAt, g.WaiverDeadlineAt, g.TradeDeadlineAt = g.DeadlineAt.UTC(), g.WaiverDeadlineAt.UTC(), g.TradeDeadlineAt.UTC()
		return g, err
	})
}

func (p *Postgres) Fixtures(ctx context.Context) ([]Fixture, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, coalesce(gameweek, 0), home_club_id, away_club_id, kickoff_at, status, home_score, away_score
		 FROM fixtures WHERE season = $1 ORDER BY kickoff_at NULLS LAST, id`, Season)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Fixture, error) {
		var f Fixture
		err := r.Scan(&f.ID, &f.Gameweek, &f.HomeClubID, &f.AwayClubID, &f.KickoffAt, &f.Status, &f.HomeScore, &f.AwayScore)
		f.KickoffAt = utc(f.KickoffAt)
		return f, err
	})
}

const pfColumns = `player_id, fixture_id, gameweek, minutes, goals, assists, clean_sheet, goals_conceded, own_goals,
	pens_saved, pens_missed, yellow, red, saves, cbi, tackles, recoveries, bps, bonus, bonus_provisional, points, breakdown, rev`

func (p *Postgres) PlayerFixtures(ctx context.Context, gw int) ([]PlayerFixture, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+pfColumns+` FROM player_fixture_stats WHERE $1 = 0 OR gameweek = $1
		 ORDER BY gameweek, player_id, fixture_id`, gw)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PlayerFixture, error) {
		var x PlayerFixture
		var bps *int
		s := &x.Stats
		err := r.Scan(&x.PlayerID, &x.FixtureID, &x.Gameweek, &s.Minutes, &s.Goals, &s.Assists, &s.CleanSheet,
			&s.GoalsConceded, &s.OwnGoals, &s.PenaltiesSaved, &s.PenaltiesMissed, &s.YellowCards, &s.RedCards,
			&s.Saves, &s.ClearancesBlocksInterceptions, &s.Tackles, &s.Recoveries, &bps, &s.Bonus,
			&x.BonusProvisional, &x.Points, &x.Breakdown, &x.Rev)
		if bps != nil {
			x.BPS = *bps
		} else {
			x.BPSUnknown = true
		}
		return x, err
	})
}

const pgwColumns = `gameweek, player_id, minutes, goals, assists, clean_sheets, goals_conceded, own_goals, pens_saved,
	pens_missed, yellow, red, saves, bonus, bps, cbi, tackles, recoveries, defensive_contribution, influence, creativity,
	threat, ict_index, expected_goals, expected_assists, expected_goal_involvements, expected_goals_conceded, starts`

func (p *Postgres) PlayerGameweeks(ctx context.Context, gw int) ([]PlayerGameweek, error) {
	return p.playerGameweeks(ctx, `SELECT `+pgwColumns+` FROM player_gameweek_stats WHERE $1 = 0 OR gameweek = $1
		 ORDER BY gameweek, player_id`, gw)
}

func (p *Postgres) PlayerGameweeksOf(ctx context.Context, playerID string) ([]PlayerGameweek, error) {
	return p.playerGameweeks(ctx, `SELECT `+pgwColumns+` FROM player_gameweek_stats WHERE player_id = $1
		 ORDER BY gameweek`, playerID)
}

func (p *Postgres) playerGameweeks(ctx context.Context, sql string, arg any) ([]PlayerGameweek, error) {
	rows, err := p.pool.Query(ctx, sql, arg)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (PlayerGameweek, error) {
		var x PlayerGameweek
		err := r.Scan(&x.Gameweek, &x.PlayerID, &x.Minutes, &x.GoalsScored, &x.Assists, &x.CleanSheets, &x.GoalsConceded,
			&x.OwnGoals, &x.PenaltiesSaved, &x.PenaltiesMissed, &x.YellowCards, &x.RedCards, &x.Saves, &x.Bonus, &x.BPS,
			&x.ClearancesBlocksInterceptions, &x.Tackles, &x.Recoveries, &x.DefensiveContribution, &x.Influence,
			&x.Creativity, &x.Threat, &x.ICTIndex, &x.ExpectedGoals, &x.ExpectedAssists, &x.ExpectedGoalInvolvements,
			&x.ExpectedGoalsConceded, &x.Starts)
		return x, err
	})
}

func (p *Postgres) Status(ctx context.Context) (Status, bool, error) {
	var st Status
	var next *int
	err := p.pool.QueryRow(ctx,
		`SELECT current_gameweek, next_gameweek, waivers_processed FROM seasons WHERE id = $1`, Season).
		Scan(&st.Current, &next, &st.WaiversProcessed)
	if errors.Is(err, pgx.ErrNoRows) {
		return Status{}, false, nil
	}
	if err != nil {
		return Status{}, false, err
	}
	if next != nil {
		st.Next = *next
	}
	// A season row created by a league insert has no gameweeks yet.
	var n int
	if err := p.pool.QueryRow(ctx, `SELECT count(*) FROM gameweeks WHERE season = $1`, Season).Scan(&n); err != nil {
		return Status{}, false, err
	}
	return st, n > 0, nil
}

func (p *Postgres) PollState(ctx context.Context) (PollState, error) {
	var (
		ps                  PollState
		attempt, good, next *time.Time
	)
	err := p.pool.QueryRow(ctx,
		`SELECT mode, gameweek, last_attempt_at, last_good_at, next_poll_at, last_error, consecutive_failures
		 FROM poll_state WHERE id = 1`).
		Scan(&ps.Mode, &ps.Gameweek, &attempt, &good, &next, &ps.LastError, &ps.ConsecutiveFailures)
	if errors.Is(err, pgx.ErrNoRows) {
		return PollState{}, nil
	}
	if err != nil {
		return PollState{}, err
	}
	for _, x := range []struct {
		src *time.Time
		dst *time.Time
	}{{attempt, &ps.LastAttemptAt}, {good, &ps.LastGoodAt}, {next, &ps.NextPollAt}} {
		if x.src != nil {
			*x.dst = x.src.UTC()
		}
	}
	return ps, nil
}

func nullTime(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

func (p *Postgres) SavePollState(ctx context.Context, ps PollState) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO poll_state (id, mode, gameweek, last_attempt_at, last_good_at, next_poll_at, last_error, consecutive_failures, updated_at)
		 VALUES (1, $1, $2, $3, $4, $5, $6, $7, now())
		 ON CONFLICT (id) DO UPDATE SET mode = EXCLUDED.mode, gameweek = EXCLUDED.gameweek,
		   last_attempt_at = EXCLUDED.last_attempt_at, last_good_at = EXCLUDED.last_good_at,
		   next_poll_at = EXCLUDED.next_poll_at, last_error = EXCLUDED.last_error,
		   consecutive_failures = EXCLUDED.consecutive_failures, updated_at = now()`,
		ps.Mode, ps.Gameweek, nullTime(ps.LastAttemptAt), nullTime(ps.LastGoodAt), nullTime(ps.NextPollAt),
		ps.LastError, ps.ConsecutiveFailures)
	return err
}

func (p *Postgres) RescorePending(ctx context.Context) ([]RescoreMark, error) {
	rows, err := p.pool.Query(ctx, `SELECT gameweek, version FROM rescore_pending ORDER BY gameweek`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowToStructByPos[RescoreMark])
}

func (p *Postgres) ClearRescore(ctx context.Context, m RescoreMark) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM rescore_pending WHERE gameweek = $1 AND version = $2`, m.Gameweek, m.Version)
	return err
}

func (p *Postgres) StartsChecked(ctx context.Context) (map[int]bool, error) {
	rows, err := p.pool.Query(ctx, `SELECT gameweek FROM starts_checked`)
	if err != nil {
		return nil, err
	}
	gws, err := pgx.CollectRows(rows, pgx.RowTo[int])
	if err != nil {
		return nil, err
	}
	out := map[int]bool{}
	for _, g := range gws {
		out[g] = true
	}
	return out, nil
}

func (p *Postgres) MarkStartsChecked(ctx context.Context, gw int) error {
	_, err := p.pool.Exec(ctx, `INSERT INTO starts_checked (gameweek) VALUES ($1) ON CONFLICT (gameweek) DO NOTHING`, gw)
	return err
}

func (p *Postgres) ExternalIDs(ctx context.Context, provider, entity string) (map[string]string, error) {
	var rows pgx.Rows
	var err error
	if entity == "player" {
		rows, err = p.pool.Query(ctx,
			`SELECT external_id, player_id FROM player_external_id WHERE provider = $1 AND season IS NULL`, provider)
	} else {
		rows, err = p.pool.Query(ctx,
			`SELECT external_id, internal_id FROM external_ref WHERE provider = $1 AND entity = $2 AND season IS NULL`,
			provider, entity)
	}
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	var ext, id string
	_, err = pgx.ForEachRow(rows, []any{&ext, &id}, func() error {
		out[ext] = id
		return nil
	})
	return out, err
}

func (p *Postgres) Apply(ctx context.Context, b Batch) error {
	return store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error { return apply(ctx, tx, b) })
}

func apply(ctx context.Context, tx pgx.Tx, b Batch) error {
	if _, err := tx.Exec(ctx, `INSERT INTO seasons (id) VALUES ($1) ON CONFLICT DO NOTHING`, Season); err != nil {
		return err
	}
	if len(b.Mappings) > 0 {
		if err := claimIDs(ctx, tx, b); err != nil {
			return err
		}
	}
	batch := &pgx.Batch{}
	for _, c := range b.Clubs {
		batch.Queue(`INSERT INTO clubs (id, season, code, name) VALUES ($1, $2, $3, $4)
		             ON CONFLICT (id) DO UPDATE SET code = EXCLUDED.code, name = EXCLUDED.name`,
			c.ID, Season, c.Code, c.Name)
	}
	for _, pl := range b.Players {
		batch.Queue(`INSERT INTO players (id, first_name, last_name, display_name, position, club_id, availability, news,
		               news_at, chance_next, draft_rank)
		             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		             ON CONFLICT (id) DO UPDATE SET first_name = EXCLUDED.first_name, last_name = EXCLUDED.last_name,
		               display_name = EXCLUDED.display_name, position = EXCLUDED.position, club_id = EXCLUDED.club_id,
		               availability = EXCLUDED.availability, news = EXCLUDED.news, news_at = EXCLUDED.news_at,
		               chance_next = EXCLUDED.chance_next, draft_rank = EXCLUDED.draft_rank, updated_at = now()
		             WHERE (players.first_name, players.last_name, players.display_name, players.position, players.club_id,
		                    players.availability, players.news, players.news_at, players.chance_next, players.draft_rank)
		                   IS DISTINCT FROM (EXCLUDED.first_name, EXCLUDED.last_name, EXCLUDED.display_name, EXCLUDED.position,
		                    EXCLUDED.club_id, EXCLUDED.availability, EXCLUDED.news, EXCLUDED.news_at, EXCLUDED.chance_next,
		                    EXCLUDED.draft_rank)`,
			pl.ID, pl.FirstName, pl.LastName, pl.DisplayName, string(pl.Position), pl.ClubID, pl.Availability, pl.News,
			pl.NewsAt, pl.ChanceNext, pl.DraftRank)
	}
	for _, f := range b.Fixtures {
		var gw *int
		if f.Gameweek != 0 {
			g := f.Gameweek
			gw = &g
		}
		batch.Queue(`INSERT INTO fixtures (id, season, gameweek, home_club_id, away_club_id, kickoff_at, status, home_score, away_score)
		             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		             ON CONFLICT (id) DO UPDATE SET gameweek = EXCLUDED.gameweek, home_club_id = EXCLUDED.home_club_id,
		               away_club_id = EXCLUDED.away_club_id, kickoff_at = EXCLUDED.kickoff_at, status = EXCLUDED.status,
		               home_score = EXCLUDED.home_score, away_score = EXCLUDED.away_score, updated_at = now()`,
			f.ID, Season, gw, f.HomeClubID, f.AwayClubID, f.KickoffAt, f.Status, f.HomeScore, f.AwayScore)
	}
	for _, m := range b.Mappings {
		if m.Entity == "player" {
			batch.Queue(`INSERT INTO player_external_id (player_id, provider, external_id, match_method, confidence)
			             VALUES ($1, $2, $3, 'exact_key', 1) ON CONFLICT DO NOTHING`, m.ID, b.Provider, m.ExternalID)
		} else {
			batch.Queue(`INSERT INTO external_ref (entity, provider, external_id, internal_id)
			             VALUES ($1, $2, $3, $4) ON CONFLICT DO NOTHING`, m.Entity, b.Provider, m.ExternalID, m.ID)
		}
	}
	for _, g := range b.Gameweeks {
		batch.Queue(`INSERT INTO gameweeks (season, number, name, deadline_at, waiver_deadline_at, trade_deadline_at, finished, bonus_added, data_checked)
		             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		             ON CONFLICT (season, number) DO UPDATE SET name = EXCLUDED.name, deadline_at = EXCLUDED.deadline_at,
		               waiver_deadline_at = EXCLUDED.waiver_deadline_at, trade_deadline_at = EXCLUDED.trade_deadline_at,
		               finished = EXCLUDED.finished, bonus_added = EXCLUDED.bonus_added, data_checked = EXCLUDED.data_checked`,
			Season, g.Number, g.Name, g.DeadlineAt, g.WaiverDeadlineAt, g.TradeDeadlineAt, g.Finished, g.BonusAdded, g.DataChecked)
	}
	if st := b.Status; st != nil {
		var next *int
		if st.Next != 0 {
			n := st.Next
			next = &n
		}
		batch.Queue(`UPDATE seasons SET current_gameweek = $2, next_gameweek = $3, waivers_processed = $4, updated_at = now()
		             WHERE id = $1`, Season, st.Current, next, st.WaiversProcessed)
	}
	if lv := b.Live; lv != nil {
		players := make([]string, 0, len(lv.Rows))
		fixtures := make([]string, 0, len(lv.Rows))
		for _, r := range lv.Rows {
			s := r.Stats
			batch.Queue(`INSERT INTO player_fixture_stats (`+pfColumns+`, updated_at)
			             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, 1, now())
			             ON CONFLICT (player_id, fixture_id) DO UPDATE SET gameweek = EXCLUDED.gameweek, minutes = EXCLUDED.minutes,
			               goals = EXCLUDED.goals, assists = EXCLUDED.assists, clean_sheet = EXCLUDED.clean_sheet,
			               goals_conceded = EXCLUDED.goals_conceded, own_goals = EXCLUDED.own_goals, pens_saved = EXCLUDED.pens_saved,
			               pens_missed = EXCLUDED.pens_missed, yellow = EXCLUDED.yellow, red = EXCLUDED.red, saves = EXCLUDED.saves,
			               cbi = EXCLUDED.cbi, tackles = EXCLUDED.tackles, recoveries = EXCLUDED.recoveries, bps = EXCLUDED.bps,
			               bonus = EXCLUDED.bonus, bonus_provisional = EXCLUDED.bonus_provisional, points = EXCLUDED.points,
			               breakdown = EXCLUDED.breakdown, rev = player_fixture_stats.rev + 1, updated_at = now()
			             WHERE (player_fixture_stats.gameweek, player_fixture_stats.minutes, player_fixture_stats.goals,
			                    player_fixture_stats.assists, player_fixture_stats.clean_sheet, player_fixture_stats.goals_conceded,
			                    player_fixture_stats.own_goals, player_fixture_stats.pens_saved, player_fixture_stats.pens_missed,
			                    player_fixture_stats.yellow, player_fixture_stats.red, player_fixture_stats.saves,
			                    player_fixture_stats.cbi, player_fixture_stats.tackles, player_fixture_stats.recoveries,
			                    player_fixture_stats.bps, player_fixture_stats.bonus, player_fixture_stats.bonus_provisional,
			                    player_fixture_stats.points, player_fixture_stats.breakdown)
			                   IS DISTINCT FROM (EXCLUDED.gameweek, EXCLUDED.minutes, EXCLUDED.goals, EXCLUDED.assists,
			                    EXCLUDED.clean_sheet, EXCLUDED.goals_conceded, EXCLUDED.own_goals, EXCLUDED.pens_saved,
			                    EXCLUDED.pens_missed, EXCLUDED.yellow, EXCLUDED.red, EXCLUDED.saves, EXCLUDED.cbi, EXCLUDED.tackles,
			                    EXCLUDED.recoveries, EXCLUDED.bps, EXCLUDED.bonus, EXCLUDED.bonus_provisional, EXCLUDED.points, EXCLUDED.breakdown)`,
				r.PlayerID, r.FixtureID, lv.Gameweek, s.Minutes, s.Goals, s.Assists, s.CleanSheet, s.GoalsConceded, s.OwnGoals,
				s.PenaltiesSaved, s.PenaltiesMissed, s.YellowCards, s.RedCards, s.Saves, s.ClearancesBlocksInterceptions,
				s.Tackles, s.Recoveries, bpsValue(r), s.Bonus, r.BonusProvisional, r.Points, breakdownValue(r))
			players = append(players, r.PlayerID)
			fixtures = append(fixtures, r.FixtureID)
		}
		batch.Queue(`DELETE FROM player_fixture_stats s WHERE s.gameweek = $1
		             AND NOT EXISTS (SELECT 1 FROM unnest($2::text[], $3::text[]) AS k(p, f) WHERE k.p = s.player_id AND k.f = s.fixture_id)`,
			lv.Gameweek, players, fixtures)
		if lv.Players != nil {
			queuePlayerGameweeks(batch, lv.Gameweek, lv.Players)
		}
	}
	for _, g := range b.Rescore {
		batch.Queue(`INSERT INTO rescore_pending (gameweek) VALUES ($1)
		             ON CONFLICT (gameweek) DO UPDATE SET version = nextval('rescore_version_seq')`, g)
	}
	if err := tx.SendBatch(ctx, batch).Close(); err != nil {
		return err
	}
	for _, s := range b.Snapshots {
		if err := saveSnapshot(ctx, tx, s); err != nil {
			return err
		}
	}
	return nil
}

// mintLock is the advisory lock key that serialises ingests minting ids.
const mintLock = 0x6d696e74 // "mint"

// claimIDs makes the batch's new upstream ids its own. Ingests that mint
// ids take a lock until they commit; under it, one that finds an id
// already mapped (to a different id of ours) fails with ErrIDTaken before
// writing anything. Without this the loser's mapping was skipped (ON
// CONFLICT DO NOTHING) while its rows stayed: a second, unmapped player.
func claimIDs(ctx context.Context, tx pgx.Tx, b Batch) error {
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock($1)`, mintLock); err != nil {
		return err
	}
	var entities, exts, ids []string
	for _, m := range b.Mappings {
		entities, exts, ids = append(entities, m.Entity), append(exts, m.ExternalID), append(ids, m.ID)
	}
	var taken bool
	err := tx.QueryRow(ctx,
		`SELECT EXISTS (
		   SELECT 1 FROM unnest($2::text[], $3::text[], $4::text[]) AS k(entity, ext, id)
		   WHERE (k.entity = 'player' AND EXISTS (SELECT 1 FROM player_external_id p
		            WHERE p.provider = $1 AND p.external_id = k.ext AND p.season IS NULL AND p.player_id <> k.id))
		      OR (k.entity <> 'player' AND EXISTS (SELECT 1 FROM external_ref r
		            WHERE r.entity = k.entity AND r.provider = $1 AND r.external_id = k.ext AND r.season IS NULL AND r.internal_id <> k.id)))`,
		b.Provider, entities, exts, ids).Scan(&taken)
	if err != nil {
		return err
	}
	if taken {
		return ErrIDTaken
	}
	return nil
}

// queuePlayerGameweeks replaces a gameweek's per-player stats: an upsert
// that skips unchanged rows, then a delete of players no longer listed.
func queuePlayerGameweeks(batch *pgx.Batch, gw int, ps []PlayerGameweek) {
	ids := make([]string, 0, len(ps))
	for _, x := range ps {
		batch.Queue(`INSERT INTO player_gameweek_stats (`+pgwColumns+`)
		             VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21,
		                     $22, $23, $24, $25, $26, $27, $28)
		             ON CONFLICT (gameweek, player_id) DO UPDATE SET minutes = EXCLUDED.minutes, goals = EXCLUDED.goals,
		               assists = EXCLUDED.assists, clean_sheets = EXCLUDED.clean_sheets, goals_conceded = EXCLUDED.goals_conceded,
		               own_goals = EXCLUDED.own_goals, pens_saved = EXCLUDED.pens_saved, pens_missed = EXCLUDED.pens_missed,
		               yellow = EXCLUDED.yellow, red = EXCLUDED.red, saves = EXCLUDED.saves, bonus = EXCLUDED.bonus,
		               bps = EXCLUDED.bps, cbi = EXCLUDED.cbi, tackles = EXCLUDED.tackles, recoveries = EXCLUDED.recoveries,
		               defensive_contribution = EXCLUDED.defensive_contribution, influence = EXCLUDED.influence,
		               creativity = EXCLUDED.creativity, threat = EXCLUDED.threat, ict_index = EXCLUDED.ict_index,
		               expected_goals = EXCLUDED.expected_goals, expected_assists = EXCLUDED.expected_assists,
		               expected_goal_involvements = EXCLUDED.expected_goal_involvements,
		               expected_goals_conceded = EXCLUDED.expected_goals_conceded, starts = EXCLUDED.starts,
		               updated_at = now()
		             WHERE (player_gameweek_stats.minutes, player_gameweek_stats.goals, player_gameweek_stats.assists,
		                    player_gameweek_stats.clean_sheets, player_gameweek_stats.goals_conceded, player_gameweek_stats.own_goals,
		                    player_gameweek_stats.pens_saved, player_gameweek_stats.pens_missed, player_gameweek_stats.yellow,
		                    player_gameweek_stats.red, player_gameweek_stats.saves, player_gameweek_stats.bonus,
		                    player_gameweek_stats.bps, player_gameweek_stats.cbi, player_gameweek_stats.tackles,
		                    player_gameweek_stats.recoveries, player_gameweek_stats.defensive_contribution,
		                    player_gameweek_stats.influence, player_gameweek_stats.creativity, player_gameweek_stats.threat,
		                    player_gameweek_stats.ict_index, player_gameweek_stats.expected_goals,
		                    player_gameweek_stats.expected_assists, player_gameweek_stats.expected_goal_involvements,
		                    player_gameweek_stats.expected_goals_conceded, player_gameweek_stats.starts)
		                   IS DISTINCT FROM (EXCLUDED.minutes, EXCLUDED.goals, EXCLUDED.assists, EXCLUDED.clean_sheets,
		                    EXCLUDED.goals_conceded, EXCLUDED.own_goals, EXCLUDED.pens_saved, EXCLUDED.pens_missed,
		                    EXCLUDED.yellow, EXCLUDED.red, EXCLUDED.saves, EXCLUDED.bonus, EXCLUDED.bps, EXCLUDED.cbi,
		                    EXCLUDED.tackles, EXCLUDED.recoveries, EXCLUDED.defensive_contribution, EXCLUDED.influence,
		                    EXCLUDED.creativity, EXCLUDED.threat, EXCLUDED.ict_index, EXCLUDED.expected_goals,
		                    EXCLUDED.expected_assists, EXCLUDED.expected_goal_involvements, EXCLUDED.expected_goals_conceded,
		                    EXCLUDED.starts)`,
			gw, x.PlayerID, x.Minutes, x.GoalsScored, x.Assists, x.CleanSheets, x.GoalsConceded, x.OwnGoals,
			x.PenaltiesSaved, x.PenaltiesMissed, x.YellowCards, x.RedCards, x.Saves, x.Bonus, x.BPS,
			x.ClearancesBlocksInterceptions, x.Tackles, x.Recoveries, x.DefensiveContribution, x.Influence, x.Creativity,
			x.Threat, x.ICTIndex, x.ExpectedGoals, x.ExpectedAssists, x.ExpectedGoalInvolvements, x.ExpectedGoalsConceded,
			x.Starts)
		ids = append(ids, x.PlayerID)
	}
	batch.Queue(`DELETE FROM player_gameweek_stats WHERE gameweek = $1 AND player_id <> ALL($2::text[])`, gw, ids)
}

// saveSnapshot stores a payload unless it equals the latest one of its
// kind. A gameweek's snapshot taken once its data is checked is final and
// is never pruned.
func saveSnapshot(ctx context.Context, tx pgx.Tx, s Snapshot) error {
	sum := sha256.Sum256(s.Payload)
	hash := hex.EncodeToString(sum[:])
	var gw *int
	if s.Gameweek != 0 {
		g := s.Gameweek
		gw = &g
	}
	_, err := tx.Exec(ctx,
		`INSERT INTO stat_snapshots (kind, gameweek, fetched_at, payload_sha256, payload, final)
		 SELECT $1, $2, $3, $4, $5::jsonb,
		        EXISTS (SELECT 1 FROM gameweeks WHERE season = $6 AND number = $2 AND data_checked)
		 WHERE (SELECT payload_sha256 FROM stat_snapshots
		        WHERE kind = $1 AND gameweek IS NOT DISTINCT FROM $2 ORDER BY id DESC LIMIT 1) IS DISTINCT FROM $4`,
		s.Kind, gw, s.FetchedAt, hash, string(s.Payload), Season)
	return err
}

// PruneSnapshots deletes raw snapshots beyond the newest keep per kind and
// gameweek, except final ones (taken once the gameweek's data was checked),
// and returns how many it deleted. cmd/admin and the prune job call it.
func (p *Postgres) PruneSnapshots(ctx context.Context, keep int) (int, error) {
	tag, err := p.pool.Exec(ctx,
		`DELETE FROM stat_snapshots s USING (
		   SELECT id, row_number() OVER (PARTITION BY kind, gameweek ORDER BY fetched_at DESC, id DESC) AS rn
		   FROM stat_snapshots) r
		 WHERE s.id = r.id AND r.rn > $1 AND NOT s.final`, max(keep, 1))
	return int(tag.RowsAffected()), err
}

// bpsValue is the bps column: NULL when the BPS is unknown.
func bpsValue(r PlayerFixture) *int {
	if r.BPSUnknown {
		return nil
	}
	return &r.BPS
}

// breakdownValue is the breakdown column: never JSON null.
func breakdownValue(r PlayerFixture) []scoring.Line {
	if r.Breakdown == nil {
		return []scoring.Line{}
	}
	return r.Breakdown
}
