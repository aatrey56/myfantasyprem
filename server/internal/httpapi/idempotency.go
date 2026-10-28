package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

const (
	idempotencyHeader = "Idempotency-Key"
	idempotencyTTL    = 24 * time.Hour
)

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

// credentialRoutes answer with a credential (a sign-in link's code), which
// must never be kept in the idempotency store: their Idempotency-Key is
// ignored, so a retry issues a new one (docs/adr/0013, "Sign-in links").
var credentialRoutes = map[string]bool{
	"POST /v1/leagues/{leagueId}/seats/{teamId}/signin-link": true,
}

// IdempotencyStore remembers the response to each (user, key) POST for 24
// hours. memoryIdempotency serves tests and the database-less server;
// PostgresIdempotency (table idempotency_keys) is shared by every API
// instance.
type IdempotencyStore interface {
	// Begin claims (user, key) for a request with this fingerprint, or
	// reports what is already stored.
	Begin(ctx context.Context, user, key, fingerprint string, now time.Time) (IdemClaim, error)
	// Finish stores the response of a claimed key.
	Finish(ctx context.Context, user, key string, status int, header http.Header, body []byte) error
	// Abort releases a claim whose response must not be replayed (5xx).
	Abort(ctx context.Context, user, key string) error
}

// IdemState is the outcome of Begin.
type IdemState int

const (
	IdemNew      IdemState = iota // claimed: run the request
	IdemMismatch                  // the key was used for another request
	IdemInFlight                  // the first request is still running
	IdemReplay                    // replay the stored response
)

// IdemClaim is Begin's answer; the response fields are set for IdemReplay.
type IdemClaim struct {
	State  IdemState
	Status int
	Header http.Header
	Body   []byte
}

// memoryIdempotency is the in-memory store.
type memoryIdempotency struct {
	mu      sync.Mutex
	entries map[string]*idemEntry
	// max caps the entries (memory); the oldest go first.
	max int
}

type idemEntry struct {
	fingerprint string // method, path and body hash
	createdAt   time.Time
	done        bool
	status      int
	header      http.Header
	body        []byte
}

// maxIdempotencyEntries caps the in-memory store (tests and the
// database-less dev server; Postgres has the prune job).
const maxIdempotencyEntries = 10_000

func newIdempotencyStore() *memoryIdempotency {
	return &memoryIdempotency{entries: map[string]*idemEntry{}, max: maxIdempotencyEntries}
}

func (m *memoryIdempotency) Begin(_ context.Context, user, key, fingerprint string, now time.Time) (IdemClaim, error) {
	id := user + "\x00" + key
	m.mu.Lock()
	defer m.mu.Unlock()
	e, ok := m.entries[id]
	if ok && now.Sub(e.createdAt) >= idempotencyTTL {
		delete(m.entries, id)
		ok = false
	}
	switch {
	case ok && e.fingerprint != fingerprint:
		return IdemClaim{State: IdemMismatch}, nil
	case ok && !e.done:
		return IdemClaim{State: IdemInFlight}, nil
	case ok:
		return IdemClaim{State: IdemReplay, Status: e.status, Header: e.header.Clone(), Body: bytes.Clone(e.body)}, nil
	}
	if m.max > 0 && len(m.entries) >= m.max {
		m.evictLocked(now)
	}
	m.entries[id] = &idemEntry{fingerprint: fingerprint, createdAt: now}
	return IdemClaim{State: IdemNew}, nil
}

// evictLocked drops expired entries and, if the store is still full, the
// oldest ones down to three quarters of max.
func (m *memoryIdempotency) evictLocked(now time.Time) {
	for id, e := range m.entries {
		if now.Sub(e.createdAt) >= idempotencyTTL {
			delete(m.entries, id)
		}
	}
	if len(m.entries) < m.max {
		return
	}
	ids := make([]string, 0, len(m.entries))
	for id := range m.entries {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b string) int { return m.entries[a].createdAt.Compare(m.entries[b].createdAt) })
	for _, id := range ids[:len(ids)-m.max*3/4] {
		delete(m.entries, id)
	}
}

func (m *memoryIdempotency) Finish(_ context.Context, user, key string, status int, header http.Header, body []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if e, ok := m.entries[user+"\x00"+key]; ok {
		e.done, e.status, e.header, e.body = true, status, header.Clone(), bytes.Clone(body)
	}
	return nil
}

func (m *memoryIdempotency) Abort(_ context.Context, user, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.entries, user+"\x00"+key)
	return nil
}

// captureWriter tees a response so it can be replayed.
type captureWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (c *captureWriter) WriteHeader(code int) {
	if c.status == 0 {
		c.status = code
	}
	c.ResponseWriter.WriteHeader(code)
}

func (c *captureWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	c.body.Write(b)
	return c.ResponseWriter.Write(b)
}

func (c *captureWriter) Unwrap() http.ResponseWriter { return c.ResponseWriter }

// idempotent makes authenticated POSTs with an Idempotency-Key safe to
// retry: the same key and request replays the stored response (with
// `Idempotent-Replayed: true`); the same key with a different request is a
// 422; a retry while the first is still running is a 409. 5xx responses
// are not stored, so they can be retried with the same key. Routes in
// credentialRoutes are never stored.
func idempotent(mux *http.ServeMux, store IdempotencyStore, now func() time.Time, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get(idempotencyHeader)
		user := userFrom(r.Context())
		if r.Method != http.MethodPost || key == "" || user == "" {
			next.ServeHTTP(w, r)
			return
		}
		if _, pattern := mux.Handler(r); credentialRoutes[pattern] {
			next.ServeHTTP(w, r)
			return
		}
		if !idempotencyKeyPattern.MatchString(key) {
			writeProblem(w, http.StatusBadRequest, "bad-request", "Bad request",
				"Idempotency-Key must be 8 to 128 characters of A-Z, a-z, 0-9, '.', '_', ':' or '-'.")
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			// Let the generated handler report the body error (413 or 400).
			r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(body), errReader{err}))
			next.ServeHTTP(w, r)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		sum := sha256.Sum256(body)
		fingerprint := r.Method + " " + r.URL.Path + " " + hex.EncodeToString(sum[:])

		ctx := r.Context()
		claim, err := store.Begin(ctx, user, key, fingerprint, now())
		if err != nil {
			if clientCanceled(r, err) {
				return
			}
			writeInternalError(w)
			return
		}
		switch claim.State {
		case IdemMismatch:
			writeProblem(w, http.StatusUnprocessableEntity, "idempotency-key-reused", "Idempotency key reused",
				"This Idempotency-Key was already used for a different request.")
			return
		case IdemInFlight:
			writeProblem(w, http.StatusConflict, "idempotency-key-in-flight", "Request in progress",
				"A request with this Idempotency-Key is still being processed. Retry shortly.")
			return
		case IdemReplay:
			// CORS headers belong to this request's origin (set by cors);
			// older stored responses may still carry the first one's.
			for k, v := range claim.Header {
				if !corsHeader(k) {
					w.Header()[k] = v
				}
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(claim.Status)
			_, _ = w.Write(claim.Body)
			return
		}

		cw := &captureWriter{ResponseWriter: w}
		defer func() {
			// The request may be over; keep the bookkeeping going.
			bg := context.WithoutCancel(ctx)
			if cw.status == 0 || cw.status >= 500 {
				_ = store.Abort(bg, user, key)
				return
			}
			header := w.Header().Clone()
			header.Del(requestIDHeader)
			for k := range header {
				if corsHeader(k) {
					header.Del(k)
				}
			}
			_ = store.Finish(bg, user, key, cw.status, header, cw.body.Bytes())
		}()
		next.ServeHTTP(cw, r)
	})
}

type errReader struct{ err error }

func (e errReader) Read([]byte) (int, error) { return 0, e.err }

// corsHeader reports a header the cors middleware sets per request
// (Access-Control-* and Vary), which a replay must not take from the
// stored response.
func corsHeader(name string) bool {
	k := http.CanonicalHeaderKey(name)
	return k == "Vary" || strings.HasPrefix(k, "Access-Control-")
}
