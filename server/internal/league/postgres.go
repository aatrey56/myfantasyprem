package league

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// Postgres is the Repo on Postgres.
type Postgres struct {
	pool  *pgxpool.Pool
	sched Scheduler
	// wrapRead, when set, wraps the transaction reads run on (tests).
	wrapRead func(store.DB) store.DB
}

var _ Repo = (*Postgres)(nil)

// NewPostgres returns a Repo on pool.
func NewPostgres(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

type pgTx struct {
	tx    pgx.Tx
	sched Scheduler
}

var _ Tx = pgTx{}

// SetScheduler makes Tx.Schedule insert jobs (River) in the transaction.
func (p *Postgres) SetScheduler(s Scheduler) { p.sched = s }

// Tx runs fn in a READ COMMITTED transaction; Load takes row locks, so
// concurrent changes to one league queue up instead of failing.
func (p *Postgres) Tx(ctx context.Context, fn func(tx Tx) error) error {
	return store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error { return fn(pgTx{tx, p.sched}) })
}

// TxWith is Tx that also hands fn the transaction itself, for writes to
// other tables that must commit with the league changes (account
// deletion deletes the user in the same transaction).
func (p *Postgres) TxWith(ctx context.Context, fn func(tx Tx, db store.DB) error) error {
	return store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error { return fn(pgTx{tx, p.sched}, tx) })
}

// read runs fn on one snapshot (store.ReadSnapshot): every read that
// makes more than one query goes through it, so a commit landing between
// its queries cannot show half-applied.
func read[T any](ctx context.Context, p *Postgres, fn func(db store.DB) (T, error)) (T, error) {
	var out T
	err := store.ReadSnapshot(ctx, p.pool, func(db store.DB) error {
		if p.wrapRead != nil {
			db = p.wrapRead(db)
		}
		var err error
		out, err = fn(db)
		return err
	})
	return out, err
}

func (t pgTx) Schedule(ctx context.Context, args JobArgs, at time.Time) error {
	if t.sched == nil {
		return nil
	}
	return t.sched.ScheduleTx(ctx, t.tx, args, at)
}

// load reads one league; lock adds FOR UPDATE on the league row.
func load(ctx context.Context, db store.DB, leagueID string, lock bool) (*League, error) {
	ls, err := loadMany(ctx, db, []string{leagueID}, lock)
	if err != nil || len(ls) == 0 {
		return nil, err
	}
	return ls[0], nil
}

// loadMany reads leagues in the order of leagueIDs, skipping missing ones,
// in four queries whatever their number.
func loadMany(ctx context.Context, db store.DB, leagueIDs []string, lock bool) ([]*League, error) {
	q := `SELECT id, name, season, status, coalesce(commissioner_id, ''), created_at, source, coalesce(fpl_league_id, 0)
	      FROM leagues WHERE id = ANY($1)`
	if lock {
		q += ` FOR UPDATE`
	}
	rows, err := db.Query(ctx, q, leagueIDs)
	if err != nil {
		return nil, err
	}
	byID := map[string]*League{}
	var l League
	if _, err := pgx.ForEachRow(rows, []any{&l.ID, &l.Name, &l.Season, &l.Status, &l.CommissionerID, &l.CreatedAt,
		&l.Source, &l.FPLLeagueID}, func() error {
		c := l
		c.CreatedAt = c.CreatedAt.UTC()
		byID[c.ID] = &c
		return nil
	}); err != nil {
		return nil, err
	}
	if len(byID) == 0 {
		return nil, nil
	}
	rows, err = db.Query(ctx,
		`SELECT DISTINCT ON (league_id) league_id, settings FROM league_settings
		 WHERE league_id = ANY($1) ORDER BY league_id, version DESC`, leagueIDs)
	if err != nil {
		return nil, err
	}
	var (
		leagueID string
		raw      []byte
	)
	settled := map[string]bool{}
	if _, err := pgx.ForEachRow(rows, []any{&leagueID, &raw}, func() error {
		if x := byID[leagueID]; x != nil {
			settled[leagueID] = true
			return json.Unmarshal(raw, &x.Settings)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	// Every league is created with settings; zero ones would be persisted
	// by the next write, so a league without them is an error.
	for id := range byID {
		if !settled[id] {
			return nil, fmt.Errorf("league %s has no settings: %w", id, pgx.ErrNoRows)
		}
	}
	rows, err = db.Query(ctx,
		`SELECT league_id, id, name, coalesce(owner_id, ''), joined_at, coalesce(fpl_entry_id, 0), coalesce(fpl_manager_name, '')
		 FROM teams WHERE league_id = ANY($1) ORDER BY joined_at, id`, leagueIDs)
	if err != nil {
		return nil, err
	}
	var t Team
	if _, err := pgx.ForEachRow(rows, []any{&leagueID, &t.ID, &t.Name, &t.OwnerID, &t.JoinedAt, &t.FPLEntryID, &t.ManagerName}, func() error {
		if x := byID[leagueID]; x != nil {
			t.JoinedAt = t.JoinedAt.UTC()
			x.Teams = append(x.Teams, t)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	rows, err = db.Query(ctx,
		`SELECT league_id, id, code, created_at, expires_at, max_uses, uses, revoked_at IS NOT NULL, coalesce(team_id, '')
		 FROM invites WHERE league_id = ANY($1) ORDER BY created_at, id`, leagueIDs)
	if err != nil {
		return nil, err
	}
	var inv Invite
	if _, err := pgx.ForEachRow(rows, []any{&leagueID, &inv.ID, &inv.Code, &inv.CreatedAt, &inv.ExpiresAt, &inv.MaxUses, &inv.Uses, &inv.Revoked, &inv.TeamID}, func() error {
		x := byID[leagueID]
		if x == nil {
			return nil
		}
		c := inv
		c.CreatedAt = c.CreatedAt.UTC()
		if c.ExpiresAt != nil {
			e := c.ExpiresAt.UTC()
			c.ExpiresAt = &e
		}
		if c.MaxUses != nil {
			n := *c.MaxUses
			c.MaxUses = &n
		}
		x.Invites = append(x.Invites, c)
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]*League, 0, len(byID))
	for _, id := range leagueIDs {
		if x := byID[id]; x != nil {
			out = append(out, x)
			delete(byID, id) // a repeated id is returned once
		}
	}
	return out, nil
}

func (p *Postgres) Get(ctx context.Context, leagueID string) (*League, error) {
	return read(ctx, p, func(db store.DB) (*League, error) { return load(ctx, db, leagueID, false) })
}

func idList(ctx context.Context, db store.DB, sql string, args ...any) ([]string, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (p *Postgres) ForUser(ctx context.Context, userID string) ([]*League, error) {
	return read(ctx, p, func(db store.DB) ([]*League, error) {
		leagueIDs, err := idList(ctx, db, `SELECT league_id FROM teams WHERE owner_id = $1`, userID)
		if err != nil {
			return nil, err
		}
		return loadMany(ctx, db, leagueIDs, false)
	})
}

func byCode(ctx context.Context, db store.DB, code string, lock bool) (*League, error) {
	var id string
	err := db.QueryRow(ctx, `SELECT league_id FROM invites WHERE code = $1`, code).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return load(ctx, db, id, lock)
}

func (p *Postgres) ByInviteCode(ctx context.Context, code string) (*League, error) {
	return read(ctx, p, func(db store.DB) (*League, error) { return byCode(ctx, db, code, false) })
}

func (p *Postgres) Game(ctx context.Context, leagueID string) (*Game, error) {
	return read(ctx, p, func(db store.DB) (*Game, error) { return loadGame(ctx, db, leagueID) })
}

func loadGame(ctx context.Context, db store.DB, leagueID string) (*Game, error) {
	g := &Game{}
	rows, err := db.Query(ctx,
		`SELECT id, team_id, player_id, from_gw, coalesce(to_gw, 0), acquired_via
		 FROM roster_slots WHERE league_id = $1 ORDER BY team_id, id`, leagueID)
	if err != nil {
		return nil, err
	}
	var r RosterSlot
	if _, err := pgx.ForEachRow(rows, []any{&r.ID, &r.TeamID, &r.PlayerID, &r.FromGW, &r.ToGW, &r.AcquiredVia}, func() error {
		g.Rosters = append(g.Rosters, r)
		return nil
	}); err != nil {
		return nil, err
	}

	rows, err = db.Query(ctx,
		`SELECT l.team_id, l.from_gw, l.slots, l.updated_at
		 FROM lineups l JOIN teams t ON t.id = l.team_id WHERE t.league_id = $1 ORDER BY l.team_id, l.from_gw`, leagueID)
	if err != nil {
		return nil, err
	}
	var lu Lineup
	if _, err := pgx.ForEachRow(rows, []any{&lu.TeamID, &lu.FromGW, &lu.Slots, &lu.UpdatedAt}, func() error {
		c := lu
		c.UpdatedAt = c.UpdatedAt.UTC()
		g.Lineups = append(g.Lineups, c)
		lu.Slots = nil
		return nil
	}); err != nil {
		return nil, err
	}

	rows, err = db.Query(ctx,
		`SELECT id, gameweek, coalesce(home_team_id, ''), coalesce(away_team_id, ''), home_points, away_points, state
		 FROM matchups WHERE league_id = $1 ORDER BY gameweek, id`, leagueID)
	if err != nil {
		return nil, err
	}
	var m Matchup
	if _, err := pgx.ForEachRow(rows, []any{&m.ID, &m.Gameweek, &m.HomeTeamID, &m.AwayTeamID, &m.HomePoints, &m.AwayPoints, &m.State}, func() error {
		g.Matchups = append(g.Matchups, m)
		return nil
	}); err != nil {
		return nil, err
	}

	rows, err = db.Query(ctx,
		`SELECT s.team_id, s.gameweek, s.points, s.bench_points, s.autosubs, s.state, s.rev
		 FROM team_gw_scores s JOIN teams t ON t.id = s.team_id WHERE t.league_id = $1 ORDER BY s.gameweek, s.team_id`, leagueID)
	if err != nil {
		return nil, err
	}
	var (
		s    TeamScore
		subs []byte
	)
	if _, err := pgx.ForEachRow(rows, []any{&s.TeamID, &s.Gameweek, &s.Points, &s.BenchPoints, &subs, &s.State, &s.Rev}, func() error {
		c := s
		c.AutoSubs = nil
		if err := json.Unmarshal(subs, &c.AutoSubs); err != nil {
			return err
		}
		g.Scores = append(g.Scores, c)
		return nil
	}); err != nil {
		return nil, err
	}
	return g, nil
}

func (p *Postgres) InSeasonLeagues(ctx context.Context) ([]string, error) {
	return idList(ctx, p.pool, `SELECT id FROM leagues WHERE status = $1 ORDER BY id`, StatusInSeason)
}

func (p *Postgres) SaveScores(ctx context.Context, leagueID string, gw int, scores []TeamScore, matchups []Matchup) ([]string, error) {
	var changed []string
	err := store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		changed = nil
		// The league lock Tx.LoadState takes: a state transaction reads
		// matchups and scores in separate statements, and a rescore
		// committing between them would show it half-applied. A deleted
		// league has nothing to save.
		//
		// The caller holds the gameweek's scoring lock (LockScores), so a
		// long league transaction stalls the gameweek's scoring while this
		// waits. That wait is bounded: league transactions do only database
		// work, none takes the scoring lock (the order is always scoring
		// lock, then league row, so no cycle), and production ends a stuck
		// one after idle_in_transaction_session_timeout (5min,
		// deploy/postgres/postgresql.conf).
		var one int
		err := tx.QueryRow(ctx, `SELECT 1 FROM leagues WHERE id = $1 FOR UPDATE`, leagueID).Scan(&one)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		now := time.Now()
		for _, s := range scores {
			subs := s.AutoSubs
			if subs == nil {
				subs = []scoring.Sub{}
			}
			raw, err := json.Marshal(subs)
			if err != nil {
				return err
			}
			var id string
			err = tx.QueryRow(ctx,
				`INSERT INTO team_gw_scores (team_id, gameweek, points, bench_points, autosubs, state, rev, updated_at)
				 VALUES ($1, $2, $3, $4, $5, $6, 1, $7)
				 ON CONFLICT (team_id, gameweek) DO UPDATE SET
				   points = EXCLUDED.points, bench_points = EXCLUDED.bench_points, autosubs = EXCLUDED.autosubs,
				   state = EXCLUDED.state, rev = team_gw_scores.rev + 1, updated_at = EXCLUDED.updated_at
				 WHERE (team_gw_scores.points, team_gw_scores.bench_points, team_gw_scores.autosubs, team_gw_scores.state)
				       IS DISTINCT FROM (EXCLUDED.points, EXCLUDED.bench_points, EXCLUDED.autosubs, EXCLUDED.state)
				 RETURNING team_id`,
				s.TeamID, gw, s.Points, s.BenchPoints, raw, string(s.State), now).Scan(&id)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
			case err != nil:
				return err
			default:
				changed = append(changed, id)
			}
		}
		for _, m := range matchups {
			if _, err := tx.Exec(ctx,
				`UPDATE matchups SET home_points = $3, away_points = $4, state = $5, updated_at = $6
				 WHERE id = $1 AND league_id = $2
				   AND (home_points, away_points, state) IS DISTINCT FROM ($3, $4, $5)`,
				m.ID, leagueID, m.HomePoints, m.AwayPoints, string(m.State), now); err != nil {
				return err
			}
		}
		return nil
	})
	return changed, err
}

// scoresLock is the first key of LockScores' advisory locks (the second
// is the gameweek).
const scoresLock = 0x73636f72 // "scor"

func (p *Postgres) LockScores(ctx context.Context, gw int, fn func() error) error {
	conn, err := p.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1, $2)`, scoresLock, gw); err != nil {
		return err
	}
	defer func() {
		if _, err := conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1, $2)`, scoresLock, gw); err != nil {
			// Ending the session releases the lock; the pool drops the
			// closed connection.
			_ = conn.Conn().Close(context.WithoutCancel(ctx))
		}
	}()
	return fn()
}

func (t pgTx) Load(ctx context.Context, leagueID string) (*League, error) {
	return load(ctx, t.tx, leagueID, true)
}

func (t pgTx) LoadByInviteCode(ctx context.Context, code string) (*League, error) {
	return byCode(ctx, t.tx, code, true)
}

func (t pgTx) LeagueIDsOfUser(ctx context.Context, userID string) ([]string, error) {
	return idList(ctx, t.tx, `SELECT league_id FROM teams WHERE owner_id = $1 ORDER BY league_id`, userID)
}

func (t pgTx) CodeTaken(ctx context.Context, code string) (bool, error) {
	var taken bool
	err := t.tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM invites WHERE code = $1)`, code).Scan(&taken)
	return taken, err
}

func nullInt64(n int64) *int64 {
	if n == 0 {
		return nil
	}
	return &n
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (t pgTx) Insert(ctx context.Context, l *League) error {
	if _, err := t.tx.Exec(ctx, `INSERT INTO seasons (id) VALUES ($1) ON CONFLICT DO NOTHING`, l.Season); err != nil {
		return err
	}
	if _, err := t.tx.Exec(ctx,
		`INSERT INTO leagues (id, name, season, status, commissioner_id, created_at, updated_at, source, fpl_league_id)
		 VALUES ($1, $2, $3, $4, $5, $6, $6, $7, $8)`,
		l.ID, l.Name, l.Season, l.Status, nullable(l.CommissionerID), l.CreatedAt, cmp.Or(l.Source, SourceNative),
		nullInt64(l.FPLLeagueID)); err != nil {
		return err
	}
	return t.saveParts(ctx, l)
}

func (t pgTx) Save(ctx context.Context, l *League) error {
	if _, err := t.tx.Exec(ctx,
		`UPDATE leagues SET name = $2, status = $3, commissioner_id = $4, updated_at = now(), source = $5, fpl_league_id = $6
		 WHERE id = $1`,
		l.ID, l.Name, l.Status, nullable(l.CommissionerID), cmp.Or(l.Source, SourceNative), nullInt64(l.FPLLeagueID)); err != nil {
		return err
	}
	return t.saveParts(ctx, l)
}

// saveParts writes settings (a new version when changed), teams and invites.
func (t pgTx) saveParts(ctx context.Context, l *League) error {
	raw, err := json.Marshal(l.Settings)
	if err != nil {
		return err
	}
	if _, err := t.tx.Exec(ctx,
		`INSERT INTO league_settings (league_id, version, settings)
		 SELECT $1, coalesce(max(version), 0) + 1, $2::jsonb FROM league_settings WHERE league_id = $1
		 HAVING (SELECT settings FROM league_settings WHERE league_id = $1 ORDER BY version DESC LIMIT 1)
		        IS DISTINCT FROM $2::jsonb`,
		l.ID, raw); err != nil {
		return err
	}
	teamIDs := make([]string, 0, len(l.Teams))
	for _, tm := range l.Teams {
		teamIDs = append(teamIDs, tm.ID)
	}
	if _, err := t.tx.Exec(ctx, `DELETE FROM teams WHERE league_id = $1 AND NOT (id = ANY($2))`, l.ID, teamIDs); err != nil {
		return err
	}
	for _, tm := range l.Teams {
		if _, err := t.tx.Exec(ctx,
			`INSERT INTO teams (id, league_id, name, owner_id, joined_at, fpl_entry_id, fpl_manager_name)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)
			 ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, owner_id = EXCLUDED.owner_id,
			   fpl_manager_name = EXCLUDED.fpl_manager_name`,
			tm.ID, l.ID, tm.Name, nullable(tm.OwnerID), tm.JoinedAt, nullInt64(tm.FPLEntryID), nullable(tm.ManagerName)); err != nil {
			return err
		}
	}
	for _, inv := range l.Invites {
		if _, err := t.tx.Exec(ctx,
			`INSERT INTO invites (id, league_id, code, max_uses, uses, created_at, expires_at, revoked_at, team_id)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, CASE WHEN $8 THEN now() END, $9)
			 ON CONFLICT (id) DO UPDATE SET uses = EXCLUDED.uses,
			   revoked_at = CASE WHEN $8 THEN coalesce(invites.revoked_at, now()) END`,
			inv.ID, l.ID, inv.Code, inv.MaxUses, inv.Uses, inv.CreatedAt, inv.ExpiresAt, inv.Revoked, nullable(inv.TeamID)); err != nil {
			return err
		}
	}
	return nil
}

func (t pgTx) Delete(ctx context.Context, leagueID string) error {
	_, err := t.tx.Exec(ctx, `DELETE FROM leagues WHERE id = $1`, leagueID)
	return err
}

func (t pgTx) StartSeason(ctx context.Context, leagueID string, g *Game) error {
	tag, err := t.tx.Exec(ctx, `UPDATE leagues SET status = $2, updated_at = now() WHERE id = $1`, leagueID, StatusInSeason)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errLeagueNotFound
	}
	b := &pgx.Batch{}
	for _, r := range g.Rosters {
		var to *int
		if r.ToGW != 0 {
			v := r.ToGW
			to = &v
		}
		b.Queue(`INSERT INTO roster_slots (league_id, team_id, player_id, from_gw, to_gw, acquired_via)
		         VALUES ($1, $2, $3, $4, $5, $6)`, leagueID, r.TeamID, r.PlayerID, r.FromGW, to, r.AcquiredVia)
	}
	for _, l := range g.Lineups {
		b.Queue(`INSERT INTO lineups (team_id, from_gw, slots, updated_at) VALUES ($1, $2, $3, $4)`,
			l.TeamID, l.FromGW, l.Slots, l.UpdatedAt)
	}
	for _, m := range g.Matchups {
		b.Queue(`INSERT INTO matchups (id, league_id, gameweek, home_team_id, away_team_id, home_points, away_points, state)
		         VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			m.ID, leagueID, m.Gameweek, nullable(m.HomeTeamID), nullable(m.AwayTeamID), m.HomePoints, m.AwayPoints, string(m.State))
	}
	return t.tx.SendBatch(ctx, b).Close()
}
