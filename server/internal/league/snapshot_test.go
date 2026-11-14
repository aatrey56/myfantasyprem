package league

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// beforeQuery runs hook once, just before the first query whose SQL
// contains match.
type beforeQuery struct {
	store.DB
	match string
	hook  func()
}

func (b *beforeQuery) fire(sql string) {
	if h := b.hook; h != nil && strings.Contains(sql, b.match) {
		b.hook = nil
		h()
	}
}

func (b *beforeQuery) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	b.fire(sql)
	return b.DB.Query(ctx, sql, args...)
}

func (b *beforeQuery) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	b.fire(sql)
	return b.DB.QueryRow(ctx, sql, args...)
}

// A pick and its autopick flag commit together while State is reading:
// after the autopick query, before the picks query. State must show both
// or neither, never the pick without the flag.
func TestStateReadsOneSnapshot(t *testing.T) {
	pool := storetest.New(t)
	seedUsers(t, pool)
	club, player := "clb_"+ids.ULID(), ids.New("plr")
	if err := stats.NewPostgres(pool).Apply(ctx, stats.Batch{
		Clubs:   []stats.Club{{ID: club, Code: "NOR", Name: "Northgate"}},
		Players: []stats.Player{{ID: player, DisplayName: player, Position: scoring.DEF, ClubID: club, Availability: "available", DraftRank: 1}},
	}); err != nil {
		t.Fatal(err)
	}
	repo := NewPostgres(pool)
	m := NewService(repo, time.Now)
	l, err := m.Create(ctx, usrA, "Snapshot League", 4, "A", SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	team := l.Teams[0].ID
	now := time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)
	draftID := ids.New("drf")
	if err := repo.Tx(ctx, func(tx Tx) error {
		s, err := tx.LoadState(ctx, l.ID)
		if err != nil {
			return err
		}
		s.Draft = &Draft{ID: draftID, Status: DraftInProgress, PickTimerS: 60, ClockStartedAt: &now, Order: []string{team},
			Autopick: map[string]bool{}, StayManual: map[string]bool{}, Queues: map[string][]string{}}
		return tx.SaveState(ctx, s)
	}); err != nil {
		t.Fatal(err)
	}

	writer, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = writer.Rollback(ctx) }()
	if _, err := writer.Exec(ctx,
		`INSERT INTO draft_picks (id, draft_id, overall, team_id, player_id, method, picked_at, missed_clock)
		 VALUES ($1, $2, 1, $3, $4, $5, $6, true)`, ids.New("pk"), draftID, team, player, PickRank, now); err != nil {
		t.Fatal(err)
	}
	if _, err := writer.Exec(ctx, `INSERT INTO draft_autopick (draft_id, team_id) VALUES ($1, $2)`, draftID, team); err != nil {
		t.Fatal(err)
	}
	committed := false
	repo.wrapRead = func(db store.DB) store.DB {
		return &beforeQuery{DB: db, match: "FROM draft_picks", hook: func() {
			committed = writer.Commit(ctx) == nil
		}}
	}
	st, err := repo.State(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !committed {
		t.Fatal("the writer did not commit during State")
	}
	picked, auto := len(st.Draft.Picks) == 1, st.Draft.Autopick[team]
	if picked != auto {
		t.Errorf("State saw pick=%v autopick=%v, want both or neither", picked, auto)
	}
}

// SaveScores waits for a transaction that holds the league (LoadState),
// so the transaction's reads of matchups and scores cannot straddle a
// rescore's commit.
func TestSaveScoresWaitsForLeagueLock(t *testing.T) {
	pool := storetest.New(t)
	seedUsers(t, pool)
	repo := NewPostgres(pool)
	l, err := NewService(repo, time.Now).Create(ctx, usrA, "Lock League", 4, "A", SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	score := []TeamScore{{TeamID: l.Teams[0].ID, Gameweek: 1, Points: 7, State: scoring.StateLive}}
	done := make(chan error, 1)
	if err := repo.Tx(ctx, func(tx Tx) error {
		if _, err := tx.LoadState(ctx, l.ID); err != nil {
			return err
		}
		go func() {
			_, err := repo.SaveScores(ctx, l.ID, 1, score, nil)
			done <- err
		}()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
			select {
			case err := <-done:
				done <- err
				t.Errorf("SaveScores finished (%v) while the league was locked", err)
				return nil
			default:
			}
			var waiting int
			if err := pool.QueryRow(ctx,
				`SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND wait_event_type = 'Lock'`).
				Scan(&waiting); err != nil {
				return err
			}
			if waiting > 0 {
				return nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Error("SaveScores neither waited nor finished")
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	g, err := repo.Game(ctx, l.ID)
	if err != nil {
		t.Fatal(err)
	}
	if s, ok := g.Score(l.Teams[0].ID, 1); !ok || s.Points != 7 {
		t.Errorf("score = %+v %v, want 7 saved after the transaction", s, ok)
	}
}
