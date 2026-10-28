package notifications

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// Postgres is the Store on Postgres (see migration 00016). Times come from
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

func (p *Postgres) stamp() time.Time { return p.now().UTC().Truncate(time.Microsecond) }

func (p *Postgres) Disabled(ctx context.Context, userID, leagueID string) (map[Type]bool, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT type FROM notification_prefs WHERE user_id = $1 AND league_id = $2 AND NOT enabled`, userID, leagueID)
	if err != nil {
		return nil, err
	}
	ts, err := pgx.CollectRows(rows, pgx.RowTo[string])
	out := map[Type]bool{}
	for _, t := range ts {
		out[Type(t)] = true
	}
	return out, err
}

func (p *Postgres) SetPreferences(ctx context.Context, userID, leagueID string, all *bool, settings map[Type]bool) error {
	return store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		set := map[Type]bool{}
		if all != nil {
			if _, err := tx.Exec(ctx, `DELETE FROM notification_prefs WHERE user_id = $1 AND league_id = $2`, userID, leagueID); err != nil {
				return err
			}
			if !*all {
				for _, i := range Catalog {
					set[i.Type] = false
				}
			}
		}
		for t, on := range settings {
			set[t] = on
		}
		for t, on := range set {
			if _, err := tx.Exec(ctx,
				`INSERT INTO notification_prefs (user_id, league_id, type, enabled) VALUES ($1, $2, $3, $4)
				 ON CONFLICT (user_id, league_id, type) DO UPDATE SET enabled = EXCLUDED.enabled`,
				userID, leagueID, string(t), on); err != nil {
				return err
			}
		}
		return nil
	})
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (p *Postgres) Add(ctx context.Context, n Notification, push bool) (Notification, bool, error) {
	n.ID = ids.New("ntf")
	n.CreatedAt = p.stamp()
	n.ReadAt = nil
	added := false
	err := store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx,
			`INSERT INTO notifications (id, user_id, league_id, type, title, body, url, dedupe_key, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) ON CONFLICT (user_id, dedupe_key) DO NOTHING`,
			n.ID, n.UserID, nullable(n.LeagueID), string(n.Type), n.Title, n.Body, n.URL, n.Key, n.CreatedAt)
		if err != nil {
			return err
		}
		added = tag.RowsAffected() == 1
		if !added || !push {
			return nil
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO push_outbox (notification_id, subscription_id, next_attempt_at)
			 SELECT $1, id, $3 FROM push_subscriptions WHERE user_id = $2`, n.ID, n.UserID, n.CreatedAt)
		return err
	})
	if !added {
		return Notification{}, false, err
	}
	return n, true, err
}

const ntfCols = `id, user_id, coalesce(league_id, ''), type, title, body, url, dedupe_key, created_at, read_at`

func scanNtf(r pgx.CollectableRow) (Notification, error) {
	var n Notification
	var t string
	err := r.Scan(&n.ID, &n.UserID, &n.LeagueID, &t, &n.Title, &n.Body, &n.URL, &n.Key, &n.CreatedAt, &n.ReadAt)
	n.Type = Type(t)
	n.CreatedAt = n.CreatedAt.UTC()
	if n.ReadAt != nil {
		r := n.ReadAt.UTC()
		n.ReadAt = &r
	}
	return n, err
}

func (p *Postgres) List(ctx context.Context, userID string, limit int, unreadOnly bool) ([]Notification, int, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+ntfCols+` FROM notifications WHERE user_id = $1 AND (NOT $2 OR read_at IS NULL)
		 ORDER BY created_at DESC, id DESC LIMIT $3`, userID, unreadOnly, limit)
	if err != nil {
		return nil, 0, err
	}
	out, err := pgx.CollectRows(rows, scanNtf)
	if err != nil {
		return nil, 0, err
	}
	if out == nil {
		out = []Notification{}
	}
	var unread int
	err = p.pool.QueryRow(ctx, `SELECT count(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL`, userID).Scan(&unread)
	return out, unread, err
}

func (p *Postgres) MarkRead(ctx context.Context, userID string, idList []string, all bool) error {
	_, err := p.pool.Exec(ctx,
		`UPDATE notifications SET read_at = $4 WHERE user_id = $1 AND read_at IS NULL AND ($3 OR id = ANY($2))`,
		userID, idList, all, p.stamp())
	return err
}

func (p *Postgres) Subscribe(ctx context.Context, sub Subscription) (Subscription, error) {
	err := store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		var s Subscription
		err := tx.QueryRow(ctx,
			`SELECT id, user_id, kind, endpoint, p256dh, auth, created_at FROM push_subscriptions
			 WHERE kind = $1 AND endpoint = $2 FOR UPDATE`, sub.Kind, sub.Endpoint).
			Scan(&s.ID, &s.UserID, &s.Kind, &s.Endpoint, &s.P256dh, &s.Auth, &s.CreatedAt)
		switch {
		case err == nil && s.UserID == sub.UserID:
			s.CreatedAt = s.CreatedAt.UTC()
			sub = s
			return nil
		case err == nil:
			// The device changed hands: it now belongs to the new user.
			if _, err := tx.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, s.ID); err != nil {
				return err
			}
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		}
		sub.ID = ids.New("psub")
		sub.CreatedAt = p.stamp()
		_, err = tx.Exec(ctx,
			`INSERT INTO push_subscriptions (id, user_id, kind, endpoint, p256dh, auth, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			sub.ID, sub.UserID, sub.Kind, sub.Endpoint, sub.P256dh, sub.Auth, sub.CreatedAt)
		return err
	})
	return sub, err
}

func (p *Postgres) Unsubscribe(ctx context.Context, userID, id string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1 AND user_id = $2`, id, userID)
	return err
}

func (p *Postgres) Drop(ctx context.Context, id string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM push_subscriptions WHERE id = $1`, id)
	return err
}

func (p *Postgres) Due(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Delivery, error) {
	rows, err := p.pool.Query(ctx,
		`WITH due AS (
		   SELECT id FROM push_outbox WHERE next_attempt_at <= $1
		   ORDER BY next_attempt_at, id LIMIT $3 FOR UPDATE SKIP LOCKED
		 ), claimed AS (
		   UPDATE push_outbox o SET next_attempt_at = $2 FROM due WHERE o.id = due.id
		   RETURNING o.id, o.notification_id, o.subscription_id, o.attempts
		 )
		 SELECT c.id, c.attempts, n.id, n.user_id, coalesce(n.league_id, ''), n.type, n.title, n.body, n.url,
		        n.dedupe_key, n.created_at, n.read_at,
		        s.id, s.user_id, s.kind, s.endpoint, s.p256dh, s.auth, s.created_at
		 FROM claimed c JOIN notifications n ON n.id = c.notification_id
		 JOIN push_subscriptions s ON s.id = c.subscription_id ORDER BY c.id`,
		now.UTC(), now.Add(lease).UTC(), limit)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Delivery, error) {
		var d Delivery
		var t string
		n, s := &d.Notification, &d.Subscription
		err := r.Scan(&d.ID, &d.Attempts, &n.ID, &n.UserID, &n.LeagueID, &t, &n.Title, &n.Body, &n.URL,
			&n.Key, &n.CreatedAt, &n.ReadAt, &s.ID, &s.UserID, &s.Kind, &s.Endpoint, &s.P256dh, &s.Auth, &s.CreatedAt)
		n.Type = Type(t)
		n.CreatedAt, s.CreatedAt = n.CreatedAt.UTC(), s.CreatedAt.UTC()
		return d, err
	})
	if out == nil {
		out = []Delivery{}
	}
	return out, err
}

func (p *Postgres) Delivered(ctx context.Context, id int64) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM push_outbox WHERE id = $1`, id)
	return err
}

func (p *Postgres) Retry(ctx context.Context, id int64, next time.Time) error {
	if next.IsZero() {
		return p.Delivered(ctx, id)
	}
	_, err := p.pool.Exec(ctx,
		`UPDATE push_outbox SET attempts = attempts + 1, next_attempt_at = $2 WHERE id = $1`, id, next.UTC())
	return err
}
