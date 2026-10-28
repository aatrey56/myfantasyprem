// Package store is the Postgres plumbing: the connection pool, embedded
// goose migrations, transactions and error helpers. It imports nothing
// from the app; each domain package (auth, league, stats) owns its own
// queries and implements its repository on top of these helpers, next to
// an in-memory implementation that unit tests use (docs/adr/0009).
//
// Jobs built on it must be idempotent so a retry or a duplicate run cannot
// change the result.
package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB is what repositories need: a pool or a transaction.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults
}

// Open connects a pool to url (a postgres:// URL) and pings it.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("store: parse DATABASE_URL: %w", err)
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 10 * time.Minute
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: connect: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: ping: %w", err)
	}
	return pool, nil
}

// InTx runs fn in a transaction at the given isolation level and commits
// when fn returns nil. A serialization failure or deadlock is retried up to
// three times, so fn must not have side effects outside the transaction.
func InTx(ctx context.Context, pool *pgxpool.Pool, iso pgx.TxIsoLevel, fn func(pgx.Tx) error) error {
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = pgx.BeginTxFunc(ctx, pool, pgx.TxOptions{IsoLevel: iso}, fn)
		if !retryable(err) {
			return err
		}
	}
	return err
}

func retryable(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && (pg.Code == "40001" || pg.Code == "40P01")
}

// IsUniqueViolation reports whether err is a unique-constraint failure,
// optionally on the named constraint.
func IsUniqueViolation(err error, constraint ...string) bool {
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" {
		return false
	}
	return len(constraint) == 0 || pg.ConstraintName == constraint[0]
}

// IsForeignKeyViolation reports whether err referenced a missing row.
func IsForeignKeyViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23503"
}

// IsExclusionViolation reports whether err broke an exclusion constraint
// (for example two owners of one player in a gameweek).
func IsExclusionViolation(err error) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23P01"
}
