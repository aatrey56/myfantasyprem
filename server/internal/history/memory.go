package history

import (
	"cmp"
	"context"
	"slices"
	"sync"
	"time"
)

// Memory is the Store in memory, for tests and the demo.
type Memory struct {
	mu      sync.Mutex
	seasons map[string]map[string]Season
	fetched map[string]time.Time
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty Memory.
func NewMemory() *Memory {
	return &Memory{seasons: map[string]map[string]Season{}, fetched: map[string]time.Time{}}
}

func (m *Memory) Save(_ context.Context, code string, seasons []Season, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seasons[code] == nil {
		m.seasons[code] = map[string]Season{}
	}
	for _, s := range seasons {
		s.PlayerExternalID = code
		m.seasons[code][s.Season] = s
	}
	m.fetched[code] = at.UTC()
	return nil
}

func (m *Memory) Seasons(_ context.Context, code string) ([]Season, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Season{}
	for _, s := range m.seasons[code] {
		out = append(out, s)
	}
	slices.SortFunc(out, func(a, b Season) int { return cmp.Compare(b.Season, a.Season) })
	return out, nil
}

func (m *Memory) Fetched(context.Context) (map[string]time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]time.Time, len(m.fetched))
	for k, v := range m.fetched {
		out[k] = v
	}
	return out, nil
}
