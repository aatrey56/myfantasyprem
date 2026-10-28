package tags

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres is the Store on Postgres (table player_tags). Times come from
// now, not the database clock, so tests can move time.
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
		`SELECT player_id, tag, updated_at FROM player_tags WHERE user_id = $1 AND league_id = $2
		 ORDER BY updated_at DESC, player_id`, userID, leagueID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Entry, error) {
		var e Entry
		err := r.Scan(&e.PlayerID, &e.Tag, &e.UpdatedAt)
		e.UpdatedAt = e.UpdatedAt.UTC()
		return e, err
	})
	if out == nil {
		out = []Entry{}
	}
	return out, err
}

func (p *Postgres) Set(ctx context.Context, userID, leagueID, playerID, tag string) (Entry, error) {
	if !Valid(tag) {
		return Entry{}, ErrUnknownTag
	}
	e := Entry{PlayerID: playerID, Tag: tag}
	// The same tag again keeps its time; a different one replaces it.
	err := p.pool.QueryRow(ctx,
		`INSERT INTO player_tags (user_id, league_id, player_id, tag, updated_at) VALUES ($1, $2, $3, $4, $5)
		 ON CONFLICT (user_id, league_id, player_id) DO UPDATE
		   SET tag = EXCLUDED.tag,
		       updated_at = CASE WHEN player_tags.tag = EXCLUDED.tag THEN player_tags.updated_at ELSE EXCLUDED.updated_at END
		 RETURNING updated_at`,
		userID, leagueID, playerID, tag, p.now().UTC().Truncate(time.Microsecond)).Scan(&e.UpdatedAt)
	e.UpdatedAt = e.UpdatedAt.UTC()
	return e, err
}

func (p *Postgres) Remove(ctx context.Context, userID, leagueID, playerID string) error {
	_, err := p.pool.Exec(ctx,
		`DELETE FROM player_tags WHERE user_id = $1 AND league_id = $2 AND player_id = $3`,
		userID, leagueID, playerID)
	return err
}
