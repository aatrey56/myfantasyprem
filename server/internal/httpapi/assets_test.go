package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/assets"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// fakeImages is a fake upstream: a 1x1 PNG for every URL, or status for
// every URL when set. It never touches the network.
type fakeImages struct {
	mu     sync.Mutex
	urls   []string
	status int
}

func (f *fakeImages) RoundTrip(req *http.Request) (*http.Response, error) {
	f.mu.Lock()
	f.urls = append(f.urls, req.URL.String())
	status := f.status
	f.mu.Unlock()
	if status == 0 {
		status = http.StatusOK
	}
	var body bytes.Buffer
	if status == http.StatusOK {
		_ = png.Encode(&body, image.NewGray(image.Rect(0, 0, 1, 1)))
	}
	return &http.Response{StatusCode: status, Body: io.NopCloser(&body), Header: http.Header{}, Request: req}, nil
}

func (f *fakeImages) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.urls...)
}

// newTestAssets is an assets service on catalog with a fake upstream.
func newTestAssets(t *testing.T, catalog assets.Catalog, enabled bool) (*assets.Service, *fakeImages) {
	t.Helper()
	up := &fakeImages{}
	proxy, err := assets.NewProxy(assets.Options{
		Dir: t.TempDir(), Transport: up, MinInterval: time.Microsecond,
		URLs: map[assets.Kind][]string{
			assets.PlayerPhoto: {"https://img.test/photos/p{code}.png"},
			assets.ClubCrest:   {"https://img.test/badges/t{code}.png"},
			assets.ClubShirt:   {"https://img.test/shirts/shirt_{code}.png"},
			assets.ClubGKShirt: {"https://img.test/shirts/shirt_{code}_1.png"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &assets.Service{Enabled: enabled, Resolver: &assets.Resolver{Catalog: catalog}, Proxy: proxy}, up
}

// assetsServer is a test server with one FPL-mapped player and club.
func assetsServer(t *testing.T, enabled bool) (http.Handler, *fakeImages, string, string) {
	t.Helper()
	srv, _ := newTestServer()
	ctx := context.Background()
	if err := srv.stats.Apply(ctx, stats.Batch{
		Provider: mirror.Provider,
		Clubs:    []stats.Club{{ID: "clb_ars", Code: "ARS", Name: "Arsenal"}},
		Players:  []stats.Player{{ID: pid(1), DisplayName: "Raya", Position: "GKP", ClubID: "clb_ars", Availability: "available", DraftRank: 1}},
		Mappings: []stats.Mapping{{Entity: "player", ExternalID: "154561", ID: pid(1)}, {Entity: "club", ExternalID: "3", ID: "clb_ars"}},
	}); err != nil {
		t.Fatal(err)
	}
	var up *fakeImages
	srv.assets, up = newTestAssets(t, srv.stats, enabled)
	opts := testOptions()
	return newHandler(opts, newRoutes(opts, srv)), up, pid(1), "ARS"
}

func get(h http.Handler, target string, headers ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", target, nil)
	for i := 0; i+1 < len(headers); i += 2 {
		req.Header.Set(headers[i], headers[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAssetsServeAndCache(t *testing.T) {
	h, up, player, club := assetsServer(t, true)
	targets := []string{
		"/v1/assets/players/" + player + "/photo",
		"/v1/assets/clubs/" + club + "/crest",
		"/v1/assets/clubs/" + strings.ToLower(club) + "/shirt",
		"/v1/assets/clubs/" + club + "/shirt?gk=true",
	}
	for _, target := range targets {
		rec := get(h, target)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" ||
			rec.Header().Get("Cache-Control") != imageCacheControl || rec.Header().Get("ETag") == "" {
			t.Fatalf("%s: %d %v", target, rec.Code, rec.Header())
		}
		// The second request is served from the cache.
		again := get(h, target)
		if again.Code != 200 || !bytes.Equal(again.Body.Bytes(), rec.Body.Bytes()) {
			t.Fatalf("%s again: %d", target, again.Code)
		}
		if nm := get(h, target, "If-None-Match", rec.Header().Get("ETag")); nm.Code != 304 || nm.Body.Len() != 0 {
			t.Fatalf("%s If-None-Match: %d", target, nm.Code)
		}
	}
	want := []string{
		"https://img.test/photos/p154561.png",
		"https://img.test/badges/t3.png",
		"https://img.test/shirts/shirt_3.png",
		"https://img.test/shirts/shirt_3_1.png",
	}
	if got := up.calls(); strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("upstream calls = %v, want %v", got, want)
	}
	// Public: no bearer token needed.
	if rec := get(h, "/v1/config"); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"brandedAssets":true`) {
		t.Fatalf("config: %d %s", rec.Code, rec.Body)
	}
}

// BRANDED_ASSETS=false: every image is 404 and upstream is never asked.
func TestAssetsSwitchedOff(t *testing.T) {
	h, up, player, club := assetsServer(t, false)
	for _, target := range []string{
		"/v1/assets/players/" + player + "/photo",
		"/v1/assets/clubs/" + club + "/crest",
		"/v1/assets/clubs/" + club + "/shirt",
		"/v1/assets/clubs/" + club + "/shirt?gk=true",
	} {
		rec := get(h, target)
		if rec.Code != 404 || rec.Header().Get("Content-Type") != "application/problem+json" {
			t.Fatalf("%s: %d %s", target, rec.Code, rec.Body)
		}
	}
	if calls := up.calls(); len(calls) != 0 {
		t.Fatalf("upstream called while switched off: %v", calls)
	}
	rec := get(h, "/v1/config")
	var body AppConfigResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != 200 || body.Data.BrandedAssets {
		t.Fatalf("config: %d %s %v", rec.Code, rec.Body, err)
	}
	// No service at all is the same.
	srv, _ := newTestServer()
	opts := testOptions()
	h = newHandler(opts, newRoutes(opts, srv))
	if rec := get(h, "/v1/assets/clubs/ARS/crest"); rec.Code != 404 {
		t.Fatalf("no service: %d", rec.Code)
	}
}

func TestAssetsUpstreamErrorIsNotFound(t *testing.T) {
	h, up, player, club := assetsServer(t, true)
	for _, status := range []int{500, 404, 403} {
		up.mu.Lock()
		up.status = status
		up.mu.Unlock()
		for _, target := range []string{"/v1/assets/players/" + player + "/photo", "/v1/assets/clubs/" + club + "/crest"} {
			if rec := get(h, target); rec.Code != 404 {
				t.Fatalf("upstream %d, %s: %d", status, target, rec.Code)
			}
		}
	}
}
