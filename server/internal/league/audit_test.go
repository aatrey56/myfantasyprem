package league

import (
	"slices"
	"testing"
	"time"
)

// joined makes a league of usrA (commissioner) and the given managers.
func joined(t *testing.T, m *Service, c *clock, others ...string) *League {
	t.Helper()
	l, err := m.Create(ctx, usrA, "Office League", 6, "Heroes", SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	for i, u := range others {
		inv, err := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		c.t = c.t.Add(time.Minute)
		if _, _, err := m.AcceptInvite(ctx, u, inv.Code, "Team "+string(rune('B'+i))); err != nil {
			t.Fatal(err)
		}
	}
	return l
}

func moves(t *testing.T, m *Service, actor, leagueID string) []AuditEntry {
	t.Helper()
	p, err := m.CommissionerMoves(ctx, actor, leagueID, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	return p.Entries
}

func actions(es []AuditEntry) []string {
	var out []string
	for _, e := range es {
		out = append(out, e.Action)
	}
	return out
}

// Every commissioner power writes an entry, newest first; refused and no-op
// requests write none.
func TestCommissionerMovesAreLogged(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l := joined(t, m, c, usrB, usrC)
		// Joining and creating are not commissioner moves; the invites above are.
		base := len(moves(t, m, usrA, l.ID))
		if base != 2 {
			t.Fatalf("setup logged %v", actions(moves(t, m, usrA, l.ID)))
		}
		step := func() { c.t = c.t.Add(time.Minute) }

		// Settings: one entry with the values before and after.
		step()
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{ReviewHours: ptr(48), VetoThreshold: ptr(0.5)}); err != nil {
			t.Fatal(err)
		}
		es := moves(t, m, usrA, l.ID)
		top := es[0]
		if top.Action != AuditSettingsChanged || top.ActorID != usrA || !top.At.Equal(c.t) ||
			top.Summary != "Changed the trade review period to 48 hours, veto threshold to 50% of managers." ||
			top.Before["reviewHours"] != float64(24) && top.Before["reviewHours"] != 24 ||
			top.After["vetoThreshold"] != 0.5 {
			t.Fatalf("settings entry = %+v", top)
		}
		// A rename is its own entry; the same settings again, a refused change
		// and a change by a manager write nothing.
		step()
		if _, err := m.Update(ctx, usrA, l.ID, ptr("Renamed League"), SettingsInput{ReviewHours: ptr(48)}); err != nil {
			t.Fatal(err)
		}
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{ReviewHours: ptr(500)}); kind(err) != KindInvalid {
			t.Fatalf("out of range: %v", err)
		}
		if _, err := m.Update(ctx, usrB, l.ID, nil, SettingsInput{ReviewHours: ptr(12)}); slug(err) != "not-commissioner" {
			t.Fatalf("manager update: %v", err)
		}
		es = moves(t, m, usrA, l.ID)
		if got := actions(es[:2]); !slices.Equal(got, []string{AuditLeagueRenamed, AuditSettingsChanged}) || len(es) != base+2 {
			t.Fatalf("after rename: %v (%d entries)", actions(es), len(es))
		}
		if es[0].Summary != "Renamed the league from “Office League” to “Renamed League”." {
			t.Fatalf("rename summary %q", es[0].Summary)
		}

		// Invites, removing a manager and handing the role over.
		inv, err := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Revoking it again changes nothing and logs nothing.
		for range 2 {
			if err := m.RevokeInvite(ctx, usrA, l.ID, inv.ID); err != nil {
				t.Fatal(err)
			}
		}
		team, _ := (&League{Teams: moves0(t, m, l.ID)}).TeamOf(usrC)
		if _, err := m.RemoveMember(ctx, usrA, l.ID, usrC); err != nil {
			t.Fatal(err)
		}
		if _, err := m.TransferCommissioner(ctx, usrA, l.ID, usrA); err != nil {
			t.Fatal(err)
		}
		if _, err := m.TransferCommissioner(ctx, usrA, l.ID, usrB); err != nil {
			t.Fatal(err)
		}
		es = moves(t, m, usrB, l.ID)
		if got := actions(es[:4]); !slices.Equal(got, []string{AuditCommissionerMoved, AuditMemberRemoved, AuditInviteRevoked, AuditInviteCreated}) {
			t.Fatalf("actions = %v", got)
		}
		if !slices.Equal(es[1].TeamIDs, []string{team.ID}) || es[1].Summary != "Removed the manager of “Team C” from the league." {
			t.Fatalf("removal = %+v", es[1])
		}
		// Seen by a manager, written by the old commissioner.
		if es[0].ActorID != usrA {
			t.Fatalf("actor = %q", es[0].ActorID)
		}

		// A manager leaving is their own move, not the commissioner's.
		before := len(es)
		if _, err := m.Leave(ctx, usrA, l.ID); err != nil {
			t.Fatal(err)
		}
		if got := len(moves(t, m, usrB, l.ID)); got != before {
			t.Fatalf("leaving logged an entry (%d -> %d)", before, got)
		}
		// Outsiders cannot read the log.
		if _, err := m.CommissionerMoves(ctx, usrE, l.ID, 0, 10); slug(err) != "not-a-member" {
			t.Fatalf("outsider: %v", err)
		}
	})
}

func moves0(t *testing.T, m *Service, id string) []Team {
	t.Helper()
	l, err := m.repo.Get(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return l.Teams
}

// The log pages newest first, a page more than asked is returned so the
// caller can tell there is a next one.
func TestCommissionerMovesPaging(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l := joined(t, m, c, usrB)
		for i := range 5 {
			c.t = c.t.Add(time.Minute)
			if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{ReviewHours: ptr(25 + i)}); err != nil {
				t.Fatal(err)
			}
		}
		all := moves(t, m, usrB, l.ID)
		if len(all) != 6 { // five changes and the invite
			t.Fatalf("%d entries", len(all))
		}
		if !slices.IsSortedFunc(all, func(a, b AuditEntry) int { return b.At.Compare(a.At) }) {
			t.Fatalf("not newest first")
		}
		p, err := m.CommissionerMoves(ctx, usrB, l.ID, 2, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(p.Entries) != 3 || p.Entries[0].ID != all[2].ID || p.Entries[1].ID != all[3].ID {
			t.Fatalf("page = %v", p.Entries)
		}
		p, _ = m.CommissionerMoves(ctx, usrB, l.ID, 5, 2)
		if len(p.Entries) != 1 || p.Entries[0].ID != all[5].ID {
			t.Fatalf("last page = %v", p.Entries)
		}
	})
}

// The banner follows the member: unseen counts per member, their own moves
// never count, marking moves a marker that never goes back, and a member
// who joined later does not inherit earlier moves.
func TestCommissionerMovesSeen(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l := joined(t, m, c, usrB)
		change := func(hours int) {
			c.t = c.t.Add(time.Minute)
			if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{ReviewHours: ptr(hours)}); err != nil {
				t.Fatal(err)
			}
		}
		summary := func(u string) AuditSummary {
			t.Helper()
			s, _, err := m.CommissionerMovesSummary(ctx, u, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			return s
		}
		change(30)
		change(31)
		if s := summary(usrB); s.Unseen != 2 || s.Latest == nil || s.Latest.After["reviewHours"] != float64(31) && s.Latest.After["reviewHours"] != 31 {
			t.Fatalf("unseen for B = %+v", s)
		}
		if s := summary(usrA); s.Unseen != 0 || s.Latest != nil {
			t.Fatalf("the commissioner's own moves count as unseen: %+v", s)
		}
		p, _ := m.CommissionerMoves(ctx, usrB, l.ID, 0, 10)
		if !p.Unseen(p.Entries[0]) || !p.Unseen(p.Entries[1]) {
			t.Fatal("entries not marked unseen")
		}

		// Seen up to the older change: one left.
		older := moves(t, m, usrB, l.ID)[1]
		if ok, err := m.repo.MarkAuditSeen(ctx, l.ID, usrB, older.ID); !ok || err != nil {
			t.Fatalf("mark: %v %v", ok, err)
		}
		if s := summary(usrB); s.Unseen != 1 {
			t.Fatalf("after marking the older: %+v", s)
		}
		// All of it, and then an older id does not bring anything back.
		s, _, err := m.MarkCommissionerMovesSeen(ctx, usrB, l.ID, "")
		if err != nil || s.Unseen != 0 || s.Latest != nil {
			t.Fatalf("mark all = %+v %v", s, err)
		}
		if _, _, err := m.MarkCommissionerMovesSeen(ctx, usrB, l.ID, older.ID); err != nil {
			t.Fatal(err)
		}
		if s := summary(usrB); s.Unseen != 0 {
			t.Fatalf("marker moved back: %+v", s)
		}
		// Unseen again when a new move lands.
		change(32)
		if s := summary(usrB); s.Unseen != 1 {
			t.Fatalf("after a new move: %+v", s)
		}
		// An id that is not this league's is a 404 and changes nothing.
		if _, _, err := m.MarkCommissionerMovesSeen(ctx, usrB, l.ID, "cmv_01k7m2xq800000000000000401"); kind(err) != KindNotFound {
			t.Fatalf("unknown id: %v", err)
		}
		if s := summary(usrB); s.Unseen != 1 {
			t.Fatalf("a refused mark moved the marker: %+v", s)
		}
		// Markers are per member; a member who joins later starts clean.
		other := joined(t, m, c, usrB)
		c.t = c.t.Add(time.Minute)
		if _, err := m.Update(ctx, usrA, other.ID, nil, SettingsInput{ReviewHours: ptr(40)}); err != nil {
			t.Fatal(err)
		}
		inv, _ := m.CreateInvite(ctx, usrA, other.ID, nil, nil)
		c.t = c.t.Add(time.Minute)
		if _, _, err := m.AcceptInvite(ctx, usrC, inv.Code, "Latecomers"); err != nil {
			t.Fatal(err)
		}
		if s, _, _ := m.CommissionerMovesSummary(ctx, usrC, other.ID); s.Unseen != 0 {
			t.Fatalf("a new member inherits earlier moves: %+v", s)
		}
		if s, _, _ := m.CommissionerMovesSummary(ctx, usrB, other.ID); s.Unseen < 1 {
			t.Fatalf("B's marker leaked across leagues: %+v", s)
		}
	})
}

// A manager who takes over a seat (an open seat after the draft, or a claim
// link's) starts with nothing unseen: the team's JoinedAt is its first
// manager's, and the moves before the newcomer arrived are not theirs.
func TestNewManagerOfASeatInheritsNoMoves(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l := joined(t, m, c, usrB, usrC)
		if err := m.Repo().Tx(ctx, func(tx Tx) error { return tx.StartSeason(ctx, l.ID, &Game{}) }); err != nil {
			t.Fatal(err)
		}
		step := func() { c.t = c.t.Add(time.Minute) }
		unseen := func(u string) int {
			t.Helper()
			s, _, err := m.CommissionerMovesSummary(ctx, u, l.ID)
			if err != nil {
				t.Fatal(err)
			}
			return s.Unseen
		}
		step()
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{ReviewHours: ptr(36)}); err != nil {
			t.Fatal(err)
		}
		// Two seats empty; their teams keep their old JoinedAt.
		for _, u := range []string{usrB, usrC} {
			step()
			if _, err := m.RemoveMember(ctx, usrA, l.ID, u); err != nil {
				t.Fatal(err)
			}
		}
		step()
		inv, err := m.CreateInvite(ctx, usrA, l.ID, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		step()
		if _, _, err := m.AcceptInvite(ctx, usrD, inv.Code, "New Owners"); err != nil {
			t.Fatal(err)
		}
		if n := unseen(usrD); n != 0 {
			t.Fatalf("D took over a seat with %d moves unseen, want 0", n)
		}
		cur, err := m.Get(ctx, usrA, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		var open string
		for _, tm := range cur.Teams {
			if tm.OwnerID == "" {
				open = tm.ID
			}
		}
		step()
		claim, err := m.ClaimInvite(ctx, usrA, l.ID, open)
		if err != nil {
			t.Fatal(err)
		}
		step()
		if _, _, err := m.ClaimSeat(ctx, claim.Code, func() (string, error) { return usrE, nil }); err != nil {
			t.Fatal(err)
		}
		// E arrived last; D has seen nothing since arriving, the claim link.
		for u, want := range map[string]int{usrD: 1, usrE: 0} {
			if n := unseen(u); n != want {
				t.Fatalf("%s: %d moves unseen, want %d", u, n, want)
			}
			p, err := m.CommissionerMoves(ctx, u, l.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			n := 0
			for _, e := range p.Entries {
				if p.Unseen(e) {
					n++
				}
			}
			if n != want {
				t.Fatalf("%s: %d listed as unseen, want %d", u, n, want)
			}
		}
		// What happens from then on is theirs to see.
		step()
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{ReviewHours: ptr(48)}); err != nil {
			t.Fatal(err)
		}
		for u, want := range map[string]int{usrD: 2, usrE: 1} {
			if n := unseen(u); n != want {
				t.Fatalf("%s: %d unseen after a new move, want %d", u, n, want)
			}
		}
	})
}

// The trade review can be changed at any time, after the draft too (ADR
// 0017), and the change is a commissioner move; the draft-time settings stay
// locked.
func TestTradeReviewChangesAfterTheDraft(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l := joined(t, m, c, usrB)
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{TradeReview: ptr(ReviewCommissioner)}); err != nil {
			t.Fatal(err)
		}
		if err := m.Repo().Tx(ctx, func(tx Tx) error { return tx.StartSeason(ctx, l.ID, &Game{}) }); err != nil {
			t.Fatal(err)
		}
		base := len(moves(t, m, usrA, l.ID))
		c.t = c.t.Add(time.Minute)

		if _, err := m.Update(ctx, usrB, l.ID, nil, SettingsInput{TradeReview: ptr(ReviewLeagueVote)}); slug(err) != "not-commissioner" {
			t.Fatalf("manager change: %v", err)
		}
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{TradeReview: ptr("mob_rule")}); kind(err) != KindInvalid {
			t.Fatalf("unknown mode: %v", err)
		}
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{PickTimerSeconds: ptr(30)}); slug(err) != "setting-locked" {
			t.Fatalf("pick clock stays locked: %v", err)
		}
		if got := len(moves(t, m, usrA, l.ID)); got != base {
			t.Fatalf("a refused change was logged (%d entries, want %d)", got, base)
		}

		got, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{TradeReview: ptr(ReviewLeagueVote), VetoThreshold: ptr(0.5)})
		if err != nil {
			t.Fatalf("switching to a vote after the draft: %v", err)
		}
		if got.Settings.TradeReview != ReviewLeagueVote || got.Settings.VetoThreshold != 0.5 {
			t.Fatalf("settings = %+v", got.Settings)
		}
		es := moves(t, m, usrA, l.ID)
		top := es[0]
		if len(es) != base+1 || top.Action != AuditSettingsChanged || top.ActorID != usrA || !top.At.Equal(c.t) ||
			top.Summary != "Changed the trade review to league vote, veto threshold to 50% of managers." ||
			top.Before["tradeReview"] != ReviewCommissioner || top.After["tradeReview"] != ReviewLeagueVote {
			t.Fatalf("entry = %+v (%d entries, want %d)", top, len(es), base+1)
		}
		// Saving the same mode again changes nothing and logs nothing.
		if _, err := m.Update(ctx, usrA, l.ID, nil, SettingsInput{TradeReview: ptr(ReviewLeagueVote)}); err != nil {
			t.Fatal(err)
		}
		if got := len(moves(t, m, usrA, l.ID)); got != base+1 {
			t.Fatalf("a no-op change was logged (%d entries)", got)
		}
		// Back to no review: the period goes to zero, as before the draft.
		got, err = m.Update(ctx, usrA, l.ID, nil, SettingsInput{TradeReview: ptr(ReviewNone)})
		if err != nil || got.Settings.TradeReview != ReviewNone || got.Settings.ReviewHours != 0 {
			t.Fatalf("to none: %+v %v", got, err)
		}
	})
}
