package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/assets"
	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/history"
	"github.com/aatrey56/myfantasyprem/server/internal/idp"
	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
	"github.com/aatrey56/myfantasyprem/server/internal/realtime"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/tags"
	"github.com/aatrey56/myfantasyprem/server/internal/watchlist"
)

const problemTypeBase = "https://myfantasyprem.example/problems/"

// Server implements StrictServerInterface.
//
//   - Accounts and sessions (/v1/auth/*, /v1/me) run on auth.Service, with
//     Apple and Google identity tokens verified by idp, and league
//     management (create, settings, invites, members, leave, rename team)
//     on league.Service: Postgres in the server (Options), memory in tests
//     and the database-less dev server.
//   - The draft, lineup edits, waivers and trades call their rule packages
//     (draft.go, lineup.go, waivers.go, trades.go), which change the
//     league's State in one transaction and report changes to Notify,
//     which publishes realtime events (events.go, realtime.go).
//   - The reads (league home, standings, schedule, matchups, lineups,
//     teams, players, clubs, the current gameweek, the transactions feed)
//     join the league's game state with the shared stats (stats.Repo) and
//     score from scratch (game.go).
//   - Importing, syncing and switching over FPL Draft leagues run on
//     mirror.Service (imports.go, docs/adr/0012); without one (the
//     database-less dev server, unless cmd/api gives it one) they answer
//     503.
//   - Handlers reject malformed ids with 400 themselves, because the
//     generated code does not enforce `pattern`.
type Server struct {
	version string
	// healthCheck pings the database for /healthz; nil: always ok.
	healthCheck func(context.Context) error
	// healthMu guards the last ping result, reused for healthCacheTTL so
	// public /healthz traffic cannot turn into one database ping per request.
	healthMu  sync.Mutex
	healthAt  time.Time
	healthErr error
	// resyncDisabled are the leagues whose re-sync answers 409.
	resyncDisabled []string
	// now is the clock; tests replace it.
	now func() time.Time
	log *slog.Logger
	// sender delivers magic links (logs them in development).
	sender auth.Sender

	auth    auth.Service
	leagues *league.Service
	stats   stats.Repo
	// notify hears about committed changes (realtime events); the server
	// itself unless replaced.
	notify league.Notifier
	// apple and google verify identity tokens; unconfigured ones answer 503.
	apple, google *idp.Verifier
	// appleTokens exchanges Apple authorization codes and revokes the
	// refresh tokens at account deletion; nil when not configured.
	appleTokens *idp.AppleTokens

	// Realtime: tickets and the replay buffer, the local hub of sockets,
	// the publisher Notify uses (it delivers to hub directly unless a
	// LISTEN loop does), and the sockets' lifetime.
	rt          realtime.Store
	hub         *realtime.Hub
	publisher   *realtime.Publisher
	realtimeURL string
	// appURL is the web app's origin that invite links open.
	appURL string
	// emailLimit limits sign-in links per email address (the per-IP
	// limits are middleware; this one needs the request body).
	emailLimit *limiter
	// wsMaxLifetime caps how long one socket stays open.
	wsMaxLifetime time.Duration
	wsOrigins     []string
	wsAnyOrigin   bool
	baseCtx       context.Context
	stop          context.CancelFunc

	// mirror imports, syncs and switches over FPL Draft leagues; nil
	// answers those operations with 503. importLimit limits each user's
	// previews and new imports, which reach FPL.
	mirror      *mirror.Service
	importLimit *limiter
	// signInLinkLimit limits each commissioner's new sign-in links.
	signInLinkLimit *limiter
	// claimSignIn lets a seat's claim link sign a signed-out person in
	// (CLAIM_LINK_SIGNIN, docs/adr/0013).
	claimSignIn bool
	// assets serves crests, shirts and player photos (/v1/assets, assets.go);
	// nil is the branded-assets switch off.
	assets *assets.Service
	// watchlist keeps each member's starred players by league.
	watchlist watchlist.Store
	// notifs keeps notification preferences, the inbox and push devices.
	notifs notifications.Store
	// history keeps players' previous seasons (player card).
	history history.Store
	// historyProvider names the external ids history is keyed by.
	historyProvider string
	// webPushKey is the VAPID public key, "" when Web Push is off.
	webPushKey string
	// tags keeps each member's private player tags by league.
	tags tags.Store
}

var _ StrictServerInterface = (*Server)(nil)

// NewServer returns a Server reporting version on /healthz.
func NewServer(version string) *Server {
	s := &Server{
		version: version,
		log:     slog.Default(),
		appURL:  devAppURL,
	}
	// The stores read the clock through s.now, so tests can replace it.
	s.now = time.Now
	clock := func() time.Time { return s.now() }
	s.auth = auth.NewMemory(clock)
	s.sender = auth.DisabledSender{Log: s.log}
	s.stats = stats.NewMemory()
	s.leagues = league.NewService(league.NewMemory(), clock).WithStats(s.stats)
	s.watchlist = watchlist.NewMemory(clock)
	s.notifs = notifications.NewMemory(clock)
	s.history = history.NewMemory()
	s.historyProvider = "fpl"
	s.tags = tags.NewMemory(clock)
	s.notify = s
	s.wsMaxLifetime = auth.AccessTTL
	s.emailLimit = newLimiter(DefaultRateLimits().AuthEmail, limiterKeys)
	s.importLimit = newLimiter(DefaultRateLimits().ImportUser, limiterKeys)
	s.signInLinkLimit = newLimiter(DefaultRateLimits().SignInLinkUser, limiterKeys)
	s.apple, s.google = idp.Apple(nil, nil), idp.Google(nil, nil)
	s.baseCtx, s.stop = context.WithCancel(context.Background())
	s.setRealtime(realtime.NewMemory(clock), false)
	return s
}

// setRealtime puts the realtime store in place with a fresh hub. With
// listen, a LISTEN loop (cmd/api, realtime.Listen) delivers published
// events to the hub; otherwise the publisher does at once.
func (s *Server) setRealtime(st realtime.Store, listen bool) {
	s.rt = st
	s.hub = realtime.NewHub(st, s.membership)
	s.publisher = &realtime.Publisher{Store: st}
	if !listen {
		s.publisher.Hub = s.hub
	}
}

// authenticator lets newRoutes find the server's session store, also through
// wrappers that embed *Server.
func (s *Server) authenticator() authenticator { return s.auth }

// notImplemented is the body of every stub response.
func notImplemented(ctx context.Context, operation string) Problem {
	return newProblem(501, "not-implemented", "Not implemented",
		operation+" is part of the contract but not implemented yet.", RequestIDFrom(ctx))
}

// badRequest is the body of a 400 caused by an invalid path or query value.
func badRequest(ctx context.Context, detail string) BadRequestApplicationProblemPlusJSONResponse {
	return BadRequestApplicationProblemPlusJSONResponse(
		newProblem(400, "bad-request", "Bad request", detail, RequestIDFrom(ctx)))
}

// badID returns a 400 detail if value is not a valid id with the prefix, else "".
func badID(param, prefix, value string) string {
	if ids.Valid(prefix, value) {
		return ""
	}
	return param + " must be a " + prefix + "_ id."
}

// badGameweek returns a 400 detail if gw is outside 1-38, else "".
func badGameweek(gw int) string {
	if gw >= 1 && gw <= 38 {
		return ""
	}
	return "gw must be between 1 and 38."
}

// healthTimeout bounds the /healthz database ping.
const healthTimeout = 2 * time.Second

func (s *Server) GetHealth(ctx context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	if err := s.pingDB(ctx); err != nil {
		s.log.WarnContext(ctx, "health check failed", "err", err)
		return GetHealth503JSONResponse{Status: HealthStatusUnavailable, Version: &s.version}, nil
	}
	return GetHealth200JSONResponse{Status: HealthStatusOk, Version: &s.version}, nil
}

// healthCacheTTL is how long a database ping result answers /healthz.
const healthCacheTTL = 5 * time.Second

// pingDB runs the health check at most once per healthCacheTTL.
func (s *Server) pingDB(ctx context.Context) error {
	if s.healthCheck == nil {
		return nil
	}
	s.healthMu.Lock()
	defer s.healthMu.Unlock()
	if !s.healthAt.IsZero() && s.now().Sub(s.healthAt) < healthCacheTTL {
		return s.healthErr
	}
	ctx, cancel := context.WithTimeout(ctx, healthTimeout)
	defer cancel()
	s.healthErr = s.healthCheck(ctx)
	s.healthAt = s.now()
	return s.healthErr
}

// resolveLeagueRef turns "exactly one of draftLeagueUrl or fplLeagueId" into
// the FPL league id. Errors other than errClassicLeagueURL are safe to show
// to the client as a 400 detail: plain English, naming no request field.
func resolveLeagueRef(draftLeagueURL *string, fplLeagueID *int64) (int64, error) {
	switch {
	case draftLeagueURL != nil && fplLeagueID != nil:
		return 0, errors.New("Send a league link or a league ID, not both.")
	case draftLeagueURL != nil:
		return parseLeagueInput(*draftLeagueURL)
	case fplLeagueID != nil:
		if *fplLeagueID < 1 {
			return 0, errors.New(msgLeagueInputInvalid)
		}
		return *fplLeagueID, nil
	default:
		return 0, errors.New(msgLeagueInputEmpty)
	}
}
