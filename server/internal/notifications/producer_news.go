package notifications

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// NewsChanged returns the players whose availability or news text moved
// between two ingests. A player new in after is not a change.
func NewsChanged(before, after map[string]stats.Player) []stats.Player {
	var out []stats.Player
	for id, a := range after {
		b, ok := before[id]
		if ok && (a.Availability != b.Availability || a.News != b.News) {
			out = append(out, a)
		}
	}
	return out
}

// NewsEvents tells each player's owner, and the owner's opponent in gw,
// about his new status. The key holds the news itself, so the same news
// heard again sends nothing.
func NewsEvents(l *league.League, g *league.Game, gw int, players []stats.Player) []Notification {
	var out []Notification
	for _, p := range players {
		sum := sha256.Sum256([]byte(p.Availability + "\x00" + p.News))
		key := "news:" + l.ID + ":" + p.ID + ":" + hex.EncodeToString(sum[:8])
		body := p.News
		if body == "" {
			body = "Available again."
		}
		for _, a := range Audiences(l, g, gw, p.ID) {
			if a.OwnerID != "" {
				out = append(out, Notification{UserID: a.OwnerID, LeagueID: l.ID, Type: PlayerNews,
					Title: "News: " + p.DisplayName, Body: body, URL: "/leagues/" + l.ID + "/players/" + p.ID, Key: key})
			}
			if a.OpponentID != "" {
				out = append(out, Notification{UserID: a.OpponentID, LeagueID: l.ID, Type: OpponentNews,
					Title: "Opponent's player: " + p.DisplayName, Body: body, URL: "/leagues/" + l.ID + "/players/" + p.ID, Key: key})
			}
		}
	}
	return out
}

// News sends player news to every in-season league (poller.NewsHook).
type News struct {
	Leagues  league.Repo
	Notifier *Notifier
	Log      *slog.Logger
}

// PlayerNews compares two ingests' players and notifies the changes.
func (n *News) PlayerNews(ctx context.Context, before, after map[string]stats.Player, gw int) {
	changed := NewsChanged(before, after)
	if len(changed) == 0 {
		return
	}
	ids, err := n.Leagues.InSeasonLeagues(ctx)
	for _, id := range ids {
		var st *league.State
		if st, err = n.Leagues.State(ctx, id); err != nil {
			break
		}
		if st == nil || st.League == nil || st.Game == nil {
			continue
		}
		if _, err = n.Notifier.Send(ctx, NewsEvents(st.League, st.Game, gw, changed)...); err != nil {
			break
		}
	}
	if err != nil && n.Log != nil {
		n.Log.ErrorContext(ctx, "news notifications", "err", err)
	}
}
