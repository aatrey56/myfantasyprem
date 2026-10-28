package notifications

import (
	"fmt"
)

// PlayerMatch is one player's stats in one fixture after a poll.
type PlayerMatch struct {
	PlayerID  string
	FixtureID string
	Name      string // the player's short name, for the message
	Position  string // GKP, DEF, MID or FWD
	Goals     int
	Assists   int
	OwnGoals  int
	RedCards  int
	Minutes   int
	// GoalsConceded counts only goals conceded while the player was on the
	// pitch, as FPL does, so a goal after he went off does not count.
	GoalsConceded int
	// DCPoints are the points from defensive contributions: zero until the
	// player reaches his position's threshold in the match.
	DCPoints int
	// Off is true once the player has left the pitch (subbed or sent off).
	Off bool
	// Finished is true at full time.
	Finished bool
}

// Audience is who hears about a player in one league in a gameweek: the
// owner of the team holding him, and that team's opponent (either may be
// empty, e.g. an open seat or a bye).
type Audience struct {
	LeagueID   string
	OwnerID    string
	OpponentID string
}

// CleanSheet reports whether the player has a clean sheet to announce: a
// goalkeeper, defender or midfielder who played 60 minutes or more without
// conceding while on the pitch, once the match is over or he has gone off.
func CleanSheet(p PlayerMatch) bool {
	return p.Position != "FWD" && p.Minutes >= 60 && p.GoalsConceded == 0 && (p.Finished || p.Off)
}

type liveKind struct {
	name            string
	owner, opponent Type
}

var (
	kGoal = liveKind{"goal", PlayerGoal, OpponentGoal}
	kAst  = liveKind{"assist", PlayerAssist, OpponentAssist}
	kOG   = liveKind{"own_goal", PlayerOwnGoal, OpponentOwnGoal}
	kRed  = liveKind{"red", PlayerRedCard, OpponentRedCard}
	kDC   = liveKind{"dc", PlayerDC, OpponentDC}
	kCS   = liveKind{"clean_sheet", PlayerCleanSheet, OpponentCleanSheet}
)

// LiveEvents returns the notifications the player's stats call for. Each
// counted event gets a key from its ordinal (the second goal is goal 2),
// so a re-poll, or a correction that removes a goal and gives it back,
// sends nothing new, and a correction that removes one sends nothing at
// all. Red cards, defensive contribution points and clean sheets are
// once per match.
func LiveEvents(p PlayerMatch, aud []Audience) []Notification {
	var out []Notification
	emit := func(k liveKind, n int, title, body string) {
		for _, a := range aud {
			for _, r := range []struct {
				user string
				t    Type
				who  string
			}{{a.OwnerID, k.owner, "Your player"}, {a.OpponentID, k.opponent, "Your opponent's"}} {
				if r.user == "" {
					continue
				}
				out = append(out, Notification{
					UserID: r.user, LeagueID: a.LeagueID, Type: r.t,
					Title: title, Body: r.who + " " + body,
					URL: "/leagues/" + a.LeagueID + "/matchup",
					Key: fmt.Sprintf("live:%s:%s:%s:%s:%d", a.LeagueID, p.FixtureID, p.PlayerID, k.name, n),
				})
			}
		}
	}
	for i := 1; i <= p.Goals; i++ {
		emit(kGoal, i, "⚽ Goal: "+p.Name, p.Name+" scored.")
	}
	for i := 1; i <= p.Assists; i++ {
		emit(kAst, i, "🅰️ Assist: "+p.Name, p.Name+" got an assist.")
	}
	for i := 1; i <= p.OwnGoals; i++ {
		emit(kOG, i, "Own goal: "+p.Name, p.Name+" scored an own goal.")
	}
	if p.RedCards > 0 {
		emit(kRed, 1, "🟥 Red card: "+p.Name, p.Name+" was sent off.")
	}
	if p.DCPoints > 0 {
		emit(kDC, 1, "🛡️ Defensive contribution: "+p.Name,
			fmt.Sprintf("%s earned %d defensive contribution points.", p.Name, p.DCPoints))
	}
	if CleanSheet(p) {
		emit(kCS, 1, "🧤 Clean sheet: "+p.Name, p.Name+" kept a clean sheet.")
	}
	return out
}
