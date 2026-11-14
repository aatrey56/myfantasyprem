package league

import (
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// A league without a league_settings row is corrupt: loading it must fail,
// not return zero settings a later write would persist.
func TestLoadLeagueWithoutSettingsFails(t *testing.T) {
	pool := storetest.New(t)
	seedUsers(t, pool)
	repo := NewPostgres(pool)
	m := NewService(repo, time.Now)
	ok, err := m.Create(ctx, usrA, "Kept League", 4, "A", SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	bad, err := m.Create(ctx, usrA, "Bare League", 4, "A", SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM league_settings WHERE league_id = $1`, bad.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, ok.ID); err != nil {
		t.Fatalf("league with settings: %v", err)
	}
	if l, err := repo.Get(ctx, bad.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("Get: want ErrNoRows, got %v (settings %+v)", err, l)
	}
	if _, err := repo.ForUser(ctx, usrA); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("ForUser: want ErrNoRows, got %v", err)
	}
	if err := repo.Tx(ctx, func(tx Tx) error {
		_, err := tx.Load(ctx, bad.ID)
		return err
	}); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("Tx Load: want ErrNoRows, got %v", err)
	}
}
