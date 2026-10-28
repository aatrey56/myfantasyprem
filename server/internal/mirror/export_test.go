package mirror

import (
	"context"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
)

// VerifyStored verifies the league as it is stored against a fresh FPL
// snapshot, without applying the plan or rescoring first, and saves the
// report like a sync does (tests: corrupt the stored league, then check
// that verification catches it and blocks the switch-over).
func (s *Service) VerifyStored(ctx context.Context, leagueID string) (*Report, error) {
	now := s.now()
	w, err := league.LoadWorld(ctx, s.Stats)
	if err != nil {
		return nil, err
	}
	st, err := s.Leagues.State(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	snap, err := s.fetch(ctx, st.League.FPLLeagueID, w, now, func(int, int) {})
	if err != nil {
		return nil, err
	}
	players, err := s.Stats.ExternalIDs(ctx, Provider, "player")
	if err != nil {
		return nil, err
	}
	plan, err := Build(snap, players, func(id string) scoring.Position { return w.Players[id].Position })
	if err != nil {
		return nil, err
	}
	rep := Verify(snap, plan, st, w, now)
	if err := s.Store.SaveReport(ctx, leagueID, rep); err != nil {
		return nil, err
	}
	return &rep, nil
}
