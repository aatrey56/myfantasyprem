package mirror

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// Import statuses (ImportStatus in the contract).
const (
	ImportQueued    = "queued"
	ImportRunning   = "running"
	ImportSucceeded = "succeeded"
	ImportFailed    = "failed"
)

// Problem is a failed import's reason, as a problem+json body shows it.
type Problem struct {
	Slug   string `json:"slug"`
	Title  string `json:"title"`
	Detail string `json:"detail"`
}

// Import is one import job (POST /v1/imports). LeagueID is minted with the
// row, so a retried job creates exactly that league.
type Import struct {
	ID          string
	UserID      string
	FPLLeagueID int64
	Seat        int
	Status      string
	Done, Total int
	Error       *Problem
	LeagueID    string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Active reports whether the import is queued or running.
func (i Import) Active() bool { return i.Status == ImportQueued || i.Status == ImportRunning }

// Pending kinds: a re-sync or a check (SyncJobKind in the contract).
const (
	PendingResync = "resync"
	PendingCheck  = "check"
)

// SyncRecord is an imported league's FPL status: when FPL's state was last
// applied, the last re-sync's or check's outcome, and the one queued or
// running.
type SyncRecord struct {
	LeagueID string
	// LastAttemptAt is when the last re-sync or check ran; LastSyncedAt
	// when FPL's state was last applied (the import or a re-sync).
	LastAttemptAt *time.Time
	LastSyncedAt  *time.Time
	LastError     string
	// Pending is the re-sync or check queued or running since
	// RequestedAt ("" for none).
	Pending     string
	RequestedAt *time.Time
	// Note is the last re-sync's notice for the members (what it
	// cancelled).
	Note string
}

// Explanation is the commissioner's note on a failing report item. It
// holds only while the item shows the same values (Ours, Theirs).
type Explanation struct {
	Key         string    `json:"key"`
	Ours        string    `json:"ours"`
	Theirs      string    `json:"theirs"`
	Note        string    `json:"note"`
	ExplainedBy string    `json:"explainedBy"`
	ExplainedAt time.Time `json:"explainedAt"`
}

// ErrImportExists: the user already has an active or successful import of
// that FPL league (CreateImport returns it).
var ErrImportExists = errors.New("mirror: import exists")

// Store keeps the mirror's own records: imports, cached picks, the sync
// record, reports and explanations. Memory and Postgres implement it;
// one test suite runs on both.
type Store interface {
	// CreateImport stores a new import and, in the same transaction,
	// schedules its job with sched (nil: none). When the user already has
	// an active or successful import of that FPL league it returns that
	// one and ErrImportExists.
	CreateImport(ctx context.Context, imp Import, sched league.Scheduler, job league.JobArgs) (Import, error)
	Import(ctx context.Context, id string) (*Import, error)
	// LatestImport is the user's newest import of the FPL league, or nil.
	LatestImport(ctx context.Context, userID string, fplLeagueID int64) (*Import, error)
	SaveImport(ctx context.Context, imp Import) error

	// Picks returns cached picks of a finished gameweek.
	Picks(ctx context.Context, provider, entryID string, gw int) (*provider.EntryPicks, error)
	SavePicks(ctx context.Context, provider string, p *provider.EntryPicks) error

	Sync(ctx context.Context, leagueID string) (*SyncRecord, error)
	SaveSync(ctx context.Context, r SyncRecord) error
	// Request marks a re-sync or check pending at at, unless one is
	// already pending since staleBefore or later; it reports whether it
	// did. Atomic, so two requests cannot both win.
	Request(ctx context.Context, leagueID, kind string, at, staleBefore time.Time) (bool, error)

	SaveReport(ctx context.Context, leagueID string, r Report) error
	// LatestReport is the league's newest report, or nil.
	LatestReport(ctx context.Context, leagueID string) (*Report, error)

	Explanations(ctx context.Context, leagueID string) ([]Explanation, error)
	SaveExplanation(ctx context.Context, leagueID string, e Explanation) error

	// SaveShadow stores a shadow check's report, keeping the newest
	// keepShadows per league.
	SaveShadow(ctx context.Context, leagueID string, r ShadowReport) error
	// LatestShadow is the league's newest shadow check, or nil.
	LatestShadow(ctx context.Context, leagueID string) (*ShadowReport, error)
}

// Memory is the in-memory Store.
type Memory struct {
	mu      sync.Mutex
	imports map[string]Import
	picks   map[string]provider.EntryPicks
	syncs   map[string]SyncRecord
	reports map[string][]Report
	explain map[string]map[string]Explanation
	shadows map[string][]ShadowReport
}

var _ Store = (*Memory)(nil)

// NewMemoryStore returns an empty Memory store.
func NewMemoryStore() *Memory {
	return &Memory{
		imports: map[string]Import{}, picks: map[string]provider.EntryPicks{}, syncs: map[string]SyncRecord{},
		reports: map[string][]Report{}, explain: map[string]map[string]Explanation{},
		shadows: map[string][]ShadowReport{},
	}
}

func (m *Memory) CreateImport(ctx context.Context, imp Import, sched league.Scheduler, job league.JobArgs) (Import, error) {
	m.mu.Lock()
	for _, o := range m.imports {
		if o.UserID == imp.UserID && o.FPLLeagueID == imp.FPLLeagueID && (o.Active() || o.Status == ImportSucceeded) {
			m.mu.Unlock()
			return o, ErrImportExists
		}
	}
	m.imports[imp.ID] = imp
	m.mu.Unlock()
	if sched != nil {
		if err := sched.ScheduleTx(ctx, nil, job, imp.CreatedAt); err != nil {
			return Import{}, err
		}
	}
	return imp, nil
}

func (m *Memory) Import(_ context.Context, id string) (*Import, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if imp, ok := m.imports[id]; ok {
		return clonePtr(&imp), nil
	}
	return nil, nil
}

func (m *Memory) LatestImport(_ context.Context, userID string, fplLeagueID int64) (*Import, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out *Import
	for _, imp := range m.imports {
		if imp.UserID == userID && imp.FPLLeagueID == fplLeagueID && (out == nil || imp.CreatedAt.After(out.CreatedAt)) {
			out = clonePtr(&imp)
		}
	}
	return out, nil
}

func (m *Memory) SaveImport(_ context.Context, imp Import) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.imports[imp.ID] = imp
	return nil
}

func picksKey(provider, entryID string, gw int) string {
	return provider + "/" + entryID + "/" + strconv.Itoa(gw)
}

func (m *Memory) Picks(_ context.Context, provider, entryID string, gw int) (*provider.EntryPicks, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.picks[picksKey(provider, entryID, gw)]
	if !ok {
		return nil, nil
	}
	p.Picks, p.AutoSubs = slices.Clone(p.Picks), slices.Clone(p.AutoSubs)
	return &p, nil
}

func (m *Memory) SavePicks(_ context.Context, provider string, p *provider.EntryPicks) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *p
	c.Picks, c.AutoSubs = slices.Clone(p.Picks), slices.Clone(p.AutoSubs)
	m.picks[picksKey(provider, p.EntryID, p.Gameweek)] = c
	return nil
}

func (m *Memory) Sync(_ context.Context, leagueID string) (*SyncRecord, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.syncs[leagueID]
	if !ok {
		return nil, nil
	}
	return &r, nil
}

func (m *Memory) SaveSync(_ context.Context, r SyncRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.syncs[r.LeagueID] = r
	return nil
}

func (m *Memory) Request(_ context.Context, leagueID, kind string, at, staleBefore time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.syncs[leagueID]
	if !ok {
		r = SyncRecord{LeagueID: leagueID}
	}
	if r.Pending != "" && r.RequestedAt != nil && !r.RequestedAt.Before(staleBefore) {
		return false, nil
	}
	r.Pending, r.RequestedAt = kind, &at
	m.syncs[leagueID] = r
	return true, nil
}

// keepReports is how many reports a league keeps.
const keepReports = 50

func (m *Memory) SaveReport(_ context.Context, leagueID string, r Report) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.Items = slices.Clone(r.Items)
	rs := append(m.reports[leagueID], r)
	if len(rs) > keepReports {
		rs = rs[len(rs)-keepReports:]
	}
	m.reports[leagueID] = rs
	return nil
}

func (m *Memory) LatestReport(_ context.Context, leagueID string) (*Report, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rs := m.reports[leagueID]
	if len(rs) == 0 {
		return nil, nil
	}
	r := rs[len(rs)-1]
	r.Items = slices.Clone(r.Items)
	return &r, nil
}

func (m *Memory) Explanations(_ context.Context, leagueID string) ([]Explanation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []Explanation
	for _, e := range m.explain[leagueID] {
		out = append(out, e)
	}
	slices.SortFunc(out, func(a, b Explanation) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})
	return out, nil
}

func (m *Memory) SaveExplanation(_ context.Context, leagueID string, e Explanation) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.explain[leagueID] == nil {
		m.explain[leagueID] = map[string]Explanation{}
	}
	m.explain[leagueID][e.Key] = e
	return nil
}

// keepShadows is how many shadow checks a league keeps (a gameweek runs
// a few dozen).
const keepShadows = 200

func (m *Memory) SaveShadow(_ context.Context, leagueID string, r ShadowReport) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	r.Items = slices.Clone(r.Items)
	rs := append(m.shadows[leagueID], r)
	if len(rs) > keepShadows {
		rs = rs[len(rs)-keepShadows:]
	}
	m.shadows[leagueID] = rs
	return nil
}

func (m *Memory) LatestShadow(_ context.Context, leagueID string) (*ShadowReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rs := m.shadows[leagueID]
	if len(rs) == 0 {
		return nil, nil
	}
	r := rs[len(rs)-1]
	r.Items = slices.Clone(r.Items)
	return &r, nil
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}
