# 0014: Projections from real points (fixture difficulty, Expected, win odds, start/sit)

Status: accepted, 2026-10-16

Update 2026-10-17 (owner): the app hides Expected points and win chances
for now. One build-time switch, `EXPO_PUBLIC_SHOW_PROJECTIONS` (off unless
`true`), gates them: off, the app never requests
`/v1/leagues/{id}/gameweeks/{gw}/projections`, so no Expected (team card,
lineup and roster rows) and no win chance (matchup cards and screen) shows.
The endpoint and `server/internal/projection` stay. Fixture difficulty and
start/sit accuracy are not projections and still show. Form, which Expected
uses, is now the mean of the last two completed gameweeks.

## Context

Sleeper shows a projection next to every player, a win probability on
every matchup and a coloured difficulty chip on every fixture. We have no
licensed projection feed and the owner does not want invented numbers:
every forward-looking figure must be arithmetic on real past points, say
what it is, and be reproducible by hand.

## Decision

The rules live in `server/internal/projection` (pure, no I/O); `httpapi`
gathers the inputs from the world and the league. Three read endpoints,
computed on every request from tier-1 stats and the league state (nothing
new is stored):

- `GET /v1/fixture-difficulty`
- `GET /v1/leagues/{leagueId}/gameweeks/{gw}/projections`
- `GET /v1/leagues/{leagueId}/start-sit`

### Windows

- **Form gameweek** as everywhere else (`statuspage.FormGameweek`): the
  season's current gameweek while it is played, the next once finished.
  For a projection of gameweek _g_ it is min(_g_, form gameweek), so a past
  gameweek uses the form it had then and a future one uses today's.
- **Recent gameweeks**: the last six _completed_ gameweeks before the form
  gameweek (fewer early in the season, none in gameweek 1).

### Fixture difficulty

For each position and each club: the points players of that position
scored against the club in its finished matches of the recent gameweeks,
divided by the club's number of those matches (so a club with a double or
a blank in the window compares fairly). Clubs are ranked by that per-match
figure, fewest first: **1st = hardest, 20th = easiest**. Clubs with the
same per-match figure share the better rank (1, 2, 2, 4). A club with no
match in the window is unranked. A player who has since changed club is
matched to neither side of an old fixture and left out.

The app colours a chip by rank: 1-4 dark red, 5-8 red, 9-12 grey, 13-16
light green, 17-20 green, and prints the ordinal ("(18th)").

### Expected points

    factor(rank) = 0.8 + 0.4 × (rank − 1) / 19     (1 for an unranked opponent)
    Expected     = form × Σ factor over the player's matches in the gameweek

So 0.80 against the hardest opponent, 1.00 between 10th and 11th, 1.20
against the easiest. A blank gameweek is 0; a double sums both factors.
Rounded to one decimal, halves up, as form is. Form is the contract's form
(mean of the last two completed gameweeks before the form gameweek; a
gameweek without minutes counts 0; divisor 2, or the number completed
early in the season). The app labels it "Exp" with "2-GW form × fixture"
in its explanation.

A team's Expected is the sum of its starting XI's Expected. **FPL's
auto-sub logic is not applied**: a starter who will not play still counts
his Expected and the bench never does. Before the deadline another team's
XI is private, so its last locked lineup stands in (`lineupBasis:
last_locked`, the order `GET /teams/{teamId}` already shows).

### Win odds

Monte Carlo over real scores, 10,000 plays. In each play every starter
(same XI as Expected, no auto-subs) scores his points so far this gameweek
plus, for each of his club's matches that has not kicked off, one of his
own gameweek totals from the recent gameweeks picked uniformly at random
(a gameweek he did not play is a real 0). Before kickoff that resamples
the whole XI. A match in progress counts his points so far plus the rest
of the match: one more such total × max(0, 90 − minute) / 90, rounded to
the nearest point with halves away from zero (half time: 6 → 3, 3 → 2,
−1 → −1). The minute is the fixture's match clock (the most minutes any
player has in it, as `GET /fixtures` shows), not his own minutes, so an
unused substitute still has the rest of the match. At minute 0 that is
the pre-kickoff draw; from minute 90 nothing is left. A finished match is
not resampled. Amended 2026-10-16: before this, a match in progress was
never resampled, so once every match had kicked off the odds were 1/0/0
even at minute 5. The odds are certain only when no draw left can change
the order (or every match is over). The league-average
opponent scores floor(mean of every team's play), as in scoring. Home win,
draw and away win are the shares of plays; they sum to 1. Once every match
of the gameweek is over (provisional or later) the odds are the result.

Each team draws from its own stream, seeded by (matchup, gameweek,
five-minute bucket, team): the same request gives the same odds, they do
not flicker between refreshes (the bucket is fixed before kickoff), and
swapping home and away swaps the odds exactly. With no completed gameweek
there is nothing to resample and the odds are null. The app labels them
"Based on recent scores".

### Start/sit accuracy

For each completed gameweek: the team's points as scored (auto-subs
included) ÷ its optimal points, the best legal XI from its 15 by actual
points (1 GKP, 3-5 DEF, 2-5 MID, 1-3 FWD; every legal formation tried with
each position's top scorers). Null when the optimal is 0 or less. The
season figure is the mean of the gameweeks' accuracies. The scored XI is
itself a legal XI from the 15, so accuracy never exceeds 100%.

## Consequences

- Every number can be checked by hand from the points the app already
  shows; tests derive expected values from these formulas, not from the
  code (`projection/*_test.go`, `httpapi/projections_test.go`).
- No new tables: the endpoints are pure reads over the world. They cost a
  world load and, for odds, about a million random draws per request.
- The factor's range (±20%) is a choice, not a fit. Changing it changes
  this ADR and the tests.
- Expected ignores minutes risk and injuries beyond what form already
  shows; win odds ignore auto-subs. Both are stated where they are shown.
