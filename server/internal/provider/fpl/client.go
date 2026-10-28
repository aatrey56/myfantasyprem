// Package fpl implements provider.StatsSource on FPL Draft's public API
// (https://draft.premierleague.com/api, RESEARCH.md §6): bootstrap-static
// (players, clubs, gameweeks, fixtures, settings), event/{gw}/live, game
// and pl/event-status; and provider.LeagueMirrorSource on its public
// league endpoints (league.go, docs/adr/0012). It never needs a login.
//
// The client is polite and defensive (RESEARCH.md §9.4, ADR 0005):
//
//   - a clear User-Agent, at most one request per MinInterval (2 per
//     second by default), a timeout per attempt;
//   - retries with exponential backoff and full jitter on network errors,
//     429 and 5xx, honouring Retry-After; other 4xx are not retried;
//   - conditional requests (If-None-Match / If-Modified-Since) when FPL
//     sends ETag or Last-Modified, answering a 304 from the previous body;
//   - every response is decoded strictly and validated (shape and internal
//     references) before it is mapped; a bad one is an error wrapping
//     provider.ErrUpstream and provider.ErrInvalid, so the caller keeps its
//     last good snapshot.
//
// FPL's per-season ids (element, team and fixture ids) never leave this
// package except as SeasonElementID/SeasonTeamID/SeasonFixtureID; external
// ids are the permanent `code` values (ADR 0006).
//
// Tests and the demo data never touch FPL: they pass a DirTransport over
// recorded JSON files.
package fpl

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// DefaultBaseURL is FPL Draft's public API.
const DefaultBaseURL = "https://draft.premierleague.com/api"

// DefaultUserAgent names the app and where to reach its maintainers.
const DefaultUserAgent = "MyFantasyPrem-stats/0.1 (+https://github.com/aatrey56/myfantasyprem)"

// maxBody caps a response (bootstrap-static is a few MB).
const maxBody = 32 << 20

// Options configures a Client. Zero values get the defaults.
type Options struct {
	BaseURL   string
	UserAgent string
	// Transport is the HTTP transport; tests and the demo pass a DirTransport.
	Transport http.RoundTripper
	// Timeout bounds one attempt. Default 15s.
	Timeout time.Duration
	// MaxAttempts per request, first try included. Default 4.
	MaxAttempts int
	// BaseBackoff and MaxBackoff shape the exponential backoff. Defaults
	// 500ms and 15s.
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	// MinInterval spaces requests out. Default 500ms (2 requests per second).
	MinInterval time.Duration
	Logger      *slog.Logger
	// Sleep waits for d or until ctx ends; tests replace it.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now is the clock; tests replace it.
	Now func() time.Time
}

// Client is a provider.StatsSource on FPL Draft. Safe for concurrent use.
type Client struct {
	opts Options
	http *http.Client

	mu       sync.Mutex
	lastReq  time.Time
	cache    map[string]cached       // conditional-request validators and bodies, by path
	lastBoot *bootstrapIndex         // per-season id joins from the last good bootstrap
	leagues  map[string]*leagueIndex // per league, from its last good details
}

type cached struct {
	etag, lastModified string
	body               []byte
}

var _ provider.StatsSource = (*Client)(nil)

// New returns a Client.
func New(opts Options) *Client {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.UserAgent == "" {
		opts.UserAgent = DefaultUserAgent
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 15 * time.Second
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = 4
	}
	if opts.BaseBackoff <= 0 {
		opts.BaseBackoff = 500 * time.Millisecond
	}
	if opts.MaxBackoff <= 0 {
		opts.MaxBackoff = 15 * time.Second
	}
	if opts.MinInterval < 0 {
		opts.MinInterval = 0
	} else if opts.MinInterval == 0 {
		opts.MinInterval = 500 * time.Millisecond
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Sleep == nil {
		opts.Sleep = sleep
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	transport := opts.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Client{
		opts:  opts,
		http:  &http.Client{Transport: transport},
		cache: map[string]cached{},
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// statusError is a non-2xx answer.
type statusError struct {
	status     int
	retryAfter time.Duration
	body       []byte // the start of the body
}

func (e *statusError) Error() string { return "status " + strconv.Itoa(e.status) }

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests || code >= 500
}

// get fetches path (for example "/bootstrap-static") with retries and
// returns the body. A 404 is provider.ErrNotFound; other failures wrap
// provider.ErrUpstream.
func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	var lastErr error
	for attempt := 1; attempt <= c.opts.MaxAttempts; attempt++ {
		body, err := c.once(ctx, path)
		if err == nil {
			return body, nil
		}
		lastErr = err
		var se *statusError
		isStatus := errors.As(err, &se)
		if isStatus && se.status == http.StatusNotFound {
			// FPL may answer a gameweek's picks before its deadline with a
			// 404 saying so: the picks exist but are private.
			if bytes.Contains(se.body, []byte(noPicks)) {
				return nil, fmt.Errorf("fpl: GET %s: %w", path, provider.ErrNotYetAvailable)
			}
			return nil, fmt.Errorf("fpl: GET %s: %w", path, provider.ErrNotFound)
		}
		if ctx.Err() != nil || (isStatus && !retryableStatus(se.status)) || attempt == c.opts.MaxAttempts {
			break
		}
		wait := c.backoff(attempt)
		if isStatus && se.retryAfter > wait {
			wait = min(se.retryAfter, c.opts.MaxBackoff*4)
		}
		c.opts.Logger.WarnContext(ctx, "fpl request failed, retrying",
			"path", path, "attempt", attempt, "err", err, "wait", wait)
		if err := c.opts.Sleep(ctx, wait); err != nil {
			lastErr = err
			break
		}
	}
	return nil, fmt.Errorf("fpl: GET %s: %w: %w", path, provider.ErrUpstream, lastErr)
}

// backoff is exponential with full jitter: uniform in [0, min(max, base*2^n)].
func (c *Client) backoff(attempt int) time.Duration {
	ceiling := c.opts.BaseBackoff << (attempt - 1)
	if ceiling <= 0 || ceiling > c.opts.MaxBackoff {
		ceiling = c.opts.MaxBackoff
	}
	return time.Duration(rand.Int64N(int64(ceiling) + 1))
}

// pace waits until MinInterval has passed since the previous request.
func (c *Client) pace(ctx context.Context) error {
	c.mu.Lock()
	wait := c.lastReq.Add(c.opts.MinInterval).Sub(c.opts.Now())
	if wait < 0 {
		wait = 0
	}
	c.lastReq = c.opts.Now().Add(wait)
	c.mu.Unlock()
	if wait == 0 {
		return ctx.Err()
	}
	return c.opts.Sleep(ctx, wait)
}

func (c *Client) once(ctx context.Context, path string) ([]byte, error) {
	if err := c.pace(ctx); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.opts.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	req.Header.Set("Accept", "application/json")
	c.mu.Lock()
	prev, hasPrev := c.cache[path]
	c.mu.Unlock()
	if hasPrev {
		if prev.etag != "" {
			req.Header.Set("If-None-Match", prev.etag)
		}
		if prev.lastModified != "" {
			req.Header.Set("If-Modified-Since", prev.lastModified)
		}
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotModified && hasPrev {
		return prev.body, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		head, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil, &statusError{status: resp.StatusCode, retryAfter: retryAfter(resp.Header.Get("Retry-After"), c.opts.Now()), body: head}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxBody {
		return nil, fmt.Errorf("response larger than %d bytes", maxBody)
	}
	if etag, lm := resp.Header.Get("ETag"), resp.Header.Get("Last-Modified"); etag != "" || lm != "" {
		c.mu.Lock()
		c.cache[path] = cached{etag: etag, lastModified: lm, body: body}
		c.mu.Unlock()
	}
	return body, nil
}

// retryAfter parses a Retry-After header: seconds or an HTTP date.
func retryAfter(v string, now time.Time) time.Duration {
	if v == "" {
		return 0
	}
	if s, err := strconv.Atoi(v); err == nil && s >= 0 {
		return time.Duration(s) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(t.Sub(now), 0)
	}
	return 0
}

// invalid wraps a validation failure.
func invalid(what string, err error) error {
	return fmt.Errorf("fpl: %s: %w: %w: %w", what, provider.ErrUpstream, provider.ErrInvalid, err)
}

// forget drops the cached body for path, so a body that failed validation
// is fetched in full next time instead of being answered from a 304.
func (c *Client) forget(path string) {
	c.mu.Lock()
	delete(c.cache, path)
	c.mu.Unlock()
}

// Bootstrap implements provider.StatsSource.
func (c *Client) Bootstrap(ctx context.Context) (*provider.Bootstrap, error) {
	b, _, err := c.bootstrap(ctx)
	return b, err
}

func (c *Client) bootstrap(ctx context.Context) (*provider.Bootstrap, *bootstrapIndex, error) {
	const path = "/bootstrap-static"
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	b, idx, err := decodeBootstrap(body)
	if err != nil {
		c.forget(path)
		return nil, nil, invalid("bootstrap-static", err)
	}
	c.mu.Lock()
	c.lastBoot = idx
	c.mu.Unlock()
	return b, idx, nil
}

// index returns the per-season id joins, fetching bootstrap if needed.
func (c *Client) index(ctx context.Context) (*bootstrapIndex, error) {
	c.mu.Lock()
	idx := c.lastBoot
	c.mu.Unlock()
	if idx != nil {
		return idx, nil
	}
	_, idx, err := c.bootstrap(ctx)
	return idx, err
}

// Fixtures implements provider.StatsSource from bootstrap-static's fixtures.
func (c *Client) Fixtures(ctx context.Context) ([]provider.Fixture, error) {
	_, idx, err := c.bootstrap(ctx)
	if err != nil {
		return nil, err
	}
	return idx.fixtures, nil
}

// GameweekStatus implements provider.StatsSource from /game and
// /pl/event-status; DataChecked is /game's current_event_finished or the
// last bootstrap's event finished (Draft has no data_checked).
func (c *Client) GameweekStatus(ctx context.Context) (*provider.GameweekStatus, error) {
	body, err := c.get(ctx, "/game")
	if err != nil {
		return nil, err
	}
	g, err := decodeGame(body)
	if err != nil {
		c.forget("/game")
		return nil, invalid("game", err)
	}
	body, err = c.get(ctx, "/pl/event-status")
	if err != nil {
		return nil, err
	}
	es, err := decodeEventStatus(body)
	if err != nil {
		c.forget("/pl/event-status")
		return nil, invalid("event-status", err)
	}
	idx, err := c.index(ctx)
	if err != nil {
		return nil, err
	}
	return gameweekStatus(g, es, idx), nil
}

// EventLive implements provider.StatsSource.
func (c *Client) EventLive(ctx context.Context, gameweek int) (*provider.EventLive, error) {
	if gameweek < 1 || gameweek > 38 {
		return nil, fmt.Errorf("fpl: gameweek %d: %w", gameweek, provider.ErrNotFound)
	}
	idx, err := c.index(ctx)
	if err != nil {
		return nil, err
	}
	path := "/event/" + strconv.Itoa(gameweek) + "/live"
	body, err := c.get(ctx, path)
	if err != nil {
		return nil, err
	}
	live, err := decodeLive(body, gameweek, idx)
	if err != nil {
		// The live data may reference a player or fixture added since the
		// cached bootstrap: refresh it once and try again.
		if _, idx2, berr := c.bootstrap(ctx); berr == nil {
			live, err = decodeLive(body, gameweek, idx2)
		}
	}
	if err != nil {
		c.forget(path)
		return nil, invalid("event live", err)
	}
	live.FetchedAt = c.opts.Now().UTC()
	return live, nil
}
