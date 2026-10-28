# Notifications

How pushes work and what the owner must configure. Design: ADR 0015.

## Types

Every type is on by default; Account → Notifications has a switch per
type and league, plus All on / All off. Every notification also lands in
the FEED inbox, whatever the switches say.

| Group    | Type                                 | When                                                         |
| -------- | ------------------------------------ | ------------------------------------------------------------ |
| Personal | `trade_offered`                      | a trade is offered to you                                    |
| Personal | `trade_accepted` / `trade_declined`  | your offer is accepted (or enters review) / declined         |
| Personal | `trade_reviewed`                     | your trade is vetoed, or approved by review                  |
| Personal | `trade_processed`                    | your trade goes through                                      |
| Personal | `waivers_due`                        | 1 hour before the league's waiver deadline                   |
| Personal | `free_agency_closing`                | 1 hour before the gameweek deadline, while free agency open  |
| Personal | `waiver_result`                      | each claim won or lost, with why                             |
| Personal | `player_goal` … `player_clean_sheet` | your players: goal, assist, own goal, red, DC points (once), |
|          |                                      | clean sheet (full time, or off after 60' with it intact)     |
| Personal | `player_news`                        | injury or status news changes                                |
| League   | `league_trade_processed`             | any other trade processed in your league                     |
| League   | `opponent_*`                         | the same live events and news for this week's opponent       |
| Extra    | `lineup_invalid`                     | 1 hour before lock with an empty or invalid XI               |
| Extra    | `draft_starting` / `draft_your_pick` | 1 hour before the draft / you are on the clock               |

## Owner setup (Web Push)

1. Make a key pair once: `npm run admin -- vapid-keys`. It prints three
   lines; keep the private key secret and never change it after launch
   (changing it orphans every subscription).
2. Set on the API (`deploy/.env` in production):
   `VAPID_PUBLIC_KEY`, `VAPID_PRIVATE_KEY`, `VAPID_SUBJECT`
   (`mailto:` your contact address). All three or none; none leaves Web
   Push off and the inbox still works. `/v1/config` then hands browsers
   the public key.
3. Serve the web app over HTTPS (browsers refuse push on plain HTTP
   except `localhost`).
4. iPhone and iPad (iOS 16.4+): Web Push works only for the web app
   added to the Home Screen (Share → Add to Home Screen), opened from
   that icon, and only after the user taps Enable in Account →
   Notifications. Safari tabs cannot receive pushes.
5. Native push (Expo) is off until there is an Apple developer account
   and an iOS build: `notifications.Expo` is a stub that drops native
   deliveries.

Job instances (`JOBS_ENABLED`) run the push dispatcher every 5 seconds;
deadline reminders ride the jobs tick, live events the poller.
