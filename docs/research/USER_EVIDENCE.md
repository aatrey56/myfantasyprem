# User evidence: Sleeper FC gaps vs official FPL Draft, and FPL Draft pain points

Prepared 2026-10-09. Research only. Raw data and scripts are in `/tmp/myfantasyprem/raw` and `/tmp/myfantasyprem/scripts`.
Confidence tags: **[H]** many independent items, **[M]** several items, **[L]** few items or inference.

## 0. Bottom line

- Sleeper's soccer game is a *different game* (own scoring, per-game lock, no FPL rules). Its users complain about **reliability and neglect** (waiver, lock, status, position and playoff bugs) and **missing FPL-style rules** (5-3-2, auto-subs, 60-minute clean sheet, fantasy assists, multi-position).
- Official FPL Draft pain is mostly **platform quality**: the in-app Draft tab crashing, logout loops, slow live points, plus restrictive rules (one bench player per position, last-place-first waivers, no bidding). It comes from store reviews. Reddit has almost no product feedback on it.
- r/FPLDraft went quiet after Feb 2026. New subs r/DraftPL / r/DraftEPL (Sept 2026: ~1,150 items) are mostly advice threads; r/DraftPL centres on **Fantrax and Sleeper** (23 and 11 mentions vs 3 for the official game). The platform behind r/DraftEPL is unclear **[L]**.
- **Unanswered:** how many users want *exact FPL Draft rules* in a nicer app. Nobody states it directly (§5).
## 1. Method

| Source | Pulled | Range | Use |
|---|---|---|---|
| r/SleeperFC posts + comments | 880 + 3,803 | 2024-06 to 2026-10-04 | A |
| r/FPLDraft posts + comments | 1,991 + 887 | posts to 2026-09; comments **partial** (late 2024 on; pull still running) | B |
| r/DraftPL | 42 + 543 | 2026-08-21 to 2026-10-03 | B |
| r/DraftEPL posts | 700 | 2026-09-13 to 2026-10-04 | B |
| r/DraftEPL **comments** | **0** | every PullPush request returned HTTP 429 (the earlier FPLDraft pull shares the IP); stopped per instructions | **missing** |
| Sleeper stores (GP + iOS) | ~10,500 after dedupe (the GB and US Google Play files are identical) | 2015-2026 | A |
| PL app stores | 6,400 | GP 2025-09 to 2026-10; iOS 2016-2026 | B |

- Analysed 3,984 Sleeper and 3,998 draft-sub items (≥25 chars, not deleted).
- **A:** 983 (25%) matched a theme. I read ~170 matched snippets plus every cited thread. Keyword precision ≈ 80% (formations ~90%, lineup/DGW/assists ~100%, waivers ~85%, bugs ~80%, playoffs/draft-room/ads ~50%). Shares are of the 983.
- **B Reddit keyword rules failed (precision <10%):** "trade", "waiver", "bench" hits were rate-my-team and advice threads. I hand-read ~150 B snippets. Real product feedback on the official game is **~40-60 of ~4,000 items (1-1.5%) [L]**; counts in §3 are hand-verified floors.
- **Stores:** `store_relevant.jsonl` (393) is poor. Only 13 of ~10,500 Sleeper reviews mention soccer (8 truly). The PL filter keeps 380 but only **45 mention "draft"** (13%): those 45 (36 GP, 9 iOS) are the B evidence.
- **Not covered:** r/DraftEPL comments, early r/FPLDraft comments, X, Discord, forums. Reddit skews to engaged users, reviews to negative; ~4% of r/SleeperFC items since Sept 2026 are NFL chatter.
## 2. A. Sleeper soccer vs official FPL Draft

983 theme-matched items; share = of those. Eras: S1 = 2024-06 to 2025-06 (522 items), S2 = 2025-07 to 2026-06 (276), S3 = 2026-07 on (185). Severity: **Blocker** = leagues leave or can't play. Type: **Hard** = design choice, **Fix** = bug or backlog.

| Theme | n | Share | S1 → S3 share | Severity | Type |
|---|---|---|---|---|---|
| Waivers / free-agent timing / FAAB | 229 | 23.3% | 26.8% → 20.5% | Blocker | Fix |
| Stat corrections, live lag, bugs | 134 | 13.6% | 16.7% → 7.0% | Major | Fix |
| Formations (5-3-2, 1-forward, flex) | 130 | 13.2% | 14.6% → 10.3% | Blocker | Fix (5-3-2 only Aug 2026) |
| Playoffs / schedule / admin | 119 | 12.1% | 10.7% → 13.5% | Major | Fix |
| Player pool / data accuracy / ADP | 82 | 8.3% | 9.4% → 8.1% | Major | Fix |
| IR / injury-status flipping | 78 | 7.9% | 6.3% → 14.6% | Major, rising | Fix |
| Positions / eligibility | 63 | 6.4% | 5.6% → 11.9% | Major, rising | Fix |
| Auto-subs / sub priority | 56 | 5.7% | 6.3% → 7.0% | Blocker | Hard; partly shipped Aug 2026 |
| Support / dev neglect | 42 | 4.3% | 2.9% → 5.4% | Trust | Fix |
| Custom vs FPL scoring | 40 | 4.1% | 4.0% → 6.5% | Major | Hard |
| Lineup-lock mechanics | 30 | 3.1% | 4.0% → 3.2% | Major | Hard |
| Assists vs FPL | 27 | 2.7% | 0.8% → 7.2% (S2 peak) | Major | Hard |
| No web / desktop | 25 | 2.5% | 2.5% → 0.5% | Major | Fix |
| DGW / blank handling | 20 | 2.0% | 2.7% → 0.5% | Major | Hard |
| Clean sheet / goals conceded | 19 | 1.9% | 2.3% → 0.0% | Major | Hard |
| Ads / betting | 8 | 0.8% | 0.4% → 1.6% | Trust **[L]** | n/a |
| Positives | 63 | 6.4% | 6.9% → 7.6% | n/a | n/a |

### Verbatim evidence (A)

**Waivers [H]**
- "Most proper platforms find a way to assign a once weekly waiver time that simply occurs between game weeks. I have no idea why sleeper haven't implemented yet." (r/SleeperFC, 2025-08-09, https://www.reddit.com/r/SleeperFC/comments/1mkoov8/_/n7q7u7s/)
- "…All other PL draft platforms I've played do it this way too." (2024-08-17, https://www.reddit.com/r/SleeperFC/comments/1euh0xo/_/lim7r9b/)

**Formations [H]**
- "The 5-3-2 formation is STILL not in the game. Unbelievable." (post, 2026-07-22, https://www.reddit.com/r/SleeperFC/comments/1v4en8i/). Shipped two weeks later (2026-08-04, https://www.reddit.com/r/SleeperFC/comments/1vf56cb/).
- "Our draft is moving off sleeper if this isn't fixed by the end of this season, its making it unplayable as the season goes on woth [sic] injuries" (2025-12-28, https://www.reddit.com/r/SleeperFC/comments/1pumy62/_/nwd2ko4/)

**Lineup lock and auto-subs [H].** A Sleeper dev (2026-08-16): "FPL locks before matchweek one, while in Sleeper you can move players in and out after games when they don't play… it's easy to cheat the system in an FPL-style system" (https://www.reddit.com/r/SleeperFC/comments/1vo13vj/_/p3ykzpi/).
- "switching a player at 230am isn't viable" (post, Australia, 2024-08-13, https://www.reddit.com/r/SleeperFC/comments/1eqx2gx/)
- A 14-team auction commissioner (2026-07-27) stayed on Fantrax because of one missing feature, auto-subs (https://www.reddit.com/r/SleeperFC/comments/1v8cgz5/).

**Status flipping / IR [M, rising]**
- "I was denied a waiver claim… because Branthwaite switched to GTD randomly the day the waiver claim was to be processed." (2025-01-23, https://www.reddit.com/r/SleeperFC/comments/1i1bu2r/_/m8tai3h/)
- "Imagine Barcola had a hat trick and you couldn't start him cuz the game had him as Out." (2026-09-12, https://www.reddit.com/r/SleeperFC/comments/1wedun6/_/p9dwar4/)

**Positions [M].**
- "Surely it shouldn't be changed mid season. Seems ridiculous that a player could have a Mid/Fwd designation now (we draft accordingly) then you remove it after 3 weeks." (2026-08-14, https://www.reddit.com/r/SleeperFC/comments/1vo13vj/_/p3pahqh/)

**Assists / clean sheets / DGW [M].**
- "Sleeper doesn't count the 'pity' assists like fpl does. They only use official stats" (2026-09-19, https://www.reddit.com/r/SleeperFC/comments/1wk26g9/_/papu9yp/)
- "unable to replicate the 60 minute clean sheet rule" (2024-08-14, https://www.reddit.com/r/SleeperFC/comments/1esar45/_/li4vtza/)

**Bugs and live scoring [H]**
- "9am CST games have no scores and have not been marked as started. This is leading to iOS users being able to sub while Android users being locked out." (Google Play, 2026-01-17, 1★)
- Dev: "the Man U game was 'frozen'… I've updated it now" (2024-08-18, https://www.reddit.com/r/SleeperFC/comments/1evvrig/_/liw86t2/)

**Playoffs, support, web, data [H/M]**
- "Playoffs! Please don't abandon these mid season… we had to create our own app to track playoffs." (2026-05-30, https://www.reddit.com/r/SleeperFC/comments/1tr7v97/_/ooq685z/)
- "they said the desktop/browser version would launch by winter. It never launched." (2025-08-10, https://www.reddit.com/r/SleeperFC/comments/1mm4835/_/n7v1x5g/)
- "All with an ADP of 999.0. It makes mock drafts completely useless." (2025-08-07, https://www.reddit.com/r/SleeperFC/comments/1mkaanm/_/n7hnquh/)
- Betting/focus claim, anecdotal **[L]**: "Sleeper US added gambling… it's clear where their focus is" (2026-07-23, https://www.reddit.com/r/SleeperFC/comments/1v4en8i/_/oza9kpk/)

**Positives [M]**
- "I think Sleeper is a vast improvement on FPL site, the scoring alone makes it muchore [sic] fun" (2026-09-11, https://www.reddit.com/r/SleeperFC/comments/1wdeh4o/_/p95fjst/)

**Trend [M]:** in Aug 2026 the devs re-engaged and shipped 5-3-2, multi-position and auto-subs; formation complaints fell, but status flipping, position locking, waivers and playoffs persist.

## 3. B. Official FPL Draft pain points

Main evidence: 45 store reviews mentioning "draft" (29 are 1★; 18 from 2025, 20 from 2026). "Reddit (read)" = hand-verified items, floors **[L]**.

| Theme | Store n (of 45) | Reddit (read) | Share of 51 sources | Severity | Type |
|---|---|---|---|---|---|
| Draft tab crashes / won't load / "Oops… reload" | 29 | 3 | 63% | **Blocker** | Fix (new in-app Draft tab, 2025-26) |
| Logged out / sign-in loop | 7 | 0 | 14% | Major | Fix |
| Draft not native / web page (pre-2025) | 6 | 1 | 14% | Major | Fix (since integrated) |
| Live points slow, refresh broken | 5 | 2 | 14% | Major | Fix |
| Squad rules (one bench per position, 3 bench spots, no flexibility) | 0 | ≥5 | n/a | Major | Hard |
| Waivers: last place first, no bidding | 1 | ≥3 | n/a | Major | Hard |
| Trade UX ("needs approval… from whom?") | 0 | 2 | n/a | Minor | Fix |
| Switching between draft teams / classic ↔ draft | 4 | 0 | 8% | Minor | Fix |
| Auto-away / countdown skipping | 1 | 0 | n/a | Minor | Fix |
| No chat / notifications / customisation | 0 | 0 direct | n/a | n/a | Third-party tools exist |
| Positives (4-5★ reviews, mostly mixed) | 9 | few | n/a | n/a | n/a |

### Verbatim evidence (B)

**Reliability [H]**
- "draft app might as well not exist. works about 5% of the time (being generous)" (Google Play, 2026-09-07, 1★)
- "Constantly getting "Oops... something went wrong" message when I try and make changes to my draft team." (Google Play, 2025-12-13, 1★)
- "Keeps logging me out… this time it's kicked me out of my draft." (App Store AU, 2025-08-22, 1★). Another (Google Play, 2026-09-11, 2★): "Every time a deadline comes for waivers or open trades the app just crashes".
- "The app bugged out on me in one of the later rounds and automatically picked Andersen." (r/FPLDraft post, 2024-08-14, https://www.reddit.com/r/FPLDraft/comments/1erw3pg/)
- "Draft Website down. Is this down for everyone?" (r/FPLDraft, 2025-08-13, https://www.reddit.com/r/FPLDraft/comments/1moz04p/)

**Live points [M]**
- "What is the point of making bonus points live if fixtures, subs and tables don't update until the following day…" (r/DraftEPL, 2026-09-21, https://www.reddit.com/r/DraftEPL/comments/1wm84i5/; the post says "Official FPL app", so it may be about the classic game rather than Draft **[L]**)

**Rules [M]**
- "The official FPL site is unusable, the fact that you can only have one bench player per position is asinine" (r/SleeperFC, 2026-09-11, https://www.reddit.com/r/SleeperFC/comments/1wdeh4o/_/p96xshw/)
- "Draft waiver order. Please change that so that last place doesn't get the first waiver every week… set up a bid option" (App Store CA, 2022-10-02, 1★)
- "it's hard to keep a hurt player on your bench in FPL draft due to only having 3 spaces" (r/FPLDraft, 2024-10-17, https://www.reddit.com/r/FPLDraft/comments/1fzkqvp/_/lsebkxe/)
- "I tried to do so and it is showing as proposed trade and needs approval… but from whom?" (post, 2025-08-16, https://www.reddit.com/r/FPLDraft/comments/1mrx8rf/)

**Platform [M]**
- "Please make Draft and Fantasy separate, smaller, streamlined apps." (Google Play, 2026-02-06, 1★)
- "Please add an 'away' feature in the DRAFT format to avoid countless countdowns to zero" (App Store CA, 2021-12-29, 4★). "Avoid the draft league, really poor interface and constantly logs out." (Google Play, 2026-02-23, 1★)
- "not being live 3 weeks ahead of the season is a bit of a joke" (draft not yet open, r/FPLDraft, 2025-07-29, https://www.reddit.com/r/FPLDraft/comments/1m92bc9/_/n5pz7l6/)

**Positives:** "great fantasy and overall likes. really good for epl draft." (Google Play, 2025-11-14, 4★). Most 4-5★ draft reviews are mixed. The newest (Google Play, 2026-10-06, 2★): "the draft keeps crashing and freezing when loading".

## 4. Switching stories and feature requests

**Official FPL Draft → Sleeper (why):**
1. "We switched over from the official EPL draft fantasy app after playing draft for 7 years there as we use Sleeper for our NFL" (2024-08-20, https://www.reddit.com/r/SleeperFC/comments/1ewkmkl/_/lj0v7fu/). Drivers: NFL familiarity, UI, richer scoring; then waiver/lock bugs and "people have actually dropped out the league".
2. "moved from the original FPL draft app as it was boring (scoring)wise and no flexibility on random player inactivity last second" (2025-09-05, https://www.reddit.com/r/SleeperFC/comments/1n97o9j/_/ncl5tvk/)
3. "one of the reasons we went away from FPL app was the insane autosub swings" (2025-08-16, https://www.reddit.com/r/SleeperFC/comments/1mrxdff/_/n911rwn/). Some users left FPL *because* of its auto-sub behaviour.

**Sleeper ↔ Fantrax (why):**
1. "We actually already migrated! Switched from Fantrax to sleeper this year just to try something new, now going back to Fantrax" (2025-05-09, https://www.reddit.com/r/SleeperFC/comments/1khve98/_/mrenhni/). Cause: a blank-screen outage.
2. "Gonna be stuck with Fantrax's horrible UI this year because of this again" (2025-07-29, https://www.reddit.com/r/SleeperFC/comments/1lntxmo/_/n5uzj8c/). Cause: auto-subs missing.
3. "Sleeper as a platform for the prem is just not quite ready yet. Fantrax you can customize every fine detail" (r/DraftPL, 2026-09-03, https://www.reddit.com/r/DraftPL/comments/1w62zz8/_/p7jyz3j/)

**Feature requests (roughly by frequency, from the community lists at https://www.reddit.com/r/SleeperFC/comments/1eujzs6/ and https://www.reddit.com/r/SleeperFC/comments/1v8cgz5/):** general auto-subs; 5-3-2 / 1-forward formations; 60-minute clean sheet and fantasy assists; multi-position; one weekly waiver run, locked once a player has played; fixtures with difficulty; playoffs from any gameweek; median matchup; web/desktop; relegated players removed; commissioner-editable positions.

**Demand signals for a *new* app [L]:** almost no "I'd use an app that…" statements. The nearest proof is third-party builders around the official game: Draft Futbol (live-score app, annual subscription, https://www.reddit.com/r/FPLDraft/comments/1n31nlf/), FPLDraftNotifications (push alerts, https://www.reddit.com/r/FPLDraft/comments/1fb5g22/), FantasyFooty (https://www.reddit.com/r/FPLDraft/comments/1r00l62/) and a web app built because the official one is "quite poorly designed" (https://www.reddit.com/r/FPLDraft/comments/1kr2enp/). About 15 of ~2,700 r/FPLDraft + r/DraftEPL posts are "I built a tool" posts (regex count).

## 5. Demand signals (posts + comments per month)

| Month | r/SleeperFC | r/FPLDraft | r/DraftPL | r/DraftEPL (posts) |
|---|---|---|---|---|
| 2024-08 | 1,091 | 388 (posts only) | n/a | n/a |
| 2024-10 | 53 | 118 | n/a | n/a |
| 2025-08 | 517 | 924 | n/a | n/a |
| 2025-09 | 283 | 398 | n/a | n/a |
| 2025-10 | 147 | 158 | n/a | n/a |
| 2026-02 | 28 | 119 | n/a | n/a |
| 2026-03 to 07 | 7 to 77 | 0 to 5 | n/a | n/a |
| 2026-08 | 620 | 1 | 67 | 0 |
| 2026-09 | 676 | 2 | 495 | 661 |
| 2026-10 (to 4 Oct) | 33 | 0 | 16 | 33 |

- r/SleeperFC is seasonal and recovering: **Sept 2026 is 2.4× Sept 2025 [M]**; Aug 2026 is 57% of the Aug 2024 launch spike.
- r/FPLDraft collapsed after Feb 2026. Combined Sept 2026 volume on the two new subs (~1,150) is 2.9× r/FPLDraft Sept 2025. Closure vs migration is unverified **[L]**.
- Nothing here sizes the official FPL Draft player base.
## 6. Top 10 takeaways for an FPL-rules app with Sleeper-style UX

1. **Wedge = FPL rules fidelity + Sleeper polish [M].** Switchers love Sleeper's UX but threaten to leave when FPL-style rules (auto-subs, 5-3-2, 60-minute clean sheet, fantasy assists, multi-position) are missing. Caveat: others left FPL because its rules felt rigid ("one bench player per position is asinine"); consider "exact rules" as a mode.
2. **Reliability is the product [H].** The official Draft tab crashing (29 of 45 reviews) and Sleeper's waiver/lock/status bugs are the top pains. A stable draft room, waiver run and live scoring beat breadth.
3. **One configurable weekly waiver run between gameweeks**, with players who have played locked. Sleeper's biggest unresolved pain [H].
4. **Settle the lock/auto-sub model first [H].** FPL's lock-at-kickoff plus ordered auto-subs is what Sleeper's per-game lock lacks; West Coast and Australian leagues cite 4am kickoffs. Offer general auto-subs, not only 1-for-1.
5. **Never flip status or positions silently mid-week or mid-season [M].** GTD/OUT flips break claims and lineups. Pin positions at kickoff and log data changes.
6. **Key players by permanent `code`, with a fast new-signing pipeline [M]:** no ADP 999 gaps, no relegated clubs.
7. **Score like FPL, quirks included [M]** (BPS, fantasy assists, DGW totals). "Official Opta assists" and "best of two games" are Sleeper's top scoring complaints. Show per-stat breakdowns.
8. **Own the live layer [M].** Slow official points and no push notifications spawned paid third-party apps (Draft Futbol, FPLDraftNotifications). Fast points, push alerts and a head-to-head view are cheap wins.
9. **Web from day one [M].** Sleeper's missed desktop promise is cited repeatedly.
10. **Treat communication as a feature [M].** Dev silence and dropped playoffs turn complaints into departures. Publish a roadmap; give commissioners stat and position overrides.

*Open question:* how many would leave official FPL Draft for a rules-identical clone. Test with a waitlist or poll in r/DraftPL before building.