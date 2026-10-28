# Account deletion

`DELETE /v1/me` (App Store 5.1.1(v); UK GDPR erasure) removes the person
and keeps the leagues they played in playable for everyone else. What
happens, in order (`httpapi.DeleteMe`; steps 1 and 2 run in one
transaction under each league's lock, so no claim or offer made meanwhile
can survive on the orphaned team; leaving and removal by the commissioner
withdraw moves the same way):

1. **Pending moves are withdrawn.** In every in-season league the user's
   pending waiver claims are cancelled, offers they made are withdrawn and
   offers they received are declined. Trades already accepted (in review)
   run their course: they involve another manager too.
2. **The user leaves every league and the account goes, in the same
   transaction** (on Postgres; `league.RemoveUserEverywhereIn` and
   `auth.DeleteUserIn` share it), so a failure leaves nothing half done
   and a retry starts over:
   - before the draft their team is deleted;
   - after it the team stays with its roster, lineups, scores and history
     as an open seat ("Open seat"); it keeps playing on its saved or
     default lineup, during a draft it picks on autopick, and the next
     person to join the league with an invite takes it over;
   - if they were the commissioner, the role passes to the
     longest-standing remaining member; a pre-draft league left with no
     members is deleted; an in-season league with no one left keeps running
     without a commissioner;
   - the user row, every session (all tokens stop working at once), the
     Apple/Google/email identities, idempotency records and realtime
     tickets cascade, and the unused magic links (and their codes) sent to
     their email and its wrong-code records are deleted (an old link must
     not recreate the account). Activity-feed
     entries and draft picks keep the team, not the person.
3. **Open realtime sockets close** (4401) on every instance.
4. **The Apple token is revoked.** At Sign in with Apple the server
   exchanges the authorization code for a refresh token (`idp.AppleTokens`,
   client secret signed with the team's Sign in with Apple key) and keeps
   it sealed with AES-256-GCM (`APPLE_TOKEN_KEY`) on the identity, but
   only when the exchange's `id_token` is the signed-in identity (same
   subject and audience, and nonce when present): another account's code
   is discarded, so deleting this account never revokes that one. The
   tokens are read before step 2 and revoked with Apple
   (`/auth/revoke`) after it commits; a failure is logged, the deletion
   stands. Without the four `APPLE_*` settings (RUNBOOK) nothing is
   exchanged or revoked and the server logs an error at start-up: that
   configuration is required before App Store submission.

Deletion is not blocked for commissioners: the role is handed on instead,
so no league is left unmanaged while it has members. The prune job also
deletes expired or used magic links, expired sessions and idempotency
records over a day old.
