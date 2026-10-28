// Package tags keeps each member's personal tags on players in a league:
// one tag per player from a fixed set (no free text), private to their
// user, like the watchlist. The API checks membership and that the player
// exists.
package tags

import (
	"context"
	"errors"
	"slices"
	"time"
)

// The tags, in display order.
const (
	Hot    = "hot"    // 🔥 in form
	Cold   = "cold"   // 🧊 out of form
	Target = "target" // 🎯 want him
	Sell   = "sell"   // 💰 move him on
	Avoid  = "avoid"  // 🛑 stay away
)

// All lists the tags in display order.
var All = []string{Hot, Cold, Target, Sell, Avoid}

// Valid reports whether tag is one of All.
func Valid(tag string) bool { return slices.Contains(All, tag) }

// ErrUnknownTag is Set with a tag not in All.
var ErrUnknownTag = errors.New("tags: unknown tag")

// Entry is one tagged player.
type Entry struct {
	PlayerID  string
	Tag       string
	UpdatedAt time.Time
}

// Store keeps tags by user and league.
type Store interface {
	// List returns the user's tags in the league, newest first.
	List(ctx context.Context, userID, leagueID string) ([]Entry, error)
	// Set tags the player, replacing any tag he had; a tag not in All is
	// ErrUnknownTag. Setting the tag he already has keeps its time.
	Set(ctx context.Context, userID, leagueID, playerID, tag string) (Entry, error)
	// Remove untags the player; one without a tag is no error.
	Remove(ctx context.Context, userID, leagueID, playerID string) error
}

// newestFirst orders entries newest first, then by player id.
func newestFirst(a, b Entry) int {
	if c := b.UpdatedAt.Compare(a.UpdatedAt); c != 0 {
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
