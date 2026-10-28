package mirror

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// LiveSource is FPL's live points feed; the FPL client is one. A Source
// that is not leaves per-player points unchecked.
type LiveSource interface {
	EventLive(ctx context.Context, gameweek int) (*provider.EventLive, error)
}

// Alerter files notifications (notifications.Notifier).
type Alerter interface {
	Send(ctx context.Context, ns ...notifications.Notification) ([]notifications.Notification, error)
}

// shadowAhead is how far ahead the hourly planner schedules checks.
const shadowAhead = 36 * time.Hour

// ShadowCheck compares an imported league with FPL now, without changing
// it (docs/adr/0016): it fetches FPL like a check, diffs with Shadow,
// stores the report and tells the commissioner about new mismatches. It
// never writes league state.
func (s *Service) ShadowCheck(ctx context.Context, leagueID, reason string) (*ShadowReport, error) {
	l, err := s.Leagues.Get(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, league.ErrLeagueNotFound
	}
	if l.Source != league.SourceFPL {
		return nil, errNotImported
	}
	now := s.now()
	w, snap, plan, err := s.plan(ctx, l.FPLLeagueID, now, nil)
	if err != nil {
		return nil, err
	}
	st, err := s.Leagues.State(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	// Decided claims and closed trades too: the outcomes are compared.
	st.Claims = nil
	for _, t := range st.League.Teams {
		cs, err := s.Leagues.Claims(ctx, leagueID, t.ID)
		if err != nil {
			return nil, err
		}
		st.Claims = append(st.Claims, cs...)
	}
	if st.Trades, err = s.Leagues.Trades(ctx, leagueID); err != nil {
		return nil, err
	}
	// Before the switch-over the league is played on FPL: claims are made
	// there and only their outcomes reach us.
	mirroring := s.ShadowUntil.IsZero() || now.Before(s.ShadowUntil)
	txns, err := s.Leagues.Transactions(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	in := ShadowInput{Snap: snap, Plan: plan, State: st, World: w, Txns: txns, Reason: reason, Now: now, Mirroring: mirroring}
	if lv, ok := s.Source.(LiveSource); ok {
		if cur := w.CurrentGW(now); cur > 0 && w.Started(cur) {
			ev, err := lv.EventLive(ctx, cur)
			if err != nil {
				return nil, upstream(err)
			}
			in.Live, in.LiveGW = map[string]int{}, cur
			for _, pl := range ev.Players {
				if id := players(plan)[pl.PlayerExternalID]; id != "" {
					in.Live[id] = pl.TotalPoints
				}
			}
		}
	}
	rep := Shadow(in)
	if err := s.Store.SaveShadow(ctx, leagueID, rep); err != nil {
		return nil, err
	}
	if err := s.alert(ctx, l, rep); err != nil {
		s.log().WarnContext(ctx, "shadow check alert failed", "league", leagueID, "err", err)
	}
	if s.Notify != nil {
		s.Notify.Notify(ctx, league.Change{LeagueID: leagueID, Kind: league.ChangeLeague})
	}
	s.log().InfoContext(ctx, "shadow check", "league", leagueID, "reason", reason, "status", rep.Status,
		"mismatches", len(rep.Mismatches()))
	return &rep, nil
}

// alert tells the commissioner about the report's mismatches. The key is
// the set of mismatches and their values, so the same differences found
// again send nothing new.
func (s *Service) alert(ctx context.Context, l *league.League, rep ShadowReport) error {
	ms := rep.Mismatches()
	if s.Alert == nil || len(ms) == 0 || l.CommissionerID == "" {
		return nil
	}
	n := ShadowAlert(l, ms)
	n.UserID = l.CommissionerID
	_, err := s.Alert.Send(ctx, n)
	return err
}

// ShadowAlert is the commissioner's notification for mismatches.
func ShadowAlert(l *league.League, ms []Item) notifications.Notification {
	keys := make([]string, len(ms))
	for i, it := range ms {
		keys[i] = it.Key + "=" + it.Ours + "|" + it.Theirs
	}
	slices.Sort(keys)
	sum := sha256.Sum256([]byte(strings.Join(keys, "\n")))
	var lines []string
	for i, it := range ms {
		if i == 3 {
			lines = append(lines, fmt.Sprintf("and %d more.", len(ms)-3))
			break
		}
		line := it.Kind
		if it.TeamID != "" {
			line = teamName(l, it.TeamID) + " " + line
		}
		if it.Gameweek > 0 {
			line += " GW" + strconv.Itoa(it.Gameweek)
		}
		lines = append(lines, line+": ours "+it.Ours+", FPL "+it.Theirs+".")
	}
	title := "FPL check: 1 difference"
	if len(ms) != 1 {
		title = "FPL check: " + strconv.Itoa(len(ms)) + " differences"
	}
	return notifications.Notification{LeagueID: l.ID, Type: notifications.FPLCheckMismatch, Title: title,
		Body: strings.Join(lines, " "), URL: "/league/" + l.ID + "/settings",
		Key: "fpl_check:" + l.ID + ":" + hex.EncodeToString(sum[:8])}
}

// PlanShadow schedules the shadow checks of every imported league in
// season for the next shadowAhead, from the FPL calendar (ShadowTimes),
// and the check after FPL marks a gameweek's data checked. Nothing after
// ShadowUntil (the switch-over; zero: no end). It returns how many it
// scheduled.
func (s *Service) PlanShadow(ctx context.Context) (int, error) {
	now := s.now()
	if s.Sched == nil || (!s.ShadowUntil.IsZero() && !now.Before(s.ShadowUntil)) {
		return 0, nil
	}
	gws, err := s.Stats.Gameweeks(ctx)
	if err != nil {
		return 0, err
	}
	fixtures, err := s.Stats.Fixtures(ctx)
	if err != nil {
		return 0, err
	}
	until := now.Add(shadowAhead)
	if !s.ShadowUntil.IsZero() && s.ShadowUntil.Before(until) {
		until = s.ShadowUntil
	}
	slots := ShadowTimes(gws, fixtures, now, until)
	if dc, ok := DataCheckedSlot(gws, fixtures, now); ok {
		slots = append(slots, dc)
	}
	if len(slots) == 0 {
		return 0, nil
	}
	ids, err := s.Leagues.LeaguesWithStatus(ctx, league.StatusInSeason)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		l, err := s.Leagues.Get(ctx, id)
		if err != nil {
			return n, err
		}
		if l == nil || l.Source != league.SourceFPL {
			continue
		}
		for _, sl := range slots {
			args := ShadowArgs{LeagueID: id, At: sl.At, Reason: sl.Reason}
			if _, seen := s.planned.LoadOrStore(args, true); seen {
				continue
			}
			at := sl.At
			if at.Before(now) {
				at = now
			}
			if err := s.Sched.ScheduleTx(ctx, nil, args, at); err != nil {
				s.planned.Delete(args)
				return n, err
			}
			n++
		}
	}
	return n, nil
}
