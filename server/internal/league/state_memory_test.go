package league

import (
	"sync"
	"testing"
	"time"
)

// Memory's State and History hand out copies: a reader writing to what
// it got must not race with a transaction mutating the stored league and
// game (go test -race).
func TestMemoryStateIsACopy(t *testing.T) {
	repo := NewMemory()
	m := NewService(repo, time.Now)
	l, err := m.Create(ctx, usrA, "Race League", 4, "A", SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	team := l.Teams[0].ID
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := range 200 {
			if err := repo.Tx(ctx, func(tx Tx) error {
				s, err := tx.LoadState(ctx, l.ID)
				if err != nil {
					return err
				}
				s.League.Name = "Race League " + string(rune('a'+i%26))
				s.Game.Acquire(team, "plr_"+string(rune('a'+i%26)), 1, "draft")
				return tx.SaveState(ctx, s)
			}); err != nil {
				t.Error(err)
				return
			}
		}
	}()
	go func() {
		defer wg.Done()
		for range 200 {
			st, err := m.GetState(ctx, usrA, l.ID)
			if err != nil {
				t.Error(err)
				return
			}
			st.League.Name, st.League.Teams[0].Name = "reader", "reader"
			st.Game.Rosters = append(st.Game.Rosters[:0:0], st.Game.Rosters...)
			h, _, err := repo.History(ctx, l.ID)
			if err != nil {
				t.Error(err)
				return
			}
			h.League.Name = "reader"
			for i := range h.Game.Rosters {
				h.Game.Rosters[i].PlayerID = "reader"
			}
		}
	}()
	wg.Wait()
	got, err := repo.Get(ctx, l.ID)
	if err != nil || got.Name == "reader" || got.Teams[0].Name == "reader" {
		t.Fatalf("a reader's write reached the store: %+v %v", got, err)
	}
}
