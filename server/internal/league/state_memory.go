package league

import (
	"cmp"
	"context"
	"errors"
	"maps"
	"slices"
	"time"
)

// memExtra is the part of a league's State that Memory keeps besides the
// League and the Game: every claim and trade (not only open ones) and the
// whole feed, so the reads have something to read.
type memExtra struct {
	draft    *Draft
	priority []string
	claims   []Claim
	trades   []Trade
	locks    []WaiverLock
	block    []BlockEntry
	runs     map[RunKey]time.Time
	txns     []Txn
	refs     map[RefKey]string
}

func (m *Memory) extra(leagueID string) *memExtra {
	x := m.extras[leagueID]
	if x == nil {
		x = &memExtra{runs: map[RunKey]time.Time{}}
		m.extras[leagueID] = x
	}
	return x
}

// stateLocked assembles a league's State; the caller holds m.mu.
func (m *Memory) stateLocked(leagueID string) *State {
	l, ok := m.leagues[leagueID]
	if !ok {
		return nil
	}
	x := m.extra(leagueID)
	s := &State{League: l, Game: m.games[leagueID], Draft: x.draft, Priority: x.priority, Locks: x.locks, Block: x.block, Runs: x.runs, Refs: x.refs}
	if s.Game == nil {
		s.Game = &Game{}
	}
	if s.Runs == nil {
		s.Runs = map[RunKey]time.Time{}
	}
	if s.Refs == nil {
		s.Refs = map[RefKey]string{}
	}
	for _, c := range x.claims {
		if c.Status == ClaimPending {
			s.Claims = append(s.Claims, c)
		}
	}
	slices.SortStableFunc(s.Claims, func(a, b Claim) int {
		return cmp.Or(cmp.Compare(a.TeamID, b.TeamID), cmp.Compare(a.Order, b.Order))
	})
	for _, t := range x.trades {
		if TradeOpen(t.Status) {
			s.Trades = append(s.Trades, t)
		}
	}
	c := s.clone()
	c.snapshot()
	return c
}

func (m *Memory) State(_ context.Context, leagueID string) (*State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stateLocked(leagueID), nil
}

func (m *Memory) LeaguesWithStatus(_ context.Context, status string) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []string
	for id, l := range m.leagues {
		if l.Status == status {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out, nil
}

func (m *Memory) Claims(_ context.Context, leagueID, teamID string) ([]Claim, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Claim
	for _, c := range m.extra(leagueID).claims {
		if c.TeamID == teamID {
			c.ProcessedAt = clonePtr(c.ProcessedAt)
			out = append(out, c)
		}
	}
	slices.SortStableFunc(out, func(a, b Claim) int { return cmp.Compare(a.Order, b.Order) })
	return out, nil
}

func (m *Memory) Trades(_ context.Context, leagueID string) ([]Trade, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Trade
	for _, t := range m.extra(leagueID).trades {
		out = append(out, t.clone())
	}
	slices.SortStableFunc(out, func(a, b Trade) int {
		return cmp.Or(b.CreatedAt.Compare(a.CreatedAt), cmp.Compare(b.ID, a.ID))
	})
	return out, nil
}

func (m *Memory) Transactions(_ context.Context, leagueID string) ([]Txn, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := slices.Clone(m.extra(leagueID).txns)
	slices.SortStableFunc(out, func(a, b Txn) int {
		return cmp.Or(b.OccurredAt.Compare(a.OccurredAt), cmp.Compare(b.ID, a.ID))
	})
	return out, nil
}

func (t memTx) LoadState(_ context.Context, leagueID string) (*State, error) {
	return t.m.stateLocked(leagueID), nil
}

func (t memTx) SaveState(_ context.Context, s *State) error {
	if s.orig == nil {
		return errors.New("league: SaveState without LoadState")
	}
	m := t.m
	s.pruneBlock()
	c := s.clone()
	m.leagues[c.League.ID] = c.League
	for i := range s.Game.Rosters {
		if s.Game.Rosters[i].ID == 0 {
			m.nextSlot++
			s.Game.Rosters[i].ID = m.nextSlot
			c.Game.Rosters[i].ID = m.nextSlot
		}
	}
	c.Game.Matchups = append(c.Game.Matchups, s.NewMatchups[len(s.orig.NewMatchups):]...)
	s.Game.Matchups = append(s.Game.Matchups, s.NewMatchups[len(s.orig.NewMatchups):]...)
	if old := m.games[c.League.ID]; old != nil {
		c.Game.Scores = old.Scores
	}
	m.games[c.League.ID] = c.Game

	x := m.extra(c.League.ID)
	if s.DropLocalMoves && !s.orig.DropLocalMoves {
		named := map[string]bool{}
		for k, id := range c.Refs {
			if k.Entity == RefTransaction || k.Entity == RefTrade {
				named[id] = true
			}
		}
		x.claims = slices.DeleteFunc(x.claims, func(cl Claim) bool { return cl.Status != ClaimPending })
		x.trades = slices.DeleteFunc(x.trades, func(tr Trade) bool { return !TradeOpen(tr.Status) && !named[tr.ID] })
		x.txns = slices.DeleteFunc(x.txns, func(tx Txn) bool { return !named[tx.ID] && (tx.TradeID == "" || !named[tx.TradeID]) })
	}
	x.draft, x.priority, x.locks, x.runs, x.refs = c.Draft, c.Priority, c.Locks, maps.Clone(c.Runs), maps.Clone(c.Refs)
	x.block = c.Block
	for _, cl := range c.Claims {
		if i := slices.IndexFunc(x.claims, func(o Claim) bool { return o.ID == cl.ID }); i >= 0 {
			x.claims[i] = cl
		} else {
			x.claims = append(x.claims, cl)
		}
	}
	for _, tr := range c.Trades {
		if i := slices.IndexFunc(x.trades, func(o Trade) bool { return o.ID == tr.ID }); i >= 0 {
			x.trades[i] = tr
		} else {
			x.trades = append(x.trades, tr)
		}
	}
	x.txns = append(x.txns, c.NewTxns[len(s.orig.NewTxns):]...)
	s.snapshot()
	return nil
}
