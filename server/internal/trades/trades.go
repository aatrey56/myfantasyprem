package trades

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/waivers"
)

// Viewer actions (TradeAction in the contract).
const (
	ActAccept   = "accept"
	ActDecline  = "decline"
	ActWithdraw = "withdraw"
	ActVote     = "vote"
	ActReview   = "review"
)

// Votes and decisions.
const (
	VoteVeto  = "veto"
	VoteAllow = "allow"

	DecisionApprove = "approve"
	DecisionVeto    = "veto"
)

// MaxMessage is the longest trade message.
const MaxMessage = 280

// ReviewArgs is the job that closes a trade's review window.
type ReviewArgs struct {
	LeagueID string    `json:"league_id"`
	TradeID  string    `json:"trade_id"`
	EndsAt   time.Time `json:"ends_at"`
}

// Kind names the job.
func (ReviewArgs) Kind() string { return "trade_review" }

var (
	errNotOpen     = league.Errorf(league.KindConflict, "trade-not-open", "Trade closed", "That trade is no longer open.")
	errNotInReview = league.Errorf(league.KindConflict, "trade-not-in-review", "Not in review", "That trade is not in review.")
	errExpired     = league.Errorf(league.KindConflict, "trade-expired", "Offer expired", "That offer has expired.")
	errReviewOver  = league.Errorf(league.KindConflict, "trade-review-closed", "Review closed",
		"The review window has closed; the trade is being settled.")
	errOnFPL = league.Errorf(league.KindConflict, "pending-on-fpl", "Pending on FPL",
		"FPL processes or vetoes this trade; the next re-sync from FPL applies what it decided.")
)

// PendingOnFPL reports whether t is a trade FPL accepted and has not
// processed or vetoed yet, imported into the league (docs/adr/0012): in
// review with no review window. FPL decides it, so nobody acts on it here
// and the trade engine never settles it.
func PendingOnFPL(t league.Trade) bool {
	return t.Status == league.TradeInReview && t.ReviewEndsAt == nil
}

// expired reports whether an offer's time is up, whether or not the tick
// has marked it yet.
func expired(t *league.Trade, now time.Time) bool {
	return t.Status == league.TradeOffered && !now.Before(t.ExpiresAt)
}

// reviewOver reports whether a trade's review window has closed, whether
// or not the tick has settled it yet.
func reviewOver(t *league.Trade, now time.Time) bool {
	return t.Status == league.TradeInReview && t.ReviewEndsAt != nil && !now.Before(*t.ReviewEndsAt)
}

func notFound(what string) error {
	return league.Errorf(league.KindNotFound, "not-found", "Not found", what+" not found.")
}

// Service runs trades. It is safe for concurrent use.
type Service struct {
	repo   league.Repo
	stats  stats.Repo
	now    func() time.Time
	notify league.Notifier
	rules  scoring.LineupRules
}

// New returns a Service. notify may be nil.
func New(repo league.Repo, st stats.Repo, now func() time.Time, notify league.Notifier) *Service {
	if notify == nil {
		notify = league.NopNotifier{}
	}
	return &Service{repo: repo, stats: st, notify: notify, rules: scoring.StandardLineup(),
		now: func() time.Time { return now().UTC().Truncate(time.Microsecond) }}
}

// EverAccepted reports whether the receiving team accepted the trade at
// some point: from then on the whole league may see it.
func EverAccepted(t league.Trade) bool {
	switch t.Status {
	case league.TradeAccepted, league.TradeInReview, league.TradeProcessed, league.TradeVetoed:
		return true
	case league.TradeFailed:
		return t.RespondedAt != nil
	}
	return false
}

// Visible reports whether a member whose team is teamID may see the trade:
// the two teams always, the rest of the league once it was accepted.
func Visible(t league.Trade, teamID string) bool {
	return t.OfferingTeamID == teamID || t.ReceivingTeamID == teamID || EverAccepted(t)
}

// Actions is what the member (userID, owner of teamID) may do with the
// trade now: nothing with a trade pending on FPL.
func Actions(t league.Trade, l *league.League, userID, teamID string) []string {
	out := []string{}
	if PendingOnFPL(t) {
		return out
	}
	if teamID != "" && t.Status == league.TradeOffered {
		if t.ReceivingTeamID == teamID {
			out = append(out, ActAccept, ActDecline)
		}
		if t.OfferingTeamID == teamID {
			out = append(out, ActWithdraw)
		}
	}
	if t.Status == league.TradeInReview {
		inTrade := t.OfferingTeamID == teamID || t.ReceivingTeamID == teamID
		if t.ReviewMode == league.ReviewLeagueVote && !inTrade && teamID != "" {
			out = append(out, ActVote)
		}
		// A commissioner cannot review a trade of their own team (ADR 0010).
		if l.CommissionerID == userID && userID != "" && !inTrade {
			out = append(out, ActReview)
		}
	}
	return out
}

// VotesNeeded is the number of veto votes that block a trade in a league
// vote: ceil(threshold × managers not in the trade), at least one.
func VotesNeeded(l *league.League, t league.Trade) int {
	eligible := 0
	for _, tm := range l.Teams {
		if tm.OwnerID != "" && tm.ID != t.OfferingTeamID && tm.ID != t.ReceivingTeamID {
			eligible++
		}
	}
	return max(1, int(math.Ceil(l.Settings.VetoThreshold*float64(eligible)-1e-9)))
}

// Tally counts the votes.
func Tally(t league.Trade) (veto, allow int) {
	for _, v := range t.Votes {
		if v == VoteVeto {
			veto++
		} else if v == VoteAllow {
			allow++
		}
	}
	return veto, allow
}

// View is trades as one member sees them.
type View struct {
	League *league.League
	World  *league.World
	Trades []league.Trade
	// UserID and TeamID are the viewer.
	UserID, TeamID string
}

// List returns the trades the actor may see, newest first.
func (s *Service) List(ctx context.Context, actor, leagueID string) (*View, error) {
	l, err := s.repo.Get(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	if l == nil {
		return nil, league.ErrLeagueNotFound
	}
	if err := league.Member(l, actor); err != nil {
		return nil, err
	}
	if l.Status != league.StatusInSeason && l.Status != league.StatusComplete {
		return nil, league.ErrSeasonNotStarted
	}
	w, err := league.SeasonWorld(ctx, s.stats)
	if err != nil {
		return nil, err
	}
	all, err := s.repo.Trades(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	t, _ := l.TeamOf(actor)
	v := &View{League: l, World: w, UserID: actor, TeamID: t.ID}
	for _, tr := range all {
		if Visible(tr, t.ID) {
			v.Trades = append(v.Trades, tr)
		}
	}
	return v, nil
}

// Get returns one trade the actor may see.
func (s *Service) Get(ctx context.Context, actor, leagueID, tradeID string) (*View, league.Trade, error) {
	v, err := s.List(ctx, actor, leagueID)
	if err != nil {
		return nil, league.Trade{}, err
	}
	for _, t := range v.Trades {
		if t.ID == tradeID {
			return v, t, nil
		}
	}
	return nil, league.Trade{}, notFound("Trade")
}

// change runs fn on the league's State in one transaction and reports the
// changes after commit. It returns the view of the trade fn names.
func (s *Service) change(ctx context.Context, actor, leagueID string,
	fn func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change, known []league.Trade) (string, error),
) (*View, league.Trade, error) {
	w, err := league.SeasonWorld(ctx, s.stats)
	if err != nil {
		return nil, league.Trade{}, err
	}
	// Every trade, read before the transaction (the memory store holds
	// one lock for both), so find can tell a closed trade from none.
	known, err := s.repo.Trades(ctx, leagueID)
	if err != nil {
		return nil, league.Trade{}, err
	}
	var (
		out     league.Trade
		l       *league.League
		changes []league.Change
	)
	err = s.repo.Tx(ctx, func(tx league.Tx) error {
		changes = nil
		st, err := tx.LoadState(ctx, leagueID)
		if err != nil {
			return err
		}
		if st == nil {
			return league.ErrLeagueNotFound
		}
		id, err := fn(tx, st, w, s.now(), &changes, known)
		if err != nil {
			return err
		}
		blocked := len(st.Block)
		if err := tx.SaveState(ctx, st); err != nil {
			return err
		}
		if len(st.Block) < blocked {
			// A player who left his team came off the trade block.
			changes = append(changes, league.Change{LeagueID: leagueID, Kind: league.ChangeTradeBlock})
		}
		if t := st.Trade(id); t != nil {
			out = t.Clone()
		}
		l = st.League
		return nil
	})
	if err != nil {
		return nil, league.Trade{}, err
	}
	s.notify.Notify(ctx, changes...)
	t, _ := l.TeamOf(actor)
	return &View{League: l, World: w, UserID: actor, TeamID: t.ID, Trades: []league.Trade{out}}, out, nil
}

// effectiveGW is the first gameweek a trade processed now counts for:
// the upcoming gameweek before its trade deadline (deadline − 48 h), the
// one after from then on.
func effectiveGW(w *league.World, now time.Time) int {
	gw := w.UpcomingGW(now)
	if g, ok := w.Gameweeks[gw]; ok && !now.Before(g.TradeDeadlineAt) {
		gw++
	}
	return gw
}

func inSeason(st *league.State, actor string) (league.Team, error) {
	t, err := league.OwnTeam(st.League, actor)
	if err != nil {
		return t, err
	}
	if st.League.Status != league.StatusInSeason {
		return t, league.ErrSeasonNotStarted
	}
	return t, nil
}

func positions(w *league.World, ids []string) string {
	var p []string
	for _, id := range ids {
		pl, ok := w.Players[id]
		if !ok {
			p = append(p, "?")
			continue
		}
		p = append(p, string(pl.Position))
	}
	slices.Sort(p)
	return strings.Join(p, ",")
}

// Propose offers players from the actor's team for players on another.
func (s *Service) Propose(ctx context.Context, actor, leagueID, receivingID string, offered, requested []string, message string) (*View, league.Trade, error) {
	return s.change(ctx, actor, leagueID, func(_ league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change, known []league.Trade) (string, error) {
		from, err := inSeason(st, actor)
		if err != nil {
			return "", err
		}
		to, ok := st.League.Team(receivingID)
		if !ok {
			return "", notFound("Team")
		}
		if to.ID == from.ID {
			return "", league.Errorf(league.KindInvalid, "trade-shape", "Invalid trade", "You cannot trade with yourself.",
				league.FieldError{Code: "same_team", Message: "Pick another team.", Field: "/receivingTeamId"})
		}
		gw := effectiveGW(w, now)
		if dl := st.League.Settings.TradeDeadlineGW; gw > 38 || (dl != nil && gw > *dl) {
			return "", league.Errorf(league.KindConflict, "trade-deadline", "Trade deadline passed", "This league's trade deadline has passed.")
		}
		var errs []league.FieldError
		if len([]rune(message)) > MaxMessage {
			errs = append(errs, league.FieldError{Code: "too_long", Message: "Keep the message to 280 characters.", Field: "/message"})
		}
		check := func(ids []string, team league.Team, field, whose string) {
			roster := st.Game.RosterAt(team.ID, gw)
			seen := map[string]bool{}
			for _, id := range ids {
				if seen[id] {
					errs = append(errs, league.FieldError{Code: "duplicate_player", Message: "A player appears twice.", Field: field, PlayerID: id})
				}
				seen[id] = true
				if !slices.Contains(roster, id) {
					errs = append(errs, league.FieldError{Code: "player_not_on_roster", Message: "Not on " + whose + ".", Field: field, PlayerID: id})
				}
			}
		}
		check(offered, from, "/offeredPlayerIds", "your team")
		check(requested, to, "/requestedPlayerIds", to.Name)
		if len(offered) == 0 || len(requested) == 0 || len(offered) > 5 || len(requested) > 5 || positions(w, offered) != positions(w, requested) {
			errs = append(errs, league.FieldError{Code: "position_mismatch",
				Message: "Both sides must trade the same number of players (1 to 5) in the same positions."})
		}
		if len(errs) > 0 {
			return "", league.Errorf(league.KindInvalid, "trade-shape", "Invalid trade", "That trade is not allowed.", errs...)
		}
		for _, id := range slices.Concat(offered, requested) {
			if st.InTradeReview(id) {
				return "", league.Errorf(league.KindConflict, "player-in-trade-review", "Player in a trade",
					w.Players[id].DisplayName+" is in a trade waiting to be processed.")
			}
		}
		if err := pendingTrade(st, w, now, slices.Concat(offered, requested)); err != nil {
			return "", err
		}
		c := waivers.CycleAt(st, w, now)
		expires := c.RunAt
		if c.RunGW > 38 {
			expires = w.Gameweeks[38].DeadlineAt.UTC()
		}
		t := league.Trade{
			ID: ids.New("trd"), OfferingTeamID: from.ID, ReceivingTeamID: to.ID,
			Offered: slices.Clone(offered), Requested: slices.Clone(requested), Status: league.TradeOffered,
			Message: message, CreatedAt: now, ExpiresAt: expires, Votes: map[string]string{},
		}
		st.Trades = append(st.Trades, t)
		*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeTrade, TradeID: t.ID})
		return t.ID, nil
	})
}

// pendingTrade is 409 player-in-trade when one of the players is in a
// processed trade that only counts from a later gameweek: he cannot be
// traded again until it does.
func pendingTrade(st *league.State, w *league.World, now time.Time, players []string) error {
	gw := w.UpcomingGW(now)
	for _, id := range players {
		if st.Game.PendingTrade(id, gw) {
			return league.Errorf(league.KindConflict, "player-in-trade", "Player in a trade",
				w.Players[id].DisplayName+" is in a trade that takes effect from next gameweek and cannot move until then.")
		}
	}
	return nil
}

// find returns the open trade in st, or not-found / 409 for a closed one.
func find(st *league.State, known []league.Trade, tradeID, teamID string) (*league.Trade, error) {
	if t := st.Trade(tradeID); t != nil && Visible(*t, teamID) {
		return t, nil
	}
	for _, t := range known {
		if t.ID == tradeID && Visible(t, teamID) {
			return nil, errNotOpen // loaded trades are the open ones
		}
	}
	return nil, notFound("Trade")
}

// Accept accepts an offer (receiving team).
func (s *Service) Accept(ctx context.Context, actor, leagueID, tradeID string) (*View, league.Trade, error) {
	return s.change(ctx, actor, leagueID, func(tx league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change, known []league.Trade) (string, error) {
		me, err := inSeason(st, actor)
		if err != nil {
			return "", err
		}
		t, err := find(st, known, tradeID, me.ID)
		if err != nil {
			return "", err
		}
		if t.ReceivingTeamID != me.ID {
			return "", league.Errorf(league.KindForbidden, "not-your-trade", "Not your trade", "Only the receiving team can accept.")
		}
		if t.Status != league.TradeOffered {
			return "", errNotOpen
		}
		if expired(t, now) {
			return "", errExpired
		}
		gw := effectiveGW(w, now)
		if dl := st.League.Settings.TradeDeadlineGW; gw > 38 || (dl != nil && gw > *dl) {
			return "", league.Errorf(league.KindConflict, "trade-deadline", "Trade deadline passed", "This league's trade deadline has passed.")
		}
		if reason := s.invalid(st, w, *t, gw); reason != "" {
			return "", league.Errorf(league.KindConflict, "player-no-longer-available", "Player no longer available",
				"A player in this trade has left his team.")
		}
		for _, id := range slices.Concat(t.Offered, t.Requested) {
			if st.InTradeReview(id) {
				return "", league.Errorf(league.KindConflict, "player-in-trade-review", "Player in a trade",
					w.Players[id].DisplayName+" is in another trade waiting to be processed.")
			}
		}
		if err := pendingTrade(st, w, now, slices.Concat(t.Offered, t.Requested)); err != nil {
			return "", err
		}
		at := now
		t.RespondedAt = &at
		mode := st.League.Settings.TradeReview
		cancelOthers(st, *t, ch)
		if mode == league.ReviewNone || mode == "" {
			s.execute(st, w, t, now, ch)
			return t.ID, nil
		}
		ends := now.Add(time.Duration(min(max(st.League.Settings.ReviewHours, 1), league.MaxReviewHours)) * time.Hour)
		t.Status, t.ReviewMode, t.ReviewEndsAt = league.TradeInReview, mode, &ends
		*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeTrade, TradeID: t.ID})
		return t.ID, tx.Schedule(ctx, ReviewArgs{LeagueID: st.League.ID, TradeID: t.ID, EndsAt: ends}, ends)
	})
}

// cancelOthers fails the other open offers that involve the players of t.
func cancelOthers(st *league.State, t league.Trade, ch *[]league.Change) {
	players := slices.Concat(t.Offered, t.Requested)
	for i := range st.Trades {
		o := &st.Trades[i]
		if o.ID == t.ID || o.Status != league.TradeOffered {
			continue
		}
		if slices.ContainsFunc(slices.Concat(o.Offered, o.Requested), func(id string) bool { return slices.Contains(players, id) }) {
			o.Status, o.FailureReason = league.TradeFailed, league.TradeFailNotOnRoster
			*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeTrade, TradeID: o.ID})
		}
	}
}

// invalid returns why t cannot execute for gameweek gw, or "".
func (s *Service) invalid(st *league.State, w *league.World, t league.Trade, gw int) string {
	from, to := st.Game.RosterAt(t.OfferingTeamID, gw), st.Game.RosterAt(t.ReceivingTeamID, gw)
	for _, id := range t.Offered {
		if !slices.Contains(from, id) {
			return league.TradeFailNotOnRoster
		}
	}
	for _, id := range t.Requested {
		if !slices.Contains(to, id) {
			return league.TradeFailNotOnRoster
		}
	}
	after := func(roster, out, in []string) map[string]scoring.Position {
		m := map[string]scoring.Position{}
		for _, id := range roster {
			if !slices.Contains(out, id) {
				m[id] = w.Players[id].Position
			}
		}
		for _, id := range in {
			m[id] = w.Players[id].Position
		}
		return m
	}
	if len(s.rules.ValidateSquad(after(from, t.Offered, t.Requested))) > 0 ||
		len(s.rules.ValidateSquad(after(to, t.Requested, t.Offered))) > 0 {
		return league.TradeFailRosterInvalid
	}
	return ""
}

// execute processes t now: re-checks it, moves the players from the
// effective gameweek on (swapping lineup slots like for like) and writes the
// feed entry; or fails it with a reason.
func (s *Service) execute(st *league.State, w *league.World, t *league.Trade, now time.Time, ch *[]league.Change) {
	at := now
	t.ProcessedAt = &at
	*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeTrade, TradeID: t.ID})
	gw := effectiveGW(w, now)
	if gw > 38 {
		t.Status, t.FailureReason = league.TradeFailed, league.TradeFailRosterInvalid
		return
	}
	if reason := s.invalid(st, w, *t, gw); reason != "" {
		t.Status, t.FailureReason = league.TradeFailed, reason
		return
	}
	// Pair players by position for the lineup swaps.
	requested := slices.Clone(t.Requested)
	for _, out := range t.Offered {
		i := slices.IndexFunc(requested, func(id string) bool { return w.Players[id].Position == w.Players[out].Position })
		in := requested[i]
		requested = slices.Delete(requested, i, i+1)
		st.Game.Release(t.OfferingTeamID, out, gw)
		st.Game.Release(t.ReceivingTeamID, in, gw)
		st.Game.Acquire(t.ReceivingTeamID, out, gw, "trade")
		st.Game.Acquire(t.OfferingTeamID, in, gw, "trade")
		st.Game.Swap(t.OfferingTeamID, out, in, gw, now)
		st.Game.Swap(t.ReceivingTeamID, in, out, gw, now)
	}
	t.Status, t.EffectiveGW = league.TradeProcessed, gw
	cancelOthers(st, *t, ch)
	st.NewTxns = append(st.NewTxns, tradeTxn(*t, gw, now))
}

func tradeTxn(t league.Trade, gw int, now time.Time) league.Txn {
	return league.Txn{
		ID: ids.New("txn"), Kind: league.TxnTrade, Gameweek: min(gw, 38), OccurredAt: now, TradeID: t.ID, Status: t.Status,
		OfferingTeamID: t.OfferingTeamID, ReceivingTeamID: t.ReceivingTeamID,
		Offered: slices.Clone(t.Offered), Requested: slices.Clone(t.Requested),
	}
}

func (s *Service) veto(st *league.State, w *league.World, t *league.Trade, now time.Time, ch *[]league.Change) {
	at := now
	t.Status, t.ProcessedAt = league.TradeVetoed, &at
	st.NewTxns = append(st.NewTxns, tradeTxn(*t, effectiveGW(w, now), now))
	*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeTrade, TradeID: t.ID})
}

// Decline turns an offer down (receiving team).
func (s *Service) Decline(ctx context.Context, actor, leagueID, tradeID string) (*View, league.Trade, error) {
	return s.respond(ctx, actor, leagueID, tradeID, false)
}

// Withdraw takes an offer back (offering team).
func (s *Service) Withdraw(ctx context.Context, actor, leagueID, tradeID string) (*View, league.Trade, error) {
	return s.respond(ctx, actor, leagueID, tradeID, true)
}

func (s *Service) respond(ctx context.Context, actor, leagueID, tradeID string, withdraw bool) (*View, league.Trade, error) {
	return s.change(ctx, actor, leagueID, func(_ league.Tx, st *league.State, _ *league.World, now time.Time, ch *[]league.Change, known []league.Trade) (string, error) {
		me, err := inSeason(st, actor)
		if err != nil {
			return "", err
		}
		t, err := find(st, known, tradeID, me.ID)
		if err != nil {
			return "", err
		}
		if withdraw && t.OfferingTeamID != me.ID {
			return "", league.Errorf(league.KindForbidden, "not-your-trade", "Not your trade", "Only the offering team can withdraw.")
		}
		if !withdraw && t.ReceivingTeamID != me.ID {
			return "", league.Errorf(league.KindForbidden, "not-your-trade", "Not your trade", "Only the receiving team can decline.")
		}
		if t.Status != league.TradeOffered {
			return "", errNotOpen
		}
		if expired(t, now) {
			return "", errExpired
		}
		if withdraw {
			t.Status = league.TradeWithdrawn
		} else {
			at := now
			t.Status, t.RespondedAt = league.TradeDeclined, &at
		}
		*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeTrade, TradeID: t.ID})
		return t.ID, nil
	})
}

// Vote records a manager's league-vote ballot; enough vetoes veto the trade.
func (s *Service) Vote(ctx context.Context, actor, leagueID, tradeID, vote string) (*View, league.Trade, error) {
	return s.change(ctx, actor, leagueID, func(_ league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change, known []league.Trade) (string, error) {
		me, err := inSeason(st, actor)
		if err != nil {
			return "", err
		}
		if vote != VoteVeto && vote != VoteAllow {
			return "", league.Errorf(league.KindInvalid, "invalid-request", "Invalid request", "vote must be veto or allow.")
		}
		t, err := find(st, known, tradeID, me.ID)
		if errorsIsNotOpen(err) {
			return "", errNotInReview
		}
		if err != nil {
			return "", err
		}
		if t.ReviewMode != league.ReviewLeagueVote && !(t.ReviewMode == "" && st.League.Settings.TradeReview == league.ReviewLeagueVote) {
			return "", league.Errorf(league.KindConflict, "no-league-vote", "No vote", "This trade is not decided by a league vote.")
		}
		if t.OfferingTeamID == me.ID || t.ReceivingTeamID == me.ID {
			return "", league.Errorf(league.KindForbidden, "in-trade", "In the trade", "Teams in the trade cannot vote on it.")
		}
		if t.Status != league.TradeInReview {
			return "", errNotInReview
		}
		if reviewOver(t, now) {
			return "", errReviewOver
		}
		if t.Votes == nil {
			t.Votes = map[string]string{}
		}
		t.Votes[me.ID] = vote
		*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeTrade, TradeID: t.ID})
		if veto, _ := Tally(*t); veto >= VotesNeeded(st.League, *t) {
			s.veto(st, w, t, now, ch)
		}
		return t.ID, nil
	})
}

func errorsIsNotOpen(err error) bool { return err == errNotOpen }

// Review is the commissioner's approve or veto while a trade is in review.
func (s *Service) Review(ctx context.Context, actor, leagueID, tradeID, decision, reason string) (*View, league.Trade, error) {
	return s.change(ctx, actor, leagueID, func(_ league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change, known []league.Trade) (string, error) {
		if err := league.Commissioner(st.League, actor); err != nil {
			return "", err
		}
		if st.League.Status != league.StatusInSeason {
			return "", league.ErrSeasonNotStarted
		}
		if decision != DecisionApprove && decision != DecisionVeto {
			return "", league.Errorf(league.KindInvalid, "invalid-request", "Invalid request", "decision must be approve or veto.")
		}
		if len([]rune(reason)) > MaxMessage {
			return "", league.Errorf(league.KindInvalid, "invalid-request", "Invalid request", "Keep the reason to 280 characters.")
		}
		me, _ := st.League.TeamOf(actor)
		t, err := find(st, known, tradeID, me.ID)
		if errorsIsNotOpen(err) {
			return "", errNotInReview
		}
		if err != nil {
			return "", err
		}
		if PendingOnFPL(*t) {
			return "", errOnFPL
		}
		if me.ID != "" && (t.OfferingTeamID == me.ID || t.ReceivingTeamID == me.ID) {
			return "", league.Errorf(league.KindForbidden, "own-trade", "Your own trade",
				"You cannot review a trade of your own team; the league's review settles it.")
		}
		if t.Status != league.TradeInReview {
			return "", errNotInReview
		}
		if reviewOver(t, now) {
			return "", errReviewOver
		}
		t.DecidedBy, t.DecisionReason = actor, reason
		if decision == DecisionVeto {
			s.veto(st, w, t, now, ch)
		} else {
			s.execute(st, w, t, now, ch)
		}
		return t.ID, nil
	})
}

// CloseDue settles the league's trades whose time is up (the review job
// and the tick): offers past their expiry expire; trades whose review
// window closed are processed (commissioner mode: approved by default;
// league vote: unless enough vetoes, which already vetoed them). It
// reports how many trades changed.
func (s *Service) CloseDue(ctx context.Context, leagueID string) (int, error) {
	st, err := s.repo.State(ctx, leagueID)
	if err != nil || st == nil || !s.due(st, s.now()) {
		return 0, err
	}
	n := 0
	_, _, err = s.change(ctx, "", leagueID, func(_ league.Tx, st *league.State, w *league.World, now time.Time, ch *[]league.Change, known []league.Trade) (string, error) {
		n = 0
		// Oldest first, so an earlier trade wins over a later one with
		// the same players.
		idx := make([]int, len(st.Trades))
		for i := range idx {
			idx[i] = i
		}
		slices.SortStableFunc(idx, func(a, b int) int {
			return cmp.Or(st.Trades[a].CreatedAt.Compare(st.Trades[b].CreatedAt), cmp.Compare(st.Trades[a].ID, st.Trades[b].ID))
		})
		for _, i := range idx {
			t := &st.Trades[i]
			switch {
			case expired(t, now):
				t.Status = league.TradeExpired
				*ch = append(*ch, league.Change{LeagueID: st.League.ID, Kind: league.ChangeTrade, TradeID: t.ID})
				n++
			case reviewOver(t, now):
				if veto, _ := Tally(*t); t.ReviewMode == league.ReviewLeagueVote && veto >= VotesNeeded(st.League, *t) {
					s.veto(st, w, t, now, ch)
				} else {
					s.execute(st, w, t, now, ch)
				}
				n++
			}
		}
		return "", nil
	})
	return n, err
}

func (s *Service) due(st *league.State, now time.Time) bool {
	for i := range st.Trades {
		if expired(&st.Trades[i], now) || reviewOver(&st.Trades[i], now) {
			return true
		}
	}
	return false
}
