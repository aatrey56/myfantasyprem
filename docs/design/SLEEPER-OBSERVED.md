# What Sleeper looks like (observed 2026-10-15)

Observed on the owner's iPhone (Sleeper iOS, a 12-team NFL league), view only.
This turns the [I] items in [BRIEF.md](BRIEF.md) into [V] where seen, and lists
what our app copies. We copy layout, density and interaction. We do not copy
Sleeper's name, logo, mascot, navy/teal palette, icons, emoji reactions,
player photos or team logos (AGENTS.md hard rules still apply: text only).

## Global

- Dark navy ground, cards one step lighter with a 1px hairline border and
  ~16px radius. Lots of bold, heavy type; numbers are the loudest thing.
- League header: back chevron, league avatar, league name in a large bold
  title (~22px/800), settings gear on the right.
- In-league nav is a row of UPPERCASE text tabs under the title (MATCH,
  TEAM, PLAYERS, LEAGUE), tracked +0.8, active tab in accent with a short
  rounded underline (3px). A red dot marks a tab with news.
  (We copy this since 2026-10-15: BRIEF §2. STATUS stays our first tab.)
- Position pill: a **solid pastel fill with dark text**, rounded 8, about
  44×28 in rosters and 30×30 (square) in head-to-head rows. QB pink, RB mint,
  WR blue, TE orange. The pill is a button (tap = swap).
- Secondary metadata uses coloured position text inline: `QB • CHI (10)`.
- Fixture lines are small and colour-coded by difficulty: green easy,
  orange/red hard (`@ ATL (20th)`).
- Points always show as `actual` over `projected`: a bold dash `-` until the
  game starts, small muted projection underneath.
- Bottom sheets: rounded 20 top, grab handle, bold centred title.

## Matchup (MATCH tab)

1. Hero card, edge to edge inside the gutter:
   - avatars pinned to the far left and far right edges;
   - under each, a win % label and a thick (6px) bar that grows **toward the
     centre**, left bar in green, right bar in red/pink, track muted;
   - small projected score above each bar's inner end;
   - a square emblem in the centre between the bars;
   - team names bold (~17px), then `@manager • 3-2 ▼2L (#5)` (record, streak
     with arrow, rank);
   - a pill button straddling the bottom edge of the card;
   - below a divider: each side's average stat on the outside, the two
     projected totals in the middle split by a vertical rule.
2. "yet to play (10)" plus the positions still to play, under each side, and a
   row of dots in the middle (one per game slot).
3. "Starters" heading with `‹ Week 6 ›` stepper in accent on the right.
4. H2H rows (~84px tall, no row borders, generous vertical spacing):
   left player name bold 17px at the outer edge, `QB • CHI (10)` and a status
   tag under it, kickoff + opponent line under that; points `-` over
   projection at the inner edge; position pill dead centre; mirror on the
   right.

## Team (TEAM tab)

- Team card: avatar with a small rank medal, team name bold + chart icon,
  `3-2 5th • PF 7th • PA 6th`, then `63% WIN` with a long progress bar and
  projected total at the right. A row of 4 quick actions under a divider:
  sched. / trade (red count badge) / trans. / news, separated by hairlines.
- "Starters" heading with an edit icon, hint text "Tap on a position button
  to update your lineup", week stepper on the right.
- Roster row: position pill (solid pastel, 44×28) · round avatar · name bold
  - `QB - CHI (10)` · a nickname line · `92% Rostered | 34% Start` · fixture
    line with status tag; points `-` over projection on the right.
- Tapping the position pill opens a **Swap Player** sheet: the current player
  row at the top, a divider chip "⇅ Choose player to swap", then eligible
  bench rows (`BN` pill, muted). Tap outside to cancel.

## Player card

- Full-screen modal over the app, closed by a **floating round ✕ button**
  bottom-right (56px, translucent).
- Status banner across the top (`? QUESTIONABLE`, amber).
- Hero: photo left (we use our shirt glyph + initials), first name small caps
  above a huge last name, then a row of bio stats with tiny labels
  (AGE 24.9 · HEIGHT · WEIGHT · EXP).
- Actions: a wide pill button (DROP / ADD) plus round icon buttons (trade
  block, watch). Tooltips explain icon buttons.
- Tabs: SUMMARY / GAME LOG, uppercase, underline indicator.
- Summary: a three-group stat strip with tiny caps labels (PLAYER RANK
  #29 QB / #126 OVERALL, OWNERSHIP 92% roster / 34% start, FPTS/GAME),
  an "Upcoming game" card with a WEEK chip notch on its top border, and a
  horizontally scrolling row of per-week projection cards.
- Game log: a dense table, one row per week (bye row greyed), column groups
  with headers (FANTASY: FPTS, SNP%, RANK; PASS: ATT, CMP, YD, TD ...).
  **Each cell is a coloured tile** (green good, yellow middling, red poor,
  grey neutral), opponent coloured by difficulty. Unplayed weeks are dashes
  on dark tiles. This heat-map is the most distinctive pattern on the card.

## Players (PLAYERS tab)

- A rounded card holding 5 icon+label buttons: Search, Trend, Available,
  Leaders, Trade (badge count). Active one in accent.
- Section title with a two-state toggle; a scope dropdown on the right
  (`WEEK 6 PROJ ⌄`).
- Filter chips: icon chip, `FA`, a divider, star, `ALL` (selected: accent
  outline + 16% fill), then positions. Horizontal scroll.
- Player row: outlined round **+** button (claim) or a waiver badge `W (Sat)`
  on the left, rank number, round avatar, name bold with `• QB • ATL (11)`,
  fixture line; a `PROJ 16.1` box on the right. Under it a full-width strip
  of labelled micro stats (PASS YD, RUSH YD, ...) on a slightly lighter band.
- Trade view: "Trade Block" carousel of round avatars with name + `WR - BAL`
  under each, a TRADE button with icon on the right; "Active Trades" cards
  with INCOMING tag, time ago, manager, "2-way trade", the two sides as
  avatar clusters separated by ⇄.

## League (LEAGUE tab)

- Matchups heading with week stepper, then one compact hero card per
  matchup (same layout as the Match hero, smaller, `VS` in the centre).

## Motion

- Tab switches are instant with the underline sliding.
- Sheets slide up with a spring; the swap sheet dims the page behind.
- Pressed rows and buttons dim slightly; nothing bounces.

## Mapping to FPL

| Sleeper                  | Ours                                              |
| ------------------------ | ------------------------------------------------- |
| QB/RB/WR/TE pill colours | GK/DEF/MID/FWD solid pills from our position hues |
| Win % bars               | Share of live points (no projections in the API)  |
| yet to play (10) + list  | Starters whose fixture has not kicked off         |
| `-` over projection      | Points; `–` before kickoff                        |
| Week stepper             | GW stepper                                        |
| Rostered % / Start %     | Owned-in-league / form when we have it, else omit |
| Game log heat tiles      | Per-GW points and stats tinted by quality         |
| Bye row                  | Blank GW row                                      |
