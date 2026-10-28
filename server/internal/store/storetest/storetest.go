// Package storetest gives integration tests a fresh, migrated Postgres
// database. Tests that use it are skipped unless TEST_DATABASE_URL points
// at a server where the user may CREATE DATABASE, for example:
//
//	docker compose up -d db
//	TEST_DATABASE_URL=postgres://myfantasyprem:myfantasyprem@localhost:5432/myfantasyprem?sslmode=disable go test ./...
//
// `npm run test:go:db` does both. Each test gets its own database, cloned
// from a migrated template, and drops it afterwards.
package storetest

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// EnvVar names the admin connection URL.
const EnvVar = "TEST_DATABASE_URL"

var (
	templateOnce sync.Once
	templateName string
	templateErr  error
)

// withDB returns admin with its database replaced by name.
func withDB(admin, name string) (string, error) {
	u, err := url.Parse(admin)
	if err != nil {
		return "", err
	}
	u.Path = "/" + name
	return u.String(), nil
}

func exec(ctx context.Context, admin, sql string) error {
	conn, err := pgx.Connect(ctx, admin)
	if err != nil {
		return err
	}
	defer conn.Close(ctx)
	_, err = conn.Exec(ctx, sql)
	return err
}

// template creates and migrates one template database per test process.
func template(ctx context.Context, admin string) (string, error) {
	templateOnce.Do(func() {
		templateName = "mfp_tpl_" + strings.ToLower(ids.ULID())
		if templateErr = exec(ctx, admin, "CREATE DATABASE "+templateName); templateErr != nil {
			return
		}
		var u string
		if u, templateErr = withDB(admin, templateName); templateErr != nil {
			return
		}
		templateErr = store.Migrate(ctx, u)
	})
	return templateName, templateErr
}

// URL returns the admin URL or skips the test.
func URL(t testing.TB) string {
	t.Helper()
	admin := os.Getenv(EnvVar)
	if admin == "" {
		t.Skipf("%s not set; skipping Postgres integration test (see internal/store/storetest)", EnvVar)
	}
	return admin
}

// New returns a pool on a fresh migrated database, dropped when t ends.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	pool, _ := NewWithURL(t)
	return pool
}

// NewWithURL is New that also returns the database URL.
func NewWithURL(t testing.TB) (*pgxpool.Pool, string) {
	t.Helper()
	admin := URL(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	tpl, err := template(ctx, admin)
	if err != nil {
		t.Fatalf("storetest: template database: %v", err)
	}
	name := "mfp_t_" + strings.ToLower(ids.ULID())
	if err := exec(ctx, admin, fmt.Sprintf("CREATE DATABASE %s TEMPLATE %s", name, tpl)); err != nil {
		t.Fatalf("storetest: create database: %v", err)
	}
	u, err := withDB(admin, name)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := store.Open(ctx, u)
	if err != nil {
		t.Fatalf("storetest: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_ = exec(ctx, admin, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})
	return pool, u
}
