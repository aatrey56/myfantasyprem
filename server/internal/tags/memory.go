package tags

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
	// tags maps user, then league, then player to the entry.
	tags map[string]map[string]map[string]Entry
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty Memory reading the clock through now.
func NewMemory(now func() time.Time) *Memory {
	return &Memory{now: now, tags: map[string]map[string]map[string]Entry{}}
}

func (m *Memory) List(_ context.Context, userID, leagueID string) ([]Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Entry{}
	for _, e := range m.tags[userID][leagueID] {
		out = append(out, e)
	}
	slices.SortFunc(out, newestFirst)
	return out, nil
}

func (m *Memory) Set(_ context.Context, userID, leagueID, playerID, tag string) (Entry, error) {
	if !Valid(tag) {
		return Entry{}, ErrUnknownTag
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.tags[userID] == nil {
		m.tags[userID] = map[string]map[string]Entry{}
	}
	list := m.tags[userID][leagueID]
	if list == nil {
		list = map[string]Entry{}
		m.tags[userID][leagueID] = list
	}
	if e, ok := list[playerID]; ok && e.Tag == tag {
		return e, nil
	}
	e := Entry{PlayerID: playerID, Tag: tag, UpdatedAt: m.now().UTC().Truncate(time.Microsecond)}
	list[playerID] = e
	return e, nil
}

func (m *Memory) Remove(_ context.Context, userID, leagueID, playerID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tags[userID][leagueID], playerID)
	return nil
}
