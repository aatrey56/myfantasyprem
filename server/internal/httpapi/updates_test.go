package httpapi

import (
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	otaRuntime = "fp-abc123"
	otaID      = "0b5c3d2e-1f4a-4b6c-8d7e-9fa0b1c2d3e4"
)

// newUpdatesAPI is the API with one published iOS update for otaRuntime.
func newUpdatesAPI(t *testing.T, opts ...func(*Options)) http.Handler {
	t.Helper()
	dir := t.TempDir()
	root := filepath.Join(dir, otaRuntime, otaID)
	upd, _ := json.Marshal(map[string]any{"id": otaID, "runtimeVersion": otaRuntime,
		"createdAt": time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)})
	for name, body := range map[string]string{
		"update.json":                     string(upd),
		"expoConfig.json":                 `{"name":"MyFantasyPrem"}`,
		"_expo/static/js/ios/entry-a.hbc": "bundle-bytes",
		"assets/9a8b7c":                   "font-bytes",
		"metadata.json": `{"fileMetadata":{"ios":{"bundle":"_expo/static/js/ios/entry-a.hbc",` +
			`"assets":[{"path":"assets/9a8b7c","ext":"ttf"}]}}}`,
	} {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "outside.txt"), []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := Options{UpdatesDir: dir}
	for _, f := range opts {
		f(&o)
	}
	return NewHandler(o)
}

func otaRequest(runtime, current string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "http://phone.ts.net:8090/updates/manifest", nil)
	r.Header.Set("expo-protocol-version", "1")
	r.Header.Set("expo-platform", "ios")
	r.Header.Set("expo-runtime-version", runtime)
	r.Header.Set("Accept", "multipart/mixed,application/expo+json,application/json")
	if current != "" {
		r.Header.Set("expo-current-update-id", current)
	}
	return r
}

// parts reads a multipart/mixed response into name -> JSON body.
func parts(t *testing.T, rec *httptest.ResponseRecorder) map[string]map[string]any {
	t.Helper()
	mt, params, err := mime.ParseMediaType(rec.Header().Get("Content-Type"))
	if err != nil || mt != "multipart/mixed" || params["boundary"] == "" {
		t.Fatalf("Content-Type = %q", rec.Header().Get("Content-Type"))
	}
	out := map[string]map[string]any{}
	mr := multipart.NewReader(rec.Body, params["boundary"])
	for {
		p, err := mr.NextPart()
		if err == io.EOF {
			return out
		}
		if err != nil {
			t.Fatal(err)
		}
		if ct := p.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Errorf("part %s Content-Type = %q", p.FormName(), ct)
		}
		var body map[string]any
		if err := json.NewDecoder(p).Decode(&body); err != nil {
			t.Fatal(err)
		}
		out[p.FormName()] = body
	}
}

func TestUpdatesManifest(t *testing.T) {
	h := newUpdatesAPI(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, otaRequest(otaRuntime, ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	for k, want := range map[string]string{
		"expo-protocol-version": "1", "expo-sfv-version": "0", "Cache-Control": "private, max-age=0",
	} {
		if got := rec.Header().Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	ps := parts(t, rec)
	m := ps["manifest"]
	if m == nil || ps["extensions"] == nil || ps["directive"] != nil {
		t.Fatalf("parts = %v", ps)
	}
	if m["id"] != otaID || m["runtimeVersion"] != otaRuntime || m["createdAt"] != "2026-10-17T09:00:00Z" {
		t.Errorf("manifest = %v", m)
	}
	launch := m["launchAsset"].(map[string]any)
	wantURL := "http://phone.ts.net:8090/updates/assets/" + otaRuntime + "/" + otaID + "/_expo/static/js/ios/entry-a.hbc"
	if launch["url"] != wantURL || launch["contentType"] != "application/javascript" {
		t.Errorf("launchAsset = %v", launch)
	}
	if extra := m["extra"].(map[string]any); extra["expoClient"] == nil {
		t.Errorf("extra = %v", extra)
	}

	// The launch asset's url serves the bundle.
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, wantURL, nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "bundle-bytes" ||
		rec.Header().Get("Content-Type") != "application/javascript" {
		t.Errorf("bundle: %d %q %q", rec.Code, rec.Header().Get("Content-Type"), rec.Body)
	}
	asset := m["assets"].([]any)[0].(map[string]any)
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, asset["url"].(string), nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "font-bytes" {
		t.Errorf("asset: %d %q", rec.Code, rec.Body)
	}
}

func TestUpdatesNoUpdateAvailable(t *testing.T) {
	h := newUpdatesAPI(t)
	for name, r := range map[string]*http.Request{
		"other runtime":  otaRequest("fp-other", ""),
		"already newest": otaRequest(otaRuntime, otaID),
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d", name, rec.Code)
		}
		ps := parts(t, rec)
		if ps["manifest"] != nil || ps["directive"]["type"] != "noUpdateAvailable" {
			t.Errorf("%s: parts = %v", name, ps)
		}
	}
	// Protocol 0 clients get a 404 instead.
	r := otaRequest("fp-other", "")
	r.Header.Del("expo-protocol-version")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusNotFound {
		t.Errorf("protocol 0: status %d", rec.Code)
	}
}

func TestUpdatesBadRequests(t *testing.T) {
	h := newUpdatesAPI(t)
	for name, mod := range map[string]func(*http.Request){
		"no platform":  func(r *http.Request) { r.Header.Del("expo-platform") },
		"web":          func(r *http.Request) { r.Header.Set("expo-platform", "web") },
		"no runtime":   func(r *http.Request) { r.Header.Del("expo-runtime-version") },
		"bad runtime":  func(r *http.Request) { r.Header.Set("expo-runtime-version", "../x") },
		"protocol two": func(r *http.Request) { r.Header.Set("expo-protocol-version", "2") },
	} {
		r := otaRequest(otaRuntime, "")
		mod(r)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d", name, rec.Code)
		}
	}
}

func TestUpdatesAssetPathTraversal(t *testing.T) {
	h := newUpdatesAPI(t)
	base := "http://h/updates/assets/" + otaRuntime + "/" + otaID + "/"
	for _, p := range []string{
		base + "update.json",
		base + "metadata.json",
		base + "../../outside.txt",
		base + "..%2F..%2Foutside.txt",
		base + "assets/..%2F..%2F..%2Foutside.txt",
		"http://h/updates/assets/..%2F/" + otaID + "/outside.txt",
		"http://h/updates/assets/" + otaRuntime + "/..%2F..%2F/outside.txt",
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
		if rec.Code == http.StatusOK || strings.Contains(rec.Body.String(), "secret") {
			t.Errorf("%s: status %d body %q", p, rec.Code, rec.Body)
		}
	}
}

func TestUpdatesOffWithoutDir(t *testing.T) {
	rec := httptest.NewRecorder()
	NewHandler(Options{}).ServeHTTP(rec, otaRequest(otaRuntime, ""))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status %d, want 404", rec.Code)
	}
}

// Behind a TLS-terminating proxy in production (forwarded headers not
// trusted), UPDATES_PUBLIC_URL gives the manifest's absolute asset urls:
// https on the public host, whatever host and scheme the request reached.
func TestUpdatesPublicURL(t *testing.T) {
	h := newUpdatesAPI(t, func(o *Options) {
		o.Production = true
		o.UpdatesPublicURL = "https://api.example.com"
	})
	rec := httptest.NewRecorder()
	r := otaRequest(otaRuntime, "")
	r.Header.Set("X-Forwarded-Proto", "http")
	h.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	m := parts(t, rec)["manifest"]
	launch := m["launchAsset"].(map[string]any)
	want := "https://api.example.com/updates/assets/" + otaRuntime + "/" + otaID + "/_expo/static/js/ios/entry-a.hbc"
	if launch["url"] != want {
		t.Errorf("launchAsset url = %v, want %s", launch["url"], want)
	}
	asset := m["assets"].([]any)[0].(map[string]any)
	if u, _ := asset["url"].(string); !strings.HasPrefix(u, "https://api.example.com/updates/assets/") {
		t.Errorf("asset url = %v", asset["url"])
	}
}
