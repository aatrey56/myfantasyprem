package league

import (
	"context"
	"slices"
	"sync"
	"time"
)

// Memory is the in-memory Repo, for unit tests and the database-less dev
// server. One mutex serialises every transaction.
type Memory struct {
	mu      sync.Mutex
	leagues map[string]*League
	games   map[string]*Game
	extras  map[string]*memExtra // the rest of each league's State
	// nextSlot numbers roster slots, as roster_slots.id does.
	nextSlot int64
	sched    Scheduler
	// scoring holds LockScores' locks by gameweek.
	scoring map[int]chan struct{}
}

// SetScheduler makes Tx.Schedule hand jobs to s.
func (m *Memory) SetScheduler(s Scheduler) { m.sched = s }

func (t memTx) Schedule(ctx context.Context, args JobArgs, at time.Time) error {
	if t.m.sched == nil {
		return nil
	}
	return t.m.sched.ScheduleTx(ctx, nil, args, at)
}

var (
	_ Repo = (*Memory)(nil)
	_ Tx   = (*memTx)(nil)
)

// NewMemory returns an empty repository.
func NewMemory() *Memory {
	return &Memory{leagues: map[string]*League{}, games: map[string]*Game{}, extras: map[string]*memExtra{}}
}

type memTx struct{ m *Memory }

// Tx runs fn under the lock. Changes are applied as fn saves them; on an
// error the earlier saves of the same transaction stay, which is fine for
// tests because every Service method saves once, at the end.
func (m *Memory) Tx(_ context.Context, fn func(tx Tx) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return fn(memTx{m})
}

func (m *Memory) Get(_ context.Context, leagueID string) (*League, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l, ok := m.leagues[leagueID]; ok {
		return clone(l), nil
	}
	return nil, nil
}

func (m *Memory) ForUser(_ context.Context, userID string) ([]*League, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*League
	for _, l := range m.leagues {
		if _, ok := l.TeamOf(userID); ok {
			out = append(out, clone(l))
		}
	}
	return out, nil
}

func (m *Memory) byCodeLocked(code string) *League {
	for _, l := range m.leagues {
		for _, inv := range l.Invites {
			if inv.Code == code {
				return l
			}
		}
	}
	return nil
}

func (m *Memory) ByInviteCode(_ context.Context, code string) (*League, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.byCodeLocked(code); l != nil {
		return clone(l), nil
	}
	return nil, nil
}

func cloneGame(g *Game) *Game {
	if g == nil {
		return &Game{}
	}
	c := &Game{
		Rosters:  slices.Clone(g.Rosters),
		Matchups: slices.Clone(g.Matchups),
		Scores:   slices.Clone(g.Scores),
	}
	for _, l := range g.Lineups {
		l.Slots = slices.Clone(l.Slots)
		c.Lineups = append(c.Lineups, l)
	}
	for i := range c.Scores {
		c.Scores[i].AutoSubs = slices.Clone(c.Scores[i].AutoSubs)
	}
	return c
}

func (m *Memory) Game(_ context.Context, leagueID string) (*Game, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneGame(m.games[leagueID]), nil
}

func (m *Memory) InSeasonLeagues(context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, l := range m.leagues {
		if l.Status == StatusInSeason {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (m *Memory) SaveScores(_ context.Context, leagueID string, gw int, scores []TeamScore, matchups []Matchup) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	g := m.games[leagueID]
	if g == nil {
		g = &Game{}
		m.games[leagueID] = g
	}
	var changed []string
	for _, s := range scores {
		s.Gameweek = gw
		s.AutoSubs = slices.Clone(s.AutoSubs)
		i := slices.IndexFunc(g.Scores, func(o TeamScore) bool { return o.TeamID == s.TeamID && o.Gameweek == gw })
		if i < 0 {
			s.Rev = 1
			g.Scores = append(g.Scores, s)
			changed = append(changed, s.TeamID)
			continue
		}
		old := g.Scores[i]
		if sameScore(old, s) {
			continue
		}
		s.Rev = old.Rev + 1
		g.Scores[i] = s
		changed = append(changed, s.TeamID)
	}
	for _, mu := range matchups {
		for i := range g.Matchups {
			if g.Matchups[i].ID == mu.ID {
				g.Matchups[i].HomePoints, g.Matchups[i].AwayPoints, g.Matchups[i].State = mu.HomePoints, mu.AwayPoints, mu.State
			}
		}
	}
	return changed, nil
}

func (m *Memory) LockScores(ctx context.Context, gw int, fn func() error) error {
	m.mu.Lock()
	if m.scoring == nil {
		m.scoring = map[int]chan struct{}{}
	}
	lock := m.scoring[gw]
	if lock == nil {
		lock = make(chan struct{}, 1)
		m.scoring[gw] = lock
	}
	m.mu.Unlock()
	select {
	case lock <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-lock }()
	return fn()
}

// sameScore compares the values a recompute produces (not Rev).
func sameScore(a, b TeamScore) bool {
	return a.Points == b.Points && a.BenchPoints == b.BenchPoints && a.State == b.State &&
		slices.Equal(a.AutoSubs, b.AutoSubs)
}

func (t memTx) Load(ctx context.Context, leagueID string) (*League, error) {
	if l, ok := t.m.leagues[leagueID]; ok {
		return clone(l), nil
	}
	return nil, nil
}

func (t memTx) LoadByInviteCode(_ context.Context, code string) (*League, error) {
	if l := t.m.byCodeLocked(code); l != nil {
		return clone(l), nil
	}
	return nil, nil
}

func (t memTx) LeagueIDsOfUser(_ context.Context, userID string) ([]string, error) {
	var out []string
	for id, l := range t.m.leagues {
		if _, ok := l.TeamOf(userID); ok {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (t memTx) CodeTaken(_ context.Context, code string) (bool, error) {
	return t.m.byCodeLocked(code) != nil, nil
}

func (t memTx) Insert(_ context.Context, l *League) error {
	t.m.leagues[l.ID] = clone(l)
	return nil
}

func (t memTx) Save(_ context.Context, l *League) error {
	t.m.leagues[l.ID] = clone(l)
	return nil
}

func (t memTx) Delete(_ context.Context, leagueID string) error {
	delete(t.m.leagues, leagueID)
	delete(t.m.games, leagueID)
	delete(t.m.extras, leagueID)
	return nil
}

func (t memTx) StartSeason(_ context.Context, leagueID string, g *Game) error {
	l, ok := t.m.leagues[leagueID]
	if !ok {
		return errLeagueNotFound
	}
	l.Status = StatusInSeason
	g = cloneGame(g)
	for i := range g.Rosters {
		t.m.nextSlot++
		g.Rosters[i].ID = t.m.nextSlot
	}
	t.m.games[leagueID] = g
	return nil
}
