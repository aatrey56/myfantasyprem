package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/pb33f/libopenapi"
	validator "github.com/pb33f/libopenapi-validator"
	v3 "github.com/pb33f/libopenapi/datamodel/high/v3"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/idp"
	"github.com/aatrey56/myfantasyprem/server/internal/idp/idptest"
	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// contractCase is one request sent through the real handler. The request and
// the response are validated against api/openapi.yaml, so a handler that
// drifts from the contract (status, headers or body schema) fails here.
//
// Placeholders in target and body are replaced with fixture values created
// before the cases run (see contractFixture). Cases of one operation run in
// order against one server, so a later case can rely on state from an
// earlier one.
type contractCase struct {
	name       string
	method     string
	target     string
	body       string
	wantStatus int
	// as picks the caller: "" the commissioner of {league} (and of {full},
	// {live} and {paused}), "member" a second manager in {league},
	// "fmember" a second manager in {full}, {live} and {paused},
	// "outsider" a signed-in user not in them, "demo" the commissioner of
	// the demo league {season}.
	as string
	// headers are extra request headers, name then value.
	headers []string
	// invalidRequest marks sad paths whose request deliberately violates the
	// contract; only the response is validated.
	invalidRequest bool
}

// webPushBody is a browser subscription with a valid P-256 key and auth secret.
const webPushBody = `{"kind":"webpush","endpoint":"https://push.example/send/abc","keys":{"p256dh":"BNcRdreALRFXTkOOUHK1EtK2wtaz5Ry4YfYCA_0QTpQtUbVlUls0VJXg7A8u-Ts1XbjhazAkj7I99e8QcYP7DkM","auth":"tBHItJI5svbpez7KI4CCXg"}}`

// Ids that match the contract patterns but exist nowhere.
const (
	tLeague   = "lg_01k7m2xq8rfv3c9a4t6wbn5hdy"
	tTeam     = "tm_01k7m2xq8s0000000000000003"
	tMatchup  = "mu_01k7m2xq8t0000000000000072"
	tPlayer   = "plr_01k7m2xq8v00000000000000a1"
	tImportNA = "imp_01k7m2xq8y0000000000000001"
	tTrade    = "trd_01k7m2xq800000000000000301"
	tClaim    = "wv_01k7m2xq800000000000000201"
	tInviteNA = "inv_01k7m2xq800000000000000401"
	tUserNA   = "usr_01k7m2xq8z0000000000000009"
	tFixture  = "fx_01k7m2xq8w0000000000000046"
)

// pid returns the nth made-up player id.
func pid(n int) string { return fmt.Sprintf("plr_01k7m2xq8v0000000000000%03d", n) }

func pids(from, to int) string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, `"`+pid(i)+`"`)
	}
	return "[" + strings.Join(out, ",") + "]"
}

const (
	fxL = "/v1/leagues/{league}" // pre-draft league, created by the commissioner
	fxS = "/v1/leagues/{season}" // the demo league, in season at gameweek 7
	fxF = "/v1/leagues/{full}"   // full 4-team league before its draft
	fxD = "/v1/leagues/{live}"   // full 4-team league, drafting
	fxP = "/v1/leagues/{paused}" // full 4-team league, draft paused
	fxI = "/v1/leagues/{imp}"    // imported league, verified clean
	fxB = "/v1/leagues/{impbad}" // imported league, a mismatch
)

var lineupBody = `{"gameweek":8,"starters":` + pids(1, 11) + `,"bench":` + pids(12, 15) + `}`

// badLineup9 names players that are on no roster, for an open gameweek.
var badLineup9 = strings.Replace(lineupBody, `"gameweek":8`, `"gameweek":9`, 1)

// contractCases must have an entry for every operationId in the spec; the test
// fails when a new operation is added without a case.
var contractCases = map[string][]contractCase{
	"getHealth": {
		{name: "ok", method: "GET", target: "/healthz", wantStatus: 200},
	},
	// ---------------------------------------------------------------- auth
	"signInWithApple": {
		{name: "signed in", method: "POST", target: "/v1/auth/apple", body: `{"identityToken":"{appletoken}","authorizationCode":"c0de","nonce":"n0nce","givenName":"Sam","familyName":"Parker"}`, wantStatus: 200},
		{name: "nonce mismatch", method: "POST", target: "/v1/auth/apple", body: `{"identityToken":"{appletoken}","authorizationCode":"c0de","nonce":"other"}`, wantStatus: 401},
		// The nonce is required: without it a stolen identity token could be replayed.
		{name: "no nonce", method: "POST", target: "/v1/auth/apple", body: `{"identityToken":"{appletoken}","authorizationCode":"c0de"}`, wantStatus: 422, invalidRequest: true},
		{name: "not a token", method: "POST", target: "/v1/auth/apple", body: `{"identityToken":"eyJ.x.y","authorizationCode":"c0de","nonce":"n0nce"}`, wantStatus: 401},
		{name: "malformed json", method: "POST", target: "/v1/auth/apple", body: `{`, wantStatus: 400, invalidRequest: true},
	},
	"signInWithGoogle": {
		{name: "signed in", method: "POST", target: "/v1/auth/google", body: `{"idToken":"{googletoken}"}`, wantStatus: 200},
		// A nonce the client sends must be the one in the token.
		{name: "with nonce", method: "POST", target: "/v1/auth/google", body: `{"idToken":"{googlenoncetoken}","nonce":"g-n0nce"}`, wantStatus: 200},
		{name: "nonce mismatch", method: "POST", target: "/v1/auth/google", body: `{"idToken":"{googlenoncetoken}","nonce":"other"}`, wantStatus: 401},
		{name: "nonce not in token", method: "POST", target: "/v1/auth/google", body: `{"idToken":"{googletoken}","nonce":"g-n0nce"}`, wantStatus: 401},
		{name: "not a token", method: "POST", target: "/v1/auth/google", body: `{"idToken":"eyJ.x.y"}`, wantStatus: 401},
		{name: "missing token", method: "POST", target: "/v1/auth/google", body: `{}`, wantStatus: 422, invalidRequest: true},
	},
	"startEmailSignIn": {
		{name: "sent", method: "POST", target: "/v1/auth/email", body: `{"email":"new@example.com"}`, wantStatus: 202},
		{name: "not an email", method: "POST", target: "/v1/auth/email", body: `{"email":"nope"}`, wantStatus: 400, invalidRequest: true},
		{name: "with next", method: "POST", target: "/v1/auth/email", body: `{"email":"next@example.com","next":"/join/K7QM2XRP4D"}`, wantStatus: 202},
		// next is a path in the app, never somewhere else.
		{name: "next off-site", method: "POST", target: "/v1/auth/email", body: `{"email":"a@example.com","next":"//evil.example/x"}`, wantStatus: 422, invalidRequest: true},
		{name: "next a URL", method: "POST", target: "/v1/auth/email", body: `{"email":"b@example.com","next":"https://evil.example"}`, wantStatus: 422, invalidRequest: true},
		{name: "next backslash", method: "POST", target: "/v1/auth/email", body: `{"email":"c@example.com","next":"/\\evil.example"}`, wantStatus: 422, invalidRequest: true},
		{name: "next dot-dot", method: "POST", target: "/v1/auth/email", body: `{"email":"d@example.com","next":"/join/../x"}`, wantStatus: 422, invalidRequest: true},
	},
	"verifyEmailSignInCode": {
		{name: "signed in", method: "POST", target: "/v1/auth/email/code", body: `{"email":"Code@Example.com","code":"{magiccode}"}`, wantStatus: 200},
		{name: "used twice", method: "POST", target: "/v1/auth/email/code", body: `{"email":"code@example.com","code":"{magiccode}"}`, wantStatus: 401},
		{name: "wrong code", method: "POST", target: "/v1/auth/email/code", body: `{"email":"code@example.com","code":"{wrongcode}"}`, wantStatus: 401},
		{name: "not six digits", method: "POST", target: "/v1/auth/email/code", body: `{"email":"code@example.com","code":"12345"}`, wantStatus: 422, invalidRequest: true},
		{name: "no email", method: "POST", target: "/v1/auth/email/code", body: `{"code":"123456"}`, wantStatus: 422, invalidRequest: true},
	},
	"claimSeatSignIn": {
		{name: "claimed and signed in", method: "POST", target: "/v1/auth/claim", body: `{"code":"{iclaim}","displayName":"Jordan K."}`, wantStatus: 200},
		{name: "used link", method: "POST", target: "/v1/auth/claim", body: `{"code":"{iclaim}","displayName":"Again"}`, wantStatus: 409},
		{name: "ordinary invite", method: "POST", target: "/v1/auth/claim", body: `{"code":"{code}","displayName":"X"}`, wantStatus: 401},
		{name: "unknown code", method: "POST", target: "/v1/auth/claim", body: `{"code":"ZZZZ2222ZZ","displayName":"X"}`, wantStatus: 404},
		{name: "no name", method: "POST", target: "/v1/auth/claim", body: `{"code":"{iclaim}","displayName":""}`, wantStatus: 422, invalidRequest: true},
		{name: "malformed code", method: "POST", target: "/v1/auth/claim", body: `{"code":"no","displayName":"X"}`, wantStatus: 400, invalidRequest: true},
	},
	"signInWithLink": {
		{name: "signed in", method: "POST", target: "/v1/auth/signin-link", body: `{"code":"{slink}"}`, wantStatus: 200},
		{name: "used twice", method: "POST", target: "/v1/auth/signin-link", body: `{"code":"{slink}"}`, wantStatus: 401},
		{name: "malformed", method: "POST", target: "/v1/auth/signin-link", body: `{"code":"short"}`, wantStatus: 400, invalidRequest: true},
	},
	"verifyEmailSignIn": {
		{name: "signed in", method: "POST", target: "/v1/auth/email/verify", body: `{"token":"{magic}"}`, wantStatus: 200},
		{name: "used twice", method: "POST", target: "/v1/auth/email/verify", body: `{"token":"{magic}"}`, wantStatus: 401},
	},
	"refreshSession": {
		{name: "rotated", method: "POST", target: "/v1/auth/refresh", body: `{"refreshToken":"{refresh}"}`, wantStatus: 200},
		{name: "unknown", method: "POST", target: "/v1/auth/refresh", body: `{"refreshToken":"0123456789abcdef0123456789"}`, wantStatus: 401},
	},
	"logout": {
		{name: "signed out", method: "POST", target: "/v1/auth/logout", wantStatus: 204},
	},
	// ---------------------------------------------------------------- me
	"getMe": {
		{name: "ok", method: "GET", target: "/v1/me", wantStatus: 200},
	},
	"updateMe": {
		{name: "renamed", method: "PATCH", target: "/v1/me", body: `{"displayName":"Sam Parker","avatarColor":"#2f6fed"}`, wantStatus: 200},
		{name: "blank name", method: "PATCH", target: "/v1/me", body: `{"displayName":"   "}`, wantStatus: 422},
		{name: "colour cleared", method: "PATCH", target: "/v1/me", body: `{"avatarColor":null}`, wantStatus: 200},
	},
	"deleteMe": {
		{name: "deleted", method: "DELETE", target: "/v1/me", wantStatus: 204},
		{name: "token gone", method: "GET", target: "/v1/me", wantStatus: 401},
	},
	"listMyLeagues": {
		{name: "ok", method: "GET", target: "/v1/me/leagues", wantStatus: 200},
		{name: "in season, with record and matchup", method: "GET", target: "/v1/me/leagues", as: "demo", wantStatus: 200},
	},
	// ---------------------------------------------------------------- leagues
	"createLeague": {
		{name: "created", method: "POST", target: "/v1/leagues", body: `{"name":"Office League","teamCount":10,"teamName":"Heroes","settings":{"trades":{"review":"league_vote"}}}`, wantStatus: 201, headers: []string{"Idempotency-Key", "create-league-0001"}},
		{name: "idempotent replay", method: "POST", target: "/v1/leagues", body: `{"name":"Office League","teamCount":10,"teamName":"Heroes","settings":{"trades":{"review":"league_vote"}}}`, wantStatus: 201, headers: []string{"Idempotency-Key", "create-league-0001"}},
		{name: "key reused for another body", method: "POST", target: "/v1/leagues", body: `{"name":"Other League","teamCount":8,"teamName":"Heroes"}`, wantStatus: 422, headers: []string{"Idempotency-Key", "create-league-0001"}},
		{name: "too few teams", method: "POST", target: "/v1/leagues", body: `{"name":"Tiny","teamCount":1,"teamName":"H"}`, wantStatus: 422, invalidRequest: true},
		{name: "two teams", method: "POST", target: "/v1/leagues", body: `{"name":"Two of Us","teamCount":2,"teamName":"H"}`, wantStatus: 201},
	},
	"getLeague": {
		{name: "in season", method: "GET", target: fxS, as: "demo", wantStatus: 200},
		{name: "pre-draft", method: "GET", target: fxL, wantStatus: 200},
		{name: "outsider", method: "GET", target: fxS, as: "outsider", wantStatus: 403},
		{name: "unknown", method: "GET", target: "/v1/leagues/" + tLeague, wantStatus: 404},
		{name: "malformed id", method: "GET", target: "/v1/leagues/lg_1", wantStatus: 400, invalidRequest: true},
	},
	"updateLeague": {
		{name: "renamed", method: "PATCH", target: fxL, body: `{"name":"Renamed League","settings":{"draft":{"pickTimerSeconds":90}}}`, wantStatus: 200},
		{name: "start and trade deadline set", method: "PATCH", target: fxL, body: `{"settings":{"draft":{"startsAt":"2027-01-01T18:00:00Z"},"trades":{"deadlineGameweek":30}}}`, wantStatus: 200},
		{name: "start and trade deadline cleared", method: "PATCH", target: fxL, body: `{"settings":{"draft":{"startsAt":null},"trades":{"deadlineGameweek":null}}}`, wantStatus: 200},
		{name: "member is not commissioner", method: "PATCH", target: fxL, body: `{"name":"Mine Now"}`, as: "member", wantStatus: 403},
		{name: "unknown league", method: "PATCH", target: "/v1/leagues/" + tLeague, body: `{"name":"Whatever"}`, wantStatus: 404},
	},
	"listMembers": {
		{name: "ok", method: "GET", target: fxL + "/members", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxL + "/members", as: "outsider", wantStatus: 403},
	},
	"removeMember": {
		{name: "commissioner cannot remove self", method: "DELETE", target: fxL + "/members/{me}", wantStatus: 409},
		{name: "member cannot remove", method: "DELETE", target: fxL + "/members/{me}", as: "member", wantStatus: 403},
		{name: "removed", method: "DELETE", target: fxL + "/members/{member}", wantStatus: 204},
		{name: "not in league", method: "DELETE", target: fxL + "/members/" + tUserNA, wantStatus: 404},
	},
	"leaveLeague": {
		{name: "commissioner must hand over", method: "POST", target: fxL + "/leave", wantStatus: 409},
		{name: "left", method: "POST", target: fxL + "/leave", as: "member", wantStatus: 204},
	},
	"transferCommissioner": {
		{name: "member is not commissioner", method: "POST", target: fxL + "/commissioner", body: `{"userId":"{me}"}`, as: "member", wantStatus: 403},
		{name: "not in league", method: "POST", target: fxL + "/commissioner", body: `{"userId":"` + tUserNA + `"}`, wantStatus: 404},
		{name: "malformed user id", method: "POST", target: fxL + "/commissioner", body: `{"userId":"usr_1"}`, wantStatus: 400, invalidRequest: true},
		{name: "handed over", method: "POST", target: fxL + "/commissioner", body: `{"userId":"{member}"}`, wantStatus: 200},
		{name: "former commissioner can leave", method: "POST", target: fxL + "/leave", wantStatus: 204},
	},
	"listInvites": {
		{name: "ok", method: "GET", target: fxL + "/invites", wantStatus: 200},
		{name: "member", method: "GET", target: fxL + "/invites", as: "member", wantStatus: 403},
	},
	"createInvite": {
		{name: "created", method: "POST", target: fxL + "/invites", body: `{"maxUses":5,"expiresInHours":72}`, wantStatus: 201},
		{name: "member", method: "POST", target: fxL + "/invites", body: `{}`, as: "member", wantStatus: 403},
	},
	"revokeInvite": {
		{name: "revoked", method: "DELETE", target: fxL + "/invites/{invite}", wantStatus: 204},
		{name: "unknown", method: "DELETE", target: fxL + "/invites/" + tInviteNA, wantStatus: 404},
	},
	"getInvite": {
		{name: "preview", method: "GET", target: "/v1/invites/{code}", wantStatus: 200},
		{name: "unknown", method: "GET", target: "/v1/invites/ZZZZ2222", wantStatus: 404},
		{name: "malformed", method: "GET", target: "/v1/invites/no", wantStatus: 400, invalidRequest: true},
	},
	"acceptInvite": {
		{name: "joined", method: "POST", target: "/v1/invites/{code}/accept", body: `{"teamName":"Third Team"}`, as: "outsider", wantStatus: 201},
		{name: "already a member", method: "POST", target: "/v1/invites/{code}/accept", body: `{"teamName":"Again"}`, as: "member", wantStatus: 409},
		{name: "unknown code", method: "POST", target: "/v1/invites/ZZZZ2222/accept", body: `{"teamName":"X"}`, wantStatus: 404},
	},
	"getTeam": {
		{name: "drafted team", method: "GET", target: fxS + "/teams/{steam}", as: "demo", wantStatus: 200},
		{name: "pre-draft team", method: "GET", target: fxL + "/teams/{team}", wantStatus: 200},
		{name: "unknown team", method: "GET", target: fxS + "/teams/" + tTeam, as: "demo", wantStatus: 404},
		{name: "wrong prefix", method: "GET", target: "/v1/leagues/" + tLeague + "/teams/" + tPlayer, wantStatus: 400, invalidRequest: true},
	},
	"updateTeam": {
		{name: "renamed", method: "PATCH", target: fxL + "/teams/{team}", body: `{"name":"New Name FC"}`, wantStatus: 200},
		{name: "not the owner", method: "PATCH", target: fxL + "/teams/{team}", body: `{"name":"Mine"}`, as: "member", wantStatus: 403},
	},
	"listStandings": {
		{name: "in season", method: "GET", target: fxS + "/standings", as: "demo", wantStatus: 200},
		{name: "pre-draft", method: "GET", target: fxL + "/standings", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxL + "/standings", as: "outsider", wantStatus: 403},
	},
	"getSchedule": {
		{name: "in season", method: "GET", target: fxS + "/schedule", as: "demo", wantStatus: 200},
		{name: "before the draft", method: "GET", target: fxL + "/schedule", wantStatus: 200},
	},
	"getLeagueStatus": {
		{name: "in season", method: "GET", target: fxS + "/status", as: "demo", wantStatus: 200},
		{name: "before the draft", method: "GET", target: fxL + "/status", wantStatus: 409},
		{name: "unknown league", method: "GET", target: "/v1/leagues/" + tLeague + "/status", wantStatus: 404},
		{name: "outsider", method: "GET", target: fxL + "/status", as: "outsider", wantStatus: 403},
		{name: "unknown league", method: "GET", target: "/v1/leagues/" + tLeague + "/status", wantStatus: 404},
		{name: "malformed league id", method: "GET", target: "/v1/leagues/nope/status", wantStatus: 400, invalidRequest: true},
	},
	// ---------------------------------------------------------------- projections (ADR 0014)
	"getFixtureDifficulty": {
		{name: "ok", method: "GET", target: "/v1/fixture-difficulty", wantStatus: 200},
	},
	"getLeagueProjections": {
		{name: "live gameweek", method: "GET", target: fxS + "/gameweeks/7/projections", as: "demo", wantStatus: 200},
		{name: "final gameweek", method: "GET", target: fxS + "/gameweeks/3/projections", as: "demo", wantStatus: 200},
		{name: "before the deadline", method: "GET", target: fxS + "/gameweeks/12/projections", as: "demo", wantStatus: 200},
		{name: "before the draft", method: "GET", target: fxL + "/gameweeks/7/projections", wantStatus: 409},
		{name: "outsider", method: "GET", target: fxL + "/gameweeks/7/projections", as: "outsider", wantStatus: 403},
		{name: "gameweek out of range", method: "GET", target: fxS + "/gameweeks/39/projections", as: "demo", wantStatus: 400, invalidRequest: true},
	},
	"getStartSitAccuracy": {
		{name: "in season", method: "GET", target: fxS + "/start-sit", as: "demo", wantStatus: 200},
		{name: "before the draft", method: "GET", target: fxL + "/start-sit", wantStatus: 409},
		{name: "malformed league id", method: "GET", target: "/v1/leagues/nope/start-sit", wantStatus: 400, invalidRequest: true},
	},
	// ---------------------------------------------------------------- draft
	"getDraft": {
		{name: "scheduled", method: "GET", target: fxL + "/draft", wantStatus: 200},
		{name: "drafting", method: "GET", target: fxD + "/draft", as: "fmember", wantStatus: 200},
		{name: "paused", method: "GET", target: fxP + "/draft", wantStatus: 200},
		{name: "complete", method: "GET", target: fxS + "/draft", as: "demo", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxL + "/draft", as: "outsider", wantStatus: 403},
		{name: "malformed league", method: "GET", target: "/v1/leagues/nope/draft", wantStatus: 400, invalidRequest: true},
	},
	"updateDraft": {
		{name: "randomised", method: "PATCH", target: fxF + "/draft", body: `{"pickTimerSeconds":90,"randomizeOrder":true,"startsAt":"2026-10-20T19:00:00Z"}`, wantStatus: 200},
		{name: "start cleared", method: "PATCH", target: fxF + "/draft", body: `{"startsAt":null}`, wantStatus: 200},
		{name: "ordered", method: "PATCH", target: fxL + "/draft", body: `{"order":["{team}"]}`, wantStatus: 422},
		{name: "member", method: "PATCH", target: fxF + "/draft", body: `{"randomizeOrder":true}`, as: "fmember", wantStatus: 403},
		{name: "started", method: "PATCH", target: fxD + "/draft", body: `{"randomizeOrder":true}`, wantStatus: 409},
		{name: "complete", method: "PATCH", target: fxS + "/draft", body: `{"randomizeOrder":true}`, as: "demo", wantStatus: 409},
		{name: "empty", method: "PATCH", target: fxF + "/draft", body: `{}`, wantStatus: 422, invalidRequest: true},
	},
	"startDraft": {
		{name: "started", method: "POST", target: fxF + "/draft/start", wantStatus: 200, headers: []string{"Idempotency-Key", "start-draft-0001"}},
		{name: "replayed", method: "POST", target: fxF + "/draft/start", wantStatus: 200, headers: []string{"Idempotency-Key", "start-draft-0001"}},
		{name: "already started", method: "POST", target: fxF + "/draft/start", wantStatus: 409},
		{name: "two of ten managers", method: "POST", target: fxL + "/draft/start", wantStatus: 200},
		{name: "member", method: "POST", target: fxD + "/draft/start", as: "fmember", wantStatus: 403},
	},
	"pauseDraft": {
		{name: "paused", method: "POST", target: fxD + "/draft/pause", wantStatus: 200},
		{name: "already paused", method: "POST", target: fxD + "/draft/pause", wantStatus: 409},
		{name: "member", method: "POST", target: fxP + "/draft/pause", as: "fmember", wantStatus: 403},
	},
	"resumeDraft": {
		{name: "resumed", method: "POST", target: fxP + "/draft/resume", wantStatus: 200},
		{name: "not paused", method: "POST", target: fxD + "/draft/resume", wantStatus: 409},
	},
	"makeDraftPick": {
		{name: "picked", method: "POST", target: fxD + "/draft/picks", body: `{"playerId":"{p1}"}`, wantStatus: 201, headers: []string{"Idempotency-Key", "pick-000001"}},
		{name: "replayed", method: "POST", target: fxD + "/draft/picks", body: `{"playerId":"{p1}"}`, wantStatus: 201, headers: []string{"Idempotency-Key", "pick-000001"}},
		{name: "taken", method: "POST", target: fxD + "/draft/picks", body: `{"playerId":"{p1}"}`, wantStatus: 409},
		{name: "commissioner picks for the clock", method: "POST", target: fxD + "/draft/picks", body: `{"playerId":"{p2}"}`, wantStatus: 201},
		{name: "not your turn", method: "POST", target: fxD + "/draft/picks", body: `{"playerId":"{p3}"}`, as: "fmember", wantStatus: 409},
		{name: "unknown player", method: "POST", target: fxD + "/draft/picks", body: `{"playerId":"` + tPlayer + `"}`, wantStatus: 404},
		{name: "paused", method: "POST", target: fxP + "/draft/picks", body: `{"playerId":"{p3}"}`, wantStatus: 409},
		{name: "outsider", method: "POST", target: fxD + "/draft/picks", body: `{"playerId":"{p3}"}`, as: "outsider", wantStatus: 403},
		{name: "bad idempotency key", method: "POST", target: fxD + "/draft/picks", body: `{"playerId":"{p3}"}`, wantStatus: 400, headers: []string{"Idempotency-Key", "no spaces"}, invalidRequest: true},
	},
	"getDraftQueue": {
		{name: "empty", method: "GET", target: fxD + "/draft/queue", as: "fmember", wantStatus: 200},
		{name: "after the draft (empty)", method: "GET", target: fxS + "/draft/queue", as: "demo", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxD + "/draft/queue", as: "outsider", wantStatus: 403},
	},
	"setDraftQueue": {
		{name: "set", method: "PUT", target: fxD + "/draft/queue", body: `{"playerIds":["{p2}","{p3}"]}`, as: "fmember", wantStatus: 200},
		{name: "unknown players", method: "PUT", target: fxD + "/draft/queue", body: `{"playerIds":` + pids(1, 3) + `}`, as: "fmember", wantStatus: 422},
	},
	"setDraftAutopick": {
		{name: "on", method: "PUT", target: fxD + "/draft/autopick", body: `{"enabled":true}`, as: "fmember", wantStatus: 200},
		{name: "on the clock picks at once", method: "PUT", target: fxD + "/draft/autopick", body: `{"enabled":true}`, wantStatus: 200},
		{name: "off", method: "PUT", target: fxD + "/draft/autopick", body: `{"enabled":false}`, wantStatus: 200},
	},
	// ---------------------------------------------------------------- lineups
	"getLineup": {
		{name: "started gameweek", method: "GET", target: fxS + "/teams/{oteam}/lineup?gameweek=7", as: "demo", wantStatus: 200},
		{name: "own lineup before the deadline", method: "GET", target: fxS + "/teams/{steam}/lineup?gameweek=9", as: "demo", wantStatus: 200},
		{name: "default gameweek", method: "GET", target: fxS + "/teams/{steam}/lineup", as: "demo", wantStatus: 200},
		{name: "private before the deadline", method: "GET", target: fxS + "/teams/{oteam}/lineup?gameweek=9", as: "demo", wantStatus: 403},
		{name: "no roster before the draft", method: "GET", target: fxL + "/teams/{team}/lineup?gameweek=8", wantStatus: 404},
		{name: "gameweek out of range", method: "GET", target: fxL + "/teams/{team}/lineup?gameweek=39", wantStatus: 400, invalidRequest: true},
	},
	"setLineup": {
		{name: "saved", method: "PUT", target: fxS + "/teams/{steam}/lineup", body: `{lineup9}`, as: "demo", wantStatus: 200},
		{name: "locked gameweek", method: "PUT", target: fxS + "/teams/{steam}/lineup", body: `{lineup7}`, as: "demo", wantStatus: 409},
		{name: "invalid", method: "PUT", target: fxS + "/teams/{steam}/lineup", body: badLineup9, as: "demo", wantStatus: 422},
		{name: "not the owner", method: "PUT", target: fxS + "/teams/{oteam}/lineup", body: `{lineup9}`, as: "demo", wantStatus: 403},
		{name: "before the draft", method: "PUT", target: fxL + "/teams/{team}/lineup", body: lineupBody, wantStatus: 409},
		{name: "malformed team", method: "PUT", target: fxL + "/teams/tm_1/lineup", body: lineupBody, wantStatus: 400, invalidRequest: true},
	},
	// ---------------------------------------------------------------- players
	"listLeaguePlayers": {
		{name: "available by rank", method: "GET", target: fxS + "/players?status=available&position=MID&club=NOR&sort=draft_rank&limit=10", as: "demo", wantStatus: 200},
		{name: "rostered, paged", method: "GET", target: fxS + "/players?status=rostered&sort=form&limit=5", as: "demo", wantStatus: 200},
		{name: "search, deprecated alias", method: "GET", target: fxS + "/players?available=true&q=a&sort=name", as: "demo", wantStatus: 200},
		{name: "one team", method: "GET", target: fxS + "/players?teamId={steam}&sort=gameweek_points", as: "demo", wantStatus: 200},
		{name: "risers", method: "GET", target: fxS + "/players?sort=form_rise&limit=5", as: "demo", wantStatus: 200},
		{name: "fallers", method: "GET", target: fxS + "/players?sort=form_fall&position=DEF&limit=5", as: "demo", wantStatus: 200},
		{name: "watchlist (empty)", method: "GET", target: fxS + "/players?watchlist=true", as: "demo", wantStatus: 200},
		{name: "tagged (empty)", method: "GET", target: fxS + "/players?tag=hot", as: "demo", wantStatus: 200},
		{name: "unknown tag", method: "GET", target: fxS + "/players?tag=meh", as: "demo", wantStatus: 400, invalidRequest: true},
		{name: "bad cursor", method: "GET", target: fxS + "/players?cursor=nope", as: "demo", wantStatus: 400},
		{name: "outsider", method: "GET", target: fxS + "/players", as: "outsider", wantStatus: 403},
		{name: "malformed league id", method: "GET", target: "/v1/leagues/nope/players", wantStatus: 400, invalidRequest: true},
		{name: "bad boolean", method: "GET", target: "/v1/leagues/" + tLeague + "/players?available=maybe", wantStatus: 400, invalidRequest: true},
	},
	"getLeaguePlayer": {
		{name: "rostered", method: "GET", target: fxS + "/players/{plr}", as: "demo", wantStatus: 200},
		{name: "free agent", method: "GET", target: fxS + "/players/{fa}", as: "demo", wantStatus: 200},
		{name: "previous seasons", method: "GET", target: fxS + "/players/{hist}", as: "demo", wantStatus: 200},
		{name: "unknown", method: "GET", target: fxS + "/players/" + tPlayer, as: "demo", wantStatus: 404},
		{name: "fpl integer id", method: "GET", target: "/v1/leagues/" + tLeague + "/players/223094", wantStatus: 400, invalidRequest: true},
		{name: "outsider", method: "GET", target: fxS + "/players/{plr}", as: "outsider", wantStatus: 403},
		{name: "unknown league", method: "GET", target: "/v1/leagues/" + tLeague + "/players/" + tPlayer, wantStatus: 404},
		{name: "malformed league id", method: "GET", target: "/v1/leagues/nope/players/" + tPlayer, wantStatus: 400, invalidRequest: true},
	},
	"getWatchlist": {
		{name: "empty", method: "GET", target: fxS + "/watchlist", as: "demo", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxS + "/watchlist", as: "outsider", wantStatus: 403},
		{name: "malformed league id", method: "GET", target: "/v1/leagues/nope/watchlist", wantStatus: 400, invalidRequest: true},
	},
	"watchPlayer": {
		{name: "watched", method: "PUT", target: fxS + "/watchlist/{fa}", as: "demo", wantStatus: 200},
		{name: "again", method: "PUT", target: fxS + "/watchlist/{fa}", as: "demo", wantStatus: 200},
		{name: "unknown player", method: "PUT", target: fxS + "/watchlist/" + tPlayer, as: "demo", wantStatus: 404},
		{name: "outsider", method: "PUT", target: fxS + "/watchlist/{fa}", as: "outsider", wantStatus: 403},
		{name: "fpl integer id", method: "PUT", target: fxS + "/watchlist/223094", as: "demo", wantStatus: 400, invalidRequest: true},
	},
	"unwatchPlayer": {
		{name: "removed", method: "DELETE", target: fxS + "/watchlist/{fa}", as: "demo", wantStatus: 204},
		{name: "not watched", method: "DELETE", target: fxS + "/watchlist/{plr}", as: "demo", wantStatus: 204},
		{name: "outsider", method: "DELETE", target: fxS + "/watchlist/{fa}", as: "outsider", wantStatus: 403},
	},
	// ---------------------------------------------------------- notifications
	"listNotifications": {
		{name: "empty inbox", method: "GET", target: "/v1/me/notifications", wantStatus: 200},
		{name: "unread only", method: "GET", target: "/v1/me/notifications?unread=true&limit=10", wantStatus: 200},
		{name: "limit too high", method: "GET", target: "/v1/me/notifications?limit=500", wantStatus: 400, invalidRequest: true},
	},
	"markNotificationsRead": {
		{name: "all", method: "POST", target: "/v1/me/notifications/read", body: `{"all":true}`, wantStatus: 204},
		{name: "unknown ids are ignored", method: "POST", target: "/v1/me/notifications/read", body: `{"ids":["ntf_01k7m2xq8z0000000000000001"]}`, wantStatus: 204},
		{name: "malformed id", method: "POST", target: "/v1/me/notifications/read", body: `{"ids":["nope"]}`, wantStatus: 400, invalidRequest: true},
	},
	"getNotificationPreferences": {
		{name: "defaults on", method: "GET", target: "/v1/me/notifications/preferences", as: "demo", wantStatus: 200},
	},
	"updateNotificationPreferences": {
		{name: "one league, all off then one on", method: "PUT", target: "/v1/me/notifications/preferences", as: "demo",
			body: `{"leagueId":"{season}","all":false,"settings":[{"type":"trade_offered","enabled":true}]}`, wantStatus: 200},
		{name: "every league", method: "PUT", target: "/v1/me/notifications/preferences", body: `{"all":true}`, wantStatus: 200},
		{name: "unknown type", method: "PUT", target: "/v1/me/notifications/preferences",
			body: `{"settings":[{"type":"nope","enabled":false}]}`, wantStatus: 422},
		{name: "outsider", method: "PUT", target: "/v1/me/notifications/preferences", as: "outsider",
			body: `{"leagueId":"{league}","all":false}`, wantStatus: 403},
		{name: "unknown league", method: "PUT", target: "/v1/me/notifications/preferences",
			body: `{"leagueId":"` + tLeague + `","all":false}`, wantStatus: 404},
	},
	"createPushSubscription": {
		{name: "webpush", method: "POST", target: "/v1/me/push-subscriptions", body: webPushBody, wantStatus: 201},
		{name: "webpush again", method: "POST", target: "/v1/me/push-subscriptions", body: webPushBody, wantStatus: 201},
		{name: "expo token", method: "POST", target: "/v1/me/push-subscriptions",
			body: `{"kind":"expo","expoToken":"ExponentPushToken[abc123]"}`, wantStatus: 201},
		{name: "http endpoint", method: "POST", target: "/v1/me/push-subscriptions",
			body: `{"kind":"webpush","endpoint":"http://push.example/1","keys":{"p256dh":"x","auth":"y"}}`, wantStatus: 422},
		{name: "bad keys", method: "POST", target: "/v1/me/push-subscriptions",
			body: `{"kind":"webpush","endpoint":"https://push.example/1","keys":{"p256dh":"x","auth":"y"}}`, wantStatus: 422},
		{name: "unknown kind", method: "POST", target: "/v1/me/push-subscriptions", body: `{"kind":"pigeon"}`, wantStatus: 422},
	},
	"deletePushSubscription": {
		{name: "unknown is fine", method: "DELETE", target: "/v1/me/push-subscriptions/psub_01k7m2xq8z0000000000000001", wantStatus: 204},
		{name: "malformed id", method: "DELETE", target: "/v1/me/push-subscriptions/nope", wantStatus: 400, invalidRequest: true},
	},
	// ---------------------------------------------------------------- wave C
	"listTradeBlock": {
		{name: "empty", method: "GET", target: fxS + "/trade-block", as: "demo", wantStatus: 200},
		{name: "before the draft", method: "GET", target: fxL + "/trade-block", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxS + "/trade-block", as: "outsider", wantStatus: 403},
		{name: "malformed league id", method: "GET", target: "/v1/leagues/nope/trade-block", wantStatus: 400, invalidRequest: true},
	},
	"setTradeBlockEntry": {
		{name: "on the block", method: "PUT", target: fxS + "/trade-block/{bplr}", body: `{"note":"Open to offers","wanted":["MID","DEF"]}`, as: "demo", wantStatus: 200},
		{name: "no body fields", method: "PUT", target: fxS + "/trade-block/{bplr}", body: `{}`, as: "demo", wantStatus: 200},
		{name: "another team's player", method: "PUT", target: fxS + "/trade-block/{oplr}", body: `{}`, as: "demo", wantStatus: 409},
		{name: "before the draft", method: "PUT", target: fxL + "/trade-block/" + tPlayer, body: `{}`, wantStatus: 409},
		{name: "note too long", method: "PUT", target: fxS + "/trade-block/{bplr}", body: `{"note":"` + strings.Repeat("x", 141) + `"}`, as: "demo", wantStatus: 422, invalidRequest: true},
		{name: "outsider", method: "PUT", target: fxS + "/trade-block/{bplr}", body: `{}`, as: "outsider", wantStatus: 403},
		{name: "fpl integer id", method: "PUT", target: fxS + "/trade-block/223094", body: `{}`, as: "demo", wantStatus: 400, invalidRequest: true},
	},
	"removeTradeBlockEntry": {
		{name: "removed", method: "DELETE", target: fxS + "/trade-block/{bplr}", as: "demo", wantStatus: 204},
		{name: "not on the block", method: "DELETE", target: fxS + "/trade-block/{bplr}", as: "demo", wantStatus: 204},
		{name: "outsider", method: "DELETE", target: fxS + "/trade-block/{bplr}", as: "outsider", wantStatus: 403},
	},
	"listPlayerNews": {
		{name: "league", method: "GET", target: fxS + "/news?limit=5", as: "demo", wantStatus: 200},
		{name: "my team", method: "GET", target: fxS + "/news?teamId={steam}", as: "demo", wantStatus: 200},
		{name: "unknown team", method: "GET", target: fxS + "/news?teamId=" + tTeam, as: "demo", wantStatus: 404},
		{name: "outsider", method: "GET", target: fxS + "/news", as: "outsider", wantStatus: 403},
		{name: "malformed team", method: "GET", target: fxS + "/news?teamId=tm_1", as: "demo", wantStatus: 400, invalidRequest: true},
	},
	"listPlayerTags": {
		{name: "empty", method: "GET", target: fxS + "/tags", as: "demo", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxS + "/tags", as: "outsider", wantStatus: 403},
	},
	"setPlayerTag": {
		{name: "tagged", method: "PUT", target: fxS + "/tags/{fa}", body: `{"tag":"target"}`, as: "demo", wantStatus: 200},
		{name: "retagged", method: "PUT", target: fxS + "/tags/{fa}", body: `{"tag":"avoid"}`, as: "demo", wantStatus: 200},
		{name: "unknown tag", method: "PUT", target: fxS + "/tags/{fa}", body: `{"tag":"meh"}`, as: "demo", wantStatus: 422, invalidRequest: true},
		{name: "unknown player", method: "PUT", target: fxS + "/tags/" + tPlayer, body: `{"tag":"hot"}`, as: "demo", wantStatus: 404},
		{name: "outsider", method: "PUT", target: fxS + "/tags/{fa}", body: `{"tag":"hot"}`, as: "outsider", wantStatus: 403},
	},
	"removePlayerTag": {
		{name: "removed", method: "DELETE", target: fxS + "/tags/{fa}", as: "demo", wantStatus: 204},
		{name: "not tagged", method: "DELETE", target: fxS + "/tags/{fa}", as: "demo", wantStatus: 204},
		{name: "outsider", method: "DELETE", target: fxS + "/tags/{fa}", as: "outsider", wantStatus: 403},
	},
	"listClubs": {
		{name: "ok", method: "GET", target: "/v1/clubs", wantStatus: 200},
	},
	"listTransactions": {
		{name: "empty feed", method: "GET", target: fxS + "/transactions?kind=trade&limit=20", as: "demo", wantStatus: 200},
		{name: "waiver and free-agent moves", method: "GET", target: "/v1/leagues/{fal}/transactions?limit=2", wantStatus: 200},
		{name: "second page", method: "GET", target: "/v1/leagues/{fal}/transactions?limit=2&cursor=bzoy", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxS + "/transactions", as: "outsider", wantStatus: 403},
		{name: "malformed league id", method: "GET", target: "/v1/leagues/nope/transactions", wantStatus: 400, invalidRequest: true},
	},
	// ---------------------------------------------------------------- waivers
	"getWaivers": {
		{name: "in season", method: "GET", target: fxS + "/waivers", as: "demo", wantStatus: 200},
		{name: "after the run", method: "GET", target: "/v1/leagues/{fal}/waivers", wantStatus: 200},
		{name: "before the draft", method: "GET", target: fxL + "/waivers", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxL + "/waivers", as: "outsider", wantStatus: 403},
	},
	"listWaiverClaims": {
		{name: "pending", method: "GET", target: fxS + "/waivers/claims", as: "demo", wantStatus: 200},
		{name: "processed", method: "GET", target: "/v1/leagues/{fal}/waivers/claims?status=processed", wantStatus: 200},
		{name: "before the draft", method: "GET", target: fxL + "/waivers/claims", wantStatus: 409},
		{name: "bad status", method: "GET", target: fxS + "/waivers/claims?status=won", as: "demo", wantStatus: 400, invalidRequest: true},
	},
	"createWaiverClaim": {
		{name: "claimed", method: "POST", target: fxS + "/waivers/claims", body: `{"addPlayerId":"{wadd}","dropPlayerId":"{wdrop}"}`, as: "demo", wantStatus: 201, headers: []string{"Idempotency-Key", "claim-000001"}},
		{name: "replayed", method: "POST", target: fxS + "/waivers/claims", body: `{"addPlayerId":"{wadd}","dropPlayerId":"{wdrop}"}`, as: "demo", wantStatus: 201, headers: []string{"Idempotency-Key", "claim-000001"}},
		{name: "duplicate", method: "POST", target: fxS + "/waivers/claims", body: `{"addPlayerId":"{wadd}","dropPlayerId":"{wdrop}"}`, as: "demo", wantStatus: 409},
		{name: "position mismatch", method: "POST", target: fxS + "/waivers/claims", body: `{"addPlayerId":"{wmid}","dropPlayerId":"{wdrop}"}`, as: "demo", wantStatus: 422},
		{name: "rostered player", method: "POST", target: fxS + "/waivers/claims", body: `{"addPlayerId":"{oplr}","dropPlayerId":"{wdrop}"}`, as: "demo", wantStatus: 409},
		{name: "free agent", method: "POST", target: "/v1/leagues/{fal}/waivers/claims", body: `{"addPlayerId":"{fadd}","dropPlayerId":"{fdrop}"}`, wantStatus: 409},
		{name: "before the draft", method: "POST", target: fxL + "/waivers/claims", body: `{"addPlayerId":"` + pid(1) + `","dropPlayerId":"` + pid(2) + `"}`, wantStatus: 409},
	},
	"updateWaiverClaim": {
		{name: "new drop", method: "PATCH", target: fxS + "/waivers/claims/{claim}", body: `{"dropPlayerId":"{wdrop2}"}`, as: "demo", wantStatus: 200},
		{name: "unknown claim", method: "PATCH", target: fxS + "/waivers/claims/" + tClaim, body: `{"dropPlayerId":"{wdrop2}"}`, as: "demo", wantStatus: 404},
		{name: "malformed claim id", method: "PATCH", target: fxL + "/waivers/claims/wv_1", body: `{"dropPlayerId":"` + pid(3) + `"}`, wantStatus: 400, invalidRequest: true},
	},
	"cancelWaiverClaim": {
		{name: "cancelled", method: "DELETE", target: fxS + "/waivers/claims/{claim}", as: "demo", wantStatus: 204},
		{name: "already cancelled", method: "DELETE", target: fxS + "/waivers/claims/{claim}", as: "demo", wantStatus: 409},
		{name: "unknown", method: "DELETE", target: fxS + "/waivers/claims/" + tClaim, as: "demo", wantStatus: 404},
	},
	"reorderWaiverClaims": {
		{name: "reordered", method: "PUT", target: fxS + "/waivers/claim-order", body: `{"claimIds":["{claim}"]}`, as: "demo", wantStatus: 200},
		{name: "missing claims", method: "PUT", target: fxS + "/waivers/claim-order", body: `{"claimIds":[]}`, as: "demo", wantStatus: 422},
	},
	"addFreeAgent": {
		{name: "added", method: "POST", target: "/v1/leagues/{fal}/free-agent-moves", body: `{"addPlayerId":"{fadd}","dropPlayerId":"{fdrop}"}`, wantStatus: 201, headers: []string{"Idempotency-Key", "fa-move-000001"}},
		{name: "replayed", method: "POST", target: "/v1/leagues/{fal}/free-agent-moves", body: `{"addPlayerId":"{fadd}","dropPlayerId":"{fdrop}"}`, wantStatus: 201, headers: []string{"Idempotency-Key", "fa-move-000001"}},
		{name: "drop not on my team", method: "POST", target: "/v1/leagues/{fal}/free-agent-moves", body: `{"addPlayerId":"{fadd}","dropPlayerId":"{fdrop}"}`, as: "fmember", wantStatus: 422},
		{name: "on waivers", method: "POST", target: fxS + "/free-agent-moves", body: `{"addPlayerId":"{wadd}","dropPlayerId":"{wdrop}"}`, as: "demo", wantStatus: 409},
		{name: "before the draft", method: "POST", target: fxL + "/free-agent-moves", body: `{"addPlayerId":"` + pid(1) + `","dropPlayerId":"` + pid(2) + `"}`, wantStatus: 409},
	},
	// ---------------------------------------------------------------- trades
	"listTrades": {
		{name: "open, one team", method: "GET", target: fxS + "/trades?status=open&teamId={oteam}&limit=20", as: "demo", wantStatus: 200},
		{name: "all", method: "GET", target: "/v1/leagues/{fal}/trades", as: "fmember", wantStatus: 200},
		{name: "paged", method: "GET", target: "/v1/leagues/{fal}/trades?limit=1&cursor=bzox", wantStatus: 200},
		{name: "before the draft", method: "GET", target: fxL + "/trades", wantStatus: 409},
		{name: "bad status", method: "GET", target: fxS + "/trades?status=pending", as: "demo", wantStatus: 400, invalidRequest: true},
	},
	"proposeTrade": {
		{name: "offered", method: "POST", target: "/v1/leagues/{fal}/trades", body: `{"receivingTeamId":"{fteam1}","offeredPlayerIds":["{fdrop}"],"requestedPlayerIds":["{fmfwd}"],"message":"Fair?"}`, wantStatus: 201, headers: []string{"Idempotency-Key", "trade-000001"}},
		{name: "replayed", method: "POST", target: "/v1/leagues/{fal}/trades", body: `{"receivingTeamId":"{fteam1}","offeredPlayerIds":["{fdrop}"],"requestedPlayerIds":["{fmfwd}"],"message":"Fair?"}`, wantStatus: 201, headers: []string{"Idempotency-Key", "trade-000001"}},
		{name: "positions differ", method: "POST", target: "/v1/leagues/{fal}/trades", body: `{"receivingTeamId":"{fteam1}","offeredPlayerIds":["{fdrop}"],"requestedPlayerIds":["{fmid}"]}`, wantStatus: 422},
		{name: "player in review", method: "POST", target: fxS + "/trades", body: `{"receivingTeamId":"{oteam}","offeredPlayerIds":["{smid}"],"requestedPlayerIds":["{rplayer}"]}`, as: "demo", wantStatus: 409},
		{name: "before the draft", method: "POST", target: fxL + "/trades", body: `{"receivingTeamId":"` + tTeam + `","offeredPlayerIds":` + pids(1, 1) + `,"requestedPlayerIds":` + pids(2, 2) + `}`, wantStatus: 409},
	},
	"getTrade": {
		{name: "offer", method: "GET", target: "/v1/leagues/{fal}/trades/{otrade}", as: "fmember", wantStatus: 200},
		{name: "in review", method: "GET", target: fxS + "/trades/{rtrade}", as: "demo", wantStatus: 200},
		{name: "unknown", method: "GET", target: fxS + "/trades/" + tTrade, as: "demo", wantStatus: 404},
		{name: "malformed trade id", method: "GET", target: fxL + "/trades/trd_1", wantStatus: 400, invalidRequest: true},
	},
	"acceptTrade": {
		{name: "processed at once", method: "POST", target: "/v1/leagues/{fal}/trades/{otrade}/accept", as: "fmember", wantStatus: 200},
		{name: "no longer open", method: "POST", target: "/v1/leagues/{fal}/trades/{otrade}/accept", as: "fmember", wantStatus: 409},
		{name: "not the receiver", method: "POST", target: fxS + "/trades/{rtrade}/accept", as: "demo", wantStatus: 403},
	},
	"declineTrade": {
		{name: "declined", method: "POST", target: "/v1/leagues/{fal}/trades/{otrade}/decline", as: "fmember", wantStatus: 200},
		{name: "again", method: "POST", target: "/v1/leagues/{fal}/trades/{otrade}/decline", as: "fmember", wantStatus: 409},
	},
	"withdrawTrade": {
		{name: "not the offerer", method: "POST", target: "/v1/leagues/{fal}/trades/{otrade}/withdraw", as: "fmember", wantStatus: 403},
		{name: "withdrawn", method: "POST", target: "/v1/leagues/{fal}/trades/{otrade}/withdraw", wantStatus: 200},
	},
	"voteOnTrade": {
		{name: "veto", method: "POST", target: fxS + "/trades/{rtrade}/votes", body: `{"vote":"veto"}`, as: "demo", wantStatus: 200},
		{name: "changed", method: "POST", target: fxS + "/trades/{rtrade}/votes", body: `{"vote":"allow"}`, as: "demo", wantStatus: 200},
		{name: "no vote in this league", method: "POST", target: "/v1/leagues/{fal}/trades/{otrade}/votes", body: `{"vote":"veto"}`, as: "fmember", wantStatus: 409},
	},
	"reviewTrade": {
		{name: "approved", method: "POST", target: fxS + "/trades/{rtrade}/review", body: `{"decision":"approve","reason":"Fine by me"}`, as: "demo", wantStatus: 200},
		{name: "already decided", method: "POST", target: fxS + "/trades/{rtrade}/review", body: `{"decision":"veto"}`, as: "demo", wantStatus: 409},
		{name: "not the commissioner", method: "POST", target: "/v1/leagues/{fal}/trades/{otrade}/review", body: `{"decision":"veto"}`, as: "fmember", wantStatus: 403},
	},
	// ---------------------------------------------------------------- realtime
	"createRealtimeTicket": {
		{name: "for a league", method: "POST", target: "/v1/realtime/tickets", body: `{"leagueId":"{league}"}`, wantStatus: 201, headers: []string{"Idempotency-Key", "ticket-000001"}},
		{name: "without a league", method: "POST", target: "/v1/realtime/tickets", body: `{}`, wantStatus: 201},
		{name: "unknown league", method: "POST", target: "/v1/realtime/tickets", body: `{"leagueId":"` + tLeague + `"}`, wantStatus: 404},
		{name: "malformed league", method: "POST", target: "/v1/realtime/tickets", body: `{"leagueId":"lg_1"}`, wantStatus: 400, invalidRequest: true},
		{name: "outsider", method: "POST", target: "/v1/realtime/tickets", body: `{"leagueId":"{league}"}`, as: "outsider", wantStatus: 403},
	},
	// ---------------------------------------------------------------- matchups
	"listMatchups": {
		{name: "live gameweek", method: "GET", target: fxS + "/gameweeks/7/matchups", as: "demo", wantStatus: 200},
		{name: "upcoming gameweek", method: "GET", target: fxS + "/gameweeks/12/matchups", as: "demo", wantStatus: 200},
		{name: "unknown league", method: "GET", target: "/v1/leagues/" + tLeague + "/gameweeks/7/matchups", wantStatus: 404},
		{name: "gameweek out of range", method: "GET", target: "/v1/leagues/" + tLeague + "/gameweeks/39/matchups", wantStatus: 400, invalidRequest: true},
		{name: "gameweek not a number", method: "GET", target: "/v1/leagues/" + tLeague + "/gameweeks/x/matchups", wantStatus: 400, invalidRequest: true},
	},
	"getMatchup": {
		{name: "two teams", method: "GET", target: fxS + "/gameweeks/7/matchups/{mu}", as: "demo", wantStatus: 200},
		{name: "against the average", method: "GET", target: fxS + "/gameweeks/7/matchups/{muavg}", as: "demo", wantStatus: 200},
		{name: "final gameweek", method: "GET", target: fxS + "/gameweeks/3/matchups/{mu3}", as: "demo", wantStatus: 200},
		{name: "before the deadline", method: "GET", target: fxS + "/gameweeks/12/matchups/{mu12}", as: "demo", wantStatus: 404},
		{name: "unknown", method: "GET", target: fxS + "/gameweeks/7/matchups/" + tMatchup, as: "demo", wantStatus: 404},
		{name: "malformed matchup id", method: "GET", target: "/v1/leagues/" + tLeague + "/gameweeks/7/matchups/mu_1", wantStatus: 400, invalidRequest: true},
	},
	"getMatchupPreview": {
		{name: "future gameweek", method: "GET", target: fxS + "/gameweeks/12/matchups/{mu12}/preview", as: "demo", wantStatus: 200},
		{name: "past gameweek", method: "GET", target: fxS + "/gameweeks/3/matchups/{mu3}/preview", as: "demo", wantStatus: 200},
		{name: "against the average", method: "GET", target: fxS + "/gameweeks/7/matchups/{muavg}/preview", as: "demo", wantStatus: 200},
		{name: "unknown", method: "GET", target: fxS + "/gameweeks/12/matchups/" + tMatchup + "/preview", as: "demo", wantStatus: 404},
		{name: "malformed matchup id", method: "GET", target: "/v1/leagues/" + tLeague + "/gameweeks/7/matchups/mu_1/preview", wantStatus: 400, invalidRequest: true},
	},
	"getCurrentGameweek": {
		{name: "ok", method: "GET", target: "/v1/gameweeks/current", wantStatus: 200},
	},
	"getGameweekAwards": {
		{name: "final gameweek", method: "GET", target: "/v1/gameweeks/6/awards", wantStatus: 200},
		{name: "live gameweek", method: "GET", target: "/v1/gameweeks/7/awards", wantStatus: 200},
		{name: "not started", method: "GET", target: "/v1/gameweeks/9/awards", wantStatus: 404},
		{name: "gameweek out of range", method: "GET", target: "/v1/gameweeks/39/awards", wantStatus: 400, invalidRequest: true},
		{name: "gameweek zero", method: "GET", target: "/v1/gameweeks/0/awards", wantStatus: 400, invalidRequest: true},
	},
	"getGameweekFixtures": {
		{name: "live gameweek", method: "GET", target: "/v1/gameweeks/7/fixtures", wantStatus: 200},
		{name: "upcoming gameweek", method: "GET", target: "/v1/gameweeks/9/fixtures", wantStatus: 200},
		{name: "gameweek out of range", method: "GET", target: "/v1/gameweeks/39/fixtures", wantStatus: 400, invalidRequest: true},
		{name: "gameweek not a number", method: "GET", target: "/v1/gameweeks/x/fixtures", wantStatus: 400, invalidRequest: true},
	},
	"getFixture": {
		{name: "live fixture", method: "GET", target: "/v1/fixtures/{fxlive}", as: "demo", wantStatus: 200},
		{name: "finished fixture", method: "GET", target: "/v1/fixtures/{fxdone}", wantStatus: 200},
		{name: "upcoming fixture", method: "GET", target: "/v1/fixtures/{fxnext}", as: "demo", wantStatus: 200},
		{name: "unknown fixture", method: "GET", target: "/v1/fixtures/" + tFixture, wantStatus: 404},
		{name: "not a fixture id", method: "GET", target: "/v1/fixtures/plr_x", wantStatus: 400, invalidRequest: true},
	},
	// ---------------------------------------------------------------- assets
	"getAppConfig": {
		{name: "ok", method: "GET", target: "/v1/config", wantStatus: 200},
	},
	"getPlayerPhoto": {
		{name: "ok", method: "GET", target: "/v1/assets/players/{fplplr}/photo", wantStatus: 200},
		{name: "not modified", method: "GET", target: "/v1/assets/players/{fplplr}/photo", headers: []string{"If-None-Match", "*"}, wantStatus: 304},
		{name: "no upstream code", method: "GET", target: "/v1/assets/players/{plr}/photo", wantStatus: 404},
		{name: "unknown", method: "GET", target: "/v1/assets/players/" + pid(999) + "/photo", wantStatus: 404},
		{name: "malformed id", method: "GET", target: "/v1/assets/players/plr_1/photo", wantStatus: 400, invalidRequest: true},
	},
	"getClubCrest": {
		{name: "ok", method: "GET", target: "/v1/assets/clubs/{fplclub}/crest", wantStatus: 200},
		{name: "not modified", method: "GET", target: "/v1/assets/clubs/{fplclub}/crest", headers: []string{"If-None-Match", "*"}, wantStatus: 304},
		{name: "no upstream code", method: "GET", target: "/v1/assets/clubs/{democlub}/crest", wantStatus: 404},
		{name: "malformed code", method: "GET", target: "/v1/assets/clubs/A1/crest", wantStatus: 400, invalidRequest: true},
	},
	"getClubShirt": {
		{name: "ok", method: "GET", target: "/v1/assets/clubs/{fplclub}/shirt", wantStatus: 200},
		{name: "goalkeeper", method: "GET", target: "/v1/assets/clubs/{fplclub}/shirt?gk=true", wantStatus: 200},
		{name: "unknown club", method: "GET", target: "/v1/assets/clubs/XYZ/shirt", wantStatus: 404},
		{name: "malformed code", method: "GET", target: "/v1/assets/clubs/TOOLONG/shirt", wantStatus: 400, invalidRequest: true},
		{name: "malformed gk", method: "GET", target: "/v1/assets/clubs/{fplclub}/shirt?gk=maybe", wantStatus: 400, invalidRequest: true},
	},
	"listPlayersOfTheWeek": {
		{name: "every started gameweek", method: "GET", target: "/v1/awards/players-of-the-week", wantStatus: 200},
	},
	// ---------------------------------------------------------------- imports
	"createImport": {
		{name: "queued from url", method: "POST", target: "/v1/imports", body: `{"draftLeagueUrl":"https://draft.premierleague.com/league/90001","seat":2}`, as: "member", wantStatus: 202},
		{name: "idempotent from id", method: "POST", target: "/v1/imports", body: `{"fplLeagueId":90001,"seat":2}`, as: "member", wantStatus: 200},
		{name: "seat out of range", method: "POST", target: "/v1/imports", body: `{"fplLeagueId":90001,"seat":13}`, as: "outsider", wantStatus: 422},
		{name: "unknown league", method: "POST", target: "/v1/imports", body: `{"fplLeagueId":12345,"seat":1}`, as: "outsider", wantStatus: 404},
		{name: "classic link", method: "POST", target: "/v1/imports", body: `{"draftLeagueUrl":"https://fantasy.premierleague.com/leagues/1/standings/c","seat":1}`, wantStatus: 422},
		{name: "no seat", method: "POST", target: "/v1/imports", body: `{"fplLeagueId":90001}`, wantStatus: 400, invalidRequest: true},
		{name: "both fields", method: "POST", target: "/v1/imports", body: `{"draftLeagueUrl":"https://draft.premierleague.com/league/1","fplLeagueId":1,"seat":1}`, wantStatus: 400, invalidRequest: true},
		{name: "empty object", method: "POST", target: "/v1/imports", body: `{}`, wantStatus: 400, invalidRequest: true},
		{name: "malformed json", method: "POST", target: "/v1/imports", body: `{`, wantStatus: 400, invalidRequest: true},
	},
	"previewImport": {
		{name: "found", method: "GET", target: "/v1/imports/preview?fplLeagueId=90001", wantStatus: 200},
		{name: "found from url", method: "GET", target: "/v1/imports/preview?draftLeagueUrl=https%3A%2F%2Fdraft.premierleague.com%2Fleague%2F90001", wantStatus: 200},
		{name: "unknown league", method: "GET", target: "/v1/imports/preview?fplLeagueId=12345", wantStatus: 404},
		{name: "classic link", method: "GET", target: "/v1/imports/preview?draftLeagueUrl=https%3A%2F%2Ffantasy.premierleague.com%2Fleagues%2F1%2Fstandings%2Fc", wantStatus: 422},
		{name: "no params", method: "GET", target: "/v1/imports/preview", wantStatus: 400},
		{name: "non-numeric id", method: "GET", target: "/v1/imports/preview?fplLeagueId=abc", wantStatus: 400, invalidRequest: true},
	},
	"getImport": {
		{name: "found", method: "GET", target: "/v1/imports/{import}", wantStatus: 200},
		{name: "someone else's", method: "GET", target: "/v1/imports/{import}", as: "member", wantStatus: 404},
		{name: "unknown", method: "GET", target: "/v1/imports/" + tImportNA, wantStatus: 404},
		{name: "malformed id", method: "GET", target: "/v1/imports/not-an-id", wantStatus: 400, invalidRequest: true},
	},
	"getLeagueSync": {
		{name: "commissioner", method: "GET", target: fxI + "/sync", wantStatus: 200},
		{name: "member", method: "GET", target: fxI + "/sync", as: "member", wantStatus: 200},
		{name: "outsider", method: "GET", target: fxI + "/sync", as: "outsider", wantStatus: 403},
		{name: "not imported", method: "GET", target: fxL + "/sync", wantStatus: 409},
		{name: "unknown league", method: "GET", target: "/v1/leagues/" + tLeague + "/sync", wantStatus: 404},
	},
	"resyncLeague": {
		{name: "not confirmed", method: "POST", target: fxI + "/sync", body: `{"confirm":false}`, wantStatus: 422, invalidRequest: true},
		{name: "member", method: "POST", target: fxI + "/sync", body: `{"confirm":true}`, as: "member", wantStatus: 403},
		{name: "queued", method: "POST", target: fxI + "/sync", body: `{"confirm":true}`, wantStatus: 202},
		{name: "pending", method: "POST", target: fxI + "/sync", body: `{"confirm":true}`, wantStatus: 409},
		{name: "not imported", method: "POST", target: fxL + "/sync", body: `{"confirm":true}`, wantStatus: 409},
	},
	"checkLeagueSync": {
		{name: "member", method: "POST", target: fxB + "/sync/check", as: "member", wantStatus: 403},
		{name: "queued", method: "POST", target: fxB + "/sync/check", wantStatus: 202},
		{name: "pending", method: "POST", target: fxB + "/sync/check", wantStatus: 409},
	},
	"explainSyncItem": {
		{name: "explained", method: "POST", target: fxB + "/sync/explanations", body: `{"key":"points:gw2:{bteam}","note":"FPL corrected it later."}`, wantStatus: 200},
		{name: "not failing", method: "POST", target: fxB + "/sync/explanations", body: `{"key":"waiver_order","note":"fine"}`, wantStatus: 404},
		{name: "member", method: "POST", target: fxB + "/sync/explanations", body: `{"key":"points:gw2:{bteam}","note":"fine"}`, as: "member", wantStatus: 403},
		{name: "no note", method: "POST", target: fxB + "/sync/explanations", body: `{"key":"points:gw2:{bteam}"}`, wantStatus: 422, invalidRequest: true},
	},
	"listSeats": {
		{name: "ok", method: "GET", target: fxI + "/seats", wantStatus: 200},
		{name: "member", method: "GET", target: fxI + "/seats", as: "member", wantStatus: 403},
		{name: "not imported", method: "GET", target: fxL + "/seats", wantStatus: 409},
	},
	"createSeatSignInLink": {
		{name: "new link", method: "POST", target: "/v1/leagues/{impbad}/seats/{bclaimed}/signin-link", wantStatus: 201},
		{name: "unclaimed seat", method: "POST", target: fxI + "/seats/{iseat}/signin-link", wantStatus: 409},
		{name: "manager has email", method: "POST", target: fxI + "/seats/{iteam}/signin-link", wantStatus: 409},
		{name: "member", method: "POST", target: "/v1/leagues/{impbad}/seats/{bclaimed}/signin-link", as: "member", wantStatus: 403},
		{name: "not imported", method: "POST", target: fxL + "/seats/{team}/signin-link", wantStatus: 409},
		{name: "unknown team", method: "POST", target: fxI + "/seats/" + tTeam + "/signin-link", wantStatus: 404},
	},
	"resendSeatInvite": {
		{name: "new link", method: "POST", target: fxI + "/seats/{iseat}/invite", wantStatus: 201},
		{name: "claimed seat", method: "POST", target: fxI + "/seats/{iteam}/invite", wantStatus: 409},
		{name: "member", method: "POST", target: fxI + "/seats/{iseat}/invite", as: "member", wantStatus: 403},
		{name: "unknown team", method: "POST", target: fxI + "/seats/" + tTeam + "/invite", wantStatus: 404},
	},
}

// specOp is one operation in the spec.
type specOp struct {
	id, method, path string
	public           bool
}

// specOperations returns every operation in the spec. An operation is
// public when it overrides the global requirement with `security: []`.
func specOperations(t *testing.T, doc libopenapi.Document) []specOp {
	t.Helper()
	model, err := doc.BuildV3Model()
	if err != nil {
		t.Fatalf("build model: %v", err)
	}
	var out []specOp
	for path := model.Model.Paths.PathItems.First(); path != nil; path = path.Next() {
		for op := path.Value().GetOperations().First(); op != nil; op = op.Next() {
			o := op.Value()
			if o.OperationId == "" {
				t.Errorf("%s %s has no operationId", op.Key(), path.Key())
			}
			out = append(out, specOp{id: o.OperationId, method: strings.ToUpper(op.Key()), path: path.Key(), public: isPublic(o)})
		}
	}
	return out
}

func isPublic(o *v3.Operation) bool { return o.Security != nil && len(o.Security) == 0 }

// contractFixture is the state every operation's cases can refer to.
type contractFixture struct {
	h            http.Handler
	tokens       map[string]string // by contractCase.as
	replacements *strings.Replacer
}

// contractEnv builds the server and options behind one fixture: memory
// stores by default, Postgres in TestContractPostgres.
var contractEnv = func(t *testing.T) (*Server, Options) {
	srv, _ := newTestServer()
	return srv, testOptions()
}

func newContractFixture(t *testing.T) contractFixture {
	t.Helper()
	srv, opts := contractEnv(t)
	srv.claimSignIn = true
	// Fake Apple and Google: local keys, never the real providers.
	idpKeys := idptest.New(t)
	srv.apple, srv.google = idp.Apple([]string{"com.example.app"}, nil), idp.Google([]string{"web-client"}, nil)
	srv.apple.JWKSURL, srv.google.JWKSURL = idpKeys.URL, idpKeys.URL
	nonceSum := sha256.Sum256([]byte("n0nce"))
	appleToken := idpKeys.Token(t, map[string]any{"iss": idp.AppleIssuer, "aud": "com.example.app", "sub": "001234.apple",
		"email": "sam@privaterelay.example", "email_verified": "true", "nonce": hex.EncodeToString(nonceSum[:])})
	googleToken := idpKeys.Token(t, map[string]any{"iss": "https://accounts.google.com", "aud": "web-client", "sub": "1098765",
		"email": "sam.google@example.com", "email_verified": true, "name": "Sam G."})
	googleNonceToken := idpKeys.Token(t, map[string]any{"iss": "https://accounts.google.com", "aud": "web-client", "sub": "1098766",
		"email": "sam.google2@example.com", "email_verified": true, "nonce": "g-n0nce"})
	h := newHandler(opts, newRoutes(opts, srv))
	commish := testToken
	member := signIn(srv, "Priya N.", "priya@example.com")
	outsider := signIn(srv, "Olu A.", "olu@example.com")

	imp := decodeImport(t, do(h, "POST", "/v1/imports", `{"fplLeagueId":90001,"seat":1}`)).Data.Id

	l, err := srv.leagues.Create(context.Background(), userOf(t, srv, commish), "The Sunday Draft", 10, "Sunday League Heroes", league.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := srv.leagues.CreateInvite(context.Background(), l.CommissionerID, l.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.leagues.AcceptInvite(context.Background(), userOf(t, srv, member), inv.Code, "Gaffer's Army"); err != nil {
		t.Fatal(err)
	}
	seeded, err := demo.Seed(context.Background(), demo.Deps{
		Auth: srv.auth, Leagues: srv.leagues, Stats: srv.stats, Now: srv.now, History: srv.history,
		Log: testOptions().Logger,
	})
	srv.historyProvider = demo.ProviderName
	if err != nil {
		t.Fatal(err)
	}
	// The demo world is not FPL's, which refuses imports
	// (import-unavailable): map one FPL player id, as if FPL's players were
	// ingested, so the createImport cases reach the import itself.
	// The same mapping gives the player, and one club, branded images
	// from a fake upstream (assets_test.go).
	clubs, _ := srv.stats.Clubs(context.Background())
	if err := srv.stats.Apply(context.Background(), stats.Batch{Provider: mirror.Provider,
		Mappings: []stats.Mapping{{Entity: "player", ExternalID: "1", ID: seededPlayer(t, srv)},
			{Entity: "club", ExternalID: "3", ID: clubs[0].ID}}}); err != nil {
		t.Fatal(err)
	}
	srv.assets, _ = newTestAssets(t, srv.stats, true)
	demoTok, err := srv.auth.Issue(context.Background(), seeded.UserIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	sl, _ := srv.leagues.Get(context.Background(), seeded.UserIDs[0], seeded.LeagueID)
	game, _ := srv.leagues.Repo().Game(context.Background(), seeded.LeagueID)
	steam, _ := sl.TeamOf(seeded.UserIDs[0])
	oteam := sl.Teams[1]
	matchup := func(gw int, average bool) string {
		for _, m := range game.MatchupsIn(gw) {
			if (m.AwayTeamID == "") == average {
				return m.ID
			}
		}
		t.Fatalf("no matchup in gameweek %d", gw)
		return ""
	}
	rostered := game.RosterAt(steam.ID, 7)[0]
	var freeAgent string
	own := map[string]bool{}
	for _, r := range game.Rosters {
		own[r.PlayerID] = true
	}
	players, _ := srv.stats.Players(context.Background())
	for _, p := range players {
		if !own[p.ID] {
			freeAgent = p.ID
			break
		}
	}
	// Three full 4-team leagues the commissioner runs: {full} before the
	// draft, {live} drafting, {paused} with the draft paused. fmember owns
	// a team in each.
	fmember := signIn(srv, "Jess K.", "jess@example.com")
	full := func(name string) *league.League {
		fl, err := srv.leagues.Create(context.Background(), userOf(t, srv, commish), name, 4, "Commish FC", league.SettingsInput{})
		if err != nil {
			t.Fatal(err)
		}
		finv, _ := srv.leagues.CreateInvite(context.Background(), fl.CommissionerID, fl.ID, nil, nil)
		joiners := []string{userOf(t, srv, fmember)}
		for i := range 2 {
			joiners = append(joiners, userOf(t, srv, signIn(srv, "Extra", fmt.Sprintf("extra%d-%s@example.com", i, name))))
		}
		for i, u := range joiners {
			if fl, _, err = srv.leagues.AcceptInvite(context.Background(), u, finv.Code, fmt.Sprintf("Side %d", i)); err != nil {
				t.Fatal(err)
			}
		}
		return fl
	}
	fullL, liveL, pausedL := full("Full"), full("Live"), full("Paused")
	if _, err := srv.drafts().Start(context.Background(), liveL.CommissionerID, liveL.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.drafts().Start(context.Background(), pausedL.CommissionerID, pausedL.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := srv.drafts().Pause(context.Background(), pausedL.CommissionerID, pausedL.ID); err != nil {
		t.Fatal(err)
	}
	// The three best-ranked players: undrafted in {live}.
	slices.SortFunc(players, func(a, b stats.Player) int { return a.DraftRank - b.DraftRank })

	world, err := league.LoadWorld(context.Background(), srv.stats, -1)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// {fal}: a fourth full league, drafted on autopick (gameweek 8 on),
	// with gameweek 8's waivers run, so free agents can be added.
	falL := full("FreeAgency")
	if _, err := srv.drafts().Start(ctx, falL.CommissionerID, falL.ID); err != nil {
		t.Fatal(err)
	}
	for _, tm := range falL.Teams {
		if _, err := srv.drafts().SetAutopick(ctx, tm.OwnerID, falL.ID, true); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := srv.waiverService().Run(ctx, falL.ID, 8); err != nil {
		t.Fatal(err)
	}
	// unowned returns the best-ranked unowned player at pos in a league's
	// gameweek 8; mine the first player at pos on a team.
	unowned := func(leagueID string, pos scoring.Position, skip int) string {
		g, _ := srv.leagues.Repo().Game(ctx, leagueID)
		own := g.OwnerAt(8)
		for _, p := range players {
			if p.Position == pos && own[p.ID] == "" {
				if skip == 0 {
					return p.ID
				}
				skip--
			}
		}
		t.Fatalf("no free %s", pos)
		return ""
	}
	mine := func(leagueID, teamID string, pos scoring.Position, n int) string {
		g, _ := srv.leagues.Repo().Game(ctx, leagueID)
		var ids []string
		for _, id := range g.RosterAt(teamID, 8) {
			if world.Players[id].Position == pos {
				ids = append(ids, id)
			}
		}
		slices.Sort(ids)
		return ids[n]
	}
	// The {fal} feed: fmember adds a free agent; the commissioner claims
	// the dropped player, and gameweek 10's waivers run.
	{
		ft := falL.Teams[1]
		add, drop := unowned(falL.ID, scoring.MID, 0), mine(falL.ID, ft.ID, scoring.MID, 0)
		if _, _, err := srv.waiverService().AddFreeAgent(ctx, ft.OwnerID, falL.ID, add, drop); err != nil {
			t.Fatal(err)
		}
		if _, _, err := srv.waiverService().CreateClaim(ctx, falL.CommissionerID, falL.ID, drop, mine(falL.ID, falL.Teams[0].ID, scoring.MID, 0)); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.waiverService().Run(ctx, falL.ID, 10); err != nil {
			t.Fatal(err)
		}
	}
	// Trades: {otrade} offered in {fal} (no review) from the commissioner
	// to fmember; {rtrade} in review in the demo league (league vote)
	// between two other teams.
	_, otrade, err := srv.tradeService().Propose(ctx, falL.CommissionerID, falL.ID, falL.Teams[1].ID,
		[]string{mine(falL.ID, falL.Teams[0].ID, scoring.FWD, 0)}, []string{mine(falL.ID, falL.Teams[1].ID, scoring.FWD, 0)}, "Fair?")
	if err != nil {
		t.Fatal(err)
	}
	ta, tb := sl.Teams[1], sl.Teams[2]
	_, rtrade, err := srv.tradeService().Propose(ctx, ta.OwnerID, sl.ID, tb.ID,
		[]string{mine(sl.ID, ta.ID, scoring.MID, 0)}, []string{mine(sl.ID, tb.ID, scoring.MID, 0)}, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := srv.tradeService().Accept(ctx, tb.OwnerID, sl.ID, rtrade.ID); err != nil {
		t.Fatal(err)
	}
	// A pending claim of the demo user's in the demo league (waiver
	// period for gameweek 8 at the test clock).
	_, claim, err := srv.waiverService().CreateClaim(ctx, seeded.UserIDs[0], seeded.LeagueID,
		unowned(seeded.LeagueID, scoring.DEF, 1), mine(seeded.LeagueID, steam.ID, scoring.DEF, 0))
	if err != nil {
		t.Fatal(err)
	}
	lineupJSON := func(gw int) string {
		lu := league.EffectiveLineup(game, world, steam.ID, gw)
		q := func(ids []string) string { return `["` + strings.Join(ids, `","`) + `"]` }
		return fmt.Sprintf(`{"gameweek":%d,"starters":%s,"bench":%s}`, gw, q(lu.Slots[:11]), q(lu.Slots[11:]))
	}

	impL := importedLeague(t, srv, userOf(t, srv, commish), userOf(t, srv, member), true)
	badL := importedLeague(t, srv, userOf(t, srv, commish), userOf(t, srv, member), false)

	iclaim, _ := impL.ClaimLink(impL.Teams[3].ID)
	// {bclaimed}: a seat of {impbad} claimed by a claim-link account (no
	// email), which can get a sign-in link; {slink} a link for another one.
	bclaim, _ := badL.ClaimLink(badL.Teams[3].ID)
	if _, _, err := srv.leagues.ClaimSeat(context.Background(), bclaim.Code, func() (string, error) {
		_, u, err := srv.auth.CreateAccount(context.Background(), "Linked")
		if err != nil {
			return "", err
		}
		return u.ID, nil
	}); err != nil {
		t.Fatal(err)
	}
	// The link's account holds a seat of {impbad} (its third), as use checks.
	var slinkUser *auth.User
	sclaim, _ := badL.ClaimLink(badL.Teams[2].ID)
	if _, _, err := srv.leagues.ClaimSeat(context.Background(), sclaim.Code, func() (string, error) {
		_, u, err := srv.auth.CreateAccount(context.Background(), "Returning")
		slinkUser = u
		if err != nil {
			return "", err
		}
		return u.ID, nil
	}); err != nil {
		t.Fatal(err)
	}
	slink, err := srv.auth.IssueSignInLink(context.Background(), slinkUser.ID, badL.ID, badL.CommissionerID)
	if err != nil {
		t.Fatal(err)
	}
	magic, _ := srv.auth.StartEmail(context.Background(), "magic@example.com")
	code, _ := srv.auth.StartEmail(context.Background(), "code@example.com")
	refresh, err := srv.auth.Issue(context.Background(), l.CommissionerID)
	if err != nil {
		t.Fatal(err)
	}
	return contractFixture{
		h:      h,
		tokens: map[string]string{"": commish, "member": member, "outsider": outsider, "demo": demoTok.AccessToken, "fmember": fmember},
		replacements: strings.NewReplacer(
			"{import}", imp,
			"{impbad}", badL.ID,
			"{imp}", impL.ID,
			"{iseat}", impL.Teams[2].ID,
			"{iclaim}", iclaim.Code,
			"{bclaimed}", badL.Teams[3].ID,
			"{slink}", slink.Code,
			"{iteam}", impL.Teams[1].ID,
			"{bteam}", badL.Teams[0].ID,
			"{league}", l.ID,
			"{team}", l.Teams[0].ID,
			"{me}", l.CommissionerID,
			"{member}", userOf(t, srv, member),
			"{invite}", inv.ID,
			"{code}", inv.Code,
			"{magic}", magic.Token,
			"{magiccode}", code.Code,
			"{wrongcode}", wrongCode(code.Code),
			"{appletoken}", appleToken,
			"{googletoken}", googleToken,
			"{googlenoncetoken}", googleNonceToken,
			"{refresh}", refresh.RefreshToken,
			"{season}", seeded.LeagueID,
			"{hist}", demoPlayer(t, srv, "9000113"),
			"{steam}", steam.ID,
			"{oteam}", oteam.ID,
			"{mu3}", matchup(3, false),
			"{mu12}", matchup(12, false),
			"{muavg}", matchup(7, true),
			"{mu}", matchup(7, false),
			"{plr}", rostered,
			"{fa}", freeAgent,
			"{full}", fullL.ID,
			"{live}", liveL.ID,
			"{paused}", pausedL.ID,
			"{fteam}", fullL.Teams[0].ID,
			"{p1}", players[0].ID,
			"{p2}", players[1].ID,
			"{p3}", players[2].ID,
			"{lineup9}", lineupJSON(9),
			"{lineup7}", lineupJSON(7),
			"{claim}", claim.ID,
			"{wadd}", unowned(seeded.LeagueID, scoring.DEF, 0),
			"{wdrop2}", mine(seeded.LeagueID, steam.ID, scoring.DEF, 1),
			"{wdrop}", mine(seeded.LeagueID, steam.ID, scoring.DEF, 0),
			"{wmid}", unowned(seeded.LeagueID, scoring.MID, 0),
			"{oplr}", mine(seeded.LeagueID, oteam.ID, scoring.DEF, 0),
			"{bplr}", mine(seeded.LeagueID, steam.ID, scoring.FWD, 0),
			"{fal}", falL.ID,
			"{fteam1}", falL.Teams[1].ID,
			"{fmfwd}", mine(falL.ID, falL.Teams[1].ID, scoring.FWD, 0),
			"{fmid}", mine(falL.ID, falL.Teams[1].ID, scoring.MID, 1),
			"{otrade}", otrade.ID,
			"{rtrade}", rtrade.ID,
			"{rplayer}", rtrade.Offered[0],
			"{smid}", mine(sl.ID, steam.ID, scoring.MID, 0),
			"{fadd}", unowned(falL.ID, scoring.FWD, 0),
			"{fdrop}", mine(falL.ID, falL.Teams[0].ID, scoring.FWD, 0),
			"{fplplr}", seededPlayer(t, srv),
			"{fxlive}", seededFixture(t, srv, stats.FixtureLive),
			"{fxdone}", seededFixture(t, srv, stats.FixtureFinished),
			"{fxnext}", seededFixture(t, srv, stats.FixtureScheduled),
			"{fplclub}", clubs[0].Code,
			"{democlub}", clubs[1].Code,
		),
	}
}

// importedLeague stores a league as an FPL import makes it (docs/adr/0012):
// four seats, the commissioner's, the member's and two with claim links,
// with a verification report: clean, or with a mismatch.
func importedLeague(t *testing.T, srv *Server, commish, member string, clean bool) *league.League {
	t.Helper()
	ctx := context.Background()
	now := srv.now()
	l := &league.League{
		ID: ids.New("lg"), Name: "Imported", Season: league.Season, Status: league.StatusInSeason,
		Settings: league.DefaultSettings(4), CommissionerID: commish, CreatedAt: now,
		Source: league.SourceFPL, FPLLeagueID: 90001,
	}
	for i, owner := range []string{commish, member, "", ""} {
		l.Teams = append(l.Teams, league.Team{ID: ids.New("tm"), Name: fmt.Sprintf("Seat %d", i+1), OwnerID: owner,
			JoinedAt: now.Add(time.Duration(i) * time.Second), FPLEntryID: int64(100 + i), ManagerName: fmt.Sprintf("FPL Manager %d", i+1)})
	}
	err := srv.leagues.Repo().Tx(ctx, func(tx league.Tx) error {
		if err := tx.Insert(ctx, l); err != nil {
			return err
		}
		for _, tm := range l.Teams[2:] {
			if _, err := league.IssueClaimInvite(ctx, tx, l, tm.ID, now); err != nil {
				return err
			}
		}
		return tx.Save(ctx, l)
	})
	if err != nil {
		t.Fatal(err)
	}
	attempt := now.Add(-10 * time.Minute)
	if err := srv.mirror.Store.SaveSync(ctx, mirror.SyncRecord{LeagueID: l.ID, LastAttemptAt: &attempt, LastSyncedAt: &attempt}); err != nil {
		t.Fatal(err)
	}
	rep := mirror.Report{CheckedAt: attempt, Status: mirror.ReportClean, Items: []mirror.Item{
		{Key: "waiver_order", Kind: mirror.KindWaiverOrder, Status: mirror.ItemOK, Ours: "a", Theirs: "a"},
	}}
	if !clean {
		rep.Status = mirror.ReportMismatch
		rep.Items = append(rep.Items, mirror.Item{Key: "points:gw2:" + l.Teams[0].ID, Kind: mirror.KindPoints, Gameweek: 2,
			TeamID: l.Teams[0].ID, Status: mirror.ItemMismatch, Ours: "41", Theirs: "43", Note: "Our XI: ..."})
	}
	if err := srv.mirror.Store.SaveReport(ctx, l.ID, rep); err != nil {
		t.Fatal(err)
	}
	return l
}

func userOf(t *testing.T, srv *Server, token string) string {
	t.Helper()
	uid, _, err := srv.auth.Authenticate(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	return uid
}

func loadSpec(t *testing.T) (libopenapi.Document, validator.Validator) {
	t.Helper()
	raw, err := os.ReadFile("../../../api/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	doc, err := libopenapi.NewDocument(raw)
	if err != nil {
		t.Fatal(err)
	}
	v, errs := validator.NewValidator(doc)
	if len(errs) > 0 {
		t.Fatalf("build validator: %v", errs)
	}
	return doc, v
}

func TestContract(t *testing.T) { runContract(t) }

// TestContractPostgres runs the whole contract suite on the Postgres
// stores (a fresh database per fixture) when TEST_DATABASE_URL is set.
func TestContractPostgres(t *testing.T) {
	storetest.URL(t)
	prev := contractEnv
	t.Cleanup(func() { contractEnv = prev })
	contractEnv = func(t *testing.T) (*Server, Options) {
		pool := storetest.New(t)
		srv, _ := newTestServerOn(pool)
		opts := testOptions()
		opts.Idempotency = NewPostgresIdempotency(pool)
		return srv, opts
	}
	runContract(t)
}

func runContract(t *testing.T) {
	doc, v := loadSpec(t)

	ops := specOperations(t, doc)
	inSpec := map[string]bool{}
	for _, op := range ops {
		inSpec[op.id] = true
		if len(contractCases[op.id]) == 0 {
			t.Errorf("operation %q is in the spec but has no contract test case", op.id)
		}
	}
	for id := range contractCases {
		if !inSpec[id] {
			t.Errorf("contract case for %q, which is not in the spec", id)
		}
	}
	if t.Failed() {
		t.FailNow()
	}

	check := func(t *testing.T, h http.Handler, req *http.Request, wantStatus int) {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != wantStatus {
			t.Fatalf("status = %d, want %d; body %s", rec.Code, wantStatus, rec.Body)
		}
		resp := rec.Result()
		defer resp.Body.Close()
		if ok, verrs := v.ValidateHttpResponse(req, resp); !ok {
			body, _ := io.ReadAll(rec.Result().Body)
			for _, e := range verrs {
				t.Errorf("response does not match the contract: %s: %s (%s)", e.Message, e.Reason, e.HowToFix)
			}
			t.Logf("response body: %s", body)
		}
	}

	for _, op := range ops {
		t.Run(op.id, func(t *testing.T) {
			// One server per operation, so cases do not share state.
			fx := newContractFixture(t)
			for _, c := range contractCases[op.id] {
				t.Run(c.name, func(t *testing.T) {
					target := fx.replacements.Replace(c.target)
					body := fx.replacements.Replace(c.body)
					newReq := func() *http.Request {
						req := httptest.NewRequest(c.method, target, strings.NewReader(body))
						if body != "" {
							req.Header.Set("Content-Type", "application/json")
						}
						req.Header.Set("Authorization", "Bearer "+fx.tokens[c.as])
						for i := 0; i+1 < len(c.headers); i += 2 {
							req.Header.Set(c.headers[i], c.headers[i+1])
						}
						return req
					}
					if !c.invalidRequest {
						if ok, verrs := v.ValidateHttpRequest(newReq()); !ok {
							for _, e := range verrs {
								t.Errorf("request does not match the contract: %s: %s", e.Message, e.Reason)
							}
						}
					}
					check(t, fx.h, newReq(), c.wantStatus)
				})
			}

			// The same first request without a token: 401 exactly when the
			// spec requires a bearer token.
			t.Run("anonymous", func(t *testing.T) {
				fx := newContractFixture(t)
				c := contractCases[op.id][0]
				req := httptest.NewRequest(c.method, fx.replacements.Replace(c.target), strings.NewReader(fx.replacements.Replace(c.body)))
				if c.body != "" {
					req.Header.Set("Content-Type", "application/json")
				}
				if op.public {
					rec := httptest.NewRecorder()
					fx.h.ServeHTTP(rec, req)
					if rec.Code == http.StatusUnauthorized {
						t.Fatalf("public operation answered 401")
					}
					return
				}
				check(t, fx.h, req, http.StatusUnauthorized)
			})
		})
	}
}

// publicRoutes must list exactly the operations with `security: []`.
func TestPublicRoutesMatchSpec(t *testing.T) {
	doc, _ := loadSpec(t)
	var fromSpec []string
	for _, op := range specOperations(t, doc) {
		if op.public {
			fromSpec = append(fromSpec, op.method+" "+op.path)
		}
	}
	var fromCode []string
	for r := range publicRoutes {
		fromCode = append(fromCode, r)
	}
	sort.Strings(fromSpec)
	sort.Strings(fromCode)
	if strings.Join(fromSpec, "\n") != strings.Join(fromCode, "\n") {
		t.Errorf("public routes differ.\nspec:\n%s\ncode:\n%s", strings.Join(fromSpec, "\n"), strings.Join(fromCode, "\n"))
	}
}

// Sanity check that the validator can fail: a response with a wrong body must
// be rejected, otherwise TestContract proves nothing.
func TestContractValidatorRejectsBadResponse(t *testing.T) {
	_, v := loadSpec(t)
	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	_, _ = rec.WriteString(`{"status":"broken"}`)
	if ok, _ := v.ValidateHttpResponse(req, rec.Result()); ok {
		t.Fatal("validator accepted a response with an invalid body")
	}
	rec = httptest.NewRecorder()
	rec.Header().Set("Content-Type", "application/json")
	rec.WriteHeader(http.StatusTeapot)
	if ok, _ := v.ValidateHttpResponse(req, rec.Result()); ok {
		t.Fatal("validator accepted an undocumented status")
	}
}

// wrongCode is a 6-digit code other than code.
func wrongCode(code string) string {
	n, _ := strconv.Atoi(code)
	return fmt.Sprintf("%06d", (n+1)%1_000_000)
}

// seededPlayer is one of the demo world's players.
// seededFixture is the id of a seeded fixture with the status.
func seededFixture(t *testing.T, srv *Server, status string) string {
	t.Helper()
	fixtures, err := srv.stats.Fixtures(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range fixtures {
		if f.Status == status {
			return f.ID
		}
	}
	t.Fatalf("no %s fixture among %d", status, len(fixtures))
	return ""
}

// demoPlayer is the plr_ id of the demo player with external id code.
func demoPlayer(t *testing.T, srv *Server, code string) string {
	t.Helper()
	ids, err := srv.stats.ExternalIDs(context.Background(), demo.ProviderName, "player")
	if err != nil || ids[code] == "" {
		t.Fatalf("demo player %s: %v", code, err)
	}
	return ids[code]
}

func seededPlayer(t *testing.T, srv *Server) string {
	t.Helper()
	players, err := srv.stats.Players(context.Background())
	if err != nil || len(players) == 0 {
		t.Fatalf("players: %v (%d)", err, len(players))
	}
	return players[0].ID
}
