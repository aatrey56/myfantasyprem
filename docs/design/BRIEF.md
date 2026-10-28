# MyFantasyPrem design brief

> **Owner's note (2026-10-12).** The owner wants Sleeper's UI and
> information architecture essentially as-is (layout, navigation, density
> and interactions), with the tweaks in this brief. Everything that
> identifies the product is our own: name, logo, icons and colour identity.
> Do not copy Sleeper's, the Premier League's or FPL's logos, icons, colours
> or artwork. Since the pivot in [ADR 0008](../adr/0008-native-leagues.md),
> leagues, drafts, lineups, waivers and trades live in this app, so the
> screens cover native leagues; anything below about importing an FPL Draft
> league describes a feature that is now "later".

Tags: [V] means confirmed by a Sleeper source or your iOS observation. [I] means inferred and not yet checked.

## 1. Principles

- **Numbers first.** Points, record and rank lead every screen. Sleeper colour-codes stats [V].
- **Dense and calm.** Rows are 52–56px with one card style. Colour only carries meaning: win, loss, live or warning.
- **Tap a number to see why.** Score taps open a breakdown [V], including for bonus and auto-subs.
- **The league is the place.** Users pick a league, then use tabs inside it [V: Matchup, League, Players, Trade tabs].
- **Show freshness.** Every screen shows live / pending / final and "updated Xm ago".
- **Instant.** Show cached data first, refresh in the background, and use skeletons rather than spinners.
- **Robust.** Handle odd team counts, DGW/blank gameweeks and unknown enums.

## 2. Navigation

**Phone (<768px):** Sleeper's layout (owner's call, 2026-10-15). Inside a league, a header (back, league avatar and name, GW chip, settings) with uppercase top tabs STATUS · MATCH · TEAM · PLAYERS · LEAGUE under it; the pages swipe like a pager. Activity opens from LEAGUE. An app-wide bottom bar (LEAGUES · SCORES · FEED · ACCOUNT) stays visible in a league.

- Do not use Sleeper's floating pill [V observed].
- Tapping the league name opens a sheet with the league switcher, Import and Deadlines.
- The header has a GW chip on the right.

**Tablet and desktop (≥768px):** a sticky horizontal top bar, like FPL Draft's (owner's call, 2026-10-13).

- From left to right: our mark (all leagues), the league switcher, the 5 tabs, then the GW chip, Draft room (pre-draft / drafting) and Settings at the right. The app's bottom bar shows at every width.
- The active tab is underlined in accent. Every target is at least 44px; the bar is a navigation landmark.
- No left sidebar or icon rail. Settings and Draft room are icon-only below 1024px.
- Content column up to 960px.
- The player card opens in a 380px right panel. Sleeper web uses that right rail for chat [I], and we have no chat.
- No page footers. No in-app "Not affiliated" notice (removed 2026-10-14, see AGENTS.md).

## 3. Screens

**Import league**

- A centred card with a URL field, a Paste button, a no-login note and a Continue button.
- Preview card, "Is this your league?":
  - shows league name, season, team count and the first 6 managers
  - Confirm / Not this one buttons
- Async progress steps.
- States:
  - invalid URL (inline)
  - not a Draft league
  - not found
  - already imported (offer Open)
  - failed (Retry)

**League home**

- An eyebrow ("GW 8 · LIVE"), then the title.
- Your-matchup score card, which links to the matchup.
- This-GW matchup rows: names stacked on the left, scores on the right, the leader bold, and a live dot.
- Standings: #, team, W-D-L, PF, form (last 5 as squares). Hide PA on phone.
- Recent activity: 5 rows plus View all. Sleeper uses League → Activity → View All → Filter [V].
- Next-deadline countdown card.
- States: preseason, stale banner with Retry, skeletons.

**Live matchup**

- Score header:
  - initials avatars, team names, records and large scores
  - a status in the centre: LIVE / Bonus pending / Final
- Optional win-prob bar (6px, split, with % at each end). Ship it hidden in v1. Sleeper shipping one natively isn't verified [I].
- Starters as rows of left player | position pill | right player, ordered GK→FWD.
- Each player cell:
  - name
  - club code + fixture ("ARS v che 15:00"; DGW shows 2 chips)
  - status
  - points on the inner edge
- Status values:
  - kickoff time
  - live dot + minute
  - ✓ finished
  - "No fixture" (blank GW)
  - injury flag
- Bench: 4 muted rows numbered 1–4.
- Auto-subs: OUT/IN arrow chips on both players plus a summary line. Sleeper shows auto-subs in the matchup view [V].
- Bonus pending: shown as a dashed "+2" until confirmed.
- League average: the right column becomes one card with a score and no lineup.
- Polling runs only while the tab has focus.

**Team / roster**

- Header: name, manager, record, rank and a GW picker.
- Starters and Bench rows: position badge, name, shirt glyph + club code, GW points.
- Next 3 fixtures as difficulty-tinted chips. DGW stacks 2 chips; a blank shows "—".
- Desktop adds a fixture grid, one column per GW.

**Players**

- Sticky search.
- Filter chips: FA / All / Mine · GK DEF MID FWD · Club.
- Sort menu: Points, Form, Own %, Next fixture.
- Rows: badge, name, club, owner (or FA), the sorted stat, next fixture.
- The player card is a bottom sheet on phone (60% / 100% snap) and the right panel on desktop. It has three tabs:
  - **Summary:** stat tiles that open a breakdown. Sleeper uses Summary tiles [V].
  - **Fixtures:** next 6.
  - **Game log.**
- Empty: "No players match" with Clear filters.

**Activity**

- Chips: All / Waivers / Free agents / Trades.
- Grouped by GW.
- Add/drop rows: a green "+" player, a muted-red "−" player, then team and time.
- Trade card: two columns of players received, plus a Vetoed badge where relevant. Only processed and vetoed trades appear.
- Empty: "No moves yet."

**Deadlines**

- Cards for the next GW deadline (large countdown), waivers and trade deadline, then the season list.
- Times in local time with a timezone label; past deadlines are marked "Passed".

## 4. Components

- AppShell (bottom tabs / top bar) and LeagueSwitcher sheet
- Card
- ScoreHeader and WinProbBar
- MatchupRow and H2H SlotRow
- PlayerRow
- PositionBadge
- ClubShirt glyph: our own SVG with club colours + code
- InitialsAvatar
- FixtureChip (FDR, DGW, blank)
- StatusChip (live, final, bonus pending, sub in/out, injury, vetoed)
- FilterChips and SortMenu
- BottomSheet / SidePanel
- Segmented Tabs
- StandingsTable
- ActivityRow and TradeCard
- Countdown
- FreshnessBanner
- Skeletons, EmptyState, ErrorState (Retry)

## 5. Tokens (ours)

Use neutral graphite, not navy, so we stay distinct from Sleeper.

| Token          | Hex                                   | Note                                   |
| -------------- | ------------------------------------- | -------------------------------------- |
| bg             | #0E1013                               |                                        |
| surface        | #171A1F                               | cards                                  |
| surface-2      | #20242B                               | sheets                                 |
| border         | #2A2F37                               | 1px                                    |
| text           | #F2F4F7                               | ~17:1                                  |
| text-2         | #A3ACB9                               | ~8:1                                   |
| text-3         | #7C8591                               | ~5:1, floor for any text               |
| accent         | #FFB020 amber                         | selected, live; use #0E1013 text on it |
| win            | #34D399                               | ~10:1                                  |
| loss           | #F87171                               | ~6.5:1                                 |
| GK/DEF/MID/FWD | #F59E0B / #38BDF8 / #34D399 / #F472B6 | 16% fill, full-colour text             |

The ratios are estimates, so check them with a contrast tool before use.

- Never put text on a raw club tint.
- Pair win and loss with a W/L letter or ▲/▼ so meaning isn't carried by colour alone.

**Type:** Inter or system, with tabular numbers.

| Style   | Size / line height | Weight   | Use                      |
| ------- | ------------------ | -------- | ------------------------ |
| Display | 32/38              | bold     | scores                   |
| Title   | 24/30              | bold     | page titles              |
| Heading | 17/22              | semibold |                          |
| Body    | 15/20              |          |                          |
| Meta    | 13/18              |          |                          |
| Eyebrow | 11/14              | semibold | uppercase, +0.6 tracking |

**Spacing:** 4-pt scale. Gutters are 16px on phone and 24px on desktop.

**Radii:** chip 999, badge 6, card 16, sheet 20.

**Motion:**

- 150–200ms ease-out
- sheets spring in ~300ms
- scores count up over 400ms with an accent flash
- respect reduced-motion

## 6. Don't copy

- Sleeper's name, logo, mascot, illustrations, reactions/celebration animations, or icon set. Use Lucide or Phosphor instead.
- The navy + teal/purple palette.
- The floating tab pill.
- Their exact tab name + icon set.
- Branded terms such as "Trade Center", "SleeperZone" or "Commish".
- Any licensed custom font.
- Onboarding visuals.
- Per AGENTS.md: no PL/FPL marks. Club crests, kits and player photos only behind the branded-assets switch (private builds), with the generic shirt as the fallback; no in-app "Not affiliated" copy (no page footers).

## 7. Sources

- [V] https://apps.apple.com/us/app/sleeper-fantasy-football/id987367543 (screenshots had no captions)
- [V] https://sleeper.com/fantasy-football
- [V] https://support.sleeper.com/en/articles/4126744-how-can-i-see-my-player-s-points-breakdown
- [V] https://support.sleeper.com/en/articles/4427447-how-can-i-view-transaction-history-for-a-past-league
- [V] https://support.sleeper.com/en/articles/4056316-where-can-i-view-my-trade-offers
- [V] https://support.sleeper.com/en/articles/4238825-welcome-to-a-new-trading-experience
- [V] https://support.sleeper.com/en/articles/9731991-how-does-player-autosubs-work
- [V] https://support.sleeper.com/en/articles/9720715-faq-sleeper-fc (Sleeper FC: DGW counts the best game)
- [V] https://www.cnbc.com/2019/08/25/sleeper-casual-fantasy-football-start-up-battling-yahoo-and-espn.html
- [I] Not found or unverified:
  - a native win-prob bar (only third-party extensions show one)
  - exact tab order and a Trends tab
  - a web chat rail
  - whether the player card is a bottom sheet
  - Sleeper's type scale and motion

  No teardowns, Dribbble case studies or Reddit UI threads turned up.
