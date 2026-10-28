# 0013. A seat's claim link signs a signed-out person in (testing phase)

Status: accepted (2026-10-14). Amends 0012 ("Unclaimed seats and claim
links") while `CLAIM_LINK_SIGNIN` is on.

## Context

The owner is testing the app with friends on the imported real league
(ADR 0012). Sign-in emails only reach the server log for now, and Apple
and Google sign-in are not configured, so a remote friend cannot sign in
and so cannot claim their seat. Every seat already has a single-use claim
link that the commissioner shares by hand.

## Decision

- **The claim link is the credential.** A signed-out person who opens a
  seat's claim link sees "Claim <team> in <league>", enters only a
  display name and taps Claim. `POST /v1/auth/claim {code, displayName}`
  creates an account with that name (no email, no provider identity),
  gives it the seat and returns a normal session (access and refresh
  tokens, `isNewUser` true) plus the membership. On Postgres it is one
  transaction (`league.ClaimSeatIn` with `auth.CreateAccountIn`): the
  link is checked under the league's row lock before the account is
  made, so a refused claim makes no account and two people racing for a
  seat get one seat and one 409.
- **Single use, per seat.** A used link answers 409 `seat-claimed` ("<team>
  has already been claimed with this link. If that wasn't you, ask the
  commissioner."). The commissioner's "new link" revokes the old one,
  which then answers 410 `invite-expired`; an unknown code 404.
- **Only claim links.** An ordinary league invite answers 401
  `sign-in-required` here and still needs sign-in. A signed-in person
  keeps the existing flow (`POST /v1/invites/{code}/accept`, their own
  account); the app only calls the new endpoint when it has no session.
- **Rate limited** with the other `/v1/auth/*` routes per client IP
  (20/min, burst 10; Security in ARCHITECTURE.md).
- **The code is a credential.** It goes in the request body, not a URL the
  API logs; the access and panic logs write invite paths as
  `/v1/invites/{code}`. The web app sends `Referrer-Policy: no-referrer`
  (header and meta, like for magic links), and after a claim it replaces
  `/join/CODE` in the history with the league.
- **Setting.** `CLAIM_LINK_SIGNIN` turns it on: default on except with
  `APP_ENV=production`, where it must be set to `true` and the API logs a
  warning at startup. Off, `POST /v1/auth/claim` answers 401
  `claim-sign-in-off` and the invite preview's `claimSignIn` is false, so
  the app asks for sign-in as before. The production compose file sets
  it on for the testing phase.

## Consequences

- Whoever holds a seat's link gets the seat, with no proof of who they
  are: the same trust the commissioner already placed in sharing it, now
  without a second factor. Ten characters of a 30-letter alphabet (about
  49 bits) behind the per-IP limit cannot be guessed.
- **No email can be added yet.** The account has no sign-in method other
  than its session (refresh tokens last 60 days and rotate). Signing out,
  clearing the browser or a lost refresh token ends access for good; the
  account screen says so and asks before signing out. The commissioner
  can remove the member to free the seat and share a new link. Adding an
  email (or Apple/Google) to an existing account is not built; it is the
  step that should come before turning this off.
- Turn `CLAIM_LINK_SIGNIN` off once real email, Google or Apple sign-in
  works for the testers; accounts made this way keep working until their
  session ends.

## Addendum (2026-10-27): sign-in links

Production runs with `EMAIL_PROVIDER=none`, so the "no email can be
added yet" consequence above meant a friend who signed out, cleared the
browser or changed phone was locked out of their seat for good.

- **The commissioner issues a sign-in link** for a claimed seat:
  `POST /v1/leagues/{leagueId}/seats/{teamId}/signin-link` (league
  settings → Claim links → "New sign-in link"). It answers the link once
  (`{appURL}/signin-link/{code}`, `expiresAt`) and the app shows "Send
  this to <manager> to sign back in. Works once, expires in 7 days." with
  copy and share.
- **Opening it signs that account in** on any device:
  `POST /v1/auth/signin-link {code}` returns a normal session (a new one;
  the account's other sessions are untouched) and the league it was
  issued in, which the app opens, replacing `/signin-link/CODE` in the
  history. Unknown, used, replaced or expired links answer 401.
- **A new route, not the claim link's.** `/join/CODE` is a league invite
  with a preview; this is a credential with no preview and a different
  code (43 URL-safe characters, 256 bits), so `/signin-link/CODE` keeps
  the two apart and needs no guessing on the server.
- **Credential rules.** Only the code's SHA-256 is stored (`signin_links`,
  with the user and league; memory and Postgres stores alike). Single
  use (the row is deleted on use), 7 days, and issuing a new link for an
  account deletes its older unused ones. The code goes in the request
  body, never a logged URL; the web app sends no referrer.
- **Who and for whom.** Commissioner of an imported league only (403
  otherwise, 409 `not-imported` for a league made here). 409
  `seat-unclaimed` for a seat nobody has claimed (send its claim link),
  and 409 `has-sign-in` for an account with an email, Apple or Google
  identity: a commissioner can only reissue access to accounts that a
  claim link made, never take over one with its own sign-in. The
  commissioner's own team is refused unless the caller is that owner
  (always true with one commissioner; kept explicit in the handler).
- **Rate limited** per commissioner (burst 5, then 2 a minute,
  `RateLimits.SignInLinkUser`); using a link shares the per-IP
  `/v1/auth/*` limit.

Consequence: the commissioner can sign in as any claim-made member of
their league, the same trust they already hold by sharing claim links.
An account in several leagues can be reissued by any of their
commissioners. Adding an email (or Apple/Google) to an account is still
the way out of this, and remains out of scope.

### Final rules (2026-10-28, security review of PR #7)

- **Tap to redeem.** Opening `/signin-link/CODE` sends nothing. The app
  shows "Sign in to your team" and spends the code only when the person
  taps it, so link scanners and previews that open the link don't use it up.
- **Signed-in confirm.** Someone already signed in sees "You're signed in
  as <name>. Sign out and use this link?" and has to confirm. "Keep me
  signed in" leaves the session as it is.
- **URL scrubbed first.** The code moves into memory and
  `/signin-link/CODE` is replaced by `/signin-link` in the URL and history
  before any request. Nothing is sent until the stored session has loaded,
  so a late load cannot overwrite the new session. A reload after that
  has no code: the person opens the link they were sent again.
- **Target has a role elsewhere.** 409 `target-has-role-elsewhere` if the
  account commissions another league or plays in a league the issuer
  doesn't run. A commissioner can only reissue access that stays inside
  their own league.
- **Issuer and membership re-checks.** Each link records its issuer
  (`issuer_id`, migration 00022). Redeeming re-checks that the issuer is
  still the league's commissioner and that the account is still a member.
  A change of commissioner, or the member leaving or being removed,
  revokes the league's outstanding links.
- **No idempotency storage.** Credential routes ignore `Idempotency-Key`,
  so the issue response (which holds the code) is never kept in
  `idempotency_keys`. A retry issues a new link, and that revokes the
  old one.
