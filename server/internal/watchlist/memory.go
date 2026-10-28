package watchlist

import (
	"context"
	"slices"
	"sync"
	"time"
)

// Memory is the Store in memory (tests and the database-less dev server).
type Memory struct {
	mu  sync.Mutex
	now func() time.Time
	// lists maps user, then league, then player to the entry.
	lists map[string]map[string]map[string]Entry
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty Memory reading the clock through now.
func NewMemory(now func() time.Time) *Memory {
	return &Memory{now: now, lists: map[string]map[string]map[string]Entry{}}
}

func (m *Memory) List(_ context.Context, userID, leagueID string) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Entry{}
	for _, e := range m.lists[userID][leagueID] {
		out = append(out, e)
	}
	slices.SortFunc(out, newestFirst)
	return out, nil
}

func (m *Memory) Add(_ context.Context, userID, leagueID, playerID string) (Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.lists[userID] == nil {
		m.lists[userID] = map[string]map[string]Entry{}
	}
	list := m.lists[userID][leagueID]
	if list == nil {
		list = map[string]Entry{}
		m.lists[userID][leagueID] = list
	}
	if e, ok := list[playerID]; ok {
		return e, nil
	}
	if len(list) >= Max {
		return Entry{}, ErrFull
	}
	e := Entry{PlayerID: playerID, AddedAt: m.now().UTC().Truncate(time.Microsecond)}
	list[playerID] = e
	return e, nil
}

func (m *Memory) Remove(_ context.Context, userID, leagueID, playerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.lists[userID][leagueID], playerID)
	return nil
}
