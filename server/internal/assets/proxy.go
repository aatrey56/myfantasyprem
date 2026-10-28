// Package assets serves official club crests, kit (shirt) images and
// player photos behind the branded-assets switch (BRANDED_ASSETS, AGENTS.md
// hard rules). The app never hotlinks upstream: it asks /v1/assets/*, the
// server maps our ids to the upstream codes (Resolver), fetches each image
// once (Proxy) and serves it from a disk cache afterwards.
//
// Upstream is treated politely: one request per MinInterval, at most
// MaxConcurrent at a time, a clear User-Agent and a timeout. An image
// upstream does not have is remembered as a miss for MissTTL; other
// failures back off for FailureBackoff. Failures are logged at debug only:
// a missing photo is routine, and the app falls back to its generic shirt.
//
// Switched off (Service.Enabled false), nothing is resolved or fetched and
// every image is ErrNotFound. Tests pass a fake Transport; nothing here is
// ever pointed at the real upstream in tests.
package assets

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// Kind is one family of images.
type Kind string

// The kinds of image. Each has its own upstream URL templates and its own
// cache directory.
const (
	PlayerPhoto Kind = "player-photo"
	ClubCrest   Kind = "club-crest"
	ClubShirt   Kind = "club-shirt"
	ClubGKShirt Kind = "club-shirt-gk"
)

// Kinds lists every Kind.
var Kinds = []Kind{PlayerPhoto, ClubCrest, ClubShirt, ClubGKShirt}

// PlayerPhotoSeasons are the upstream per-season photo prefixes, newest
// first. Each season's headshots live under their own prefix; a new season
// is one line at the top (e.g. "premierleague26" for 2026-27), and a
// player without a photo there falls back to the previous season.
var PlayerPhotoSeasons = []string{
	"premierleague25",
}

// legacyPlayerPhotoURL is the old, unversioned archive. It answers for
// most players but with headshots that can be seasons out of date (a
// player shown in a former club's shirt), so it is only the last resort.
const legacyPlayerPhotoURL = "https://resources.premierleague.com/premierleague/photos/players/110x140/p{code}.png"

// PlayerPhotoURLs returns the photo templates for seasons (newest first),
// then the legacy archive.
func PlayerPhotoURLs(seasons []string) []string {
	urls := make([]string, 0, len(seasons)+1)
	for _, s := range seasons {
		urls = append(urls, "https://resources.premierleague.com/"+s+"/photos/players/110x140/{code}.png")
	}
	return append(urls, legacyPlayerPhotoURL)
}

// The default upstream URL templates, tried in order; {code} is the FPL
// element code (player photos) or team code (crests, shirts), the
// permanent ids bootstrap-static carries as `code` (docs/adr/0006).
var (
	DefaultPlayerPhotoURLs = PlayerPhotoURLs(PlayerPhotoSeasons)
	DefaultClubCrestURLs   = []string{
		"https://resources.premierleague.com/premierleague/badges/70/t{code}.png",
		"https://resources.premierleague.com/premierleague/badges/t{code}.png",
	}
	DefaultClubShirtURLs = []string{
		"https://fantasy.premierleague.com/dist/img/shirts/standard/shirt_{code}-110.png",
		"https://draft.premierleague.com/img/shirts/standard/shirt_{code}-110.png",
	}
	DefaultClubGKShirtURLs = []string{
		"https://fantasy.premierleague.com/dist/img/shirts/standard/shirt_{code}_1-110.png",
		"https://draft.premierleague.com/img/shirts/standard/shirt_{code}_1-110.png",
	}
)

// ErrNotFound means there is no image to serve: unknown id, switched off,
// upstream does not have it, or upstream failed.
var ErrNotFound = errors.New("assets: image not found")

// maxImage caps one upstream image (photos are tens of KB).
const maxImage = 2 << 20

// Options configures a Proxy. Zero values get the defaults.
type Options struct {
	// Dir is the cache directory. Default: myfantasyprem-assets under the
	// OS temp dir.
	Dir string
	// URLs are the upstream URL templates per kind; missing kinds use the
	// Default*URLs.
	URLs map[Kind][]string
	// UserAgent names the app (the FPL client's, FPL_USER_AGENT).
	UserAgent string
	// Transport is the HTTP transport; tests pass a fake upstream.
	Transport http.RoundTripper
	// Timeout bounds one upstream request. Default 10s.
	Timeout time.Duration
	// MinInterval spaces upstream requests out. Default 250ms.
	MinInterval time.Duration
	// MaxConcurrent caps upstream requests in flight. Default 2.
	MaxConcurrent int
	// MissTTL is how long an image upstream does not have stays a miss.
	// Default 24h.
	MissTTL time.Duration
	// FailureBackoff is how long an image whose fetch failed (network,
	// 5xx, not an image) is not retried. Default 10m.
	FailureBackoff time.Duration
	// RefreshAfter is how old a cached image may be before it is fetched
	// again (a player's new headshot, a club's new shirt). The cached copy
	// is served meanwhile and kept if upstream fails. Default 7 days.
	RefreshAfter time.Duration
	Logger       *slog.Logger
	// Now is the clock; tests replace it.
	Now func() time.Time
}

// Image is one cached image.
type Image struct {
	Body        []byte
	ContentType string
	// ETag is a strong validator of Body.
	ETag    string
	ModTime time.Time
}

// Proxy fetches images from upstream once and serves them from disk. Safe
// for concurrent use.
type Proxy struct {
	opts   Options
	client *http.Client
	sem    chan struct{}

	mu       sync.Mutex
	next     time.Time // the earliest the next upstream request may start
	inflight map[string]*call
	failed   map[string]time.Time // key -> retry after
	bg       sync.WaitGroup       // background refreshes, for tests
}

type call struct {
	done chan struct{}
	img  *Image
	err  error
}

// NewProxy returns a Proxy and creates its cache directory.
func NewProxy(o Options) (*Proxy, error) {
	if o.Dir == "" {
		o.Dir = filepath.Join(os.TempDir(), "myfantasyprem-assets")
	}
	urls := map[Kind][]string{
		PlayerPhoto: DefaultPlayerPhotoURLs, ClubCrest: DefaultClubCrestURLs,
		ClubShirt: DefaultClubShirtURLs, ClubGKShirt: DefaultClubGKShirtURLs,
	}
	for k, v := range o.URLs {
		if len(v) > 0 {
			urls[k] = v
		}
	}
	o.URLs = urls
	if o.UserAgent == "" {
		o.UserAgent = "MyFantasyPrem-assets/0.1"
	}
	if o.Timeout <= 0 {
		o.Timeout = 10 * time.Second
	}
	if o.MinInterval <= 0 {
		o.MinInterval = 250 * time.Millisecond
	}
	if o.MaxConcurrent <= 0 {
		o.MaxConcurrent = 2
	}
	if o.MissTTL <= 0 {
		o.MissTTL = 24 * time.Hour
	}
	if o.FailureBackoff <= 0 {
		o.FailureBackoff = 10 * time.Minute
	}
	if o.RefreshAfter <= 0 {
		o.RefreshAfter = 7 * 24 * time.Hour
	}
	if o.Logger == nil {
		o.Logger = slog.Default()
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	for _, k := range Kinds {
		if err := os.MkdirAll(filepath.Join(o.Dir, string(k)), 0o755); err != nil {
			return nil, fmt.Errorf("assets: cache dir: %w", err)
		}
	}
	transport := o.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Proxy{
		opts: o,
		client: &http.Client{Transport: transport, Timeout: o.Timeout,
			// Upstream CDNs redirect within themselves; never far.
			CheckRedirect: func(_ *http.Request, via []*http.Request) error {
				if len(via) >= 3 {
					return http.ErrUseLastResponse
				}
				return nil
			}},
		sem:      make(chan struct{}, o.MaxConcurrent),
		inflight: map[string]*call{},
		failed:   map[string]time.Time{},
	}, nil
}

// codePattern is an upstream code: FPL's are small positive integers.
var codePattern = regexp.MustCompile(`^[0-9]{1,10}$`)

// Get returns the image of kind for the upstream code: from the cache, or
// fetched once from upstream. Every failure is ErrNotFound (wrapped).
func (p *Proxy) Get(ctx context.Context, kind Kind, code string) (*Image, error) {
	if _, ok := p.opts.URLs[kind]; !ok || !codePattern.MatchString(code) {
		return nil, ErrNotFound
	}
	key := string(kind) + "/" + code
	if img, err := p.read(kind, code); err == nil {
		if p.opts.Now().Sub(img.ModTime) >= p.opts.RefreshAfter {
			p.refresh(ctx, kind, code)
		}
		return img, nil
	}
	if p.missed(kind, code) {
		return nil, ErrNotFound
	}
	p.mu.Lock()
	if until, ok := p.failed[key]; ok && p.opts.Now().Before(until) {
		p.mu.Unlock()
		return nil, ErrNotFound
	}
	if c, ok := p.inflight[key]; ok {
		p.mu.Unlock()
		select {
		case <-c.done:
			return c.img, c.err
		case <-ctx.Done():
			return nil, fmt.Errorf("%w: %w", ErrNotFound, ctx.Err())
		}
	}
	c := &call{done: make(chan struct{})}
	p.inflight[key] = c
	p.mu.Unlock()

	// The fetch outlives a client that hangs up: the next request gets it.
	c.img, c.err = p.fetch(context.WithoutCancel(ctx), kind, code, false)

	p.mu.Lock()
	delete(p.inflight, key)
	p.mu.Unlock()
	close(c.done)
	return c.img, c.err
}

// refresh fetches a stale cached image again in the background, once at a
// time per key and not within a failure backoff. The caller keeps serving
// the cached copy.
func (p *Proxy) refresh(ctx context.Context, kind Kind, code string) {
	key := string(kind) + "/" + code
	p.mu.Lock()
	if until, ok := p.failed[key]; ok && p.opts.Now().Before(until) {
		p.mu.Unlock()
		return
	}
	if _, ok := p.inflight[key]; ok {
		p.mu.Unlock()
		return
	}
	c := &call{done: make(chan struct{})}
	p.inflight[key] = c
	p.mu.Unlock()
	p.bg.Add(1)
	go func() {
		defer p.bg.Done()
		c.img, c.err = p.fetch(context.WithoutCancel(ctx), kind, code, true)
		p.mu.Lock()
		delete(p.inflight, key)
		p.mu.Unlock()
		close(c.done)
	}()
}

func (p *Proxy) path(kind Kind, code string) string {
	return filepath.Join(p.opts.Dir, string(kind), code)
}

// read returns the cached image, if any.
func (p *Proxy) read(kind Kind, code string) (*Image, error) {
	f := p.path(kind, code)
	body, err := os.ReadFile(f)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(f)
	if err != nil {
		return nil, err
	}
	ct, ok := imageType(body)
	if !ok {
		return nil, ErrNotFound
	}
	sum := sha256.Sum256(body)
	return &Image{Body: body, ContentType: ct, ETag: `"` + hex.EncodeToString(sum[:12]) + `"`, ModTime: st.ModTime()}, nil
}

// missed reports a recent "upstream does not have it".
func (p *Proxy) missed(kind Kind, code string) bool {
	st, err := os.Stat(p.path(kind, code) + ".miss")
	return err == nil && p.opts.Now().Sub(st.ModTime()) < p.opts.MissTTL
}

// fetch tries each URL template in turn and caches the first image. On a
// refresh (a cached copy exists), "upstream has none" keeps the cached copy
// and restarts its refresh age instead of recording a miss.
func (p *Proxy) fetch(ctx context.Context, kind Kind, code string, refresh bool) (*Image, error) {
	log := p.opts.Logger.With("component", "assets", "kind", string(kind), "code", code)
	for _, tmpl := range p.opts.URLs[kind] {
		body, status, err := p.get(ctx, strings.ReplaceAll(tmpl, "{code}", code))
		switch {
		case err != nil:
			log.Debug("upstream image failed", "err", err)
			return nil, p.fail(kind, code, err)
		case status == http.StatusNotFound || status == http.StatusForbidden || status == http.StatusGone:
			continue // not at this URL; try the next
		case status != http.StatusOK:
			log.Debug("upstream image failed", "status", status)
			return nil, p.fail(kind, code, fmt.Errorf("upstream status %d", status))
		}
		if _, ok := imageType(body); !ok {
			log.Debug("upstream answered something that is not an image")
			return nil, p.fail(kind, code, errors.New("not an image"))
		}
		if err := p.write(p.path(kind, code), body); err != nil {
			log.Warn("cannot cache an image", "err", err)
		} else {
			// The refresh age runs on the proxy's clock.
			now := p.opts.Now()
			_ = os.Chtimes(p.path(kind, code), now, now)
		}
		_ = os.Remove(p.path(kind, code) + ".miss")
		img, err := p.read(kind, code)
		if err != nil {
			// Not cached (a full disk): serve it anyway.
			sum := sha256.Sum256(body)
			ct, _ := imageType(body)
			img = &Image{Body: body, ContentType: ct, ETag: `"` + hex.EncodeToString(sum[:12]) + `"`, ModTime: p.opts.Now()}
		}
		return img, nil
	}
	log.Debug("upstream has no such image")
	if refresh {
		now := p.opts.Now()
		_ = os.Chtimes(p.path(kind, code), now, now)
		return nil, ErrNotFound
	}
	_ = p.write(p.path(kind, code)+".miss", nil)
	return nil, ErrNotFound
}

// fail backs the key off and returns ErrNotFound wrapping err.
func (p *Proxy) fail(kind Kind, code string, err error) error {
	p.mu.Lock()
	p.failed[string(kind)+"/"+code] = p.opts.Now().Add(p.opts.FailureBackoff)
	p.mu.Unlock()
	return fmt.Errorf("%w: %w", ErrNotFound, err)
}

// get makes one upstream request within the rate and concurrency limits.
func (p *Proxy) get(ctx context.Context, url string) ([]byte, int, error) {
	if err := p.wait(ctx); err != nil {
		return nil, 0, err
	}
	select {
	case p.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, 0, ctx.Err()
	}
	defer func() { <-p.sem }()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("User-Agent", p.opts.UserAgent)
	req.Header.Set("Accept", "image/png,image/jpeg,image/webp,image/*;q=0.8")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return nil, resp.StatusCode, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImage+1))
	if err != nil {
		return nil, 0, err
	}
	if len(body) > maxImage {
		return nil, 0, errors.New("image too large")
	}
	return body, resp.StatusCode, nil
}

// wait takes the next upstream request slot, MinInterval after the last.
func (p *Proxy) wait(ctx context.Context) error {
	p.mu.Lock()
	now := p.opts.Now()
	at := p.next
	if at.Before(now) {
		at = now
	}
	p.next = at.Add(p.opts.MinInterval)
	p.mu.Unlock()
	d := at.Sub(now)
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// write replaces file atomically.
func (p *Proxy) write(file string, body []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(file), ".tmp-*")
	if err != nil {
		return err
	}
	_, werr := io.Copy(tmp, bytes.NewReader(body))
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(tmp.Name())
		return errors.Join(werr, cerr)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// imageType sniffs a raster image type. Anything else (HTML error pages,
// SVG with scripts) is refused.
func imageType(body []byte) (string, bool) {
	switch ct := http.DetectContentType(body); ct {
	case "image/png", "image/jpeg", "image/webp", "image/gif":
		return ct, true
	}
	return "", false
}
