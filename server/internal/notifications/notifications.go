// Package notifications turns domain events (trades, waivers, deadlines,
// live stats, player news, the draft) into notifications for users.
//
// Every notification lands in the user's inbox (the FEED tab). A push copy
// goes to the user's registered devices only when their preference for that
// league and type is on; every type is on by default. Producers give each
// notification a dedupe key, unique per user, so a re-poll, a retried job
// or a score correction never sends one twice; Store.Add ignores a key it
// has seen. Pushes queue in an outbox that Dispatcher drains with retries.
package notifications

import (
	"context"
	"errors"
	"time"
)

// Type names a kind of notification (an open enum in the API).
type Type string

// Group is where a type's switch sits in the settings: personal or league.
type Group string

const (
	Personal Group = "personal"
	League   Group = "league"
)

const (
	TradeOffered   Type = "trade_offered"
	TradeAccepted  Type = "trade_accepted"
	TradeDeclined  Type = "trade_declined"
	TradeReviewed  Type = "trade_reviewed"
	TradeProcessed Type = "trade_processed"

	WaiversDue        Type = "waivers_due"
	FreeAgencyClosing Type = "free_agency_closing"
	WaiverResult      Type = "waiver_result"

	PlayerGoal       Type = "player_goal"
	PlayerAssist     Type = "player_assist"
	PlayerOwnGoal    Type = "player_own_goal"
	PlayerRedCard    Type = "player_red_card"
	PlayerDC         Type = "player_defensive_contribution"
	PlayerCleanSheet Type = "player_clean_sheet"
	PlayerNews       Type = "player_news"

	LeagueTradeProcessed Type = "league_trade_processed"
	OpponentGoal         Type = "opponent_goal"
	OpponentAssist       Type = "opponent_assist"
	OpponentOwnGoal      Type = "opponent_own_goal"
	OpponentRedCard      Type = "opponent_red_card"
	OpponentDC           Type = "opponent_defensive_contribution"
	OpponentCleanSheet   Type = "opponent_clean_sheet"
	OpponentNews         Type = "opponent_news"

	// Extras beyond the owner's list.
	LineupInvalid Type = "lineup_invalid"
	DraftStarting Type = "draft_starting"
	DraftYourPick Type = "draft_your_pick"

	// FPLCheckMismatch goes to an imported league's commissioner when a
	// shadow check finds finalized data that differs from FPL (package
	// mirror). Not in the Catalog: it has no switch.
	FPLCheckMismatch Type = "fpl_check_mismatch"
)

// Info describes a type for the settings screen.
type Info struct {
	Type  Type
	Group Group
	Label string
}

// Catalog lists every type in the order the settings show them.
var Catalog = []Info{
	{TradeOffered, Personal, "Trade offered to me"},
	{TradeAccepted, Personal, "My trade accepted"},
	{TradeDeclined, Personal, "My trade declined"},
	{TradeReviewed, Personal, "My trade approved or vetoed"},
	{TradeProcessed, Personal, "My trade processed"},
	{WaiversDue, Personal, "Waivers due in 1 hour"},
	{FreeAgencyClosing, Personal, "Free agency closes in 1 hour"},
	{WaiverResult, Personal, "My waiver results"},
	{LineupInvalid, Personal, "Lineup locks in 1 hour with an invalid XI"},
	{DraftStarting, Personal, "Draft starting"},
	{DraftYourPick, Personal, "My pick is up"},
	{PlayerGoal, Personal, "My players: goal"},
	{PlayerAssist, Personal, "My players: assist"},
	{PlayerOwnGoal, Personal, "My players: own goal"},
	{PlayerRedCard, Personal, "My players: red card"},
	{PlayerDC, Personal, "My players: defensive contribution points"},
	{PlayerCleanSheet, Personal, "My players: clean sheet"},
	{PlayerNews, Personal, "My players: injury and status news"},
	{LeagueTradeProcessed, League, "Any trade processed in my league"},
	{OpponentGoal, League, "Opponent's players: goal"},
	{OpponentAssist, League, "Opponent's players: assist"},
	{OpponentOwnGoal, League, "Opponent's players: own goal"},
	{OpponentRedCard, League, "Opponent's players: red card"},
	{OpponentDC, League, "Opponent's players: defensive contribution points"},
	{OpponentCleanSheet, League, "Opponent's players: clean sheet"},
	{OpponentNews, League, "Opponent's players: injury and status news"},
}

// Known reports whether t is in the Catalog.
func Known(t Type) bool {
	for _, i := range Catalog {
		if i.Type == t {
			return true
		}
	}
	return false
}

// Notification is one inbox item.
type Notification struct {
	ID       string // ntf_...
	UserID   string
	LeagueID string // empty for none
	Type     Type
	Title    string
	Body     string
	URL      string
	// Key is unique per user: a second Add with it is ignored.
	Key       string
	CreatedAt time.Time
	ReadAt    *time.Time
}

// Subscription is one device registered for push.
type Subscription struct {
	ID        string // psub_...
	UserID    string
	Kind      string // webpush or expo
	Endpoint  string // webpush endpoint, or the Expo token
	P256dh    string
	Auth      string
	CreatedAt time.Time
}

// Delivery is one queued push of a notification to one subscription.
type Delivery struct {
	ID           int64
	Notification Notification
	Subscription Subscription
	Attempts     int
}

// Kinds of subscription.
const (
	KindWebPush = "webpush"
	KindExpo    = "expo"
)

// ErrNotFound is a subscription that is not the user's.
var ErrNotFound = errors.New("notifications: not found")

// Store keeps preferences, the inbox, subscriptions and the push outbox.
type Store interface {
	// Disabled returns the types the user turned off in the league.
	Disabled(ctx context.Context, userID, leagueID string) (map[Type]bool, error)
	// SetPreferences applies all (when non-nil: true clears every
	// switch to on, false turns every Catalog type off), then settings.
	SetPreferences(ctx context.Context, userID, leagueID string, all *bool, settings map[Type]bool) error

	// Add stores n in the inbox unless the user already has n.Key, and
	// when push is true queues a delivery to each of the user's
	// subscriptions. It reports whether n was new; n.ID and CreatedAt
	// are set by the store.
	Add(ctx context.Context, n Notification, push bool) (Notification, bool, error)
	// List returns up to limit inbox items, newest first, and the unread count.
	List(ctx context.Context, userID string, limit int, unreadOnly bool) ([]Notification, int, error)
	// MarkRead marks the ids (or every item when all) read.
	MarkRead(ctx context.Context, userID string, ids []string, all bool) error

	// Subscribe registers sub; an endpoint seen before returns the stored one.
	Subscribe(ctx context.Context, sub Subscription) (Subscription, error)
	// Unsubscribe removes the user's subscription; a missing one is no error.
	Unsubscribe(ctx context.Context, userID, id string) error
	// Drop removes a subscription the push service called gone, with its deliveries.
	Drop(ctx context.Context, id string) error

	// Due claims up to limit deliveries due at now, pushing each one's
	// next attempt to lease from now so a second dispatcher skips it.
	Due(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]Delivery, error)
	// Delivered removes a delivery that went out.
	Delivered(ctx context.Context, id int64) error
	// Retry records a failed attempt and the next one at next; a zero
	// next gives up and removes the delivery.
	Retry(ctx context.Context, id int64, next time.Time) error
}
