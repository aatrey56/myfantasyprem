package league

import (
	"context"
	"crypto/rand"
	"errors"
	"math/big"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Season is the current season label.
const Season = "2026-27"

// Status values (LeagueStatus in the contract).
const (
	StatusPreDraft = "pre_draft"
	StatusDrafting = "drafting"
	StatusInSeason = "in_season"
	StatusComplete = "complete"
)

// League sources (LeagueSource in the contract; docs/adr/0012).
const (
	SourceNative = "native"
	SourceFPL    = "fpl_import"
)

// Trade review modes (TradeReview in the contract).
const (
	ReviewNone         = "none"
	ReviewCommissioner = "commissioner"
	ReviewLeagueVote   = "league_vote"
)

// Kind classifies an Error. httpapi maps it to a status code.
type Kind int

const (
	KindInvalid   Kind = iota + 1 // 422
	KindForbidden                 // 403
	KindNotFound                  // 404
	KindConflict                  // 409
	KindGone                      // 410
)

// Error is a domain error with a stable slug (the problem type) and a
// message that is safe to show.
type Error struct {
	Kind   Kind
	Slug   string
	Title  string
	Detail string
	// LeagueID is set on `already-member`, so the caller can open the league.
	LeagueID string
	// ReopensAt is set on `transactions-locked`: when free-agent signings
	// reopen (the next waiver run), if known.
	ReopensAt *time.Time
	// Errors lists every broken rule of a 422 (ProblemError in the contract).
	Errors []FieldError
}

// FieldError is one broken rule.
type FieldError struct {
	Code     string
	Message  string
	Field    string // JSON pointer into the request body, or ""
	PlayerID string
}

func (e *Error) Error() string { return e.Slug + ": " + e.Detail }

func errorf(kind Kind, slug, title, detail string) *Error {
	return &Error{Kind: kind, Slug: slug, Title: title, Detail: detail}
}

// Errorf returns a domain error; the draft, lineup, waiver and trade rules
// use it so httpapi maps every rule failure the same way.
func Errorf(kind Kind, slug, title, detail string, errs ...FieldError) *Error {
	e := errorf(kind, slug, title, detail)
	e.Errors = errs
	return e
}

// Shared errors of the rule packages.
var (
	ErrLeagueNotFound = errLeagueNotFound
	ErrNotMember      = errNotMember
	ErrNotCommish     = errNotCommish
)

// Member checks that actor is in the league (owns a team or is the
// commissioner).
func Member(l *League, actor string) error { return member(l, actor) }

// Commissioner checks that actor is the league's commissioner.
func Commissioner(l *League, actor string) error { return commissioner(l, actor) }

var (
	errLeagueNotFound = errorf(KindNotFound, "not-found", "Not found", "League not found.")
	errNotMember      = errorf(KindForbidden, "not-a-member", "Not a member", "You are not in this league.")
	errNotCommish     = errorf(KindForbidden, "not-commissioner", "Commissioner only", "Only the commissioner can do that.")
	errInviteNotFound = errorf(KindNotFound, "not-found", "Not found", "Invite not found.")
)

// Settings are a league's rules. Scoring (FPL standard), schedule (H2H),
// roster (2/5/5/3) and lineup rules are fixed in v1 and not stored.
type Settings struct {
	TeamCount        int        `json:"teamCount"`
	PickTimerSeconds int        `json:"pickTimerSeconds"`
	DraftStartsAt    *time.Time `json:"draftStartsAt,omitempty"`
	TradeReview      string     `json:"tradeReview"`
	ReviewHours      int        `json:"reviewHours"`
	VetoThreshold    float64    `json:"vetoThreshold"`
	TradeDeadlineGW  *int       `json:"tradeDeadlineGw,omitempty"`
	StartGameweek    int        `json:"startGameweek"`
	EndGameweek      int        `json:"endGameweek"`
	// WaiverOrder is WaiverReverseStandings or WaiverRolling; empty (a
	// league stored before the setting) means rolling, see WaiverRule.
	WaiverOrder string `json:"waiverOrder,omitempty"`
}

// Team count limits (ADR 0008 rule 15).
const (
	MinTeams = 2
	MaxTeams = 20
)

// DefaultSettings for a new league.
func DefaultSettings(teamCount int) Settings {
	return Settings{
		TeamCount:        teamCount,
		PickTimerSeconds: 60,
		TradeReview:      ReviewNone,
		ReviewHours:      0,
		VetoThreshold:    0.5,
		StartGameweek:    1,
		EndGameweek:      38,
		WaiverOrder:      WaiverReverseStandings,
	}
}

// SettingsInput is a partial update; nil means unchanged. ClearX sets the
// nullable field to null.
type SettingsInput struct {
	TeamCount          *int
	PickTimerSeconds   *int
	DraftStartsAt      *time.Time
	ClearDraftStartsAt bool
	TradeReview        *string
	ReviewHours        *int
	VetoThreshold      *float64
	TradeDeadlineGW    *int
	ClearTradeDeadline bool
	StartGameweek      *int
	// WaiverOrder changes the waiver order rule from the next waiver run.
	WaiverOrder *string
}

// Team is a team in a league. OwnerID is "" for an orphaned team (its
// manager left after the draft or deleted their account) and for an
// imported seat nobody has claimed yet.
type Team struct {
	ID       string
	Name     string
	OwnerID  string
	JoinedAt time.Time
	// FPLEntryID and ManagerName are an imported seat's FPL entry and
	// manager (docs/adr/0012); zero for a team made here.
	FPLEntryID  int64
	ManagerName string
}

// Invite is an invite link. A claim invite (TeamID set) gives its user
// that team, which has no manager, instead of a new one.
type Invite struct {
	ID        string
	Code      string
	CreatedAt time.Time
	ExpiresAt *time.Time
	MaxUses   *int
	Uses      int
	Revoked   bool
	TeamID    string
}

// League is a snapshot of one league: the aggregate the Service loads,
// changes and saves as a whole.
type League struct {
	ID             string
	Name           string
	Season         string
	Status         string
	Settings       Settings
	CommissionerID string
	CreatedAt      time.Time
	Teams          []Team // in join order; the commissioner's first
	Invites        []Invite

	// Source is SourceNative or SourceFPL. An imported league also has
	// its FPL league id; it plays like a native one (docs/adr/0012).
	Source      string
	FPLLeagueID int64
}

// TeamOf returns the team owned by userID.
func (l *League) TeamOf(userID string) (Team, bool) {
	for _, t := range l.Teams {
		if t.OwnerID == userID && userID != "" {
			return t, true
		}
	}
	return Team{}, false
}

// Team returns the team with id.
func (l *League) Team(id string) (Team, bool) {
	for _, t := range l.Teams {
		if t.ID == id {
			return t, true
		}
	}
	return Team{}, false
}

// HasPlace reports whether an ordinary invite can still give someone a
// team: before the draft a place without a team; once it has started an
// open seat (a team without a manager), which the joiner takes over.
func (l *League) HasPlace() bool {
	if l.Status == StatusPreDraft {
		return len(l.Teams) < l.Settings.TeamCount
	}
	return slices.ContainsFunc(l.Teams, func(t Team) bool { return t.OwnerID == "" })
}

// MemberCount is the number of teams with an owner.
func (l *League) MemberCount() int {
	n := 0
	for _, t := range l.Teams {
		if t.OwnerID != "" {
			n++
		}
	}
	return n
}

func clone(l *League) *League {
	c := *l
	c.Teams = slices.Clone(l.Teams)
	c.Invites = slices.Clone(l.Invites)
	return &c
}

// Service is league management: the rules, over a Repo. It is safe for
// concurrent use when the Repo is.
type Service struct {
	repo  Repo
	now   func() time.Time
	stats stats.Repo
}

// NewService returns a Service on repo using clock now.
func NewService(repo Repo, now func() time.Time) *Service {
	// Postgres keeps microseconds; truncating here makes both repos agree.
	return &Service{repo: repo, now: func() time.Time { return now().UTC().Truncate(time.Microsecond) }}
}

// WithStats gives the service the season calendar, which changing an
// in-season league's waiver order needs. It returns m.
func (m *Service) WithStats(r stats.Repo) *Service {
	m.stats = r
	return m
}

// Repo returns the underlying repository (game state reads and writes).
func (m *Service) Repo() Repo { return m.repo }

// mutate loads a league for update, runs fn on it and saves it.
func (m *Service) mutate(ctx context.Context, leagueID string, fn func(tx Tx, l *League) error) (*League, error) {
	var out *League
	err := m.repo.Tx(ctx, func(tx Tx) error {
		l, err := tx.Load(ctx, leagueID)
		if err != nil {
			return err
		}
		if l == nil {
			return errLeagueNotFound
		}
		if err := fn(tx, l); err != nil {
			return err
		}
		out = clone(l)
		return tx.Save(ctx, l)
	})
	return out, err
}

func member(l *League, actor string) error {
	if _, ok := l.TeamOf(actor); !ok && l.CommissionerID != actor {
		return errNotMember
	}
	return nil
}

func commissioner(l *League, actor string) error {
	if err := member(l, actor); err != nil {
		return err
	}
	if l.CommissionerID != actor {
		return errNotCommish
	}
	return nil
}

func validName(name string, minLen, maxLen int, field string) error {
	n := len([]rune(strings.TrimSpace(name)))
	if n < minLen || n > maxLen {
		return errorf(KindInvalid, "invalid-request", "Invalid request",
			field+" must be "+strconv.Itoa(minLen)+" to "+strconv.Itoa(maxLen)+" characters.")
	}
	return nil
}

// Settings bounds; the contract's LeagueSettingsInput states the same
// minimum and maximum.
const (
	MinPickTimerSeconds = 15
	MaxPickTimerSeconds = 86400 // a day
	MaxReviewHours      = 72
)

// settingsRangeErrors lists every value of in outside its bounds.
func settingsRangeErrors(in SettingsInput) []FieldError {
	var errs []FieldError
	add := func(field, msg string) {
		errs = append(errs, FieldError{Code: "out_of_range", Message: msg, Field: field})
	}
	if v := in.PickTimerSeconds; v != nil && (*v < MinPickTimerSeconds || *v > MaxPickTimerSeconds) {
		add("/settings/draft/pickTimerSeconds", "The pick timer must be 15 to 86400 seconds.")
	}
	if v := in.ReviewHours; v != nil && (*v < 0 || *v > MaxReviewHours) {
		add("/settings/trades/reviewHours", "The trade review period must be 0 to 72 hours.")
	}
	if v := in.VetoThreshold; v != nil && !(*v >= 0 && *v <= 1) { // also NaN
		add("/settings/trades/vetoThreshold", "The veto threshold must be between 0 and 1.")
	}
	if v := in.TradeDeadlineGW; v != nil && (*v < 1 || *v > 38) {
		add("/settings/trades/deadlineGameweek", "The trade deadline must be gameweek 1 to 38.")
	}
	if v := in.StartGameweek; v != nil && (*v < 1 || *v > 38) {
		add("/settings/startGameweek", "The start gameweek must be 1 to 38.")
	}
	if v := in.WaiverOrder; v != nil && *v != WaiverReverseStandings && *v != WaiverRolling {
		add("/settings/waivers/order", "The waiver order must be reverse_standings or rolling.")
	}
	return errs
}

// applySettings validates and applies in to s. draftStarted locks the
// draft-time settings.
func applySettings(s *Settings, in SettingsInput, draftStarted bool, teams int) error {
	if errs := settingsRangeErrors(in); len(errs) > 0 {
		return Errorf(KindInvalid, "invalid-request", "Invalid request", "Some settings are out of range.", errs...)
	}
	lockedErr := func(name string) error {
		return errorf(KindConflict, "setting-locked", "Setting locked",
			name+" cannot change once the draft has started.")
	}
	if in.TeamCount != nil {
		if draftStarted {
			return lockedErr("teamCount")
		}
		if *in.TeamCount < MinTeams || *in.TeamCount > MaxTeams {
			return errorf(KindInvalid, "invalid-request", "Invalid request", "teamCount must be 2 to 20.")
		}
		if *in.TeamCount < teams {
			return errorf(KindConflict, "too-many-teams", "Too many teams",
				"The league already has more teams than that.")
		}
		s.TeamCount = *in.TeamCount
	}
	if in.TradeReview != nil {
		if draftStarted {
			return lockedErr("trades.review")
		}
		switch *in.TradeReview {
		case ReviewNone, ReviewCommissioner, ReviewLeagueVote:
			s.TradeReview = *in.TradeReview
		default:
			return errorf(KindInvalid, "invalid-request", "Invalid request", "Unknown trades.review.")
		}
	}
	if in.PickTimerSeconds != nil {
		if draftStarted {
			return lockedErr("draft.pickTimerSeconds")
		}
		s.PickTimerSeconds = *in.PickTimerSeconds
	}
	if in.ClearDraftStartsAt || in.DraftStartsAt != nil {
		if draftStarted {
			return lockedErr("draft.startsAt")
		}
	}
	if in.ClearDraftStartsAt {
		s.DraftStartsAt = nil
	} else if in.DraftStartsAt != nil {
		t := *in.DraftStartsAt
		s.DraftStartsAt = &t
	}
	if in.ReviewHours != nil {
		s.ReviewHours = *in.ReviewHours
	}
	if in.VetoThreshold != nil {
		s.VetoThreshold = *in.VetoThreshold
	}
	if in.ClearTradeDeadline {
		s.TradeDeadlineGW = nil
	} else if in.TradeDeadlineGW != nil {
		g := *in.TradeDeadlineGW
		s.TradeDeadlineGW = &g
	}
	if in.StartGameweek != nil {
		if draftStarted {
			return lockedErr("startGameweek")
		}
		s.StartGameweek = *in.StartGameweek
	}
	if in.WaiverOrder != nil {
		s.WaiverOrder = *in.WaiverOrder
	}
	if s.TradeReview == ReviewNone {
		s.ReviewHours = 0
	} else if s.ReviewHours == 0 {
		s.ReviewHours = 24
	}
	return nil
}

// Create makes a league with actor as commissioner and owner of its first team.
func (m *Service) Create(ctx context.Context, actor, name string, teamCount int, teamName string, in SettingsInput) (*League, error) {
	if err := validName(name, 3, 40, "name"); err != nil {
		return nil, err
	}
	if err := validName(teamName, 1, 32, "teamName"); err != nil {
		return nil, err
	}
	in.TeamCount = &teamCount
	s := DefaultSettings(teamCount)
	if err := applySettings(&s, in, false, 1); err != nil {
		return nil, err
	}
	now := m.now()
	l := &League{
		ID: ids.New("lg"), Name: strings.TrimSpace(name), Season: Season, Status: StatusPreDraft,
		Settings: s, CommissionerID: actor, CreatedAt: now, Source: SourceNative,
		Teams: []Team{{ID: ids.New("tm"), Name: strings.TrimSpace(teamName), OwnerID: actor, JoinedAt: now}},
	}
	if err := m.repo.Tx(ctx, func(tx Tx) error { return tx.Insert(ctx, l) }); err != nil {
		return nil, err
	}
	return clone(l), nil
}

// Get returns a league the actor is in.
func (m *Service) Get(ctx context.Context, actor, leagueID string) (*League, error) {
	l, err := m.repo.Get(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, errLeagueNotFound
	}
	if err := member(l, actor); err != nil {
		return nil, err
	}
	return l, nil
}

// ForUser returns the actor's leagues, newest first.
func (m *Service) ForUser(ctx context.Context, actor string) ([]*League, error) {
	out, err := m.repo.ForUser(ctx, actor)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(out, func(a, b *League) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return strings.Compare(b.ID, a.ID)
	})
	return out, nil
}

// Update renames the league and/or changes settings (commissioner only).
// A waiver order change needs the game state and the season calendar
// (setWaiverOrder), so it loads the whole State.
func (m *Service) Update(ctx context.Context, actor, leagueID string, name *string, in SettingsInput) (*League, error) {
	update := func(l *League) error {
		if err := commissioner(l, actor); err != nil {
			return err
		}
		if name != nil {
			if err := validName(*name, 3, 40, "name"); err != nil {
				return err
			}
		}
		s := l.Settings
		if err := applySettings(&s, in, l.Status != StatusPreDraft, len(l.Teams)); err != nil {
			return err
		}
		if name != nil {
			l.Name = strings.TrimSpace(*name)
		}
		l.Settings = s
		return nil
	}
	if in.WaiverOrder == nil {
		return m.mutate(ctx, leagueID, func(_ Tx, l *League) error { return update(l) })
	}
	var out *League
	err := m.repo.Tx(ctx, func(tx Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		if st == nil {
			return errLeagueNotFound
		}
		prev := st.League.Settings.WaiverRule()
		if err := update(st.League); err != nil {
			return err
		}
		var w *World
		if st.League.Status == StatusInSeason {
			if m.stats == nil {
				return ErrNoSeasonData
			}
			if w, err = SeasonWorld(ctx, m.stats); err != nil {
				return err
			}
		}
		if err := switchWaiverOrder(st, w, m.now(), prev); err != nil {
			return err
		}
		out = clone(st.League)
		if st.League.Status == StatusInSeason {
			return tx.SaveState(ctx, st)
		}
		return tx.Save(ctx, st.League)
	})
	return out, err
}

// RenameTeam renames the actor's own team.
func (m *Service) RenameTeam(ctx context.Context, actor, leagueID, teamID, name string) (Team, error) {
	var out Team
	_, err := m.mutate(ctx, leagueID, func(_ Tx, l *League) error {
		if err := member(l, actor); err != nil {
			return err
		}
		if err := validName(name, 1, 32, "name"); err != nil {
			return err
		}
		for i := range l.Teams {
			if l.Teams[i].ID == teamID {
				if l.Teams[i].OwnerID != actor {
					return errorf(KindForbidden, "not-team-owner", "Not your team", "Only the team's manager can rename it.")
				}
				l.Teams[i].Name = strings.TrimSpace(name)
				out = l.Teams[i]
				return nil
			}
		}
		return errorf(KindNotFound, "not-found", "Not found", "Team not found.")
	})
	return out, err
}

// removeTeam deletes the user's team before the draft, or orphans it after.
func removeTeam(l *League, userID string) {
	for i := range l.Teams {
		if l.Teams[i].OwnerID != userID {
			continue
		}
		if l.Status == StatusPreDraft {
			l.Teams = slices.Delete(l.Teams, i, i+1)
		} else {
			l.Teams[i].OwnerID = ""
		}
		return
	}
}

// RemoveMember removes another member (commissioner only). It returns the
// changes to report (offers the member's departure closed).
func (m *Service) RemoveMember(ctx context.Context, actor, leagueID, userID string) ([]Change, error) {
	return m.leave(ctx, leagueID, userID, func(l *League) error {
		if err := commissioner(l, actor); err != nil {
			return err
		}
		if userID == actor {
			return errorf(KindConflict, "commissioner-cannot-leave", "Commissioner cannot leave",
				"Hand the commissioner role to someone else first.")
		}
		if _, ok := l.TeamOf(userID); !ok {
			return errorf(KindNotFound, "not-found", "Not found", "That user is not in this league.")
		}
		return nil
	})
}

// Leave removes the actor from the league. It returns the changes to
// report (offers the departure closed).
func (m *Service) Leave(ctx context.Context, actor, leagueID string) ([]Change, error) {
	return m.leave(ctx, leagueID, actor, func(l *League) error {
		if err := member(l, actor); err != nil {
			return err
		}
		if l.CommissionerID == actor {
			return errorf(KindConflict, "commissioner-cannot-leave", "Commissioner cannot leave",
				"Hand the commissioner role to someone else first.")
		}
		return nil
	})
}

// leave takes userID out of the league once check allows it, in one
// transaction under the league lock: the pending moves are withdrawn and
// the team deleted or orphaned together, so no claim or offer made
// meanwhile can survive on the orphaned team.
func (m *Service) leave(ctx context.Context, leagueID, userID string, check func(l *League) error) ([]Change, error) {
	var changes []Change
	err := m.repo.Tx(ctx, func(tx Tx) error {
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		if st == nil {
			return errLeagueNotFound
		}
		if err := check(st.League); err != nil {
			return err
		}
		changes = releaseTeam(st, userID, m.now())
		removeTeam(st.League, userID)
		return saveLeaving(ctx, tx, st)
	})
	return changes, err
}

// releaseTeam withdraws the pending moves of userID's team in an
// in-season league (docs/account-deletion.md): pending waiver claims are
// cancelled, offers it made withdrawn and offers it received declined.
// Accepted trades run their course: they involve another manager too.
func releaseTeam(st *State, userID string, now time.Time) []Change {
	team, ok := st.League.TeamOf(userID)
	if !ok || st.League.Status != StatusInSeason {
		return nil
	}
	for i := range st.Claims {
		if c := &st.Claims[i]; c.TeamID == team.ID && c.Status == ClaimPending {
			c.Status = ClaimCancelled
		}
	}
	var changes []Change
	for i := range st.Trades {
		t := &st.Trades[i]
		if t.Status != TradeOffered {
			continue
		}
		switch team.ID {
		case t.OfferingTeamID:
			t.Status = TradeWithdrawn
		case t.ReceivingTeamID:
			at := now
			t.Status, t.RespondedAt = TradeDeclined, &at
		default:
			continue
		}
		changes = append(changes, Change{LeagueID: st.League.ID, Kind: ChangeTrade, TradeID: t.ID})
	}
	return changes
}

// saveLeaving stores a league after a departure: the whole State in
// season (claims and trades may have changed), else the league.
func saveLeaving(ctx context.Context, tx Tx, st *State) error {
	if st.League.Status == StatusInSeason {
		return tx.SaveState(ctx, st)
	}
	return tx.Save(ctx, st.League)
}

// TransferCommissioner hands the commissioner role to another member
// (commissioner only). The target must own a team in the league; naming
// yourself changes nothing.
func (m *Service) TransferCommissioner(ctx context.Context, actor, leagueID, userID string) (*League, error) {
	return m.mutate(ctx, leagueID, func(_ Tx, l *League) error {
		if err := commissioner(l, actor); err != nil {
			return err
		}
		if _, ok := l.TeamOf(userID); !ok {
			return errorf(KindNotFound, "not-found", "Not found", "That user is not in this league.")
		}
		l.CommissionerID = userID
		return nil
	})
}

// RemoveUserEverywhere is account deletion: the user's pending moves are
// withdrawn and the user leaves every league, in one transaction; a sole
// commissioner's role passes to the longest-standing member. A pre-draft
// league left with no members is deleted. It returns the changes to report.
func (m *Service) RemoveUserEverywhere(ctx context.Context, userID string) ([]Change, error) {
	var changes []Change
	err := m.repo.Tx(ctx, func(tx Tx) error {
		var err error
		changes, err = RemoveUserEverywhereIn(ctx, tx, userID, m.now())
		return err
	})
	return changes, err
}

// RemoveUserEverywhereIn is RemoveUserEverywhere in the caller's
// transaction (account deletion deletes the user in the same one).
func RemoveUserEverywhereIn(ctx context.Context, tx Tx, userID string, now time.Time) ([]Change, error) {
	leagueIDs, err := tx.LeagueIDsOfUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	var changes []Change
	for _, id := range leagueIDs {
		st, err := tx.LoadState(ctx, id)
		if err != nil {
			return nil, err
		}
		if st == nil {
			continue
		}
		l := st.League
		changes = append(changes, releaseTeam(st, userID, now)...)
		removeTeam(l, userID)
		if l.CommissionerID == userID {
			l.CommissionerID = ""
			for _, t := range l.Teams {
				if t.OwnerID != "" {
					l.CommissionerID = t.OwnerID
					break
				}
			}
		}
		if l.MemberCount() == 0 && l.Status == StatusPreDraft {
			err = tx.Delete(ctx, l.ID)
		} else {
			err = saveLeaving(ctx, tx, st)
		}
		if err != nil {
			return nil, err
		}
	}
	return changes, nil
}

// codeAlphabet has no look-alikes: no 0, O, 1, I, L or U.
const codeAlphabet = "23456789ABCDEFGHJKMNPQRSTVWXYZ"

// CodeLength is the length of new invite codes: 30^10 (about 2^49)
// codes, with the rate limit on previews, makes guessing a live one
// hopeless. Codes made before were 8 characters; they keep working until
// they expire.
const CodeLength = 10

// InviteTTL is how long an invite works when the commissioner does not
// say.
const InviteTTL = 7 * 24 * time.Hour

func newCode() string {
	var b strings.Builder
	for range CodeLength {
		n, err := rand.Int(rand.Reader, big.NewInt(int64(len(codeAlphabet))))
		if err != nil {
			panic("league: crypto/rand: " + err.Error())
		}
		b.WriteByte(codeAlphabet[n.Int64()])
	}
	return b.String()
}

// CreateInvite makes an invite link (commissioner only).
func (m *Service) CreateInvite(ctx context.Context, actor, leagueID string, maxUses *int, expiresIn *time.Duration) (Invite, error) {
	var inv Invite
	_, err := m.mutate(ctx, leagueID, func(tx Tx, l *League) error {
		if err := commissioner(l, actor); err != nil {
			return err
		}
		if !l.HasPlace() {
			return errorf(KindConflict, "league-full", "League full", "Every team in this league has a manager.")
		}
		now := m.now()
		inv = Invite{ID: ids.New("inv"), CreatedAt: now, MaxUses: maxUses}
		for {
			inv.Code = newCode()
			taken, err := tx.CodeTaken(ctx, inv.Code)
			if err != nil {
				return err
			}
			if !taken {
				break
			}
		}
		ttl := InviteTTL
		if expiresIn != nil {
			ttl = *expiresIn
		}
		t := now.Add(ttl)
		inv.ExpiresAt = &t
		l.Invites = append(l.Invites, inv)
		return nil
	})
	return inv, err
}

// IssueClaimInvite revokes the team's claim invites and adds a new one:
// single use, no expiry (docs/adr/0012). The team must have no manager.
// tx checks the code is unused.
func IssueClaimInvite(ctx context.Context, tx Tx, l *League, teamID string, now time.Time) (Invite, error) {
	t, ok := l.Team(teamID)
	if !ok {
		return Invite{}, errorf(KindNotFound, "not-found", "Not found", "Team not found.")
	}
	if t.OwnerID != "" {
		return Invite{}, errorf(KindConflict, "seat-claimed", "Already claimed", t.Name+" already has a manager.")
	}
	for i := range l.Invites {
		if l.Invites[i].TeamID == teamID {
			l.Invites[i].Revoked = true
		}
	}
	one := 1
	inv := Invite{ID: ids.New("inv"), CreatedAt: now, MaxUses: &one, TeamID: teamID}
	for {
		inv.Code = newCode()
		taken, err := tx.CodeTaken(ctx, inv.Code)
		if err != nil {
			return Invite{}, err
		}
		if !taken && !slices.ContainsFunc(l.Invites, func(o Invite) bool { return o.Code == inv.Code }) {
			break
		}
	}
	l.Invites = append(l.Invites, inv)
	return inv, nil
}

// ClaimInvite issues a new claim link for an unclaimed team, revoking its
// previous one (commissioner only): "resend" in the app.
func (m *Service) ClaimInvite(ctx context.Context, actor, leagueID, teamID string) (Invite, error) {
	var inv Invite
	_, err := m.mutate(ctx, leagueID, func(tx Tx, l *League) error {
		if err := commissioner(l, actor); err != nil {
			return err
		}
		var err error
		inv, err = IssueClaimInvite(ctx, tx, l, teamID, m.now())
		return err
	})
	return inv, err
}

// ClaimLink returns the team's current claim invite: the newest one not
// revoked and not used.
func (l *League) ClaimLink(teamID string) (Invite, bool) {
	for i := len(l.Invites) - 1; i >= 0; i-- {
		inv := l.Invites[i]
		if inv.TeamID == teamID && !inv.Revoked && (inv.MaxUses == nil || inv.Uses < *inv.MaxUses) {
			return inv, true
		}
	}
	return Invite{}, false
}

// ListInvites returns the league's invites, newest first (commissioner only).
func (m *Service) ListInvites(ctx context.Context, actor, leagueID string) ([]Invite, error) {
	l, err := m.repo.Get(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, errLeagueNotFound
	}
	if err := commissioner(l, actor); err != nil {
		return nil, err
	}
	out := slices.Clone(l.Invites)
	slices.Reverse(out)
	return out, nil
}

// RevokeInvite (commissioner only).
func (m *Service) RevokeInvite(ctx context.Context, actor, leagueID, inviteID string) error {
	_, err := m.mutate(ctx, leagueID, func(_ Tx, l *League) error {
		if err := commissioner(l, actor); err != nil {
			return err
		}
		for i := range l.Invites {
			if l.Invites[i].ID == inviteID {
				l.Invites[i].Revoked = true
				return nil
			}
		}
		return errInviteNotFound
	})
	return err
}

// usableInvite finds the invite with code in l and checks it still works.
func (m *Service) usableInvite(l *League, code string) (*Invite, error) {
	if l == nil {
		return nil, errInviteNotFound
	}
	for i := range l.Invites {
		inv := &l.Invites[i]
		if inv.Code != code {
			continue
		}
		claimed := false
		if inv.TeamID != "" {
			t, ok := l.Team(inv.TeamID)
			claimed = !ok || t.OwnerID != ""
		}
		if inv.Revoked || claimed || (inv.ExpiresAt != nil && !m.now().Before(*inv.ExpiresAt)) ||
			(inv.MaxUses != nil && inv.Uses >= *inv.MaxUses) {
			return nil, errorf(KindGone, "invite-expired", "Invite expired", "This invite link no longer works. Ask the commissioner for a new one.")
		}
		return inv, nil
	}
	return nil, errInviteNotFound
}

// PreviewInvite returns the league behind a usable invite code. Anyone may call it.
func (m *Service) PreviewInvite(ctx context.Context, code string) (*League, Invite, error) {
	code = strings.ToUpper(code)
	l, err := m.repo.ByInviteCode(ctx, code)
	if err != nil {
		return nil, Invite{}, err
	}
	inv, err := m.usableInvite(l, code)
	if err != nil {
		return nil, Invite{}, err
	}
	return l, *inv, nil
}

// ErrNotClaimLink is ClaimSeat's error for an ordinary invite: joining
// with one needs an account.
var ErrNotClaimLink = errors.New("league: not a claim link")

// ClaimSeat gives a claim link's seat to the user owner returns, called
// only once the link is known to be a usable claim link: a signed-out
// person's claim creates their account there (docs/adr/0013), so a
// refused claim creates none. An ordinary invite is ErrNotClaimLink; a
// link replaced by a newer one (or expired) is 410 invite-expired; a used
// one, or one whose seat has a manager, is 409 seat-claimed.
func (m *Service) ClaimSeat(ctx context.Context, code string, owner func() (string, error)) (*League, Team, error) {
	var (
		out *League
		t   Team
	)
	err := m.repo.Tx(ctx, func(tx Tx) error {
		var err error
		out, t, err = ClaimSeatIn(ctx, tx, code, m.now(), owner)
		return err
	})
	if err != nil {
		return nil, Team{}, err
	}
	return out, t, nil
}

// ClaimSeatIn is ClaimSeat in the caller's transaction (on Postgres the
// account is created in the same one).
func ClaimSeatIn(ctx context.Context, tx Tx, code string, now time.Time, owner func() (string, error)) (*League, Team, error) {
	code = strings.ToUpper(code)
	l, err := tx.LoadByInviteCode(ctx, code)
	if err != nil {
		return nil, Team{}, err
	}
	if l == nil {
		return nil, Team{}, errInviteNotFound
	}
	i := slices.IndexFunc(l.Invites, func(inv Invite) bool { return inv.Code == code })
	if i < 0 {
		return nil, Team{}, errInviteNotFound
	}
	inv := &l.Invites[i]
	if inv.TeamID == "" {
		return nil, Team{}, ErrNotClaimLink
	}
	if inv.Revoked || (inv.ExpiresAt != nil && !now.Before(*inv.ExpiresAt)) {
		return nil, Team{}, errorf(KindGone, "invite-expired", "Invite expired",
			"This claim link was replaced by a newer one. Ask the commissioner for the current link.")
	}
	ti := slices.IndexFunc(l.Teams, func(t Team) bool { return t.ID == inv.TeamID })
	if ti < 0 {
		return nil, Team{}, errInviteNotFound
	}
	if l.Teams[ti].OwnerID != "" || (inv.MaxUses != nil && inv.Uses >= *inv.MaxUses) {
		return nil, Team{}, errorf(KindConflict, "seat-claimed", "Already claimed",
			l.Teams[ti].Name+" has already been claimed with this link. If that wasn't you, ask the commissioner.")
	}
	uid, err := owner()
	if err != nil {
		return nil, Team{}, err
	}
	if _, ok := l.TeamOf(uid); ok {
		e := errorf(KindConflict, "already-member", "Already a member", "You are already in this league.")
		e.LeagueID = l.ID
		return nil, Team{}, e
	}
	l.Teams[ti].OwnerID = uid
	inv.Uses++
	out := clone(l)
	if err := tx.Save(ctx, l); err != nil {
		return nil, Team{}, err
	}
	return out, out.Teams[ti], nil
}

// AcceptInvite adds a team for the actor before the draft; once it has
// started, it gives the actor the first open seat (a team without a
// manager), renamed to teamName unless the league is imported. A claim
// invite gives the actor the invite's team (teamName is then ignored: an
// imported seat keeps its FPL name, docs/adr/0012).
func (m *Service) AcceptInvite(ctx context.Context, actor, code, teamName string) (*League, Team, error) {
	code = strings.ToUpper(code)
	var (
		out *League
		t   Team
	)
	err := m.repo.Tx(ctx, func(tx Tx) error {
		l, err := tx.LoadByInviteCode(ctx, code)
		if err != nil {
			return err
		}
		inv, err := m.usableInvite(l, code)
		if err != nil {
			return err
		}
		if _, ok := l.TeamOf(actor); ok {
			e := errorf(KindConflict, "already-member", "Already a member", "You are already in this league.")
			e.LeagueID = l.ID
			return e
		}
		if inv.TeamID != "" {
			for i := range l.Teams {
				if l.Teams[i].ID == inv.TeamID {
					l.Teams[i].OwnerID = actor
					t = l.Teams[i]
				}
			}
			inv.Uses++
			out = clone(l)
			return tx.Save(ctx, l)
		}
		if err := validName(teamName, 1, 32, "teamName"); err != nil {
			return err
		}
		if !l.HasPlace() {
			return errorf(KindConflict, "league-full", "League full", "Every team in this league has a manager.")
		}
		if l.Status == StatusPreDraft {
			t = Team{ID: ids.New("tm"), Name: strings.TrimSpace(teamName), OwnerID: actor, JoinedAt: m.now()}
			l.Teams = append(l.Teams, t)
		} else {
			i := slices.IndexFunc(l.Teams, func(t Team) bool { return t.OwnerID == "" })
			l.Teams[i].OwnerID = actor
			if l.Source != SourceFPL {
				l.Teams[i].Name = strings.TrimSpace(teamName)
			}
			t = l.Teams[i]
		}
		inv.Uses++
		out = clone(l)
		return tx.Save(ctx, l)
	})
	if err != nil {
		return nil, Team{}, err
	}
	return out, t, nil
}
