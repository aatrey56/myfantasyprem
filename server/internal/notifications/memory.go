package notifications

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
)

// Memory is the Store in memory (tests and the database-less dev server).
type Memory struct {
	mu    sync.Mutex
	now   func() time.Time
	off   map[[2]string]map[Type]bool // user, league -> types off
	items []Notification              // oldest first
	keys  map[[2]string]bool          // user, key
	subs  []Subscription
	out   []memDelivery
	seq   int64
}

type memDelivery struct {
	id    int64
	ntf   string
	sub   string
	tries int
	next  time.Time
}

var _ Store = (*Memory)(nil)

// NewMemory returns an empty Memory reading the clock through now.
func NewMemory(now func() time.Time) *Memory {
	return &Memory{now: now, off: map[[2]string]map[Type]bool{}, keys: map[[2]string]bool{}}
}

func (m *Memory) Disabled(_ context.Context, userID, leagueID string) (map[Type]bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := map[Type]bool{}
	for t, off := range m.off[[2]string{userID, leagueID}] {
		if off {
			out[t] = true
		}
	}
	return out, nil
}

func (m *Memory) SetPreferences(_ context.Context, userID, leagueID string, all *bool, settings map[Type]bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [2]string{userID, leagueID}
	if m.off[k] == nil || (all != nil && *all) {
		m.off[k] = map[Type]bool{}
	}
	if all != nil && !*all {
		for _, i := range Catalog {
			m.off[k][i.Type] = true
		}
	}
	for t, on := range settings {
		m.off[k][t] = !on
	}
	return nil
}

func (m *Memory) Add(_ context.Context, n Notification, push bool) (Notification, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := [2]string{n.UserID, n.Key}
	if m.keys[k] {
		return Notification{}, false, nil
	}
	m.keys[k] = true
	n.ID = ids.New("ntf")
	n.CreatedAt = m.now().UTC().Truncate(time.Microsecond)
	n.ReadAt = nil
	m.items = append(m.items, n)
	if push {
		for _, s := range m.subs {
			if s.UserID == n.UserID {
				m.seq++
				m.out = append(m.out, memDelivery{id: m.seq, ntf: n.ID, sub: s.ID, next: n.CreatedAt})
			}
		}
	}
	return n, true, nil
}

func (m *Memory) List(_ context.Context, userID string, limit int, unreadOnly bool) ([]Notification, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := []Notification{}
	unread := 0
	for i := len(m.items) - 1; i >= 0; i-- {
		n := m.items[i]
		if n.UserID != userID {
			continue
		}
		if n.ReadAt == nil {
			unread++
		}
		if len(out) < limit && (!unreadOnly || n.ReadAt == nil) {
			out = append(out, n)
		}
	}
	return out, unread, nil
}

func (m *Memory) MarkRead(_ context.Context, userID string, idList []string, all bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now().UTC().Truncate(time.Microsecond)
	for i := range m.items {
		n := &m.items[i]
		if n.UserID == userID && n.ReadAt == nil && (all || slices.Contains(idList, n.ID)) {
			n.ReadAt = &now
		}
	}
	return nil
}

func (m *Memory) Subscribe(_ context.Context, sub Subscription) (Subscription, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, s := range m.subs {
		if s.Kind == sub.Kind && s.Endpoint == sub.Endpoint {
			if s.UserID == sub.UserID {
				return s, nil
			}
			// The device changed hands: it now belongs to the new user.
			m.subs = slices.Delete(m.subs, i, i+1)
			m.dropLocked(s.ID)
			break
		}
	}
	sub.ID = ids.New("psub")
	sub.CreatedAt = m.now().UTC().Truncate(time.Microsecond)
	m.subs = append(m.subs, sub)
	return sub, nil
}

func (m *Memory) Unsubscribe(_ context.Context, userID, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs = slices.DeleteFunc(m.subs, func(s Subscription) bool { return s.ID == id && s.UserID == userID })
	m.dropDeliveriesLocked()
	return nil
}

func (m *Memory) Drop(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dropLocked(id)
	return nil
}

func (m *Memory) dropLocked(id string) {
	m.subs = slices.DeleteFunc(m.subs, func(s Subscription) bool { return s.ID == id })
	m.dropDeliveriesLocked()
}

// dropDeliveriesLocked removes deliveries whose subscription is gone.
func (m *Memory) dropDeliveriesLocked() {
	m.out = slices.DeleteFunc(m.out, func(d memDelivery) bool {
		return !slices.ContainsFunc(m.subs, func(s Subscription) bool { return s.ID == d.sub })
	})
}

func (m *Memory) Due(_ context.Context, now time.Time, lease time.Duration, limit int) ([]Delivery, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	slices.SortStableFunc(m.out, func(a, b memDelivery) int { return a.next.Compare(b.next) })
	out := []Delivery{}
	for i := range m.out {
		d := &m.out[i]
		if len(out) == limit || d.next.After(now) {
			break
		}
		d.next = now.Add(lease)
		ni := slices.IndexFunc(m.items, func(n Notification) bool { return n.ID == d.ntf })
		si := slices.IndexFunc(m.subs, func(s Subscription) bool { return s.ID == d.sub })
		out = append(out, Delivery{ID: d.id, Notification: m.items[ni], Subscription: m.subs[si], Attempts: d.tries})
	}
	return out, nil
}

func (m *Memory) Delivered(_ context.Context, id int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.out = slices.DeleteFunc(m.out, func(d memDelivery) bool { return d.id == id })
	return nil
}

func (m *Memory) Retry(_ context.Context, id int64, next time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if next.IsZero() {
		m.out = slices.DeleteFunc(m.out, func(d memDelivery) bool { return d.id == id })
		return nil
	}
	for i := range m.out {
		if m.out[i].id == id {
			m.out[i].tries++
			m.out[i].next = next
		}
	}
	return nil
}
