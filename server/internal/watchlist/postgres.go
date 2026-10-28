package watchlist

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// Postgres is the Store on Postgres (table watchlist_entries). Times come
// from now, not the database clock, so tests can move time.
type Postgres struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

var _ Store = (*Postgres)(nil)

// NewPostgres returns a Store on pool.
func NewPostgres(pool *pgxpool.Pool, now func() time.Time) *Postgres {
	return &Postgres{pool: pool, now: now}
}

func (p *Postgres) List(ctx context.Context, userID, leagueID string) ([]Entry, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT player_id, added_at FROM watchlist_entries WHERE user_id = $1 AND league_id = $2
		 ORDER BY added_at DESC, player_id`, userID, leagueID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) {
		var e Entry
		err := r.Scan(&e.PlayerID, &e.AddedAt)
		e.AddedAt = e.AddedAt.UTC()
		return e, err
	})
	if out == nil {
		out = []Entry{}
	}
	return out, err
}

func (p *Postgres) Add(ctx context.Context, userID, leagueID, playerID string) (Entry, error) {
	var e Entry
	err := store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx,
			`SELECT player_id, added_at FROM watchlist_entries WHERE user_id = $1 AND league_id = $2 AND player_id = $3`,
			userID, leagueID, playerID).Scan(&e.PlayerID, &e.AddedAt)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Serialise adds to one list so two at once cannot pass Max.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1 || '/' || $2))`, userID, leagueID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT count(*) FROM watchlist_entries WHERE user_id = $1 AND league_id = $2`,
			userID, leagueID).Scan(&n); err != nil {
			return err
		}
		if n >= Max {
			return ErrFull
		}
		e = Entry{PlayerID: playerID, AddedAt: p.now().UTC().Truncate(time.Microsecond)}
		return tx.QueryRow(ctx,
			`INSERT INTO watchlist_entries (user_id, league_id, player_id, added_at) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (user_id, league_id, player_id) DO UPDATE SET added_at = watchlist_entries.added_at
			 RETURNING added_at`,
			userID, leagueID, playerID, e.AddedAt).Scan(&e.AddedAt)
	})
	e.AddedAt = e.AddedAt.UTC()
	return e, err
}

func (p *Postgres) Remove(ctx context.Context, userID, leagueID, playerID string) error {
	_, err := p.pool.Exec(ctx,
		`DELETE FROM watchlist_entries WHERE user_id = $1 AND league_id = $2 AND player_id = $3`,
		userID, leagueID, playerID)
	return err
}
