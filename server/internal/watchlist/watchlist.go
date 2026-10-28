// Package watchlist keeps each member's watchlist in a league: the players
// they starred on the Players tab or a player card. Entries are private to
// their user. The API checks membership and that the player exists.
package watchlist

import (
	"context"
	"errors"
	"time"
)

// Max is how many players one watchlist holds.
const Max = 200

// ErrFull is Add past Max players.
var ErrFull = errors.New("watchlist: full")

// Entry is one watched player.
type Entry struct {
	PlayerID string
	AddedAt  time.Time
}

// Store keeps watchlists by user and league.
type Store interface {
	// List returns the user's watchlist in the league, newest first.
	List(ctx context.Context, userID, leagueID string) ([]Entry, error)
	// Add watches the player. Watching a player already on the list
	// returns the existing entry; a new one past Max is ErrFull.
	Add(ctx context.Context, userID, leagueID, playerID string) (Entry, error)
	// Remove unwatches the player; removing one not on the list is no error.
	Remove(ctx context.Context, userID, leagueID, playerID string) error
}

// newestFirst orders entries newest first, then by player id.
func newestFirst(a, b Entry) int {
	if c := b.AddedAt.Compare(a.AddedAt); c != 0 {
		return c
	}
	switch {
	case a.PlayerID < b.PlayerID:
		return -1
	case a.PlayerID > b.PlayerID:
		return 1
	}
	return 0
}
