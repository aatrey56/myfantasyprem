package assets

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// tinyPNG is a 1x1 PNG.
func tinyPNG(t testing.TB) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 200, A: 255})
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fakeUpstream answers from a map of URL -> response; anything else is 404.
// It never touches the network.
type fakeUpstream struct {
	mu       sync.Mutex
	files    map[string][]byte
	status   map[string]int
	err      error
	calls    []string
	agents   []string
	active   atomic.Int32
	peak     atomic.Int32
	hold     chan struct{} // when set, every request waits for it
	inFlight chan struct{} // signalled as each request starts
}

func (f *fakeUpstream) RoundTrip(req *http.Request) (*http.Response, error) {
	n := f.active.Add(1)
	defer f.active.Add(-1)
	for {
		p := f.peak.Load()
		if n <= p || f.peak.CompareAndSwap(p, n) {
			break
		}
	}
	if f.inFlight != nil {
		f.inFlight <- struct{}{}
	}
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	url := req.URL.String()
	f.calls = append(f.calls, url)
	f.agents = append(f.agents, req.Header.Get("User-Agent"))
	body, ok := f.files[url]
	status, hasStatus := f.status[url]
	err := f.err
	f.mu.Unlock()
	if err != nil {
		return nil, err
	}
	switch {
	case hasStatus:
	case ok:
		status = http.StatusOK
	default:
		status = http.StatusNotFound
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(bytes.NewReader(body)), Header: http.Header{}, Request: req}, nil
}

func (f *fakeUpstream) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

var testURLs = map[Kind][]string{
	PlayerPhoto: {"https://up.test/a/p{code}.png", "https://up.test/b/{code}.png"},
	ClubCrest:   {"https://up.test/badges/t{code}.png"},
	ClubShirt:   {"https://up.test/shirts/shirt_{code}.png"},
	ClubGKShirt: {"https://up.test/shirts/shirt_{code}_1.png"},
}

func newProxy(t *testing.T, up *fakeUpstream, dir string) *Proxy {
	t.Helper()
	if dir == "" {
		dir = t.TempDir()
	}
	p, err := NewProxy(Options{Dir: dir, URLs: testURLs, Transport: up, UserAgent: "MFP-test/1.0", MinInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestProxyCachesOnDisk(t *testing.T) {
	pic := tinyPNG(t)
	up := &fakeUpstream{files: map[string][]byte{"https://up.test/a/p154561.png": pic}}
	dir := t.TempDir()
	p := newProxy(t, up, dir)
	ctx := context.Background()
	img, err := p.Get(ctx, PlayerPhoto, "154561")
	if err != nil || !bytes.Equal(img.Body, pic) || img.ContentType != "image/png" || img.ETag == "" {
		t.Fatalf("first get = %+v %v", img, err)
	}
	again, err := p.Get(ctx, PlayerPhoto, "154561")
	if err != nil || again.ETag != img.ETag {
		t.Fatalf("second get = %+v %v", again, err)
	}
	if up.count() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (the second request is served from the cache)", up.count())
	}
	if up.agents[0] != "MFP-test/1.0" {
		t.Errorf("user agent = %q", up.agents[0])
	}
	// A new process on the same directory never asks upstream.
	fresh := newProxy(t, up, dir)
	if _, err := fresh.Get(ctx, PlayerPhoto, "154561"); err != nil || up.count() != 1 {
		t.Fatalf("restart: %v, upstream calls %d", err, up.count())
	}
}

func TestProxyTriesTemplatesInOrder(t *testing.T) {
	up := &fakeUpstream{files: map[string][]byte{"https://up.test/b/7.png": tinyPNG(t)}}
	p := newProxy(t, up, "")
	if _, err := p.Get(context.Background(), PlayerPhoto, "7"); err != nil {
		t.Fatal(err)
	}
	want := []string{"https://up.test/a/p7.png", "https://up.test/b/7.png"}
	if strings.Join(up.calls, " ") != strings.Join(want, " ") {
		t.Fatalf("calls = %v, want %v", up.calls, want)
	}
}

func TestProxyKindsUseTheirTemplates(t *testing.T) {
	pic := tinyPNG(t)
	up := &fakeUpstream{files: map[string][]byte{
		"https://up.test/badges/t3.png":        pic,
		"https://up.test/shirts/shirt_3.png":   pic,
		"https://up.test/shirts/shirt_3_1.png": pic,
	}}
	p := newProxy(t, up, "")
	for _, k := range []Kind{ClubCrest, ClubShirt, ClubGKShirt} {
		if _, err := p.Get(context.Background(), k, "3"); err != nil {
			t.Errorf("%s: %v", k, err)
		}
	}
	if up.count() != 3 {
		t.Fatalf("calls = %v", up.calls)
	}
}

func TestProxyMissIsRemembered(t *testing.T) {
	up := &fakeUpstream{}
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	p := newProxy(t, up, "")
	p.opts.Now = func() time.Time { return now }
	for i := 0; i < 3; i++ {
		if _, err := p.Get(context.Background(), ClubCrest, "99"); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	}
	if up.count() != 1 {
		t.Fatalf("upstream calls = %d, want 1 (the miss is cached)", up.count())
	}
	// The miss file's age is real time; a clock past MissTTL asks again.
	now = time.Now().Add(25 * time.Hour)
	_, _ = p.Get(context.Background(), ClubCrest, "99")
	if up.count() != 2 {
		t.Fatalf("after MissTTL: upstream calls = %d, want 2", up.count())
	}
}

func TestProxyUpstreamErrorsAreNotFound(t *testing.T) {
	for name, up := range map[string]*fakeUpstream{
		"server error":   {status: map[string]int{"https://up.test/badges/t5.png": 503}},
		"network":        {err: errors.New("connection refused")},
		"not an image":   {files: map[string][]byte{"https://up.test/badges/t5.png": []byte("<html>oops</html>")}},
		"rate limited":   {status: map[string]int{"https://up.test/badges/t5.png": 429}},
		"svg is refused": {files: map[string][]byte{"https://up.test/badges/t5.png": []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script/></svg>`)}},
	} {
		t.Run(name, func(t *testing.T) {
			p := newProxy(t, up, "")
			if _, err := p.Get(context.Background(), ClubCrest, "5"); !errors.Is(err, ErrNotFound) {
				t.Fatalf("err = %v, want ErrNotFound", err)
			}
			// It backs off: no second upstream request straight away.
			calls := up.count()
			if _, err := p.Get(context.Background(), ClubCrest, "5"); !errors.Is(err, ErrNotFound) || up.count() != calls {
				t.Fatalf("retry: %v, calls %d -> %d", err, calls, up.count())
			}
		})
	}
}

func TestProxyRejectsBadCodes(t *testing.T) {
	up := &fakeUpstream{}
	p := newProxy(t, up, "")
	for _, code := range []string{"", "../etc/passwd", "12a", "12345678901", "-1"} {
		if _, err := p.Get(context.Background(), PlayerPhoto, code); !errors.Is(err, ErrNotFound) {
			t.Errorf("%q: %v", code, err)
		}
	}
	if _, err := p.Get(context.Background(), Kind("kit"), "1"); !errors.Is(err, ErrNotFound) {
		t.Error("unknown kind fetched")
	}
	if up.count() != 0 {
		t.Fatalf("upstream calls = %v", up.calls)
	}
}

// Concurrent requests for one image share one upstream request, and
// requests for different images respect MaxConcurrent.
func TestProxyConcurrency(t *testing.T) {
	pic := tinyPNG(t)
	files := map[string][]byte{}
	for _, c := range []string{"1", "2", "3", "4", "5", "6"} {
		files["https://up.test/a/p"+c+".png"] = pic
	}
	up := &fakeUpstream{files: files, hold: make(chan struct{}), inFlight: make(chan struct{}, 100)}
	p := newProxy(t, up, "")
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Get(context.Background(), PlayerPhoto, "1")
			errs <- err
		}()
	}
	<-up.inFlight // the shared request has started
	for _, c := range []string{"2", "3", "4", "5", "6"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := p.Get(context.Background(), PlayerPhoto, c)
			errs <- err
		}()
	}
	<-up.inFlight // a second request is in flight; the limit is 2
	time.Sleep(50 * time.Millisecond)
	if n := up.active.Load(); n > 2 {
		t.Fatalf("%d upstream requests in flight, want at most 2", n)
	}
	close(up.hold)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if up.count() != 6 || up.peak.Load() > 2 {
		t.Fatalf("upstream calls = %d (want 6: one per image), peak in flight %d", up.count(), up.peak.Load())
	}
}

// fakeCatalog is the stats id maps of an FPL world plus a demo player.
type fakeCatalog struct {
	loads atomic.Int32
	clubs []stats.Club
	ext   map[string]map[string]string // entity -> external -> ours
}

func (c *fakeCatalog) Clubs(context.Context) ([]stats.Club, error) { return c.clubs, nil }

func (c *fakeCatalog) ExternalIDs(_ context.Context, provider, entity string) (map[string]string, error) {
	if entity == "player" {
		c.loads.Add(1)
	}
	if provider != Provider {
		return map[string]string{}, nil
	}
	out := map[string]string{}
	for k, v := range c.ext[entity] {
		out[k] = v
	}
	return out, nil
}

const (
	raya  = "plr_01k7m2xq8v00000000000000a1"
	saka  = "plr_01k7m2xq8v00000000000000a2"
	ghost = "plr_01k7m2xq8v00000000000000zz"
)

func newCatalog() *fakeCatalog {
	return &fakeCatalog{
		clubs: []stats.Club{{ID: "clb_ars", Code: "ARS", Name: "Arsenal"}, {ID: "clb_nor", Code: "NOR", Name: "Northgate"}},
		ext: map[string]map[string]string{
			"player": {"154561": raya, "223340": saka},
			"club":   {"3": "clb_ars"},
		},
	}
}

func TestResolverMapsIDsToUpstreamCodes(t *testing.T) {
	ctx := context.Background()
	r := &Resolver{Catalog: newCatalog()}
	if code, err := r.PlayerCode(ctx, raya); err != nil || code != "154561" {
		t.Fatalf("raya = %q %v", code, err)
	}
	for _, short := range []string{"ARS", "ars"} {
		if code, err := r.ClubCode(ctx, short); err != nil || code != "3" {
			t.Fatalf("%s = %q %v", short, code, err)
		}
	}
	// A club with no FPL team code (the demo world) and unknown ids.
	for _, short := range []string{"NOR", "XYZ"} {
		if _, err := r.ClubCode(ctx, short); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s: %v", short, err)
		}
	}
	if _, err := r.PlayerCode(ctx, ghost); !errors.Is(err, ErrNotFound) {
		t.Errorf("ghost: %v", err)
	}
}

func TestResolverReloads(t *testing.T) {
	ctx := context.Background()
	cat := newCatalog()
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC)
	r := &Resolver{Catalog: cat, Now: func() time.Time { return now }}
	_, _ = r.PlayerCode(ctx, raya)
	_, _ = r.PlayerCode(ctx, ghost) // unknown, but the maps are fresh
	_, _ = r.PlayerCode(ctx, ghost)
	if cat.loads.Load() != 1 {
		t.Fatalf("loads = %d, want 1", cat.loads.Load())
	}
	// A new signing appears; an unknown id reloads after a minute.
	cat.ext["player"]["300000"] = ghost
	now = now.Add(2 * time.Minute)
	if code, err := r.PlayerCode(ctx, ghost); err != nil || code != "300000" {
		t.Fatalf("new signing = %q %v", code, err)
	}
	// Known ids keep the maps until TTL.
	now = now.Add(5 * time.Minute)
	_, _ = r.PlayerCode(ctx, raya)
	if cat.loads.Load() != 2 {
		t.Fatalf("loads = %d, want 2", cat.loads.Load())
	}
	now = now.Add(11 * time.Minute)
	_, _ = r.PlayerCode(ctx, raya)
	if cat.loads.Load() != 3 {
		t.Fatalf("loads after TTL = %d, want 3", cat.loads.Load())
	}
}

func TestServiceEndToEnd(t *testing.T) {
	ctx := context.Background()
	pic := tinyPNG(t)
	up := &fakeUpstream{files: map[string][]byte{
		"https://up.test/a/p154561.png":        pic,
		"https://up.test/badges/t3.png":        pic,
		"https://up.test/shirts/shirt_3.png":   pic,
		"https://up.test/shirts/shirt_3_1.png": pic,
	}}
	s := &Service{Enabled: true, Resolver: &Resolver{Catalog: newCatalog()}, Proxy: newProxy(t, up, "")}
	if _, err := s.PlayerPhoto(ctx, raya); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClubCrest(ctx, "ARS"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClubShirt(ctx, "ARS", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ClubShirt(ctx, "ARS", true); err != nil {
		t.Fatal(err)
	}
	want := "https://up.test/a/p154561.png https://up.test/badges/t3.png https://up.test/shirts/shirt_3.png https://up.test/shirts/shirt_3_1.png"
	if strings.Join(up.calls, " ") != want {
		t.Fatalf("calls = %v", up.calls)
	}
	// Saka has no photo upstream: 404s on both templates, then a miss.
	if _, err := s.PlayerPhoto(ctx, saka); !errors.Is(err, ErrNotFound) {
		t.Fatalf("saka: %v", err)
	}
	// Ids with no upstream code never reach upstream.
	n := up.count()
	if _, err := s.PlayerPhoto(ctx, ghost); !errors.Is(err, ErrNotFound) || up.count() != n {
		t.Fatalf("ghost: %v, calls %d -> %d", err, n, up.count())
	}
	if _, err := s.ClubCrest(ctx, "NOR"); !errors.Is(err, ErrNotFound) || up.count() != n {
		t.Fatalf("demo club: %v", err)
	}
}

// Switched off, nothing is resolved or fetched.
func TestServiceOff(t *testing.T) {
	ctx := context.Background()
	up := &fakeUpstream{files: map[string][]byte{"https://up.test/a/p154561.png": tinyPNG(t)}}
	cat := newCatalog()
	for name, s := range map[string]*Service{
		"disabled": {Enabled: false, Resolver: &Resolver{Catalog: cat}, Proxy: newProxy(t, up, "")},
		"nil":      nil,
	} {
		if s.On() {
			t.Errorf("%s: On", name)
		}
		if _, err := s.PlayerPhoto(ctx, raya); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s photo: %v", name, err)
		}
		if _, err := s.ClubCrest(ctx, "ARS"); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s crest: %v", name, err)
		}
		if _, err := s.ClubShirt(ctx, "ARS", true); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s shirt: %v", name, err)
		}
	}
	if up.count() != 0 || cat.loads.Load() != 0 {
		t.Fatalf("switched off: upstream calls %d, catalog loads %d", up.count(), cat.loads.Load())
	}
}

func TestDefaultPlayerPhotosPreferNewestSeason(t *testing.T) {
	got := PlayerPhotoURLs([]string{"premierleague26", "premierleague25"})
	want := []string{
		"https://resources.premierleague.com/premierleague26/photos/players/110x140/{code}.png",
		"https://resources.premierleague.com/premierleague25/photos/players/110x140/{code}.png",
		"https://resources.premierleague.com/premierleague/photos/players/110x140/p{code}.png",
	}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("urls = %v, want %v", got, want)
	}
	// The old archive answers with outdated headshots: it must be last, so
	// a player with a current-season photo never gets the archive one.
	last := DefaultPlayerPhotoURLs[len(DefaultPlayerPhotoURLs)-1]
	if !strings.Contains(last, "/premierleague/photos/") || strings.Contains(DefaultPlayerPhotoURLs[0], "/premierleague/photos/") {
		t.Fatalf("defaults = %v: the legacy archive must come last", DefaultPlayerPhotoURLs)
	}

	old, current := tinyPNG(t), append(tinyPNG(t), 0) // distinct bodies
	up := &fakeUpstream{files: map[string][]byte{
		"https://resources.premierleague.com/premierleague/photos/players/110x140/p116643.png":  old,
		"https://resources.premierleague.com/premierleague25/photos/players/110x140/116643.png": current,
	}}
	p, err := NewProxy(Options{Dir: t.TempDir(), Transport: up, MinInterval: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	img, err := p.Get(context.Background(), PlayerPhoto, "116643")
	if err != nil || !bytes.Equal(img.Body, current) {
		t.Fatalf("got the archive photo (or %v); want the current season's", err)
	}
	if up.count() != 1 {
		t.Fatalf("calls = %v, want only the current season", up.calls)
	}
}

func TestProxyRefreshesStaleHits(t *testing.T) {
	old, fresh := tinyPNG(t), append(tinyPNG(t), 1)
	url := "https://up.test/a/p42.png"
	up := &fakeUpstream{files: map[string][]byte{url: old}}
	now := time.Now()
	p := newProxy(t, up, "")
	p.opts.Now = func() time.Time { return now }
	ctx := context.Background()
	if _, err := p.Get(ctx, PlayerPhoto, "42"); err != nil {
		t.Fatal(err)
	}
	// Six days on: still fresh, no upstream call.
	now = now.Add(6 * 24 * time.Hour)
	if img, _ := p.Get(ctx, PlayerPhoto, "42"); !bytes.Equal(img.Body, old) || up.count() != 1 {
		t.Fatalf("within RefreshAfter: calls %d", up.count())
	}
	// Eight days on, upstream has a new photo: the stale copy is served
	// right away and replaced in the background.
	now = now.Add(2 * 24 * time.Hour)
	up.mu.Lock()
	up.files[url] = fresh
	up.mu.Unlock()
	img, err := p.Get(ctx, PlayerPhoto, "42")
	if err != nil || !bytes.Equal(img.Body, old) {
		t.Fatalf("stale hit = %v; want the cached copy served meanwhile", err)
	}
	p.bg.Wait()
	if img, _ := p.Get(ctx, PlayerPhoto, "42"); !bytes.Equal(img.Body, fresh) {
		t.Fatal("after refresh: still the old photo")
	}
	if up.count() != 2 {
		t.Fatalf("calls = %d, want 2", up.count())
	}

	// Upstream later drops the photo: the cached one stays and is not
	// asked for again until it is stale once more.
	now = now.Add(8 * 24 * time.Hour)
	up.mu.Lock()
	delete(up.files, url)
	up.mu.Unlock()
	_, _ = p.Get(ctx, PlayerPhoto, "42")
	p.bg.Wait()
	img, err = p.Get(ctx, PlayerPhoto, "42")
	p.bg.Wait()
	if err != nil || !bytes.Equal(img.Body, fresh) {
		t.Fatalf("after upstream 404: %v; want the cached copy kept", err)
	}
	if up.count() != 4 { // one refresh, both templates 404
		t.Fatalf("calls = %d, want 4 (one refresh, then fresh again)", up.count())
	}
}
