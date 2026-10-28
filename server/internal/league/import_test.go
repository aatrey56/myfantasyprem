package league

import (
	"maps"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
)

// importedLeague stores a league as the FPL import makes it (docs/adr/0012):
// usrA commissioner with the first seat, two seats without a manager.
func importedLeague(t *testing.T, m *Service, now time.Time) *League {
	t.Helper()
	l := &League{
		ID: ids.New("lg"), Name: "Imported", Season: Season, Status: StatusInSeason, Settings: DefaultSettings(3),
		CommissionerID: usrA, CreatedAt: now, Source: SourceFPL, FPLLeagueID: 90001,
		Teams: []Team{
			{ID: ids.New("tm"), Name: "One", OwnerID: usrA, JoinedAt: now, FPLEntryID: 101, ManagerName: "Alex Applegate"},
			{ID: ids.New("tm"), Name: "Two", JoinedAt: now.Add(time.Second), FPLEntryID: 102, ManagerName: "Billie Brookfield"},
			{ID: ids.New("tm"), Name: "Three", JoinedAt: now.Add(2 * time.Second), FPLEntryID: 103, ManagerName: "Casey Cobble"},
		},
	}
	if err := m.Repo().Tx(ctx, func(tx Tx) error { return tx.Insert(ctx, l) }); err != nil {
		t.Fatal(err)
	}
	return l
}

func TestImportedLeagueRoundTrip(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l := importedLeague(t, m, c.t)
		got, err := m.Repo().Get(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Source != SourceFPL || got.FPLLeagueID != 90001 ||
			got.Teams[1].FPLEntryID != 102 || got.Teams[1].ManagerName != "Billie Brookfield" || got.Teams[1].OwnerID != "" {
			t.Fatalf("league = %+v", got)
		}

		// Refs commit with the state and are only added.
		err = m.Repo().Tx(ctx, func(tx Tx) error {
			s, err := tx.LoadState(ctx, l.ID)
			if err != nil {
				return err
			}
			for i, tm := range s.League.Teams {
				s.SetRef(RefTeam, "fpl", string(rune('a'+i)), tm.ID)
			}
			s.SetRef(RefTransaction, "fpl", "463248", ids.New("txn"))
			return tx.SaveState(ctx, s)
		})
		if err != nil {
			t.Fatal(err)
		}
		want := map[RefKey]string{}
		err = m.Repo().Tx(ctx, func(tx Tx) error {
			s, err := tx.LoadState(ctx, l.ID)
			if err != nil {
				return err
			}
			if len(s.Refs) != 4 {
				t.Errorf("refs = %v", s.Refs)
			}
			s.SetRef(RefTrade, "fpl", "322640", ids.New("trd"))
			want = maps.Clone(s.Refs)
			return tx.SaveState(ctx, s)
		})
		if err != nil {
			t.Fatal(err)
		}
		st, err := m.Repo().State(ctx, l.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !maps.Equal(st.Refs, want) {
			t.Errorf("refs = %v, want %v", st.Refs, want)
		}
		if id, ok := st.Ref(RefTrade, "fpl", "322640"); !ok || id != want[RefKey{RefTrade, "fpl", "322640"}] {
			t.Errorf("trade ref = %q %v", id, ok)
		}
	})
}

func TestClaimInvites(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l := importedLeague(t, m, c.t)
		two, three := l.Teams[1].ID, l.Teams[2].ID
		if _, err := m.ClaimInvite(ctx, usrB, l.ID, two); slug(err) != "not-a-member" {
			t.Errorf("non-member: %v", err)
		}
		if _, err := m.ClaimInvite(ctx, usrA, l.ID, l.Teams[0].ID); slug(err) != "seat-claimed" {
			t.Errorf("own seat: %v", err)
		}
		first, err := m.ClaimInvite(ctx, usrA, l.ID, two)
		if err != nil {
			t.Fatal(err)
		}
		if first.TeamID != two || first.ExpiresAt != nil || first.MaxUses == nil || *first.MaxUses != 1 {
			t.Errorf("claim invite = %+v", first)
		}
		// Resending revokes the first link.
		inv, err := m.ClaimInvite(ctx, usrA, l.ID, two)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := m.PreviewInvite(ctx, first.Code); slug(err) != "invite-expired" {
			t.Errorf("revoked link: %v", err)
		}
		pl, pinv, err := m.PreviewInvite(ctx, inv.Code)
		if err != nil || pl.ID != l.ID || pinv.TeamID != two {
			t.Fatalf("preview = %v %+v %v", pl, pinv, err)
		}
		if got, ok := pl.ClaimLink(two); !ok || got.Code != inv.Code {
			t.Errorf("claim link = %+v %v", got, ok)
		}

		// Accepting gives the seat (its FPL name kept; no team name needed)
		// even though the league has drafted.
		joined, team, err := m.AcceptInvite(ctx, usrB, inv.Code, "")
		if err != nil {
			t.Fatal(err)
		}
		if team.ID != two || team.OwnerID != usrB || team.Name != "Two" || len(joined.Teams) != 3 {
			t.Errorf("claimed team = %+v (%d teams)", team, len(joined.Teams))
		}
		if _, ok := joined.ClaimLink(two); ok {
			t.Error("a used claim link is still current")
		}
		if _, _, err := m.AcceptInvite(ctx, usrC, inv.Code, ""); slug(err) != "invite-expired" {
			t.Errorf("second claim: %v", err)
		}
		if _, err := m.ClaimInvite(ctx, usrA, l.ID, two); slug(err) != "seat-claimed" {
			t.Errorf("resend for a claimed seat: %v", err)
		}
		// One seat per user.
		other, err := m.ClaimInvite(ctx, usrA, l.ID, three)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := m.AcceptInvite(ctx, usrB, other.Code, ""); slug(err) != "already-member" {
			t.Errorf("second seat: %v", err)
		}
		// Leaving orphans the seat; a new link can be issued.
		if _, err := m.Leave(ctx, usrB, l.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := m.ClaimInvite(ctx, usrA, l.ID, two); err != nil {
			t.Errorf("after leaving: %v", err)
		}
	})
}

// An imported league plays like a native one from the moment it is
// imported (owner decision 2026-10-14, docs/adr/0012): its name, settings
// and team names are the commissioner's and managers' to change.
func TestImportedLeagueIsEditable(t *testing.T) {
	impls(t, func(t *testing.T, m *Service, c *clock) {
		l := importedLeague(t, m, c.t)
		name := "Renamed League"
		got, err := m.Update(ctx, usrA, l.ID, &name, SettingsInput{})
		if err != nil {
			t.Fatalf("update: %v", err)
		}
		if got.Name != name {
			t.Errorf("league name = %q", got.Name)
		}
		if _, err := m.RenameTeam(ctx, usrA, l.ID, l.Teams[0].ID, "Mine"); err != nil {
			t.Errorf("rename team: %v", err)
		}
	})
}
