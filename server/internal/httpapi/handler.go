package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/netip"
	"runtime/debug"
	"slices"
	"strings"
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
	"github.com/aatrey56/myfantasyprem/server/internal/updates"
	"github.com/aatrey56/myfantasyprem/server/internal/watchlist"
)

const (
	// maxBodyBytes caps every request body. The largest legitimate body is a
	// sign-in request with an identity token (a few KB).
	maxBodyBytes = 1 << 20

	requestIDHeader = "X-Request-ID"

	// devAppURL is the Expo web dev server, the web app's origin when
	// Options.AppURL is empty (config.DevAppURL).
	devAppURL = "http://localhost:8081"
)

// Options configures NewHandler.
type Options struct {
	Logger         *slog.Logger
	Version        string
	AllowedOrigins []string
	// Sender delivers magic links. Nil means DisabledSender.
	Sender auth.Sender
	// The stores. Nil ones are in memory (tests and the database-less dev
	// server); cmd/api passes the Postgres implementations.
	Auth        auth.Service
	Leagues     league.Repo
	Stats       stats.Repo
	Idempotency IdempotencyStore
	// Realtime stores tickets and the replay buffer; nil is in memory.
	Realtime realtime.Store
	// RealtimeListen: a realtime.Listen loop delivers events to the hub
	// (several instances on Postgres); otherwise publishing delivers them.
	RealtimeListen bool
	// AppURL is the web app's origin (APP_URL); invite links open its
	// /join/{code} route. Empty: the Expo web dev server.
	AppURL string
	// RealtimeURL is the WebSocket URL tickets point at, for example
	// wss://api.example/v1/realtime; empty derives it from the request.
	RealtimeURL string
	// Apple and Google verify Sign in with Apple and Google identity
	// tokens (idp.Apple, idp.Google with the app's client ids); nil or
	// without client ids, those sign-ins answer 503.
	Apple, Google *idp.Verifier
	// AppleTokens exchanges Apple authorization codes at sign-in and
	// revokes the refresh tokens at account deletion; nil: not configured
	// (cmd/api logs it; required before App Store submission).
	AppleTokens *idp.AppleTokens
	// Production (APP_ENV=production) stops trusting X-Forwarded-Host and
	// X-Forwarded-Proto for absolute URLs.
	Production bool
	// RateLimits are the per-IP and per-user limits; nil is
	// DefaultRateLimits.
	RateLimits *RateLimits
	// rateClock is the rate limiter's clock; nil is time.Now. Tests freeze
	// it so a bucket cannot refill between two requests on a loaded machine.
	rateClock func() time.Time
	// Mirror imports, syncs and switches over FPL Draft leagues (docs/adr/0012);
	// nil answers those operations with 503.
	Mirror *mirror.Service
	// ClaimLinkSignIn lets a seat's claim link sign a signed-out person in
	// with only a display name (CLAIM_LINK_SIGNIN, docs/adr/0013).
	ClaimLinkSignIn bool
	// Assets serves club crests, shirts and player photos (BRANDED_ASSETS);
	// nil or not Enabled answers every image with 404.
	Assets *assets.Service
	// Watchlist keeps members' watchlists; nil is in memory.
	Watchlist watchlist.Store
	// Tags keeps members' player tags; nil is in memory.
	Tags tags.Store
	// Notifications keeps notification settings, the inbox and push
	// devices; nil is in memory.
	Notifications notifications.Store
	// History keeps players' previous seasons; nil is in memory.
	History history.Store
	// HistoryProvider is the provider whose player ids key History (the
	// poller's: "fpl", or "demo" for the demo world). Empty: "fpl".
	HistoryProvider string
	// WebPushPublicKey is the VAPID public key /v1/config hands browsers;
	// "" when Web Push is off.
	WebPushPublicKey string
	// TrustedProxies are the networks of the reverse proxies in front of
	// the server: only a connection from one of them has its
	// X-Forwarded-For read for the client address. Empty: never read.
	TrustedProxies []netip.Prefix
	// UpdatesDir holds the native app's over-the-air updates
	// (UPDATES_DIR, served under /updates/, updates.go); "" turns them off.
	UpdatesDir string
	// UpdatesPublicURL, when set, is the absolute base of update manifest
	// and asset urls (UPDATES_PUBLIC_URL); empty derives it from the request.
	UpdatesPublicURL string
	// HealthCheck, when set, runs on every GET /healthz (with a short
	// timeout); an error answers 503. cmd/api pings the database.
	HealthCheck func(context.Context) error
	// ResyncDisabledLeagues are league ids whose re-sync answers 409
	// resync-disabled (FPL_RESYNC_DISABLED_LEAGUES).
	ResyncDisabledLeagues []string
}

// API is the HTTP API and the parts cmd/api wires to the rest: the
// realtime hub (for the LISTEN loop) and the notifier that turns league
// changes into realtime events (for the jobs and the poller).
type API struct {
	Handler  http.Handler
	Hub      *realtime.Hub
	Notifier league.Notifier
	srv      *Server
}

// Close ends every WebSocket (call it before http.Server.Shutdown, which
// does not wait for hijacked connections).
func (a *API) Close() { a.srv.stop() }

// NewHandler returns the full HTTP handler: generated routes plus request ids,
// recovery, access logging, CORS and a body size limit. Every error, including
// unknown paths and wrong methods, is rendered as RFC 9457 problem+json.
func NewHandler(opts Options) http.Handler { return NewAPI(opts).Handler }

// NewAPI builds the API on opts' stores.
func NewAPI(opts Options) *API {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	srv := NewServer(opts.Version)
	srv.log = opts.Logger
	srv.sender = opts.Sender
	if srv.sender == nil {
		srv.sender = auth.DisabledSender{Log: opts.Logger}
	}
	clock := func() time.Time { return srv.now() }
	if opts.Auth != nil {
		srv.auth = opts.Auth
	}
	if opts.Stats != nil {
		srv.stats = opts.Stats
	}
	if opts.Leagues != nil {
		srv.leagues = league.NewService(opts.Leagues, clock).WithStats(srv.stats)
	}
	if opts.Realtime != nil || opts.RealtimeListen {
		st := opts.Realtime
		if st == nil {
			st = realtime.NewMemory(clock)
		}
		srv.setRealtime(st, opts.RealtimeListen)
	}
	srv.realtimeURL = opts.RealtimeURL
	if opts.AppURL != "" {
		srv.appURL = strings.TrimRight(opts.AppURL, "/")
	}
	if opts.Apple != nil {
		srv.apple = opts.Apple
	}
	if opts.Google != nil {
		srv.google = opts.Google
	}
	srv.appleTokens = opts.AppleTokens
	srv.mirror = opts.Mirror
	srv.healthCheck = opts.HealthCheck
	srv.resyncDisabled = opts.ResyncDisabledLeagues
	srv.claimSignIn = opts.ClaimLinkSignIn
	srv.assets = opts.Assets
	if opts.Watchlist != nil {
		srv.watchlist = opts.Watchlist
	}
	if opts.Tags != nil {
		srv.tags = opts.Tags
	}
	if opts.Notifications != nil {
		srv.notifs = opts.Notifications
	}
	if opts.History != nil {
		srv.history = opts.History
	}
	if opts.HistoryProvider != "" {
		srv.historyProvider = opts.HistoryProvider
	}
	srv.webPushKey = opts.WebPushPublicKey
	if opts.RateLimits != nil {
		srv.emailLimit = newLimiter(opts.RateLimits.AuthEmail, limiterKeys)
		srv.importLimit = newLimiter(opts.RateLimits.ImportUser, limiterKeys)
		srv.signInLinkLimit = newLimiter(opts.RateLimits.SignInLinkUser, limiterKeys)
	}
	srv.wsOrigins, srv.wsAnyOrigin = originPatterns(opts.AllowedOrigins)
	routes := newRoutes(opts, srv)
	if opts.UpdatesDir != "" {
		mux := http.NewServeMux()
		mux.Handle("/updates/", newUpdatesHandler(updates.Store{Dir: opts.UpdatesDir}, !opts.Production, opts.UpdatesPublicURL, opts.Logger))
		mux.Handle("/", routes)
		routes = mux
	}
	return &API{Handler: newHandler(opts, routes), Hub: srv.hub, Notifier: srv, srv: srv}
}

// newHandler wraps routes in the middleware chain, outermost first: recoverer,
// withSlowRouteDeadline, requestID, accessLog, cors, maxBytes. The recoverer is outermost so that it
// also catches panics in the other middleware.
func newHandler(opts Options, routes http.Handler) http.Handler {
	return recoverer(opts.Logger,
		withSlowRouteDeadline(requestID(
			accessLog(opts.Logger,
				cors(opts.AllowedOrigins,
					maxBytes(routes))))))
}

// routesServer is what newRoutes needs: the operations, the session
// store and the WebSocket endpoint. *Server and wrappers that embed it
// satisfy it.
type routesServer interface {
	StrictServerInterface
	authenticator() authenticator
	serveRealtime(w http.ResponseWriter, r *http.Request)
}

// newRoutes registers the generated routes for srv on a fresh mux, requires
// a bearer token on every non-public route, applies Idempotency-Key replay
// and makes unmatched requests answer with problem+json.
func newRoutes(opts Options, srv routesServer) http.Handler {
	strict := NewStrictHandlerWithOptions(srv, nil, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			var tooBig *http.MaxBytesError
			if errors.As(err, &tooBig) {
				writeProblem(w, http.StatusRequestEntityTooLarge, "payload-too-large", "Payload too large",
					fmt.Sprintf("The request body must be at most %d bytes.", maxBodyBytes))
				return
			}
			writeProblem(w, http.StatusBadRequest, "bad-request", "Bad request", "The request body is not valid JSON.")
		},
		ResponseErrorHandlerFunc: func(w http.ResponseWriter, r *http.Request, err error) {
			if clientCanceled(r, err) {
				opts.Logger.DebugContext(r.Context(), "client canceled",
					"request_id", RequestIDFrom(r.Context()), "err", err)
				return
			}
			opts.Logger.ErrorContext(r.Context(), "handler error",
				"request_id", RequestIDFrom(r.Context()), "err", err)
			writeInternalError(w)
		},
	})
	mux := http.NewServeMux()
	HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseRouter: mux,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			writeProblem(w, http.StatusBadRequest, "bad-request", "Bad request", paramErrorDetail(err))
		},
	})
	// The WebSocket endpoint (x-realtime) authenticates with its ticket.
	mux.HandleFunc("GET "+realtimePath, srv.serveRealtime)
	var idem IdempotencyStore = newIdempotencyStore()
	if opts.Idempotency != nil {
		idem = opts.Idempotency
	}
	limits := DefaultRateLimits()
	if opts.RateLimits != nil {
		limits = *opts.RateLimits
	}
	rateClock := time.Now
	if opts.rateClock != nil {
		rateClock = opts.rateClock
	}
	rl := newRateLimiter(limits, opts.TrustedProxies, rateClock)
	return withOrigin(!opts.Production, rl.limitByIP(mux, problemOnUnmatched(mux,
		authenticate(mux, srv.authenticator(), rl.chargeIP(mux), rl.limitByUser(mux, idempotent(mux, idem, time.Now, rawJSON(mux)))))))
}

// paramErrorDetail describes a parameter binding error without echoing the
// underlying parser error or the offending value.
func paramErrorDetail(err error) string {
	var invalid *InvalidParamFormatError
	var required *RequiredParamError
	var many *TooManyValuesForParamError
	switch {
	case errors.As(err, &invalid):
		return "Invalid format for parameter " + invalid.ParamName + "."
	case errors.As(err, &required):
		return "Missing required parameter " + required.ParamName + "."
	case errors.As(err, &many):
		return "Parameter " + many.ParamName + " must be given once."
	default:
		return "Invalid request parameters."
	}
}

// problemOnUnmatched makes unknown paths (404) and wrong methods (405) answer
// with problem+json instead of ServeMux's text/plain.
//
// A catch-all "/" route would swallow wrong-method requests, so instead it asks
// the mux which route matches (an empty pattern means none) and, for misses,
// lets the mux produce its own 404/405 into a probe writer to learn the status
// and the Allow header, then renders that as a problem.
func problemOnUnmatched(mux *http.ServeMux, matched http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, pattern := mux.Handler(r); pattern != "" {
			matched.ServeHTTP(w, r)
			return
		}
		probe := &probeWriter{header: http.Header{}, status: http.StatusNotFound}
		mux.ServeHTTP(probe, r)
		if probe.status == http.StatusMethodNotAllowed {
			w.Header().Set("Allow", probe.header.Get("Allow"))
			writeProblem(w, http.StatusMethodNotAllowed, "method-not-allowed", "Method not allowed",
				"This resource does not support "+r.Method+".")
			return
		}
		writeProblem(w, http.StatusNotFound, "not-found", "Not found", "No such resource.")
	})
}

// probeWriter records the status and headers of a mux miss and drops the body.
type probeWriter struct {
	header http.Header
	status int
}

func (p *probeWriter) Header() http.Header         { return p.header }
func (p *probeWriter) Write(b []byte) (int, error) { return len(b), nil }
func (p *probeWriter) WriteHeader(code int)        { p.status = code }

// newProblem builds an RFC 9457 body. slug is the last segment of the problem
// type URI. requestID may be empty.
func newProblem(status int, slug, title, detail, requestID string) Problem {
	typ := problemTypeBase + slug
	p := Problem{Type: &typ, Title: title, Status: status}
	if detail != "" {
		p.Detail = &detail
	}
	if requestID != "" {
		p.RequestId = &requestID
	}
	return p
}

// writeProblem writes a problem+json response. The request id is read from
// the X-Request-ID response header, which the requestID middleware sets before
// any handler runs.
func writeProblem(w http.ResponseWriter, status int, slug, title, detail string) {
	p := newProblem(status, slug, title, detail, w.Header().Get(requestIDHeader))
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// writeInternalError is the only 500 body clients ever see: no internals.
func writeInternalError(w http.ResponseWriter) {
	writeProblem(w, http.StatusInternalServerError, "internal", "Internal server error",
		"Something went wrong on our side.")
}

// statusClientClosed is nginx's 499: the client went away before the
// response. It is only logged, never sent.
const statusClientClosed = 499

// clientCanceled reports whether err is the request's own context being
// canceled because the client disconnected (or gave up): not a server
// fault, so no 500 and no error log.
func clientCanceled(r *http.Request, err error) bool {
	return errors.Is(err, context.Canceled) && errors.Is(r.Context().Err(), context.Canceled)
}

type requestIDKey struct{}

// RequestIDFrom returns the request id set by the requestID middleware, or ""
// outside a request.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// validRequestID accepts 1 to 64 characters of [A-Za-z0-9._-], so a client
// supplied id is safe to log and echo.
func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '.', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// requestID keeps a valid incoming X-Request-ID or mints a ULID, echoes it on
// the response and stores it in the request context.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if !validRequestID(id) {
			id = ids.ULID()
		}
		w.Header().Set(requestIDHeader, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// statusRecorder captures the status and size of a response for the access
// log. Unwrap lets http.NewResponseController reach the real writer for
// Flush and deadlines.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
	wrote  bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wrote {
		r.status = code
		r.wrote = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	r.wrote = true
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func accessLog(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		if !rec.wrote && errors.Is(r.Context().Err(), context.Canceled) {
			// The handler stopped because the client left: 499, not a 200.
			log.InfoContext(r.Context(), "client canceled",
				"request_id", RequestIDFrom(r.Context()),
				"method", r.Method, "path", logPath(r.URL.Path), "status", statusClientClosed,
				"bytes", rec.bytes, "duration_ms", time.Since(start).Milliseconds())
			return
		}
		log.InfoContext(r.Context(), "request",
			"request_id", RequestIDFrom(r.Context()),
			"method", r.Method, "path", logPath(r.URL.Path), "status", rec.status,
			"bytes", rec.bytes, "duration_ms", time.Since(start).Milliseconds())
	})
}

// logPath is a request path as logged: an invite code (a seat's claim
// link signs people in, docs/adr/0013) is a credential and is replaced.
func logPath(path string) string {
	const invites = "/v1/invites/"
	if !strings.HasPrefix(path, invites) {
		return path
	}
	_, rest, _ := strings.Cut(path[len(invites):], "/")
	if rest != "" {
		rest = "/" + rest
	}
	return invites + "{code}" + rest
}

// recoverer turns a panic into a 500 problem and logs the stack. It sits
// outside requestID, so it reads the id from the response header that
// requestID already set, and mints one itself if the panic happened before
// that. A panicking request skips accessLog, so the panic log carries the
// method and path.
func recoverer(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			id := w.Header().Get(requestIDHeader)
			if id == "" {
				id = ids.ULID()
				w.Header().Set(requestIDHeader, id)
			}
			log.ErrorContext(r.Context(), "panic", "request_id", id,
				"method", r.Method, "path", logPath(r.URL.Path),
				"value", v, "stack", string(debug.Stack()))
			writeInternalError(w)
		}()
		next.ServeHTTP(w, r)
	})
}

// maxBytes caps request bodies at maxBodyBytes. Reading past the cap fails
// with *http.MaxBytesError, which the request error handler maps to 413.
func maxBytes(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// cors allows cross-origin reads from the configured origins. The response
// varies by Origin whether or not the origin is allowed, so Vary: Origin is
// always set. A preflight from a disallowed origin gets no allow headers and
// falls through to the routes (which answer 405).
func cors(allowed []string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Add("Vary", "Origin")
		preflight := r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != ""
		if preflight {
			h.Add("Vary", "Access-Control-Request-Method")
			h.Add("Vary", "Access-Control-Request-Headers")
		}
		origin := r.Header.Get("Origin")
		if origin != "" && (slices.Contains(allowed, origin) || slices.Contains(allowed, "*")) {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Expose-Headers", "Location, Retry-After, X-Request-ID, WWW-Authenticate, Idempotent-Replayed")
			if preflight {
				h.Set("Access-Control-Allow-Methods", strings.Join([]string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}, ", "))
				h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Request-ID")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
