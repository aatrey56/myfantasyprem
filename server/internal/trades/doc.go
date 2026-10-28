// Package trades runs trade offers between two teams of a league (ADR 0008
// rules 13 and 14, ADR 0010): propose (equal numbers of players with
// matching positions, so both squads stay 2/5/5/3), accept, decline,
// withdraw, expire at the next waiver run, and review by the league's
// setting: none processes on acceptance; commissioner waits for the
// commissioner's decision and approves when the window closes; league_vote
// vetoes once veto votes reach ceil(vetoThreshold × managers not in the
// trade) and otherwise processes when the window closes. Processing
// re-checks that every player is still on his team and that both squads
// stay legal, else the trade fails with a reason. Accepting or processing a
// trade fails the other open offers that involve the same players.
//
// Every change is one transaction on the league's State; the review window
// is a job scheduled in the same transaction and the periodic tick catches
// any window that closed while no job ran. Depends on league, scoring,
// stats and waivers, never on net/http.
package trades
