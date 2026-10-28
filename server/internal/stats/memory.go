package stats

import (
	"bytes"
	"cmp"
	"context"
	"maps"
	"reflect"
	"slices"
	"sync"
)

// Memory is the in-memory Repo, for unit tests and the database-less dev
// server.
type Memory struct {
	mu        sync.Mutex
	clubs     map[string]Club
	players   map[string]Player
	gameweeks map[int]Gameweek
	fixtures  map[string]Fixture
	rows      map[[2]string]PlayerFixture // by player, fixture
	gwStats   map[int]map[string]PlayerGameweek
	status    *Status
	poll      PollState
	rescore   map[int]int64        // gameweek -> mark version
	checked   map[int]bool         // StartsChecked
	version   int64                // the last mark version
	external  map[[3]string]string // provider, entity, external id -> id
	snapshots map[[2]any][]byte    // kind, gameweek -> last payload
	// SnapshotCount counts stored (deduplicated) snapshots, for tests.
	SnapshotCount int
}

var _ Repo = (*Memory)(nil)

// NewMemory returns an empty repository.
func NewMemory() *Memory {
	return &Memory{
		clubs:     map[string]Club{},
		players:   map[string]Player{},
		gameweeks: map[int]Gameweek{},
		fixtures:  map[string]Fixture{},
		rows:      map[[2]string]PlayerFixture{},
		gwStats:   map[int]map[string]PlayerGameweek{},
		rescore:   map[int]int64{},
		checked:   map[int]bool{},
		external:  map[[3]string]string{},
		snapshots: map[[2]any][]byte{},
	}
}

func values[K comparable, V any](m map[K]V, less func(a, b V) int) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	slices.SortFunc(out, less)
	return out
}

func (m *Memory) Clubs(context.Context) ([]Club, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return values(m.clubs, func(a, b Club) int { return cmp.Compare(a.Name, b.Name) }), nil
}

func (m *Memory) Players(context.Context) ([]Player, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return values(m.players, func(a, b Player) int { return cmp.Compare(a.ID, b.ID) }), nil
}

func (m *Memory) Gameweeks(context.Context) ([]Gameweek, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return values(m.gameweeks, func(a, b Gameweek) int { return cmp.Compare(a.Number, b.Number) }), nil
}

func (m *Memory) Fixtures(context.Context) ([]Fixture, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return values(m.fixtures, compareFixtures), nil
}

// compareFixtures orders by kickoff (unscheduled last), then id.
func compareFixtures(a, b Fixture) int {
	switch {
	case a.KickoffAt == nil && b.KickoffAt != nil:
		return 1
	case a.KickoffAt != nil && b.KickoffAt == nil:
		return -1
	case a.KickoffAt != nil && !a.KickoffAt.Equal(*b.KickoffAt):
		return a.KickoffAt.Compare(*b.KickoffAt)
	}
	return cmp.Compare(a.ID, b.ID)
}

func (m *Memory) PlayerFixtures(_ context.Context, gw int) ([]PlayerFixture, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []PlayerFixture
	for _, r := range m.rows {
		if gw == 0 || r.Gameweek == gw {
			out = append(out, r)
		}
	}
	slices.SortFunc(out, func(a, b PlayerFixture) int {
		return cmp.Or(cmp.Compare(a.Gameweek, b.Gameweek), cmp.Compare(a.PlayerID, b.PlayerID), cmp.Compare(a.FixtureID, b.FixtureID))
	})
	return out, nil
}

func (m *Memory) PlayerGameweeks(_ context.Context, gw int) ([]PlayerGameweek, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []PlayerGameweek
	for g, players := range m.gwStats {
		if gw == 0 || g == gw {
			for _, p := range players {
				out = append(out, p)
			}
		}
	}
	slices.SortFunc(out, comparePlayerGameweeks)
	return out, nil
}

func (m *Memory) PlayerGameweeksOf(_ context.Context, playerID string) ([]PlayerGameweek, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []PlayerGameweek
	for _, players := range m.gwStats {
		if p, ok := players[playerID]; ok {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, comparePlayerGameweeks)
	return out, nil
}

func comparePlayerGameweeks(a, b PlayerGameweek) int {
	return cmp.Or(cmp.Compare(a.Gameweek, b.Gameweek), cmp.Compare(a.PlayerID, b.PlayerID))
}

func (m *Memory) Status(context.Context) (Status, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status == nil {
		return Status{}, false, nil
	}
	return *m.status, true, nil
}

func (m *Memory) PollState(context.Context) (PollState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.poll, nil
}

func (m *Memory) SavePollState(_ context.Context, ps PollState) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.poll = ps
	return nil
}

func (m *Memory) RescorePending(context.Context) ([]RescoreMark, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]RescoreMark, 0, len(m.rescore))
	for g, v := range m.rescore {
		out = append(out, RescoreMark{Gameweek: g, Version: v})
	}
	slices.SortFunc(out, func(a, b RescoreMark) int { return cmp.Compare(a.Gameweek, b.Gameweek) })
	return out, nil
}

func (m *Memory) ClearRescore(_ context.Context, mk RescoreMark) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.rescore[mk.Gameweek]; ok && v == mk.Version {
		delete(m.rescore, mk.Gameweek)
	}
	return nil
}

func (m *Memory) StartsChecked(context.Context) (map[int]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return maps.Clone(m.checked), nil
}

func (m *Memory) MarkStartsChecked(_ context.Context, gw int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.checked[gw] = true
	return nil
}

func (m *Memory) ExternalIDs(_ context.Context, provider, entity string) (map[string]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[string]string{}
	for k, v := range m.external {
		if k[0] == provider && k[1] == entity {
			out[k[2]] = v
		}
	}
	return out, nil
}

// sameRow compares the values an ingest produces (not Rev).
func sameRow(a, b PlayerFixture) bool {
	if !slices.Equal(a.Breakdown, b.Breakdown) {
		return false
	}
	a.Rev, b.Rev = 0, 0
	a.Breakdown, b.Breakdown = nil, nil
	return reflect.DeepEqual(a, b)
}

func (m *Memory) Apply(_ context.Context, b Batch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mp := range b.Mappings {
		if id, ok := m.external[[3]string{b.Provider, mp.Entity, mp.ExternalID}]; ok && id != mp.ID {
			return ErrIDTaken
		}
	}
	for _, mp := range b.Mappings {
		m.external[[3]string{b.Provider, mp.Entity, mp.ExternalID}] = mp.ID
	}
	for _, c := range b.Clubs {
		m.clubs[c.ID] = c
	}
	for _, p := range b.Players {
		m.players[p.ID] = p
	}
	for _, g := range b.Gameweeks {
		m.gameweeks[g.Number] = g
	}
	for _, f := range b.Fixtures {
		m.fixtures[f.ID] = f
	}
	if b.Status != nil {
		st := *b.Status
		m.status = &st
	}
	if b.Live != nil {
		keep := map[[2]string]bool{}
		for _, r := range b.Live.Rows {
			k := [2]string{r.PlayerID, r.FixtureID}
			keep[k] = true
			r.Gameweek = b.Live.Gameweek
			old, ok := m.rows[k]
			switch {
			case !ok:
				r.Rev = 1
			case sameRow(old, r):
				continue
			default:
				r.Rev = old.Rev + 1
			}
			m.rows[k] = r
		}
		for k, r := range m.rows {
			if r.Gameweek == b.Live.Gameweek && !keep[k] {
				delete(m.rows, k)
			}
		}
		if b.Live.Players != nil {
			players := map[string]PlayerGameweek{}
			for _, p := range b.Live.Players {
				p.Gameweek = b.Live.Gameweek
				players[p.PlayerID] = p
			}
			m.gwStats[b.Live.Gameweek] = players
		}
	}
	for _, g := range b.Rescore {
		m.version++
		m.rescore[g] = m.version
	}
	for _, s := range b.Snapshots {
		k := [2]any{s.Kind, s.Gameweek}
		if !bytes.Equal(m.snapshots[k], s.Payload) {
			m.snapshots[k] = s.Payload
			m.SnapshotCount++
		}
	}
	return nil
}
