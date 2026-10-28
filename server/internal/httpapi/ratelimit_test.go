package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
)

// limitedHandler is the full handler with opts' rate limits.
func limitedHandler(t *testing.T, mutate func(*Options)) (http.Handler, *Server) {
	t.Helper()
	srv, _ := newTestServer()
	opts := testOptions()
	// A frozen clock: no bucket refills during the test, however slow the
	// machine (the auth bucket gains a token every 3 s on the real clock).
	frozen := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	opts.rateClock = func() time.Time { return frozen }
	if mutate != nil {
		mutate(&opts)
	}
	return newHandler(opts, newRoutes(opts, srv)), srv
}

// from sends a request as if from ip (through RemoteAddr), with an
// optional bearer token and X-Forwarded-For.
func from(h http.Handler, ip, token, xff, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.RemoteAddr = ip + ":40000"
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if xff != "" {
		req.Header.Set("X-Forwarded-For", xff)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// check429 asserts a contract-valid 429 with a Retry-After.
func check429(t *testing.T, rec *httptest.ResponseRecorder, req *http.Request) {
	t.Helper()
	p := decodeProblem(t, rec, http.StatusTooManyRequests)
	if problemType(p) != problemTypeBase+"rate-limited" {
		t.Errorf("type = %s", problemType(p))
	}
	if n, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || n < 1 {
		t.Errorf("Retry-After = %q", rec.Header().Get("Retry-After"))
	}
	_, v := loadSpec(t)
	if ok, errs := v.ValidateHttpResponse(req, rec.Result()); !ok {
		for _, e := range errs {
			t.Errorf("429 does not match the contract: %s: %s", e.Message, e.Reason)
		}
	}
}

// hitUntil429 sends requests until one is limited and returns how many
// went through first.
func hitUntil429(t *testing.T, max int, send func(i int) *httptest.ResponseRecorder) (int, *httptest.ResponseRecorder) {
	t.Helper()
	for i := range max {
		rec := send(i)
		if rec.Code == http.StatusTooManyRequests {
			return i, rec
		}
	}
	t.Fatalf("no 429 after %d requests", max)
	return 0, nil
}

// emailBody is a sign-in request for the i-th address: tests of the
// per-IP limits use a new address each time, clear of the per-address one.
func emailBody(i int) string { return `{"email":"sam` + strconv.Itoa(i) + `@example.com"}` }

func TestRateLimitAuthPerIP(t *testing.T) {
	h, _ := limitedHandler(t, nil)
	body := emailBody(1000)
	ok, rec := hitUntil429(t, 100, func(i int) *httptest.ResponseRecorder {
		return from(h, "203.0.113.7", "", "", "POST", "/v1/auth/email", emailBody(i))
	})
	if want := DefaultRateLimits().Auth.Burst; ok != want {
		t.Errorf("%d sign-in requests went through, want the burst of %d", ok, want)
	}
	// Every /v1/auth route shares the bucket; another client is unaffected.
	if rec := from(h, "203.0.113.7", "", "", "POST", "/v1/auth/refresh", `{"refreshToken":"x"}`); rec.Code != 429 {
		t.Errorf("refresh from the same IP = %d", rec.Code)
	}
	check429(t, rec, httptest.NewRequest("POST", "/v1/auth/email", strings.NewReader(body)))
	if rec := from(h, "203.0.113.8", "", "", "POST", "/v1/auth/email", body); rec.Code != 202 {
		t.Errorf("another IP = %d", rec.Code)
	}
}

func TestRateLimitInvitePreview(t *testing.T) {
	h, _ := limitedHandler(t, nil)
	_, rec := hitUntil429(t, 100, func(int) *httptest.ResponseRecorder {
		return from(h, "203.0.113.7", "", "", "GET", "/v1/invites/ZZZZZZZZZZ", "")
	})
	check429(t, rec, httptest.NewRequest("GET", "/v1/invites/ZZZZZZZZZZ", nil))
}

func TestRateLimitInviteAcceptPerUser(t *testing.T) {
	h, _ := limitedHandler(t, nil)
	// The same account from many addresses is still one user.
	ok, rec := hitUntil429(t, 100, func(i int) *httptest.ResponseRecorder {
		return from(h, "198.51.100."+strconv.Itoa(i+1), testToken, "", "POST", "/v1/invites/ZZZZZZZZZZ/accept", `{"teamName":"X"}`)
	})
	if want := DefaultRateLimits().InviteAcceptUser.Burst; ok != want {
		t.Errorf("%d accepts went through, want the per-user burst of %d", ok, want)
	}
	check429(t, rec, httptest.NewRequest("POST", "/v1/invites/ZZZZZZZZZZ/accept", strings.NewReader(`{"teamName":"X"}`)))
}

func TestRateLimitRealtimeTicketsPerUser(t *testing.T) {
	h, _ := limitedHandler(t, nil)
	ok, rec := hitUntil429(t, 100, func(i int) *httptest.ResponseRecorder {
		return from(h, "198.51.100."+strconv.Itoa(i+1), testToken, "", "POST", "/v1/realtime/tickets", `{}`)
	})
	if want := DefaultRateLimits().TicketUser.Burst; ok != want {
		t.Errorf("%d tickets went through, want the per-user burst of %d", ok, want)
	}
	check429(t, rec, httptest.NewRequest("POST", "/v1/realtime/tickets", strings.NewReader(`{}`)))
}

func TestRateLimitGlobalPerIP(t *testing.T) {
	h, _ := limitedHandler(t, func(o *Options) {
		l := DefaultRateLimits()
		l.Global = Limit{PerMinute: 60, Burst: 3}
		o.RateLimits = &l
	})
	ok, rec := hitUntil429(t, 10, func(int) *httptest.ResponseRecorder {
		return from(h, "203.0.113.7", "", "", "GET", "/v1/clubs", "")
	})
	if ok != 3 {
		t.Errorf("%d requests went through, want 3", ok)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("no Retry-After")
	}
}

// A list screen loads a photo and crests per row: images have their own
// bucket, so loading them never spends the API budget (the API calls that
// followed a Players tab failed with 429), and the API never spends theirs.
func TestRateLimitAssetsApartFromGlobal(t *testing.T) {
	h, _ := limitedHandler(t, func(o *Options) {
		l := DefaultRateLimits()
		l.Global = Limit{PerMinute: 60, Burst: 3}
		l.Assets = Limit{PerMinute: 60, Burst: 5}
		o.RateLimits = &l
	})
	const ip = "203.0.113.9"
	image := func(i int) *httptest.ResponseRecorder {
		return from(h, ip, "", "", "GET", "/v1/assets/players/plr_01k7m2xq8z00000000000000"+strconv.Itoa(10+i)+"/photo", "")
	}
	ok, rec := hitUntil429(t, 20, image)
	if ok != 5 {
		t.Errorf("%d images went through, want the assets burst of 5", ok)
	}
	check429(t, rec, httptest.NewRequest("GET", "/v1/assets/players/plr_01k7m2xq8z0000000000000010/photo", nil))
	ok, _ = hitUntil429(t, 10, func(int) *httptest.ResponseRecorder {
		return from(h, ip, "", "", "GET", "/v1/clubs", "")
	})
	if ok != 3 {
		t.Errorf("%d API requests went through after the images, want the global burst of 3", ok)
	}
	// An unknown path under /v1/assets/ is no image: it stays on Global.
	if rec := from(h, ip, "", "", "GET", "/v1/assets/nope", ""); rec.Code != http.StatusTooManyRequests {
		t.Errorf("unknown asset path = %d, want 429 from the spent global limit", rec.Code)
	}
}

// X-Forwarded-For counts only when the connection comes from a trusted
// proxy; otherwise anyone could rotate it to dodge the limit.
func TestRateLimitTrustedProxies(t *testing.T) {
	h, _ := limitedHandler(t, func(o *Options) {
		o.TrustedProxies = []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}
	})
	// Through the trusted proxy, each forwarded client has its own bucket
	// (the rightmost untrusted address counts, not what the client wrote).
	for i := range 30 {
		xff := "6.6.6.6, 198.51.100." + strconv.Itoa(i+1) + ", 10.0.0.9"
		if rec := from(h, "10.0.0.1", "", xff, "POST", "/v1/auth/email", emailBody(i)); rec.Code != 202 {
			t.Fatalf("client %d behind the proxy = %d", i, rec.Code)
		}
	}
	// Straight from the internet, a rotating X-Forwarded-For is ignored.
	hitUntil429(t, 100, func(i int) *httptest.ResponseRecorder {
		return from(h, "203.0.113.7", "", "198.51.100."+strconv.Itoa(i+1), "POST", "/v1/auth/email", emailBody(i))
	})
	// Without trusted proxies the header is never read.
	h, _ = limitedHandler(t, nil)
	hitUntil429(t, 100, func(i int) *httptest.ResponseRecorder {
		return from(h, "10.0.0.1", "", "198.51.100."+strconv.Itoa(i+1), "POST", "/v1/auth/email", emailBody(i))
	})
}

func TestLimiterRefills(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	l := newLimiter(Limit{PerMinute: 60, Burst: 2}, 10)
	for i := range 2 {
		if ok, _ := l.allow("a", now); !ok {
			t.Fatalf("request %d limited", i)
		}
	}
	ok, wait := l.allow("a", now)
	if ok || wait <= 0 || wait > time.Second {
		t.Fatalf("third = %v, wait %v", ok, wait)
	}
	if ok, _ := l.allow("a", now.Add(time.Second)); !ok {
		t.Fatal("no refill after a second")
	}
	// The table is bounded: idle keys go first.
	for i := range 50 {
		l.allow(strconv.Itoa(i), now.Add(time.Hour))
	}
	if n := l.size(); n > 10 {
		t.Fatalf("limiter holds %d keys, cap 10", n)
	}
	// A zero limit never limits.
	off := newLimiter(Limit{}, 10)
	for range 100 {
		if ok, _ := off.allow("a", now); !ok {
			t.Fatal("an unset limit limited")
		}
	}
}

// A full table never resets throttles: an attacker with many addresses
// (cheap with IPv6) must not refill a key it has exhausted.
func TestLimiterFullTableKeepsExhaustedKeys(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	l := newLimiter(Limit{PerMinute: 1, Burst: 2}, 10)
	for range 2 {
		l.allow("victim", now)
	}
	if ok, _ := l.allow("victim", now); ok {
		t.Fatal("burst not enforced")
	}
	for i := range 1000 {
		l.allow("flood"+strconv.Itoa(i), now)
	}
	if ok, _ := l.allow("victim", now); ok {
		t.Fatal("an exhausted key was refilled when the table filled up")
	}
	if n := l.size(); n > 10 {
		t.Fatalf("limiter holds %d keys, cap 10", n)
	}
}

// IPv6 clients are limited per /64: one host usually has the whole
// prefix and could otherwise rotate addresses.
func TestRateLimitIPv6PerSlash64(t *testing.T) {
	h, _ := limitedHandler(t, nil)
	hitUntil429(t, 100, func(i int) *httptest.ResponseRecorder {
		// A new address and email each time: only the /64 is shared.
		return from(h, "[2001:db8:1:2::"+strconv.FormatInt(int64(i+1), 16)+"]", "", "", "POST", "/v1/auth/email", emailBody(i))
	})
	if rec := from(h, "[2001:db8:1:3::1]", "", "", "POST", "/v1/auth/email", `{"email":"kim@example.com"}`); rec.Code != 202 {
		t.Errorf("another /64 = %d", rec.Code)
	}
}

// POST /v1/auth/email is limited per IP (TestRateLimitAuthPerIP) and per
// address: many IPs cannot mail-bomb one inbox.
func TestRateLimitEmailPerAddress(t *testing.T) {
	h, _ := limitedHandler(t, nil)
	ok, rec := hitUntil429(t, 100, func(i int) *httptest.ResponseRecorder {
		return from(h, "198.51.100."+strconv.Itoa(i+1), "", "", "POST", "/v1/auth/email", `{"email":" Sam@Example.com"}`)
	})
	if want := DefaultRateLimits().AuthEmail.Burst; ok != want {
		t.Errorf("%d links went to one address, want the burst of %d", ok, want)
	}
	check429(t, rec, httptest.NewRequest("POST", "/v1/auth/email", strings.NewReader(`{"email":"sam@example.com"}`)))
	if rec := from(h, "198.51.100.200", "", "", "POST", "/v1/auth/email", `{"email":"kim@example.com"}`); rec.Code != 202 {
		t.Errorf("another address = %d", rec.Code)
	}
}

// Once a sweep frees room, new keys get their own buckets again: the
// overflow bucket an attacker drained is not shared with later clients.
func TestLimiterOverflowRetiredAfterSweep(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	l := newLimiter(Limit{PerMinute: 1, Burst: 2}, 10)
	// Fill the table with keys still limited, then drain the overflow.
	for i := range 10 {
		l.allow("flood"+strconv.Itoa(i), now)
		l.allow("flood"+strconv.Itoa(i), now)
	}
	for i := range 20 {
		l.allow("spill"+strconv.Itoa(i), now)
	}
	if ok, _ := l.allow("legit-early", now); ok {
		t.Fatal("overflow not shared while the table is full")
	}
	// Half the flood keys are still drained; the rest refill and are swept.
	later := now.Add(2 * time.Minute)
	for i := range 5 {
		l.allow("flood"+strconv.Itoa(i), later)
		l.allow("flood"+strconv.Itoa(i), later)
	}
	later = later.Add(sweepEvery)
	for i := range 4 {
		key := "legit" + strconv.Itoa(i)
		for j := range 2 {
			if ok, _ := l.allow(key, later); !ok {
				t.Fatalf("%s request %d limited after the table had room", key, j)
			}
		}
	}
	for i := range 5 {
		if ok, _ := l.allow("flood"+strconv.Itoa(i), later); ok {
			t.Fatalf("flood%d refilled by the sweep", i)
		}
	}
}
