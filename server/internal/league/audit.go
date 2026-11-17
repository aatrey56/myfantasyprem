package league

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
)

// Commissioner move actions (CommissionerMove.action in the contract, an
// open enum). One entry is written whenever the commissioner uses a power a
// manager does not have: never for moves on their own team.
const (
	AuditSettingsChanged   = "settings_changed"
	AuditLeagueRenamed     = "league_renamed"
	AuditDraftUpdated      = "draft_updated"
	AuditDraftStarted      = "draft_started"
	AuditDraftPaused       = "draft_paused"
	AuditDraftResumed      = "draft_resumed"
	AuditDraftPick         = "draft_pick_for_team"
	AuditMemberRemoved     = "member_removed"
	AuditCommissionerMoved = "commissioner_transferred"
	AuditInviteCreated     = "invite_created"
	AuditInviteRevoked     = "invite_revoked"
	AuditSeatLinkIssued    = "seat_link_issued"
	AuditSignInLinkIssued  = "signin_link_issued"
	AuditTradeVetoed       = "trade_vetoed"
	AuditTradeApproved     = "trade_approved"
	AuditLeagueResynced    = "league_resynced"
)

// AuditEntry is one commissioner move (docs/adr/0017).
type AuditEntry struct {
	ID       string
	LeagueID string
	// Seq orders a league's entries; the store assigns it on AddAudit.
	Seq int64
	// ActorID is the commissioner's user id ("" once their account is gone).
	ActorID string
	Action  string
	// Summary is a sentence a manager can read, written when it happened.
	Summary string
	// TeamIDs are the teams it affected.
	TeamIDs []string
	// Before and After describe what changed, when that is a few values.
	Before, After map[string]any
	At            time.Time
}

// NewAudit returns an entry for l's commissioner actor.
func NewAudit(l *League, actor, action, summary string, teams []string, at time.Time) AuditEntry {
	return AuditEntry{ID: ids.New("cmv"), LeagueID: l.ID, ActorID: actor, Action: action, Summary: summary,
		TeamIDs: teams, At: at.UTC().Truncate(time.Microsecond)}
}

// Record writes the commissioner move into tx. Call it last in the action's
// transaction (see Tx.AddAudit). teams are the teams it affected, before and
// after the values it changed (both may be nil).
func Record(ctx context.Context, tx Tx, l *League, actor string, at time.Time, action, summary string,
	teams []string, before, after map[string]any,
) error {
	e := NewAudit(l, actor, action, summary, teams, at)
	e.Before, e.After = before, after
	return tx.AddAudit(ctx, &e)
}

// TeamLabel quotes a team's name for a summary.
func TeamLabel(l *League, teamID string) string {
	if t, ok := l.Team(teamID); ok {
		return "“" + t.Name + "”"
	}
	return "a team"
}

// AuditSummary is what a member has not seen yet: the entries after their
// seen-up-to marker that someone else made (nobody needs telling about their
// own moves).
type AuditSummary struct {
	Unseen int
	// Latest is the newest unseen entry, nil when none.
	Latest *AuditEntry
}

func cloneAudit(e AuditEntry) AuditEntry {
	e.TeamIDs = append([]string(nil), e.TeamIDs...)
	e.Before, e.After = cloneMap(e.Before), cloneMap(e.After)
	return e
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// TradeReviewLabel is the setting's value in words.
func TradeReviewLabel(mode string) string {
	switch mode {
	case ReviewNone:
		return "no review"
	case ReviewCommissioner:
		return "commissioner review"
	case ReviewLeagueVote:
		return "league vote"
	}
	return mode
}

// SettingsDiff describes what changed between two settings: a summary
// sentence and the values before and after. summary is "" when nothing a
// manager sees changed.
func SettingsDiff(before, after Settings) (summary string, from, to map[string]any) {
	from, to = map[string]any{}, map[string]any{}
	var parts []string
	note := func(key, text string, b, a any) {
		from[key], to[key] = b, a
		parts = append(parts, text)
	}
	if before.TeamCount != after.TeamCount {
		note("teamCount", fmt.Sprintf("team count to %d", after.TeamCount), before.TeamCount, after.TeamCount)
	}
	if before.TradeReview != after.TradeReview {
		note("tradeReview", "trade review to "+TradeReviewLabel(after.TradeReview), before.TradeReview, after.TradeReview)
	}
	if before.ReviewHours != after.ReviewHours {
		note("reviewHours", fmt.Sprintf("trade review period to %d hours", after.ReviewHours), before.ReviewHours, after.ReviewHours)
	}
	if before.VetoThreshold != after.VetoThreshold {
		note("vetoThreshold", fmt.Sprintf("veto threshold to %d%% of managers", int(after.VetoThreshold*100+0.5)),
			before.VetoThreshold, after.VetoThreshold)
	}
	if !sameInt(before.TradeDeadlineGW, after.TradeDeadlineGW) {
		text := "trade deadline removed"
		var b, a any
		if before.TradeDeadlineGW != nil {
			b = *before.TradeDeadlineGW
		}
		if after.TradeDeadlineGW != nil {
			a = *after.TradeDeadlineGW
			text = fmt.Sprintf("trade deadline to gameweek %d", *after.TradeDeadlineGW)
		}
		note("tradeDeadlineGameweek", text, b, a)
	}
	if before.PickTimerSeconds != after.PickTimerSeconds {
		note("pickTimerSeconds", fmt.Sprintf("pick timer to %d seconds", after.PickTimerSeconds),
			before.PickTimerSeconds, after.PickTimerSeconds)
	}
	if !sameTime(before.DraftStartsAt, after.DraftStartsAt) {
		text := "draft start time cleared"
		var b, a any
		if before.DraftStartsAt != nil {
			b = before.DraftStartsAt.UTC().Format(time.RFC3339)
		}
		if after.DraftStartsAt != nil {
			a = after.DraftStartsAt.UTC().Format(time.RFC3339)
			text = "draft start time to " + a.(string)
		}
		note("draftStartsAt", text, b, a)
	}
	if before.StartGameweek != after.StartGameweek {
		note("startGameweek", fmt.Sprintf("start gameweek to %d", after.StartGameweek), before.StartGameweek, after.StartGameweek)
	}
	if before.WaiverRule() != after.WaiverRule() {
		note("waiverOrder", "waiver order to "+strings.ReplaceAll(after.WaiverRule(), "_", " "), before.WaiverRule(), after.WaiverRule())
	}
	if len(parts) == 0 {
		return "", nil, nil
	}
	return "Changed the " + strings.Join(parts, ", ") + ".", from, to
}

func sameInt(a, b *int) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }

func sameTime(a, b *time.Time) bool {
	return (a == nil && b == nil) || (a != nil && b != nil && a.Equal(*b))
}

// AuditRepo is the read side of the commissioner log; Repo includes it. The
// write side, Tx.AddAudit, commits with the action it records.
type AuditRepo interface {
	// Audit returns a page of the league's entries, newest first.
	Audit(ctx context.Context, leagueID string, offset, limit int) ([]AuditEntry, error)
	// AuditSeenSeq is the highest Seq userID has seen in the league (0 when
	// they have seen none).
	AuditSeenSeq(ctx context.Context, leagueID, userID string) (int64, error)
	// AuditSummary counts what userID has not seen: entries after their
	// marker, made since they joined (since) by someone else.
	AuditSummary(ctx context.Context, leagueID, userID string, since time.Time) (AuditSummary, error)
	// MarkAuditSeen moves userID's seen-up-to marker to the entry throughID
	// (the league's newest when ""), never backwards. It returns false when
	// throughID is not an entry of the league.
	MarkAuditSeen(ctx context.Context, leagueID, userID, throughID string) (bool, error)
}
