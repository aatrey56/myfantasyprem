package httpapi

import (
	"context"
	"math"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Rate limiting (docs/ARCHITECTURE.md "Security"): in-process token
// buckets keyed by client IP and, on routes that need a signed-in user, by
// user id. Each instance limits on its own, so the effective limit behind
// a load balancer is the per-instance limit times the instance count.

// Limit is a token bucket: Burst requests at once, refilled at PerMinute.
// A zero PerMinute never limits.
type Limit struct {
	PerMinute float64
	Burst     int
}

// RateLimits are the limits the server applies.
type RateLimits struct {
	// Global applies to every request but the images, per client IP.
	Global Limit
	// Assets applies to the images (GET /v1/assets/*) instead of Global,
	// per client IP. A list screen loads a photo and crests per row, so
	// images on Global would spend the API budget and the screen's next
	// API calls would fail. Images come from the disk cache; Proxy keeps
	// its own pace upstream.
	Assets Limit
	// Auth applies to every /v1/auth/* route together, per client IP.
	Auth Limit
	// AuthEmail is POST /v1/auth/email per email address (an inbox
	// flooded with sign-in links from many IPs).
	AuthEmail Limit
	// InvitePreview is GET /v1/invites/{code} per client IP (guessing codes).
	InvitePreview Limit
	// InviteAcceptIP and InviteAcceptUser are POST /v1/invites/{code}/accept.
	InviteAcceptIP, InviteAcceptUser Limit
	// TicketIP and TicketUser are POST /v1/realtime/tickets.
	TicketIP, TicketUser Limit
	// ImportUser is the previews and new imports per user that reach FPL
	// Draft (checked in the handlers, after validation), which we keep to a
	// polite rate.
	ImportUser Limit
	// SignInLinkUser is the sign-in links a commissioner issues (checked
	// in the handler, per user; docs/adr/0013).
	SignInLinkUser Limit
	// Revoke is the revocations (revocationRoutes) per session. A request
	// that authenticates is limited by this alone: no per-IP limit applies,
	// so other people's traffic from a shared address cannot stop someone
	// revoking their own session. One that does not authenticate is
	// charged to the per-IP limits like any other.
	Revoke Limit
}

// DefaultRateLimits are generous for a person using the app and tight
// for a script: sign-in and invite codes are what an attacker would guess
// at.
func DefaultRateLimits() RateLimits {
	return RateLimits{
		Global:           Limit{PerMinute: 600, Burst: 200},
		Assets:           Limit{PerMinute: 1200, Burst: 600},
		Auth:             Limit{PerMinute: 20, Burst: 10},
		AuthEmail:        Limit{PerMinute: 0.2, Burst: 5},
		InvitePreview:    Limit{PerMinute: 20, Burst: 10},
		InviteAcceptIP:   Limit{PerMinute: 20, Burst: 10},
		InviteAcceptUser: Limit{PerMinute: 10, Burst: 5},
		TicketIP:         Limit{PerMinute: 60, Burst: 20},
		TicketUser:       Limit{PerMinute: 20, Burst: 10},
		ImportUser:       Limit{PerMinute: 10, Burst: 10},
		SignInLinkUser:   Limit{PerMinute: 2, Burst: 5},
		Revoke:           Limit{PerMinute: 60, Burst: 30},
	}
}

// revocationRoutes end the caller's sessions: sign-out and account
// deletion. They must work whatever else comes from the caller's IP.
var revocationRoutes = map[string]bool{
	"POST /v1/auth/logout": true,
	"DELETE /v1/me":        true,
}

// assetsPattern starts the route pattern of every image (RateLimits.Assets).
// Matching the pattern, not the path, keeps unknown /v1/assets/ paths on
// Global.
const assetsPattern = "GET /v1/assets/"

// limiterKeys bounds each limiter's table (memory under a flood of
// distinct addresses).
const limiterKeys = 100_000

// sweepEvery spaces out the sweeps of a full table, so a flood of new
// keys does not cost a full scan per request.
const sweepEvery = time.Second

type bucket struct {
	tokens float64
	last   time.Time
}

// limiter is one Limit's buckets by key.
type limiter struct {
	rate  float64 // tokens per second
	burst float64
	max   int

	mu      sync.Mutex
	buckets map[string]*bucket
	// overflow is shared by the new keys that arrive while the table is
	// full of keys still limited. Evicting those instead would refill
	// them: an attacker with many addresses could reset its own throttle.
	overflow  *bucket
	lastSweep time.Time
}

func newLimiter(l Limit, maxKeys int) *limiter {
	return &limiter{rate: l.PerMinute / 60, burst: float64(max(l.Burst, 1)), max: maxKeys, buckets: map[string]*bucket{}}
}

// allow takes a token from key's bucket. When there is none it returns
// false and how long until there is.
func (l *limiter) allow(key string, now time.Time) (bool, time.Duration) {
	if l == nil || l.rate <= 0 {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= l.max && now.Sub(l.lastSweep) >= sweepEvery {
			l.sweepLocked(now)
		}
		switch {
		case len(l.buckets) < l.max:
			b = &bucket{tokens: l.burst, last: now}
			l.buckets[key] = b
		case l.overflow != nil:
			b = l.overflow
		default:
			b = &bucket{tokens: l.burst, last: now}
			l.overflow = b
		}
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
		b.last = now
	}
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// sweepLocked drops the buckets that have refilled (idle keys: dropping
// them changes nothing). Buckets still limiting are kept.
func (l *limiter) sweepLocked(now time.Time) {
	l.lastSweep = now
	for k, b := range l.buckets {
		if b.tokens+now.Sub(b.last).Seconds()*l.rate >= l.burst {
			delete(l.buckets, k)
		}
	}
}

func (l *limiter) size() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// rateLimiter holds the server's limiters.
type rateLimiter struct {
	now                              func() time.Time
	global, assets, auth             *limiter
	invitePreview                    *limiter
	inviteAcceptIP, inviteAcceptUser *limiter
	ticketIP, ticketUser             *limiter
	revoke                           *limiter
	byIP, byUser                     map[string]*limiter // route pattern -> limiter
	trusted                          []netip.Prefix
}

func newRateLimiter(l RateLimits, trusted []netip.Prefix, now func() time.Time) *rateLimiter {
	n := func(x Limit) *limiter { return newLimiter(x, limiterKeys) }
	rl := &rateLimiter{
		now: now, trusted: trusted,
		global: n(l.Global), assets: n(l.Assets), auth: n(l.Auth), invitePreview: n(l.InvitePreview),
		inviteAcceptIP: n(l.InviteAcceptIP), inviteAcceptUser: n(l.InviteAcceptUser),
		ticketIP: n(l.TicketIP), ticketUser: n(l.TicketUser),
		revoke: n(l.Revoke),
	}
	rl.byIP = map[string]*limiter{
		"GET /v1/invites/{code}":         rl.invitePreview,
		"POST /v1/invites/{code}/accept": rl.inviteAcceptIP,
		"POST /v1/realtime/tickets":      rl.ticketIP,
	}
	rl.byUser = map[string]*limiter{
		"POST /v1/invites/{code}/accept": rl.inviteAcceptUser,
		"POST /v1/realtime/tickets":      rl.ticketUser,
	}
	return rl
}

type clientIPKey struct{}

// clientIPFrom returns the client address the clientIP middleware found.
func clientIPFrom(ctx context.Context) string {
	ip, _ := ctx.Value(clientIPKey{}).(string)
	return ip
}

// clientIP finds the client's address: the connection's peer, unless the
// peer is a trusted proxy, in which case X-Forwarded-For is read from the
// right, skipping trusted proxies, and the first other address is the
// client. A client can prepend anything to the header, so the leftmost
// entries are never trusted.
func (rl *rateLimiter) clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return host
	}
	peer = peer.Unmap()
	if !rl.isTrusted(peer) {
		return peer.String()
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break // a malformed hop: stop at the last good one
		}
		a = a.Unmap()
		if !rl.isTrusted(a) {
			return a.String()
		}
		peer = a
	}
	return peer.String()
}

func (rl *rateLimiter) isTrusted(a netip.Addr) bool {
	for _, p := range rl.trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ipKey is the rate-limit key of a client address: IPv6 addresses by
// their /64 (one subscriber usually has the whole prefix), IPv4 as is.
func ipKey(ip string) string {
	a, err := netip.ParseAddr(ip)
	if err != nil || !a.Is6() {
		return ip
	}
	return netip.PrefixFrom(a, 64).Masked().String()
}

// retryAfter is a wait in whole seconds, at least one.
func retryAfter(wait time.Duration) int { return max(1, int(math.Ceil(wait.Seconds()))) }

// tooMany writes the 429 problem with Retry-After in whole seconds.
func tooMany(w http.ResponseWriter, wait time.Duration) {
	secs := retryAfter(wait)
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeProblem(w, http.StatusTooManyRequests, "rate-limited", "Too many requests",
		"Slow down and try again in "+strconv.Itoa(secs)+" seconds.")
}

// ipLimitsDeferredKey marks a revocation whose per-IP limits wait until
// authentication has failed (chargeIP).
type ipLimitsDeferredKey struct{}

// ipChecks are the per-IP limiters that apply to r.
func (rl *rateLimiter) ipChecks(r *http.Request, pattern string) []*limiter {
	checks := []*limiter{rl.global}
	if strings.HasPrefix(pattern, assetsPattern) {
		checks = []*limiter{rl.assets}
	}
	if strings.HasPrefix(r.URL.Path, "/v1/auth/") {
		checks = append(checks, rl.auth)
	}
	if l := rl.byIP[pattern]; l != nil {
		checks = append(checks, l)
	}
	return checks
}

// allowIP applies the per-IP limits; when one is spent it writes the 429
// and returns false.
func (rl *rateLimiter) allowIP(w http.ResponseWriter, r *http.Request, pattern string) bool {
	key, now := ipKey(clientIPFrom(r.Context())), rl.now()
	for _, l := range rl.ipChecks(r, pattern) {
		if ok, wait := l.allow(key, now); !ok {
			tooMany(w, wait)
			return false
		}
	}
	return true
}

// limitByIP records the client IP and applies the per-IP limits: the
// global one (the assets one for images), the shared /v1/auth/* one, and the route's own. A
// revocation with a bearer token skips them here: if the token
// authenticates, only the per-session limit applies (limitByUser);
// otherwise authenticate charges the IP (chargeIP) before its 401.
func (rl *rateLimiter) limitByIP(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := rl.clientIP(r)
		r = r.WithContext(context.WithValue(r.Context(), clientIPKey{}, ip))
		_, pattern := mux.Handler(r)
		if revocationRoutes[pattern] && bearerToken(r) != "" {
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ipLimitsDeferredKey{}, true)))
			return
		}
		if rl.allowIP(w, r, pattern) {
			next.ServeHTTP(w, r)
		}
	})
}

// chargeIP applies the per-IP limits a revocation skipped, once its token
// has failed to authenticate. It reports whether it wrote a 429.
func (rl *rateLimiter) chargeIP(mux *http.ServeMux) func(http.ResponseWriter, *http.Request) bool {
	return func(w http.ResponseWriter, r *http.Request) bool {
		if deferred, _ := r.Context().Value(ipLimitsDeferredKey{}).(bool); !deferred {
			return false
		}
		_, pattern := mux.Handler(r)
		return !rl.allowIP(w, r, pattern)
	}
}

// limitByUser applies the per-user limits and the per-session revocation
// limit; it runs after authenticate.
func (rl *rateLimiter) limitByUser(mux *http.ServeMux, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if uid := userFrom(r.Context()); uid != "" {
			_, pattern := mux.Handler(r)
			l, key := rl.byUser[pattern], uid
			if revocationRoutes[pattern] {
				l, key = rl.revoke, sessionFrom(r.Context())
			}
			if l != nil {
				if ok, wait := l.allow(key, rl.now()); !ok {
					tooMany(w, wait)
					return
				}
			}
		}
		next.ServeHTTP(w, r)
	})
}
