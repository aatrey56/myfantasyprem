package league

import (
	"context"
	"time"
)

// MaxAuditPage is the most entries one page returns.
const MaxAuditPage = 100

// AuditPage is a page of the commissioner log as one member sees it.
type AuditPage struct {
	Entries []AuditEntry
	// SeenSeq is the member's seen-up-to marker; Unseen says which entries
	// they have not seen.
	SeenSeq int64
	League  *League
	userID  string
}

// Unseen reports whether the member has not seen e: it is after their
// marker, someone else made it, and it came after they joined.
func (p AuditPage) Unseen(e AuditEntry) bool {
	t, ok := p.League.TeamOf(p.userID)
	return e.Seq > p.SeenSeq && e.ActorID != p.userID && (!ok || !e.At.Before(t.JoinedAt))
}

// CommissionerMoves returns a page of the league's commissioner log, newest
// first, to a member of the league.
func (m *Service) CommissionerMoves(ctx context.Context, actor, leagueID string, offset, limit int) (*AuditPage, error) {
	l, err := m.Get(ctx, actor, leagueID)
	if err != nil {
		return nil, err
	}
	limit = min(max(limit, 1), MaxAuditPage)
	entries, err := m.repo.Audit(ctx, leagueID, offset, limit+1)
	if err != nil {
		return nil, err
	}
	seen, err := m.repo.AuditSeenSeq(ctx, leagueID, actor)
	if err != nil {
		return nil, err
	}
	return &AuditPage{Entries: entries, SeenSeq: seen, League: l, userID: actor}, nil
}

// CommissionerMovesSummary is what the member has not seen.
func (m *Service) CommissionerMovesSummary(ctx context.Context, actor, leagueID string) (AuditSummary, *League, error) {
	l, err := m.Get(ctx, actor, leagueID)
	if err != nil {
		return AuditSummary{}, nil, err
	}
	s, err := m.repo.AuditSummary(ctx, leagueID, actor, joinedAt(l, actor))
	return s, l, err
}

func joinedAt(l *League, userID string) (at time.Time) {
	if t, ok := l.TeamOf(userID); ok {
		return t.JoinedAt
	}
	return
}

// MarkCommissionerMovesSeen moves the member's marker to throughId (the
// newest move when ""), then returns what is left unseen. 404 when
// throughId is not a move of the league.
func (m *Service) MarkCommissionerMovesSeen(ctx context.Context, actor, leagueID, throughID string) (AuditSummary, *League, error) {
	l, err := m.Get(ctx, actor, leagueID)
	if err != nil {
		return AuditSummary{}, nil, err
	}
	ok, err := m.repo.MarkAuditSeen(ctx, leagueID, actor, throughID)
	if err != nil {
		return AuditSummary{}, nil, err
	}
	if !ok {
		return AuditSummary{}, nil, errorf(KindNotFound, "not-found", "Not found", "Commissioner move not found.")
	}
	s, err := m.repo.AuditSummary(ctx, leagueID, actor, joinedAt(l, actor))
	return s, l, err
}
