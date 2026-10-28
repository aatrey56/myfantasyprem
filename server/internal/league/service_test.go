package league

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

var ctx = context.Background()

// Users the tests act as. Postgres needs them to exist (foreign keys).
const (
	usrA = "usr_0000000000000000000000000a"
	usrB = "usr_0000000000000000000000000b"
	usrC = "usr_0000000000000000000000000c"
	usrD = "usr_0000000000000000000000000d"
	usrE = "usr_0000000000000000000000000e"
)

// impls runs a test against the Service on every Repo: Memory always,
// Postgres when TEST_DATABASE_URL is set (see storetest).
func impls(t *testing.T, test func(t *testing.T, m *Service, c *clock)) {
	t.Run("memory", func(t *testing.T) {
		c := &clock{t: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
		test(t, NewService(NewMemory(), c.now), c)
	})
	t.Run("postgres", func(t *testing.T) {
		pool := storetest.New(t)
		seedUsers(t, pool)
		c := &clock{t: time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)}
		test(t, NewService(NewPostgres(pool), c.now), c)
	})
}

func seedUsers(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	for _, id := range []string{usrA, usrB, usrC, usrD, usrE} {
		if _, err := pool.Exec(ctx, `INSERT INTO users (id, display_name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
}

func kind(err error) Kind {
	var e *Error
	if errors.As(err, &e) {
		return e.Kind
	}
	return 0
}

func slug(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Slug
	}
	return ""
}

func ptr[T any](v T) *T { return &v }

func TestCreateDefaults(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, _ *clock) {
		l, err := m.Create(ctx, usrA, "Office League", 10, "Heroes", SettingsInput{})
		if err != nil {
			t.Fatal(err)
		}
		if l.Status != StatusPreDraft || l.CommissionerID != usrA || len(l.Teams) != 1 || l.Teams[0].OwnerID != usrA {
			t.Errorf("league = %+v", l)
		}
		s := l.Settings
		if s.TeamCount != 10 || s.TradeReview != ReviewNone || s.ReviewHours != 0 || s.PickTimerSeconds != 60 || s.EndGameweek != 38 {
			t.Errorf("settings = %+v", s)
		}
		l2, _ := m.Create(ctx, usrA, "Vote League", 8, "Heroes", SettingsInput{TradeReview: ptr(ReviewLeagueVote)})
		if l2.Settings.ReviewHours != 24 {
			t.Errorf("review window default = %d", l2.Settings.ReviewHours)
		}
		for _, bad := range []struct {
			name string
			n    int
		}{{"ab", 10}, {"Fine name", 1}, {"Fine name", 21}} {
			if _, err := m.Create(ctx, usrA, bad.name, bad.n, "T", SettingsInput{}); kind(err) != KindInvalid {
				t.Errorf("%+v: err = %v", bad, err)
			}
		}
		// Two teams is the smallest league (a small test league).
		if l3, err := m.Create(ctx, usrA, "Two of Us", 2, "Heroes", SettingsInput{}); err != nil || l3.Settings.TeamCount != 2 {
			t.Errorf("two-team league = %+v, %v", l3, err)
		}
	})
}

// Invites expire after a week unless asked otherwise, and codes are 10
// characters of the unambiguous alphabet; 8-character codes made before
// that keep working until they expire.
func TestInviteDefaultExpiryAndCodeLength(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l, _ := m.Create(ctx, usrA, "Week League", 4, "A", SettingsInput{})
		inv, err := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if inv.ExpiresAt == nil || !inv.ExpiresAt.Equal(c.t.Add(7*24*time.Hour)) {
			t.Errorf("default expiry = %v, want a week from now", inv.ExpiresAt)
		}
		if len(inv.Code) < 10 {
			t.Errorf("code %q is shorter than 10", inv.Code)
		}
		for _, r := range inv.Code {
			if !strings.ContainsRune(codeAlphabet, r) {
				t.Errorf("code %q has %q, outside the alphabet", inv.Code, r)
			}
		}
		// A legacy 8-character code still previews and joins until it
		// expires.
		exp := c.t.Add(time.Hour)
		if _, err := m.mutate(ctx, l.ID, func(_ Tx, l *League) error {
			l.Invites = append(l.Invites, Invite{ID: "inv_01k7m2xq800000000000000401", Code: "K7QM2XRP", CreatedAt: c.t, ExpiresAt: &exp})
			return nil
		}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := m.PreviewInvite(ctx, "k7qm2xrp"); err != nil {
			t.Fatalf("legacy code: %v", err)
		}
		if _, _, err := m.AcceptInvite(ctx, usrB, "K7QM2XRP", "B"); err != nil {
			t.Fatalf("legacy code join: %v", err)
		}
		c.t = exp
		if _, _, err := m.PreviewInvite(ctx, "K7QM2XRP"); kind(err) != KindGone {
			t.Errorf("expired legacy code: %v", err)
		}
	})
}

// Settings out of range are refused with every violation listed, on
// create and update, and nothing changes.
func TestSettingsBounds(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		bad := SettingsInput{
			PickTimerSeconds: ptr(-5), ReviewHours: ptr(1_000_000_000), VetoThreshold: ptr(1.5),
			TradeDeadlineGW: ptr(0), StartGameweek: ptr(39), TradeReview: ptr(ReviewCommissioner),
		}
		_, err := m.Create(ctx, usrA, "Bad League", 4, "A", bad)
		var e *Error
		if !errors.As(err, &e) || e.Kind != KindInvalid || len(e.Errors) != 5 {
			t.Fatalf("create with bad settings: %v (%+v)", err, e)
		}
		fields := map[string]bool{}
		for _, fe := range e.Errors {
			fields[fe.Field] = true
		}
		for _, f := range []string{"/settings/draft/pickTimerSeconds", "/settings/trades/reviewHours",
			"/settings/trades/vetoThreshold", "/settings/trades/deadlineGameweek", "/settings/startGameweek"} {
			if !fields[f] {
				t.Errorf("no violation for %s in %+v", f, e.Errors)
			}
		}
		l, err := m.Create(ctx, usrA, "Good League", 4, "A", SettingsInput{})
		if err != nil {
			t.Fatal(err)
		}
		for name, in := range map[string]SettingsInput{
			"huge review":   {ReviewHours: ptr(73)},
			"negative veto": {VetoThreshold: ptr(-0.1)},
			"tiny timer":    {PickTimerSeconds: ptr(14)},
			"huge timer":    {PickTimerSeconds: ptr(86401)},
		} {
			if _, err := m.Update(ctx, usrA, l.ID, nil, in); kind(err) != KindInvalid {
				t.Errorf("%s: %v", name, err)
			}
		}
		got, _ := m.Get(ctx, usrA, l.ID)
		if got.Settings != l.Settings {
			t.Errorf("settings changed by refused updates: %+v", got.Settings)
		}
		// The bounds themselves are fine.
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{PickTimerSeconds: ptr(15), ReviewHours: ptr(72),
			VetoThreshold: ptr(1.0), TradeDeadlineGW: ptr(38), StartGameweek: ptr(1)}); err != nil {
			t.Errorf("settings at the bounds: %v", err)
		}
	})
}

func TestInviteJoinAndLimits(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l, _ := m.Create(ctx, usrA, "Small League", 4, "A", SettingsInput{})
		if _, err := m.CreateInvite(ctx, usrB, l.ID, nil, nil); kind(err) != KindForbidden {
			t.Fatalf("non-member invite: %v", err)
		}
		inv, err := m.CreateInvite(ctx, usrA, l.ID, ptr(2), ptr(time.Hour))
		if err != nil {
			t.Fatal(err)
		}
		if len(inv.Code) != 10 {
			t.Errorf("code = %q", inv.Code)
		}
		if _, _, err := m.PreviewInvite(ctx, "nope1234"); kind(err) != KindNotFound {
			t.Errorf("unknown code: %v", err)
		}
		if _, team, err := m.AcceptInvite(ctx, usrB, inv.Code, "B"); err != nil || team.OwnerID != usrB {
			t.Fatalf("accept: %+v %v", team, err)
		}
		if _, _, err := m.AcceptInvite(ctx, usrB, inv.Code, "B2"); slug(err) != "already-member" {
			t.Errorf("rejoin: %v", err)
		} else if e := err.(*Error); e.LeagueID != l.ID {
			t.Errorf("rejoin leagueID = %q, want %q", e.LeagueID, l.ID)
		}
		// Codes are case-insensitive.
		if _, _, err := m.AcceptInvite(ctx, usrC, toLower(inv.Code), "C"); err != nil {
			t.Fatal(err)
		}
		// maxUses 2 reached.
		if _, _, err := m.AcceptInvite(ctx, usrD, inv.Code, "D"); kind(err) != KindGone {
			t.Errorf("used up: %v", err)
		}
		inv2, _ := m.CreateInvite(ctx, usrA, l.ID, nil, ptr(time.Minute))
		c.t = c.t.Add(time.Minute)
		if _, _, err := m.AcceptInvite(ctx, usrD, inv2.Code, "D"); kind(err) != KindGone {
			t.Errorf("expired: %v", err)
		}
		inv3, _ := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		if _, _, err := m.AcceptInvite(ctx, usrD, inv3.Code, "D"); err != nil {
			t.Fatal(err)
		}
		if _, err := m.CreateInvite(ctx, usrA, l.ID, nil, nil); slug(err) != "league-full" {
			t.Errorf("full league invite: %v", err)
		}
		if _, _, err := m.AcceptInvite(ctx, usrE, inv3.Code, "E"); slug(err) != "league-full" {
			t.Errorf("full league join: %v", err)
		}
		if err := m.RevokeInvite(ctx, usrA, l.ID, inv3.ID); err != nil {
			t.Fatal(err)
		}
		if _, _, err := m.PreviewInvite(ctx, inv3.Code); kind(err) != KindGone {
			t.Errorf("revoked: %v", err)
		}
		if got, _ := m.ForUser(ctx, usrD); len(got) != 1 || got[0].ID != l.ID {
			t.Errorf("ForUser = %v", got)
		}
	})
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}

func TestLeaveRemoveAndDeleteAccount(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, _ *clock) {
		l, _ := m.Create(ctx, usrA, "League", 6, "A", SettingsInput{})
		inv, _ := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		m.AcceptInvite(ctx, usrB, inv.Code, "B")
		m.AcceptInvite(ctx, usrC, inv.Code, "C")

		if _, err := m.Leave(ctx, usrA, l.ID); slug(err) != "commissioner-cannot-leave" {
			t.Errorf("commissioner leave: %v", err)
		}
		if _, err := m.RemoveMember(ctx, usrB, l.ID, usrC); kind(err) != KindForbidden {
			t.Errorf("member removes member: %v", err)
		}
		if _, err := m.Leave(ctx, usrC, l.ID); err != nil {
			t.Fatal(err)
		}
		got, _ := m.Get(ctx, usrA, l.ID)
		if len(got.Teams) != 2 {
			t.Errorf("pre-draft leave deletes the team: %+v", got.Teams)
		}
		if _, err := m.Get(ctx, usrC, l.ID); kind(err) != KindForbidden {
			t.Errorf("former member can read: %v", err)
		}

		// After the draft a departing manager's team stays, orphaned.
		if err := m.Repo().Tx(ctx, func(tx Tx) error { return tx.StartSeason(ctx, l.ID, &Game{}) }); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{TeamCount: ptr(8)}); slug(err) != "setting-locked" {
			t.Errorf("locked setting: %v", err)
		}
		if _, err := m.Update(ctx, usrA, l.ID, ptr("Renamed"), SettingsInput{ReviewHours: ptr(12)}); err != nil {
			t.Errorf("unlocked change: %v", err)
		}
		m.RemoveUserEverywhere(ctx, usrA)
		got, _ = m.Get(ctx, usrB, l.ID)
		if got.CommissionerID != usrB || len(got.Teams) != 2 || got.Teams[0].OwnerID != "" || got.MemberCount() != 1 {
			t.Errorf("after delete: %+v", got)
		}
	})
}

func TestRenameTeam(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, _ *clock) {
		l, _ := m.Create(ctx, usrA, "League", 6, "A", SettingsInput{})
		inv, _ := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		_, tb, _ := m.AcceptInvite(ctx, usrB, inv.Code, "B")
		if _, err := m.RenameTeam(ctx, usrA, l.ID, tb.ID, "Mine now"); kind(err) != KindForbidden {
			t.Errorf("rename other's team: %v", err)
		}
		if got, err := m.RenameTeam(ctx, usrB, l.ID, tb.ID, "  Bees "); err != nil || got.Name != "Bees" {
			t.Errorf("rename: %+v %v", got, err)
		}
	})
}

// The commissioner hands the role to a member and can then leave a
// populated league; only the commissioner can, and only to a member.
func TestTransferCommissioner(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, _ *clock) {
		l, _ := m.Create(ctx, usrA, "League", 6, "A", SettingsInput{})
		inv, _ := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		m.AcceptInvite(ctx, usrB, inv.Code, "B")
		m.AcceptInvite(ctx, usrC, inv.Code, "C")

		if _, err := m.TransferCommissioner(ctx, usrB, l.ID, usrB); kind(err) != KindForbidden {
			t.Errorf("member transfers: %v", err)
		}
		if _, err := m.TransferCommissioner(ctx, usrA, l.ID, usrD); kind(err) != KindNotFound {
			t.Errorf("transfer to a non-member: %v", err)
		}
		got, err := m.TransferCommissioner(ctx, usrA, l.ID, usrB)
		if err != nil || got.CommissionerID != usrB {
			t.Fatalf("transfer: %+v %v", got, err)
		}
		if stored, _ := m.Repo().Get(ctx, l.ID); stored.CommissionerID != usrB {
			t.Errorf("stored commissioner = %s", stored.CommissionerID)
		}
		if _, err := m.TransferCommissioner(ctx, usrA, l.ID, usrC); kind(err) != KindForbidden {
			t.Errorf("former commissioner transfers again: %v", err)
		}
		if _, err := m.Leave(ctx, usrA, l.ID); err != nil {
			t.Errorf("former commissioner leaves: %v", err)
		}
		if got, _ := m.Get(ctx, usrB, l.ID); got.MemberCount() != 2 || got.CommissionerID != usrB {
			t.Errorf("after leave: %+v", got)
		}
	})
}
