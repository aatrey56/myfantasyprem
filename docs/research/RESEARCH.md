# MyFantasyPrem: research on a Sleeper-style FPL Draft app

Compiled 2026-10-09 from six research reports: product and market, legal and
data, API, import and rules, architecture and effort, Sleeper vs FPL gaps, and
FPL Draft pain points. Claims marked UNVERIFIED could not be confirmed against
a source.

---

## 0. Summary

- **The idea.** A Sleeper-quality mobile and web app for FPL Draft leagues. It
  runs the draft, waivers, free agency, trades with veto, and live head-to-head
  scoring under the exact FPL rules. It can import an existing league
  mid-season.
- **Sleeper already runs Premier League draft leagues** (Sleeper FC, since
  2024/25), but it can't run a league by FPL rules:
  - no bonus points (BPS) and no defensive-contribution threshold;
  - lineups lock per player at kickoff;
  - auto-subs work the NFL way;
  - mobile only;
  - unreliable, and until mid-2026, neglected.
- **FPL Draft has the rules but a bad app:**
  - login and logout problems since the July 2025 Premier League app relaunch;
  - no team page showing your matchup and record;
  - glitchy draft room, lineups and trades;
  - no league chat;
  - unreliable trade notifications;
  - no counter-offers.
- **The gap:** Sleeper's UX, FPL's exact rules, and league import from a league
  ID. Neither product offers that.
- **Import is feasible.** The league ID alone, with no login, gets every settled
  piece of league state. Only in-flight state needs a login: pending claims,
  open offers, watchlists, and next gameweek's lineup edits.
- **Scoring is simpler than it looks.** FPL's live feed gives each player's
  points. A team's score is the sum of FPL's points for its lineup plus
  auto-subs. You only need a raw-stats scoring engine if you stop using FPL's
  data.
- **Legal risk depends on who uses it:**
  - a private app for friends is low risk;
  - a public App Store app is fragile while it depends on FPL's API (FPL terms
    clause 28(d), Apple guideline 5.2.2, UK database right in live match data);
  - always use text-only names and branding, and never handle money.
- **Demand is unproven.** r/FPLDraft has about 5k members and no moderator, and
  nobody is asking there for an alternative. FPL doesn't publish Draft player
  numbers. Your league is a strong first user; strangers aren't proven.
- **Recommended path:**
  1. Build a companion now: import, live matchups, rosters, transactions, push
     notifications, and a matchup home screen.
  2. Add chat and trade negotiation.
  3. Run leagues natively from a fresh draft in August 2027. A mid-season
     switch is possible at a gameweek boundary, but only after a replay
     simulator matches FPL's real results exactly.
- **Effort:** about 39 person-weeks in total, about 11 of them for the first
  version.

---

## 1. What's being asked

The request has three layers:

1. **A better UI for FPL Draft:** live matchups, rosters, a transactions feed,
   push notifications and chat.
2. **An app that runs the league:** draft room, waivers, free agency, trades
   with veto, lineups and scoring.
3. **Moving a live league over mid-season** and leaving
   draft.premierleague.com.

---

## 2. Market and competitors

| Product | What it is | Price | Gaps |
|---|---|---|---|
| Official FPL Draft | Free official game. 2–16 managers, up to 3 extra redrafts per season | Free | No chat. Trades stay pending until the waiver deadline in approval leagues. Outages. Poor app UX (§4) |
| Sleeper (soccer) | Head-to-head draft for EPL, LaLiga and Ligue 1. 4–14 teams. Snake, linear or auction drafts. Rolling, reverse or FAAB waivers. Chat | Free (money comes from betting products) | Not FPL rules. Mobile only. Reliability problems (§3) |
| Fantrax | Highly customisable EPL draft, web and app | Free core, paid premium tiers | Clunky UI, daunting setup |
| Draft Futbol | Solo-developer iOS companion for FPL Draft: live H2H, standings, waivers, trades | Free, $2.99 unlock | Tiny traction (3 ratings) |
| Draft FC | Rankings, xPts, podcast, guides | Partly paid | Content and analysis, not a league host |
| Smart Draft Board, FantasyFooty, fplprophet, Draft FPL Live | Add-ons that read the official API | Free or ads | Built around FPL, not replacing it |
| Draft Fantasy (draftfantasy.com) | Draft and auction across many leagues | Unknown | Little information |

**About Sleeper the company:**
- Stack: React Native, TypeScript and GraphQL on the front end. Go, Java,
  Python, Node and Kafka on the backend (from job ads).
- Funding: about $40–67M raised, and a reported $400M valuation (UNVERIFIED).
- Leagues are free; revenue comes from Picks/DFS and prediction products.
- Import only works from US platforms, through a desktop Chrome extension.
  There is no FPL import.

---

## 3. Sleeper soccer vs FPL Draft: gaps and issues

### 3.1 Comparison

| Area | Official FPL Draft | Sleeper soccer |
|---|---|---|
| Scoring | FPL scoring (§7). Bonus 3/2/1 from BPS. 2 points for 10 (DEF) or 12 (MID/FWD) defensive contributions. −1 per 2 goals conceded. Clean sheet needs 60 minutes | Customisable, but no bonus/BPS category, no defensive-contribution threshold, and goals conceded is −2 each. Per-action scoring (tackles, interceptions, key passes). **Cannot replicate FPL** |
| Assists | FPL's own "fantasy assist" | Stricter: "official fpl app counted it as an assist and sleeper didn't" |
| Positions | Fixed GKP/DEF/MID/FWD for the season | Primary position scores. Multi-position eligibility added 14 Aug 2026. Positions reshuffled mid-draft in 2024 and Aug 2026 |
| Squad and formation | 15 players (2/5/5/3). XI: 1 GKP, 3–5 DEF, 2–5 MID, 1–3 FWD. No captains | Default 17, configurable. Preset formations only (4-5-1, 5-3-2 and 5-4-1 still being requested in May 2026) |
| Lineup lock | Whole squad locks at the gameweek deadline (first kickoff −90 minutes) | Each player locks at kickoff. Staff say FPL-style locking is "easy to cheat" |
| Auto-subs | At gameweek end, in bench order, keeping the formation legal | Shipped about 11 Sep 2026. Commissioner must turn it on. "more similar to our NFL autosubs" |
| Double/blank gameweeks | Both fixtures count | Users report only the best game counts (UNVERIFIED) |
| Waivers | Reverse-standings priority, then free agency | Rolling, reverse or FAAB. Custom waiver days are "really confusing", and claims run on odd schedules |
| Trades | None, all, admin approval or manager approval. Vetoed if at least 50% object within 24 hours. Like-for-like positions | Trades, trade block and veto exist in the NFL help docs. Soccer specifics UNVERIFIED |
| Draft | Snake, timed, autopick from watchlist | Snake, linear and auction. Queue. In-draft chat |
| Playoffs | None (standings only) | Removed mid-season in 2024-25 and 2025-26. Back in 2026-27, reliability UNVERIFIED |
| Keeper/dynasty | Redraft only | Dynasty leagues exist |
| League import | n/a | None for soccer |
| Web | Website and Premier League app | **Mobile only** |
| Ads | None noted | Real-money Picks promoted. A dues promo appeared in league chat in Aug 2026 and was patched after complaints |

### 3.2 Sleeper's biggest gaps for an FPL Draft league, ranked

1. **Scoring isn't FPL's.** This is a hard limitation: no bonus, no
   defensive-contribution threshold, and different goals-conceded and assist
   rules. You end up drafting for a different game.
2. **Lineup lock and auto-subs aren't FPL-style.** This is a deliberate design
   choice; staff have rejected gameweek locking.
3. **Positions and formations have bugs.**
   - A locked player gets moved when the formation changes, and you can't undo
     it.
   - Players eligible at two positions sometimes score at the wrong one. One
     scored a midfielder's goal as a forward. Another played as a midfielder
     but was scored as a defender, conceded goals included.
4. **Injured-reserve bug.** IR status is removed when a match ends, which locks
   the roster mid-gameweek. It has been reported for 2 years and still recurs
   after a patch.
5. **League structure is unstable.** Playoffs were removed mid-season twice.
   Final standings got stuck at "37 weeks", and support "knew nothing about
   fantasy soccer".
6. **No browser version.** Fantrax has one; Sleeper doesn't.
7. **Waivers and the player pool behave oddly.**
   - The pool includes anyone at a Premier League club since 2023.
   - Players appear before they've signed, so they skip waivers.
   - Waiver days are awkward to set up.
8. **Missing league features:** no league-median game, no divisions, and no
   home/away shown on past games.

### 3.3 Where Sleeper beats FPL Draft

- Custom scoring and roster sizes.
- IR slots, FAAB and auction drafts.
- Built-in chat, and an app people prefer to Fantrax's.
- Staff are present on Reddit and ship fixes within weeks.

### 3.4 Sleeper reliability and data issues

- **Stat corrections change results a day later:** "Lost by 1 point… next day
  ended up with a tie."
- **Stale win-probability bars**, such as 99% for a team that's losing.
- **Missing or out-of-date data:**
  - points missing for actions Opta records;
  - transfers and promoted clubs updated more slowly than on Fantrax;
  - outdated player photos.
- **Inconsistent positions** for wingers.
- **Draft failures,** such as "unable to click draft button".

### 3.5 Momentum

- **May 2026:** a user wrote "Back to back shambolic season… you guys
  abandoned the app". Sleeper replied it was putting "a team on it to fix".
- **Since then:**
  - multi-position eligibility shipped on 14 Aug;
  - an injury-designation patch followed in early September;
  - auto-subs shipped on about 11 Sep.
- **Overall:** improving, but with a thin team. The soccer subreddits are
  small.

---

## 4. FPL Draft pain points

**Where Draft lives:**
- draft.premierleague.com;
- inside the Premier League app, which offers "Classic, Draft and Challenge".
  The app probably embeds the website through a `?webview` mode (UNVERIFIED).

**Platform:**
- The Premier League relaunched its app and website on 1 Jul 2025, with
  Microsoft Copilot features. Many sign-in problems followed.
- On iOS in the UK the app is rated 4.77 from about 657k ratings, for the
  whole app. Of the 500 most recent reviews, 317 are five-star and 89 are
  one-star.

**Pain points**, ranked by how often and how badly they come up. UI means a
design problem; RULES means a rules or feature gap.

1. **Login and session (platform).** The most common complaint, though it
   covers the whole app, not just Draft.
   - Repeated logouts and failed sign-ins through Sep 2026.
   - "Kicked out of my draft."
   - Requiring iOS 17 from Apr 2026 locked out older phones.
2. **Draft league navigation (UI).**
   - "The fixture list in the Draft league is so unconventional… Why isn't
     there a team page and you can see who you play… and record."
   - "Interface kind of sucks. It's confusing and hard to navigate. The draft
     was a nightmare."
3. **Glitches (UI).**
   - "Draft was glitchy. Setting your lineup is glitchy. Sending trades is
     another disaster… we now don't have a fantasy league."
   - The draft loads slowly.
   - The mock draft room is "very poor".
4. **Trades (RULES and UI).**
   - Trades stay pending until the waiver deadline, which leaves no time to
     claim replacements.
   - Like-for-like numbers only.
   - Accept or reject only, with no counter-offers (UNVERIFIED as a
     complaint).
   - Trade notifications are unreliable, so offers get missed.
5. **Waivers (RULES).**
   - The waiver deadline, 24 hours before the gameweek deadline, catches
     people out.
   - No FAAB.
   - "App gets stuck on transactions."
6. **Live scoring.**
   - "Player points not updating live on the squad screen."
   - Bonus and defensive points settle late.
   - People fall back to livefpl.net.
7. **Fixed format (RULES):** no custom scoring, no auction draft, and the
   trade mode is fixed when the league is created.
8. **History:** past seasons are hard to see, and third-party tools archive
   them (UNVERIFIED for Draft).
9. **Outages:** pre-season "Game Updating" pages. Draft opened late in 2025.
10. **Social:** no league chat or draft chat found anywhere.

**What FPL Draft does well, and must be kept:**
- official data and scoring, with the same login as classic FPL;
- league renewals and up to 3 redrafts;
- trade approval by manager vote or by the admin;
- "The league system is so clean and is so fun competing with friends."

**What players ask for:**
- a team page showing opponent and record;
- live auto-subs;
- the commissioner choosing the draft order (this exists, but is hard to
  find);
- fixing the logouts;
- better trade notifications;
- from Sleeper and Fantrax fans: chat, custom scoring, FAAB and auctions.

---

## 5. Demand signals

- **r/FPLDraft:** about 5k members, growing about 7% a year, and no moderator
  ("available for adoption", Apr 2026). Only 3 posts in the first 7 weeks of
  2026/27, and no posts asking for an alternative.
- **Where people actually talk:** r/DraftEPL and r/SleeperFC are where the
  Draft conversation happens.
- **Player numbers:** FPL Draft's aren't published. Classic FPL has about 13
  million players.
- **Reviews:** only about 12 of 1,300 recent Premier League app reviews
  mention Draft.
- **Ecosystem:** people build add-ons around the official game rather than
  leaving it.
- **Reading:** the pain is real but low in volume. The opportunity is leagues
  that want Sleeper's UX while keeping FPL's rules. Validate with the
  companion before building league operations.

---

## 6. FPL Draft API and mid-season import

### 6.1 Endpoints

The base URL is `https://draft.premierleague.com/api`. Endpoints don't need a
login unless noted.

| Endpoint | Contents |
|---|---|
| `/game` | Current and next gameweek, processing status, `waivers_processed` |
| `/bootstrap-static` | Players (`code`, `draft_rank`, status, news), teams, positions, events (deadline, waiver and trade times), fixtures, **settings** (scoring, squad rules) |
| `/league/{id}/details` | Settings, `league_entries` (including `waiver_pick`), all 38 gameweeks of `matches` (H2H schedule and results), standings |
| `/league/{id}/element-status` | Who owns each player, status (a/o/l), `in_accepted_trade` |
| `/draft/{id}/choices` | Every draft pick (round, pick, `was_auto`, time) |
| `/draft/league/{id}/transactions` | Every waiver and free-agent move, including failed claims (a/di/do), with priority and index |
| `/draft/league/{id}/trades` | Trades with their state and the players involved. Seen: p = processed, v = vetoed, a = accepted and waiting for approval or processing (its players are `in_accepted_trade`). Codes for proposed, rejected, withdrawn, invalid and expired are unconfirmed (ADR 0012) |
| `/entry/{id}/event/{gw}` | Locked lineup for past and current gameweeks, including auto-subs. Returns 404 before the deadline |
| `/entry/{id}/public`, `/entry/{id}/history` | Team details and points per gameweek |
| `/event/{gw}/live` | **Global** live stats and points for every player, with an `explain` breakdown per fixture, plus fixtures |
| `/pl/event-status` | `bonus_added` and `leagues_updated` for each day |
| `/bootstrap-dynamic` | Empty without a login |
| `/entry/{id}/my-team`, `/draft/entry/{id}/transactions`, `/watchlist/{id}` | **403: login required** |

Pulse (`footballapi.pulselive.com`) gives official team sheets about an hour
before kickoff. It needs an Origin header claiming to be premierleague.com,
which is a scraping risk.

### 6.2 What an import can get

| League state | Available |
|---|---|
| Settings: H2H or classic, trade mode, waiver mode, pick timer, start and end gameweek | Public |
| Members and teams | Public (names only) |
| Draft picks | Public |
| Current squads | Public |
| Past locked lineups | Public |
| H2H schedule for all 38 gameweeks, results and standings | Public |
| Transactions and trade history, including vetoes | Public |
| Current waiver order (`waiver_pick` = reverse standings) | Public |
| Next gameweek's lineup before the deadline | Login only |
| Pending waiver claims | Login only |
| Open trade offers | Possibly public (UNVERIFIED) |
| Watchlists | Login only |
| When a locked player unlocks | Not exposed; rebuild it from transaction times |

**How to import:**
- An import needs only the league ID. Switch over at a gameweek boundary,
  right after waivers are processed:
  1. Freeze FPL.
  2. Import the league.
  3. Have managers re-enter their lineups and claims.
- Don't collect FPL logins. The old login flow broke in Aug 2025. The new one
  uses PingOne OIDC and doesn't let third parties register as a client.
  Collecting logins would also break FPL's terms of service.
- To link each team to its real manager, the commissioner sends an invite link
  and managers claim their team.

---

## 7. FPL Draft rules for 2026-27

Taken from `bootstrap.settings` and the official help pages.

### Squad and lineups

- **Squad:** 15 players: 2 GKP, 5 DEF, 5 MID, 3 FWD. No limit per club.
- **Starting XI:** 1 GKP, 3–5 DEF, 2–5 MID, 1–3 FWD. Bench slot 12 must be a
  goalkeeper.
- **No captains and no chips.**
- **Deadline:** the first kickoff −90 minutes. Waivers close at the
  deadline −24 hours. Trades must be in by the deadline −48 hours
  (`trades_time`).
- **Lineups carry over** each week. Changes made after the deadline apply
  from the next gameweek.

### Scoring

| Event | GKP | DEF | MID | FWD |
|---|---|---|---|---|
| Played under 60 min / 60+ min | 1 / 2 | 1 / 2 | 1 / 2 | 1 / 2 |
| Goal | 10 | 6 | 5 | 4 |
| Assist | 3 | 3 | 3 | 3 |
| Clean sheet (60+ min) | 4 | 4 | 1 | 0 |
| Per 2 goals conceded | −1 | −1 | 0 | 0 |
| Per 3 saves (whoever keeps goal) | 1 | 1 | 1 | 1 |
| Penalty saved / missed | 5 / −2 | 5 / −2 | 5 / −2 | 5 / −2 |
| Yellow / red card | −1 / −3 | −1 / −3 | −1 / −3 | −1 / −3 |
| Own goal | −2 | −2 | −2 | −2 |
| Defensive contribution | 0 | +2 at 10 CBIT | +2 at 12 CBIRT | +2 at 12 CBIRT |

- **Defensive contribution:** CBIT counts clearances, blocks, interceptions and
  tackles. CBIRT also counts recoveries.
- **Playing** (for auto-subs and scoring) means "making an appearance on
  the pitch or receiving a yellow / red card" (FPL and FPL Draft rules,
  https://fantasy.premierleague.com/help/rules,
  https://draft.premierleague.com/help). A player booked on the bench
  scores the card only (−1 or −3; appearance points need minutes) and is
  not auto-subbed.
- **Red cards:** "If a player receives a red card, they will continue to be
  penalised for goals conceded by their team" (same pages), so goals after
  a dismissal still cost a GKP or DEF and his clean sheet.
- **Outfield player in goal:** "they will be able to collect points for
  saves, but will score points as per their position classification"
  (FPL FAQ "How does FPL decide the player positions?",
  https://fantasy.premierleague.com/help/faqs). Saves score for any
  position.
- **Bonus with low BPS:** "The three best performing players in each match
  will be awarded bonus points" (FPL rules, "Bonus points"): BPS of 0 or
  below ranks like any other.
- **Bonus:** the top 3 players by BPS in each match get 3, 2 and 1 points,
  and tied players share. In a double gameweek each match is ranked on its
  own BPS (the `bps` list in that fixture's `stats`).
- **Bonus timing (new in 2026-27):** FPL adds _projected_ bonus to live
  points after 20 minutes of each match and keeps revising it until the
  bonus is confirmed, so a bonus value in `event/{gw}/live` is not
  confirmation. A match's bonus is confirmed when the fixture is
  `finished` or `/pl/event-status` has `bonus_added` for its day.
- **Score lock (new in 2026-27):** a gameweek's scores become final at
  09:00 UK time on the day after its last match (previously later).
- **BPS changes (2026-27):** 1 BPS per 3 clearances, blocks and
  interceptions (was per 2); no deduction for being tackled; goalkeepers
  get 2 BPS for every save (no longer only for shots from outside the box)
  plus 1 for saving a big chance; a penalty save is worth 7 BPS (one less
  than before). We read BPS from FPL and never recompute it, so these only
  matter for synthetic data (`tools/genworld`). Sources: FPL's 2026-27
  rules (https://www.premierleague.com/en/news/2174909), Fantasy Football
  Scout (2026-07-20), Fantasy Football Hub.
- **Checking a scoring engine:** `live.explain` is FPL's own per-fixture
  breakdown of points, so it can serve as the expected answer in tests.

### Auto-subs, double gameweeks and H2H

- **Auto-subs:** at the end of the gameweek, a starter with 0 minutes is
  replaced by the first bench player who played, in bench order. Goalkeepers
  only replace goalkeepers, and the formation must stay legal.
- **Double and blank gameweeks:** in a double gameweek a player scores in both
  fixtures. A player with no fixture scores 0 and can be auto-subbed. A
  postponed match counts in the gameweek it's replayed.
- **H2H:** win 3, draw 1, loss 0. Ties are broken on points for. With an odd
  number of teams, an "average" opponent is added.

### Draft

- **Order:** snake. Round 1 is random, or reverse standings for redrafts.
- **Pick timer:** 30, 60, 90 or 120 seconds.
- **Missed pick:** the system takes the top watchlist player at a needed
  position, otherwise the highest `draft_rank`, and marks the manager idle.
- **League size:** 2–16 teams.
- **Scheduling:** the draft cutoff is 3 hours before the draft time. A draft
  can be postponed by 24 hours, up to 3 times. At most 4 drafts per season.

### Waivers and free agency

- **Priority:** reverse standings (reverse draft order before gameweek 1),
  reset every gameweek. A team that wins a claim drops to the back for the
  rest of that run.
- **Processing:** league-wide in index order, with each manager's claims taken
  in their priority order.
- **Failed claims:** `di` means the player was already taken. `do` means the
  drop player had already been used.
- **Free agency:** first come, first served, after waivers until the
  deadline.
- **Locks:** dropped players and free-agent adds are locked until the next
  waiver deadline (24 hours).

### Trades

- **Modes:** none, all, admin approval or manager approval. The mode is fixed
  once the draft starts.
- **Manager veto:** a trade is vetoed if at least 50% of managers object
  within 24 hours.
- **Shape:** equal numbers of players, matched by position.
- **Processing:** trades process before waivers. Accepting one trade cancels
  other offers involving the same players. Unaccepted offers expire at the
  waiver deadline.

---

## 8. Legal, data and platform risk

This is not legal advice.

### FPL and Premier League terms

- Clause 28(d) bans "automated systems to access the Game and extract
  information".
- Clause 28(b) bans commercial exploitation of Game information.
- Clause 29: the Premier League owns all the intellectual property.
- Clause 27 bans account sharing, so never collect FPL logins.
- The Premier League's website terms ban building databases from downloaded
  material. Data licensing goes through Football DataCo.
- No public cease-and-desist letters against FPL tools were found. Companion
  sites operate on tolerance plus disclaimers. A full replacement is a more
  natural target than an analysis tool.

### Intellectual property

- **Never use:**
  - club crests, kits or player photos;
  - "Premier League" or "FPL" in the app's name, icon or developer name
    (Apple guideline 5.2.1).
- **Low risk:**
  - text player names and stats (US: *C.B.C. v MLBAM*; the UK has no
    publicity right);
  - club codes such as ARS and CHE.
- **Disclaimer:** state that the app is not affiliated with the Premier
  League.
- **Database right:** in *Football DataCo v Sportradar* [2013] EWCA Civ 27,
  the court held that live match data (goals, cards, substitutions) is
  protected. Fixture lists are not (*Football DataCo v Yahoo*, CJEU 2012). So
  systematically re-serving FPL's live data in the UK is exposed.

### Licensed data instead of FPL's

| Provider | Price | Gaps |
|---|---|---|
| Opta / Stats Perform | Custom quote (estimated €250 to £2k+ a month, UNVERIFIED) | Exact match with FPL, which is built on Opta |
| Sportradar | Custom (estimated €1.3k–5k+ a month) | Different event definitions |
| Sportmonks | €29, €99 or €249 a month | No ball recoveries or goal-line clearances, so midfielder and forward defensive contributions and bonus can only be approximated |
| API-Football | Free tier (100 requests a day), then $19–39 a month | Clearances and recoveries probably missing; slower updates |
| FotMob / SofaScore | Unofficial | Breaches their terms; avoid |

- **Assists:** FPL's "fantasy assist" is broader than a standard assist, so
  feeds other than Opta undercount assists.
- **Bonus:** without Opta, bonus can only be approximated. Label it
  "provisional".

### Relying on the FPL API

- **Limits:** no published rate limits.
- **Blocking:** it blocks non-browser user agents and some VPNs.
- **Outages:** around deadlines.
- **Caching:** one global `event/live` fetch serves every league, so a server
  cache is feasible.
- **The real risk** is policy, not load.

### App store

- Apple 5.2.2: an app that shows another service's content needs that
  service's permission. FPL apps exist today, so enforcement seems to happen
  only when someone complains.
- Apple 4.8: if you offer other social logins, you must also offer Sign in
  with Apple.
- Apple 1.2: chat needs report, block and filter tools.
- Apple 5.3: contests and prizes have their own rules, and real money needs a
  licence.

### Gambling

- A free app with no prizes is fine.
- Paid entry plus prizes counts as betting under the UK Gambling Act 2005.
- If entry fees fund the prize pool, that's pool betting. It needs a Gambling
  Commission licence, and running it unlicensed is a crime.
- **Never collect, hold or track money or side-pots in the app.**

### Privacy (UK GDPR)

- FPL names and IDs are personal data. You need a privacy policy and account
  deletion.
- Importing league-mates who haven't signed up means you must tell them
  (Article 14).
- The ICO Children's Code applies; make the app 13+.

### Safest setups

- **(a) Private app for friends (TestFlight or web).**
  - FPL API with a cache.
  - No Premier League branding.
  - No money.
  - Low risk.
- **(b) Public free app.**
  - Text-only branding.
  - Import started by the user.
  - A licensed live feed, for example Sportmonks, with bonus and defensive
    contributions marked provisional.
  - 13+, a privacy policy and no prizes.
- **(c) Paid app.**
  - Everything in (b).
  - Subscriptions or ads only; never entry fees.
  - Consider an Opta licence and a review by a UK sports/IP lawyer.

---

## 9. Architecture

### 9.1 Three levels of scope

**A. Companion: a read-only mirror.**
- Imports the league from FPL and keeps it in sync.
- All actions still happen on FPL.
- Very low risk.
- To prove who owns each team, the commissioner sends an invite and managers
  claim their team.

**B. Hybrid.**
- The app owns chat, trade proposals, counter-offers and polls.
- Trades are executed on FPL and synced back.
- The app shows honestly when something is still "pending on FPL".

**C. Full replacement.**
- The app runs the draft, waivers, trades and vetoes, lineups and locks,
  auto-subs, scoring and the schedule.
- Moving a league over mid-season means freezing it at a gameweek boundary:
  1. Wait until the previous gameweek is final and waivers have processed.
  2. Snapshot ownership, squads, waiver order, the schedule and standings.
  3. Cancel everything pending.
  4. Everyone stops using FPL.

### 9.2 Tech stack

**App: Expo (React Native and TypeScript, with expo-router).**
- Sleeper itself uses React Native.
- Over-the-air updates through EAS Update let you fix match-day bugs without
  waiting for App Store review.
- AI agents write TypeScript well.

**Alternatives ruled out:**
- Flutter: a new language, and agents write it less well.
- SwiftUI: leaves out Android.
- A web app (PWA): on iOS, push notifications only work after adding the app
  to the Home Screen.

**Backend:** one Go service plus Postgres.
- It covers the API, the WebSocket hub and background jobs (River, a job queue
  that runs on Postgres).
- It reuses this repo's existing Go packages.

**Realtime:** your own WebSockets plus Postgres LISTEN/NOTIFY. Hosted options
(Ably's free tier allows 200 connections, Supabase Realtime about 500) get
expensive as you grow.

**Not Supabase or Firebase:** waivers, trades and draft picks change many rows
at once under complex rules. That needs strict database transactions, which
belong in Go.

**Login:**
- Sign in with Apple, Google, and an email magic link.
- Go issues its own tokens.
- Revoke the Apple token when an account is deleted.

**Push notifications:** Expo Push, which delivers through Apple's and Google's
push services.

**Background jobs:**
- FPL sync;
- the live-score poller;
- waiver runs;
- lineup locks;
- the draft pick timer.

**Hosting:** Fly.io plus Neon Postgres, moving to Fly Managed Postgres at
scale.

### 9.3 Database tables (Postgres)

```
users, push_tokens
seasons, gameweeks(deadline_at, finished, data_checked)
players(code PERMANENT, season, fpl_id, team, position, status)  -- cross-season joins on code
fixtures
leagues(mode mirror|native, fpl_league_id), league_settings(versioned)
teams(league, user NULL, fpl_entry_id NULL, claim_status)
roster_slots(team, player_code, from_gw, to_gw NULL, acquired_via, txn)  -- half-open GW ranges
lineups(team, gw, slot 1..15, player_code, locked_at)
drafts, draft_picks UNIQUE(draft, player_code), draft_queue
waiver_priority(league, gw, team, rank), waiver_claims(..., pref, status, failure_reason)
transactions(kind, payload, source fpl_sync|native, fpl_txn_id UNIQUE)
trades, trade_items, trade_votes
matchups, player_gw_points(stats, points, bonus_provisional, rev), team_gw_scores(rev, final)
chat_messages, notifications(dedupe_key UNIQUE)
```

**Rules the database must enforce:**
- **One owner per player:** a player can only be on one roster per league per
  gameweek. A database constraint on overlapping gameweek ranges enforces it.
- **Waiver runs:** each run is a single strict transaction and safe to repeat
  for the same league and gameweek. A failed claim falls through to the team's
  next claim.
- **Trades and waivers:**
  - Executing a trade cancels claims that conflict with it.
  - A trade under veto review blocks drops of its players.
  - A trade is re-checked at the moment it executes.
- **Lineup locks:** enforced in the API, not just the app.
- **Scores:** recomputed from scratch whenever FPL corrects data, never patched
  incrementally. A score depends only on the roster that gameweek, the lineup
  and the latest points data.

### 9.4 Live scoring service

**One poller for all leagues.** It polls `event/{gw}/live`:
- every 30–60 seconds while matches are on;
- every 10 minutes until bonus is added;
- every 6 hours until FPL marks the data checked, but waking at the 09:00
  UK score lock (2026-27) and polling every 10 minutes for two hours after
  it.

**Processing each poll:**
- Detect which players' stats changed, and recompute only the teams that own
  them.
- Send updates over WebSockets, and batch push notifications.

**Score states:** live → provisional → official → final. A late correction
triggers a recompute and a "score corrected" event.

**Failure handling:**
- Keep the last good snapshot, and never zero scores after a bad response.
- Mark data as stale if there's been no good poll for 3 minutes.
- Back off on errors, send a polite user agent, and keep a fallback network
  route.

### 9.5 Monthly costs (USD, approximate)

| | 10 users | 1k users | 20k users |
|---|---|---|---|
| Go app (Fly.io) | $3–5 | ~$15 | $80–120 |
| Postgres | Neon $5–10 | $40–80 | $150–300 |
| Expo EAS | Free | $19 | $199 |
| Error tracking and logs | $0 | $0–26 | ~$80 |
| Apple ($99/yr) and Google ($25 once) | ~$8 | ~$8 | ~$8 |
| **Total** | **≈ $20–30** | **≈ $85–150** | **≈ $520–720** |

At scale, a licensed data feed would cost more than the hosting.

---

## 10. Phases and effort

Estimates are in person-weeks (pw), assuming heavy use of AI agents and about
60% of your time.

| Phase | Scope | pw |
|---|---|---|
| 0. Foundation | Repo, shared Go packages pulled out of this repo, Postgres, login, TestFlight builds, CI | 2 |
| 1. Companion | Import and team claiming, FPL sync and transactions feed, live scoring over WebSockets, app screens, push notifications, App Store submission | 11 |
| 2. Chat and trades | Chat, moderation (Apple 1.2), trade proposals, counter-offers and votes synced with FPL, trade analyser | 6 |
| 3. Running the league | League settings and commissioner tools; draft room (clock, queue, autopick, reconnecting); waiver engine; free agency; trades with veto; lineups, locks, auto-subs and scoring; **replay simulator** that runs past seasons and compares the results with FPL's; mock drafts and a beta | 20 |

**Timeline:**
- Total: about 39 person-weeks, roughly 10 months part-time.
- Phase 1 could be with your league around gameweek 14 (Dec 2026).
- A mid-season move is possible around Feb or Mar 2027, once Phase 3 is built
  without the draft room and the replay simulator passes.
- A fresh draft would be in Aug 2027.
- Never cut the replay simulator.

**Biggest risks:**
1. Depending on the FPL API: blocking, format changes, terms of service.
2. Getting every manager in the league to switch.
3. Disputes over waiver results and scoring.
4. Draft room reliability: reconnecting, clock drift, iOS suspending the app
   in the background.
5. App Store review: intellectual property, guideline 5.2.2, chat moderation.

---

## 11. Code from fpl-draft-mcp that can be reused

**Reusable almost as-is:**
- `internal/autosub`: pure and tested. Mark a fixture as done only when all of
  a club's fixtures are done, so double gameweeks work.
- `internal/reconcile`: `BuildOwnershipMapAtGW` rebuilds who owned whom at any
  gameweek from the draft, transactions and trades. Ideal for checking an
  import.
- `internal/ledger`: draft history and per-team snapshots.
- `internal/pulse`: `match.go` and `roles.go` are pure.
- `internal/fetch`: needs cancellation, retries with backoff, response caching
  (ETags), and the file store removed.
- Python `backend/ml`:
  - `lineup.py`: formations, optimal XI and auto-sub simulation;
  - `matchmodel.py`, `projection.py` and `player_values.py`: a nightly job for
    projections, expected points and trade values.

**Needs rework:**
- `fpl-server/*` is all in one `main` package. Pull `deadlines.go`,
  `gw_live.go`, `head_to_head.go` and the manager summaries out into reusable
  packages.
- `internal/store` should be replaced by a database.
- `internal/points` doesn't handle auto-subs.
- `internal/summary` handles a single league and writes to files.
- `cmd/dev` handles a single league and uses US Eastern refresh windows.
- `waiver.py` and `myweek.py` are built for a single user.

**Missing entirely:**
- a points engine that works from raw stats (not needed while using FPL's
  points);
- a waiver processor;
- trade and veto handling;
- the draft room.

---

## 12. UX direction ("Sleeper-style")

**Look and feel:**
- Dark mode first: near-black navy, raised cards, one accent colour. Green and
  red only for winning and losing.
- Big numbers, haptics on picks and goals, and live pulses.
- System messages with personality, such as "Saka just put you up 8".

**Bottom tabs:** League · Matchup · Team · Players · Chat.

**The matchup screen is the centrepiece:**
- both teams' scores, with a win-probability bar from the expected-points
  model;
- starters side by side, each with fixture status and live points;
- the bench below, with projected auto-subs highlighted;
- tap a player to see their points breakdown.

**Screens for the first version:**
1. Sign in.
2. Import a league (paste its ID or link) and claim your team.
3. League home: standings, this gameweek's matchups, transactions.
4. Live matchup.
5. Scoreboard.
6. Team and roster, with fixture difficulty chips.
7. Player sheet.
8. Players: free agents sorted by expected points.
9. Transactions feed.
10. Deadlines calendar, with a waiver countdown.
11. Notification settings.
12. Profile and account deletion.

**Fix FPL's known complaints directly:**
- a team page showing your opponent and record;
- clear trade states: pending, in the veto window, processed;
- a countdown to the waiver deadline;
- push notifications that arrive;
- chat;
- a web version.

---

## 13. Getting at Reddit and Google Play (research access)

### Reddit

Reddit blocks unauthenticated scraping:
- `www.reddit.com/*.json` returns 403 even with a browser user agent.
- old.reddit redirects to a login wall.
- WebFetch is blocked.
- Arctic Shift returned a 522 error on 2026-10-09.

What works:
- **PullPush archive.**
  - `https://api.pullpush.io/reddit/search/comment/?subreddit=X&q=Y` returns
    posts **and comments**.
  - Tested on 2026-10-09; it returned r/SleeperFC comments.
- **Reddit's official API with OAuth.**
  - Create a free "script" app at https://www.reddit.com/prefs/apps and use
    PRAW with its client ID and secret.
  - Free non-commercial use is limited to about 100 queries a minute.
  - This is the most complete and reliable route.
- **Subreddit RSS** (`/r/X/.rss`): posts only, rate-limited.

### Google Play

- Reviews load through JavaScript, so a plain page fetch gets nothing.
- The open-source Python package `google-play-scraper` calls the same internal
  endpoint the Play website uses.
- Tested on 2026-10-09: it pulled 200 Sleeper reviews with no login.
- Run it with `uv run --with google-play-scraper python ...`.

### Apple App Store

- Public RSS feed:
  `https://itunes.apple.com/{cc}/rss/customerreviews/page={1..10}/id={id}/sortby=mostrecent/json`.
- 50 reviews per page, 10 pages at most. Some countries only return page 1.
- App IDs: Premier League app 1138895159, Sleeper 987367543.

---

## 14. Open questions

- Do open trade offers appear in the public `/draft/league/{id}/trades`
  endpoint? Accepted ones waiting for approval do (state `a`, seen on
  2026-10-14); unanswered offers have not been seen.
- What exactly do the league settings codes `trades` and `variety` mean?
- Does the draft API accept the new PingOne login token? (Don't depend on it.)
- Does Sleeper really count only one game in a double gameweek?
- How much real demand is there? This needs a deeper read of Reddit comments
  and Google Play reviews.
- Two answers are still needed:
  - Is the first public version TestFlight for your league, or the App Store?
  - When should your league see it?

---

## 15. Key sources

- Sleeper soccer scoring: https://support.sleeper.com/en/articles/9702800-scoring-system
- Sleeper import (US platforms only): https://support.sleeper.com/en/articles/16203584-how-to-import-your-league-to-sleeper
- r/SleeperFC, Sleeper PM on positions and locking: https://www.reddit.com/r/SleeperFC/comments/1vo13vj/
- r/SleeperFC, auto-subs and IR thread: https://www.reddit.com/r/SleeperFC/comments/1wes1oe/
- r/SleeperFC, "shambolic season" thread and staff reply: https://www.reddit.com/r/SleeperFC/comments/1tr7v97/
- r/SleeperFC, IR lock bug: https://www.reddit.com/r/SleeperFC/comments/1wfhhtj/
- r/DraftEPL, Fantrax vs Sleeper (Oct 2026): https://www.reddit.com/r/DraftEPL/comments/1wv5qa9/
- Sleeper App Store lookup: https://itunes.apple.com/lookup?id=987367543
- Premier League app reviews (UK): https://itunes.apple.com/gb/rss/customerreviews/page=1/id=1138895159/sortby=mostrecent/json
- FPL Draft 2026/27 launch: https://www.premierleague.com/en/news/4683615
- FPL Draft 2025/26 changes: https://www.premierleague.com/en/news/4363847/fpl-draft-is-live-see-whats-new-for-202526
- Premier League digital relaunch (Jul 2025): https://www.premierleague.com/en/news/4337361/premier-league-launches-new-fan-facing-platforms-as-part-of-digital-transformation
- Relaunch login problems: https://www.consultancy.uk/news/41215/ai-ux-fantasy-premier-league-overhaul-leaves-some-fans-disenchanted
- Draft FC trade rules: https://draftfc.co.uk/fpl-draft-trades
- FISO "Trades with Manager Veto": https://www.fiso.co.uk/forum/viewtopic.php?t=133869
- FPL game terms: https://fantasy.premierleague.com/help/terms
- Premier League website terms: https://www.premierleague.com/en/terms-and-conditions
- Football DataCo v Sportradar [2013] EWCA Civ 27: https://caselaw.nationalarchives.gov.uk/ewca/civ/2013/27
- Defensive contributions (2025-26): https://www.premierleague.com/en/news/4362211
- BPS changes (2025-26): https://www.premierleague.com/en/news/4362127/
- FPL 2026-27 rules: https://www.premierleague.com/en/news/2174909
- FPL 2026-27 changes (live bonus, 09:00 lock, BPS): https://www.fantasyfootballscout.co.uk/2026/07/20/fpl-2026-27-5-rule-changes-new-features-announced
- BPS changes (2026-27): https://www.fantasyfootballhub.co.uk/articles/fpl-bonus-points
- Sportmonks pricing: https://www.sportmonks.com/football-api/plans-pricing
- Sportmonks player statistics: https://docs.sportmonks.com/v3/definitions/types/statistics/player-statistics
- Apple App Review Guidelines: https://developer.apple.com/app-store/review/guidelines/
- UK Gambling Act s.11 notes: https://www.legislation.gov.uk/ukpga/2005/19/section/11/notes/data.htm
- ICO Children's Code: https://ico.org.uk/about-the-ico/what-we-do/background-to-the-children-s-code/
- Expo pricing: https://expo.dev/pricing
- Fly.io pricing: https://fly.io/pricing.md
- Neon pricing: https://neon.com/pricing
- r/FPLDraft stats: https://gummysearch.com/r/fpldraft/
