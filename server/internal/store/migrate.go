package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"
	"github.com/riverqueue/river/rivermigrate"
)

//go:embed migrations/*.sql
var migrations embed.FS

// migrator opens a goose provider on url. goose speaks database/sql, so it
// gets its own short-lived connection through pgx's stdlib driver.
func migrator(url string) (*goose.Provider, *sql.DB, error) {
	db, err := sql.Open("pgx", url)
	if err != nil {
		return nil, nil, err
	}
	sub, err := fs.Sub(migrations, "migrations")
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	// A session-level advisory lock serialises concurrent migrators
	// (cmd/migrate, cmd/seed, cmd/api with MIGRATE_ON_START). goose
	// disables locking unless a locker is configured.
	locker, err := lock.NewPostgresSessionLocker(lock.WithLockTimeout(1, 600)) // retry each second, up to 10 min
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	p, err := goose.NewProvider(goose.DialectPostgres, db, sub, goose.WithSessionLocker(locker))
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return p, db, nil
}

// Migrate applies every pending migration: ours (goose), then River's job
// tables (rivermigrate, versioned with the River module in go.mod; ADR
// 0010). It is safe to run on every start and from several processes: the
// goose provider and rivermigrate each hold a Postgres advisory lock.
func Migrate(ctx context.Context, url string) error {
	p, db, err := migrator(url)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer db.Close()
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("store: migrate up: %w", err)
	}
	return migrateRiver(ctx, url, rivermigrate.DirectionUp)
}

// migrateRiver applies (or, down, removes) River's own tables.
func migrateRiver(ctx context.Context, url string, dir rivermigrate.Direction) error {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return fmt.Errorf("store: river migrate: %w", err)
	}
	defer pool.Close()
	m, err := rivermigrate.New(riverpgxv5.New(pool), nil)
	if err != nil {
		return fmt.Errorf("store: river migrate: %w", err)
	}
	opts := &rivermigrate.MigrateOpts{}
	if dir == rivermigrate.DirectionDown {
		opts.TargetVersion = -1 // every River migration
	}
	if _, err := m.Migrate(ctx, dir, opts); err != nil {
		return fmt.Errorf("store: river migrate %s: %w", dir, err)
	}
	return nil
}

// MigrateDown rolls back to version (0 = an empty database, River's tables
// included).
func MigrateDown(ctx context.Context, url string, version int64) error {
	p, db, err := migrator(url)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer db.Close()
	if version == 0 {
		if err := migrateRiver(ctx, url, rivermigrate.DirectionDown); err != nil {
			return err
		}
	}
	if _, err := p.DownTo(ctx, version); err != nil {
		return fmt.Errorf("store: migrate down: %w", err)
	}
	return nil
}

// MigrationStatus writes one line per migration to w.
func MigrationStatus(ctx context.Context, url string, w io.Writer) error {
	p, db, err := migrator(url)
	if err != nil {
		return fmt.Errorf("store: migrate: %w", err)
	}
	defer db.Close()
	st, err := p.Status(ctx)
	if err != nil {
		return err
	}
	for _, s := range st {
		applied := "pending"
		if s.State == goose.StateApplied {
			applied = "applied " + s.AppliedAt.UTC().Format("2006-01-02 15:04:05Z")
		}
		fmt.Fprintf(w, "%05d %-40s %s\n", s.Source.Version, s.Source.Path, applied)
	}
	return nil
}

// stdlib registers the "pgx" database/sql driver.
var _ = stdlib.GetDefaultDriver
