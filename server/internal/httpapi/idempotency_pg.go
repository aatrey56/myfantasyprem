package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresIdempotency is the IdempotencyStore on the idempotency_keys
// table, so a retry that lands on another API instance still replays.
type PostgresIdempotency struct {
	pool *pgxpool.Pool
}

// NewPostgresIdempotency returns a store on pool.
func NewPostgresIdempotency(pool *pgxpool.Pool) *PostgresIdempotency {
	return &PostgresIdempotency{pool: pool}
}

// Prune deletes the records created before the cutoff (the prune job).
func (p *PostgresIdempotency) Prune(ctx context.Context, before time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE created_at < $1`, before)
	return int(tag.RowsAffected()), err
}

func (p *PostgresIdempotency) Begin(ctx context.Context, user, key, fingerprint string, now time.Time) (IdemClaim, error) {
	// An expired entry no longer counts.
	if _, err := p.pool.Exec(ctx,
		`DELETE FROM idempotency_keys WHERE user_id = $1 AND key = $2 AND created_at <= $3`,
		user, key, now.Add(-idempotencyTTL)); err != nil {
		return IdemClaim{}, err
	}
	tag, err := p.pool.Exec(ctx,
		`INSERT INTO idempotency_keys (user_id, key, fingerprint, status, created_at)
		 VALUES ($1, $2, $3, 'in_flight', $4) ON CONFLICT (user_id, key) DO NOTHING`,
		user, key, fingerprint, now)
	if err != nil {
		return IdemClaim{}, err
	}
	if tag.RowsAffected() == 1 {
		return IdemClaim{State: IdemNew}, nil
	}
	var (
		fp, status string
		code       *int
		header     []byte
		body       []byte
	)
	err = p.pool.QueryRow(ctx,
		`SELECT fingerprint, status, response_status, response_headers, response_body
		 FROM idempotency_keys WHERE user_id = $1 AND key = $2`, user, key).
		Scan(&fp, &status, &code, &header, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		// Released between the insert and the read: treat as in flight; the
		// client retries.
		return IdemClaim{State: IdemInFlight}, nil
	}
	if err != nil {
		return IdemClaim{}, err
	}
	switch {
	case fp != fingerprint:
		return IdemClaim{State: IdemMismatch}, nil
	case status != "done" || code == nil:
		return IdemClaim{State: IdemInFlight}, nil
	}
	h := http.Header{}
	if len(header) > 0 {
		if err := json.Unmarshal(header, &h); err != nil {
			return IdemClaim{}, err
		}
	}
	return IdemClaim{State: IdemReplay, Status: *code, Header: h, Body: body}, nil
}

func (p *PostgresIdempotency) Finish(ctx context.Context, user, key string, status int, header http.Header, body []byte) error {
	raw, err := json.Marshal(header)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx,
		`UPDATE idempotency_keys SET status = 'done', response_status = $3, response_headers = $4, response_body = $5
		 WHERE user_id = $1 AND key = $2`, user, key, status, raw, body)
	return err
}

func (p *PostgresIdempotency) Abort(ctx context.Context, user, key string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM idempotency_keys WHERE user_id = $1 AND key = $2`, user, key)
	return err
}
