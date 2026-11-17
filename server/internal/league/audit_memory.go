package league

import (
	"context"
	"slices"
	"time"
)

func (t memTx) AddAudit(_ context.Context, e *AuditEntry) error {
	t.m.nextSeq++
	e.Seq = t.m.nextSeq
	t.m.audit[e.LeagueID] = append(t.m.audit[e.LeagueID], cloneAudit(*e))
	return nil
}

func (m *Memory) Audit(_ context.Context, leagueID string, offset, limit int) ([]AuditEntry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.audit[leagueID]
	var out []AuditEntry
	for i := len(all) - 1 - offset; i >= 0 && len(out) < limit; i-- {
		out = append(out, cloneAudit(all[i]))
	}
	return out, nil
}

func (m *Memory) AuditSeenSeq(_ context.Context, leagueID, userID string) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.auditSeen[leagueID][userID], nil
}

func (m *Memory) AuditSummary(_ context.Context, leagueID, userID string, since time.Time) (AuditSummary, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var s AuditSummary
	seen := m.auditSeen[leagueID][userID]
	all := m.audit[leagueID]
	for i := len(all) - 1; i >= 0; i-- {
		e := all[i]
		if e.Seq <= seen {
			break
		}
		if e.ActorID == userID || e.At.Before(since) {
			continue
		}
		if s.Latest == nil {
			c := cloneAudit(e)
			s.Latest = &c
		}
		s.Unseen++
	}
	return s, nil
}

func (m *Memory) MarkAuditSeen(_ context.Context, leagueID, userID, throughID string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	all := m.audit[leagueID]
	var through int64
	if throughID == "" {
		if len(all) > 0 {
			through = all[len(all)-1].Seq
		}
	} else {
		i := slices.IndexFunc(all, func(e AuditEntry) bool { return e.ID == throughID })
		if i < 0 {
			return false, nil
		}
		through = all[i].Seq
	}
	if m.auditSeen[leagueID] == nil {
		m.auditSeen[leagueID] = map[string]int64{}
	}
	m.auditSeen[leagueID][userID] = max(m.auditSeen[leagueID][userID], through)
	return true, nil
}
