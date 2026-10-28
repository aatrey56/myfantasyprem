package notifications

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/draft"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// ownerOf returns the user owning teamID ("" for an open seat or none).
func ownerOf(l *league.League, teamID string) string {
	for _, t := range l.Teams {
		if t.ID == teamID {
			return t.OwnerID
		}
	}
	return ""
}

func teamName(l *league.League, teamID string) string {
	for _, t := range l.Teams {
		if t.ID == teamID {
			return t.Name
		}
	}
	return "A team"
}

// Names looks up a player's display name.
type Names func(playerID string) string

func list(ids []string, name Names) string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, name(id))
	}
	if len(out) == 0 {
		return "nothing"
	}
	return strings.Join(out, ", ")
}

// TradeEvents returns the notifications a trade's current status calls
// for. Each status has its own key, so hearing the same trade twice sends
// nothing new.
func TradeEvents(l *league.League, tr league.Trade, name Names) []Notification {
	from, to := ownerOf(l, tr.OfferingTeamID), ownerOf(l, tr.ReceivingTeamID)
	url := "/leagues/" + l.ID + "/players/trades"
	summary := fmt.Sprintf("%s for %s", list(tr.Offered, name), list(tr.Requested, name))
	var out []Notification
	add := func(user string, t Type, title, body string) {
		if user == "" {
			return
		}
		out = append(out, Notification{UserID: user, LeagueID: l.ID, Type: t, Title: title, Body: body, URL: url,
			Key: fmt.Sprintf("trade:%s:%s", tr.ID, t)})
	}
	switch tr.Status {
	case league.TradeOffered:
		add(to, TradeOffered, "New trade offer", teamName(l, tr.OfferingTeamID)+" offers "+summary+".")
	case league.TradeAccepted, league.TradeInReview:
		add(from, TradeAccepted, "Trade accepted", teamName(l, tr.ReceivingTeamID)+" accepted "+summary+".")
	case league.TradeDeclined:
		add(from, TradeDeclined, "Trade declined", teamName(l, tr.ReceivingTeamID)+" declined "+summary+".")
	case league.TradeVetoed:
		// Accepted, then vetoed: the proposer hears both (once each).
		add(from, TradeAccepted, "Trade accepted", teamName(l, tr.ReceivingTeamID)+" accepted "+summary+".")
		add(from, TradeReviewed, "Trade vetoed", "Your trade ("+summary+") was vetoed.")
		add(to, TradeReviewed, "Trade vetoed", "Your trade ("+summary+") was vetoed.")
	case league.TradeProcessed:
		// A trade with no review goes from offered to processed at once.
		add(from, TradeAccepted, "Trade accepted", teamName(l, tr.ReceivingTeamID)+" accepted "+summary+".")
		if tr.ReviewMode != "" {
			add(from, TradeReviewed, "Trade approved", "Your trade ("+summary+") passed review.")
			add(to, TradeReviewed, "Trade approved", "Your trade ("+summary+") passed review.")
		}
		add(from, TradeProcessed, "Trade processed", "Your trade went through: "+summary+".")
		add(to, TradeProcessed, "Trade processed", "Your trade went through: "+summary+".")
		body := fmt.Sprintf("%s and %s traded: %s.", teamName(l, tr.OfferingTeamID), teamName(l, tr.ReceivingTeamID), summary)
		for _, t := range l.Teams {
			if t.OwnerID != "" && t.OwnerID != from && t.OwnerID != to {
				add(t.OwnerID, LeagueTradeProcessed, "Trade in your league", body)
			}
		}
	}
	return out
}

var claimReasons = map[string]string{
	league.ClaimInUnavailable:  "the player was already taken",
	league.ClaimOutUnavailable: "the player to drop was no longer on your roster",
	league.ClaimRosterLocked:   "the player to drop had already played",
}

// WaiverEvents returns the results of the gameweek's claims to their owners.
func WaiverEvents(l *league.League, claims []league.Claim, gw int, name Names) []Notification {
	var out []Notification
	for _, c := range claims {
		if c.Gameweek != gw || (c.Status != league.ClaimWon && c.Status != league.ClaimLost) {
			continue
		}
		user := ownerOf(l, c.TeamID)
		if user == "" {
			continue
		}
		n := Notification{UserID: user, LeagueID: l.ID, Type: WaiverResult, URL: "/leagues/" + l.ID + "/players/waivers",
			Key: "claim:" + c.ID}
		if c.Status == league.ClaimWon {
			n.Title = "Waiver claim won"
			n.Body = "You got " + name(c.AddPlayerID) + "."
			if c.DropPlayerID != "" {
				n.Body = fmt.Sprintf("You got %s and dropped %s.", name(c.AddPlayerID), name(c.DropPlayerID))
			}
		} else {
			why := claimReasons[c.FailureReason]
			if why == "" {
				why = "a team ahead of you in the waiver order claimed him"
			}
			n.Title = "Waiver claim lost"
			n.Body = fmt.Sprintf("You missed out on %s: %s.", name(c.AddPlayerID), why)
		}
		out = append(out, n)
	}
	return out
}

// PickEvent tells the owner on the clock that their pick is up.
func PickEvent(l *league.League, d *league.Draft) []Notification {
	if d == nil || d.Status != league.DraftInProgress {
		return nil
	}
	n := len(d.Picks) + 1
	team, round, inRound := draft.Slot(d.Order, n)
	user := ownerOf(l, team)
	if user == "" || d.Autopick[team] {
		return nil
	}
	return []Notification{{UserID: user, LeagueID: l.ID, Type: DraftYourPick, Title: "You're on the clock",
		Body: fmt.Sprintf("Pick %d.%d in %s is yours.", round, inRound, l.Name), URL: "/leagues/" + l.ID + "/draft",
		Key: fmt.Sprintf("pick:%s:%d", l.ID, n)}}
}

// Lead is how long before a deadline its reminder goes out.
const Lead = time.Hour

// within reports whether at is in (now, now+Lead].
func within(at, now time.Time) bool { return !at.IsZero() && at.After(now) && !at.After(now.Add(Lead)) }

// DeadlineEvents returns the reminders due at now: the draft starting,
// waivers due, free agency closing (the gameweek deadline, when free
// agency is open) and a lineup that will lock with an empty or invalid
// XI. Keys name the deadline, so a tick every few seconds sends each once.
func DeadlineEvents(st *league.State, w *league.World, now time.Time) []Notification {
	l := st.League
	var out []Notification
	all := func(t Type, key, title, body, url string, only func(teamID string) bool) {
		for _, tm := range l.Teams {
			if tm.OwnerID != "" && (only == nil || only(tm.ID)) {
				out = append(out, Notification{UserID: tm.OwnerID, LeagueID: l.ID, Type: t, Title: title, Body: body,
					URL: url, Key: key})
			}
		}
	}
	base := "/leagues/" + l.ID
	switch l.Status {
	case league.StatusPreDraft:
		if at := l.Settings.DraftStartsAt; at != nil && within(*at, now) {
			all(DraftStarting, fmt.Sprintf("draftstart:%s:%d", l.ID, at.Unix()), "Draft starts soon",
				l.Name+"'s draft starts within the hour.", base+"/draft", nil)
		}
		return out
	case league.StatusInSeason:
	default:
		return out
	}
	gw := w.UpcomingGW(now)
	g, ok := w.Gameweeks[gw]
	if !ok {
		return out
	}
	if _, ran := st.Ran(league.RunWaivers, gw); !ran && within(g.WaiverDeadlineAt, now) {
		all(WaiversDue, fmt.Sprintf("waivers:%s:%d", l.ID, gw), "Waivers run in 1 hour",
			fmt.Sprintf("Get your %s claims in.", g.Name), base+"/players/waivers", nil)
	}
	if within(g.DeadlineAt, now) {
		if _, ran := st.Ran(league.RunWaivers, gw); ran {
			all(FreeAgencyClosing, fmt.Sprintf("fa:%s:%d", l.ID, gw), "Free agency closes in 1 hour",
				fmt.Sprintf("Free agent moves for %s close at the deadline.", g.Name), base+"/players", nil)
		}
		all(LineupInvalid, fmt.Sprintf("lineup:%s:%d", l.ID, gw), "Fix your lineup",
			fmt.Sprintf("Your %s lineup locks in 1 hour and its starting XI is incomplete.", g.Name), base+"/team",
			func(teamID string) bool { return !validXI(st.Game, teamID, gw) })
	}
	return out
}

// validXI reports whether the team has eleven starters, all on its roster
// for gw. (Formation rules are enforced when a lineup is saved.)
func validXI(g *league.Game, teamID string, gw int) bool {
	lu, ok := g.LineupAt(teamID, gw)
	if !ok || len(lu.Slots) < 11 {
		return false
	}
	roster := map[string]bool{}
	for _, p := range g.RosterAt(teamID, gw) {
		roster[p] = true
	}
	for _, p := range lu.Slots[:11] {
		if p == "" || !roster[p] {
			return false
		}
	}
	return true
}

// Changes is a league.Notifier that turns committed changes into
// notifications and passes every change on to Next (the realtime feed).
type Changes struct {
	Next     league.Notifier
	Leagues  league.Repo
	Stats    stats.Repo
	Notifier *Notifier
	Log      *slog.Logger
}

var _ league.Notifier = (*Changes)(nil)

func (c *Changes) Notify(ctx context.Context, changes ...league.Change) {
	if c.Next != nil {
		c.Next.Notify(ctx, changes...)
	}
	// A draft cascade reports dozens of picks in one call: only who is on
	// the clock afterwards matters, so read each league's draft once.
	drafts := map[string]bool{}
	for _, ch := range changes {
		if ch.Kind == league.ChangePick || ch.Kind == league.ChangeDraft {
			if drafts[ch.LeagueID] {
				continue
			}
			drafts[ch.LeagueID] = true
		}
		if err := c.notify(ctx, ch); err != nil && c.Log != nil {
			c.Log.ErrorContext(ctx, "notifications", "league", ch.LeagueID, "kind", ch.Kind, "err", err)
		}
	}
}

func (c *Changes) names(ctx context.Context) Names {
	w, err := league.SeasonWorld(ctx, c.Stats)
	return func(id string) string {
		if err == nil {
			if p, ok := w.Players[id]; ok && p.DisplayName != "" {
				return p.DisplayName
			}
		}
		return "a player"
	}
}

func (c *Changes) notify(ctx context.Context, ch league.Change) error {
	switch ch.Kind {
	case league.ChangeTrade, league.ChangeWaivers, league.ChangePick, league.ChangeDraft:
	default:
		return nil
	}
	st, err := c.Leagues.State(ctx, ch.LeagueID)
	if err != nil || st == nil || st.League == nil {
		return err
	}
	var ns []Notification
	switch ch.Kind {
	case league.ChangeTrade:
		for _, tr := range st.Trades {
			if tr.ID == ch.TradeID {
				ns = TradeEvents(st.League, tr, c.names(ctx))
			}
		}
	case league.ChangeWaivers:
		ns = WaiverEvents(st.League, st.Claims, ch.Gameweek, c.names(ctx))
	case league.ChangePick, league.ChangeDraft:
		ns = PickEvent(st.League, st.Draft)
	}
	_, err = c.Notifier.Send(ctx, ns...)
	return err
}

// Deadlines sends the deadline reminders due for one league.
func (c *Changes) Deadlines(ctx context.Context, leagueID string, now time.Time) error {
	st, err := c.Leagues.State(ctx, leagueID)
	if err != nil || st == nil || st.League == nil {
		return err
	}
	w, err := league.SeasonWorld(ctx, c.Stats)
	if err != nil {
		return err
	}
	_, err = c.Notifier.Send(ctx, DeadlineEvents(st, w, now)...)
	return err
}
