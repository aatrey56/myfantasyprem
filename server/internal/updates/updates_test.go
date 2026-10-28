package updates

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

const (
	runtime = "abc123"
	idOld   = "11111111-1111-4111-8111-111111111111"
	idNew   = "22222222-2222-4222-8222-222222222222"
)

// writeUpdate lays out an update as scripts/updates-publish.mjs does.
func writeUpdate(t *testing.T, dir, runtime, id string, created time.Time, bundle string) {
	t.Helper()
	root := filepath.Join(dir, runtime, id)
	files := map[string]string{
		"_expo/static/js/ios/entry-x.hbc": bundle,
		"assets/0f1e2d":                   "png bytes",
		"metadata.json": `{"version":0,"bundler":"metro","fileMetadata":{"ios":{` +
			`"bundle":"_expo/static/js/ios/entry-x.hbc","assets":[{"path":"assets/0f1e2d","ext":"png"}]}}}`,
		"expoConfig.json": `{"name":"MyFantasyPrem","slug":"myfantasyprem"}`,
	}
	u, _ := json.Marshal(map[string]any{"id": id, "createdAt": created, "runtimeVersion": runtime})
	files["update.json"] = string(u)
	for name, body := range files {
		p := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLatestPicksNewestForRuntime(t *testing.T) {
	dir := t.TempDir()
	t0 := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	writeUpdate(t, dir, runtime, idNew, t0.Add(time.Hour), "new")
	writeUpdate(t, dir, runtime, idOld, t0, "old")
	// Half-written: no update.json yet.
	if err := os.MkdirAll(filepath.Join(dir, runtime, "33333333-3333-4333-8333-333333333333"), 0o755); err != nil {
		t.Fatal(err)
	}
	s := Store{Dir: dir}
	u, err := s.Latest(runtime, "ios")
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	if u.ID != idNew {
		t.Fatalf("Latest = %s, want %s", u.ID, idNew)
	}
	for _, c := range []struct{ runtime, platform string }{
		{"other", "ios"}, {runtime, "android"}, {"../" + runtime, "ios"}, {"..", "ios"}, {runtime, "web"},
	} {
		if _, err := s.Latest(c.runtime, c.platform); !errors.Is(err, ErrNotFound) {
			t.Errorf("Latest(%q, %q) err = %v, want ErrNotFound", c.runtime, c.platform, err)
		}
	}
}

func TestManifest(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	writeUpdate(t, dir, runtime, idOld, created, "bundle")
	u, err := Store{Dir: dir}.Open(runtime, idOld)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	m, err := u.Manifest("ios", "http://h:8090/updates/assets/")
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("bundle"))
	want := Asset{
		Hash:        base64.RawURLEncoding.EncodeToString(sum[:]),
		ContentType: "application/javascript", FileExtension: ".bundle",
		URL: "http://h:8090/updates/assets/abc123/" + idOld + "/_expo/static/js/ios/entry-x.hbc",
	}
	want.Key = m.LaunchAsset.Key // md5 of the file; any stable string works for the client
	if m.LaunchAsset != want {
		t.Errorf("launchAsset = %+v, want %+v", m.LaunchAsset, want)
	}
	if len(m.LaunchAsset.Key) != 32 {
		t.Errorf("key %q is not an md5 hex", m.LaunchAsset.Key)
	}
	if m.ID != idOld || m.RuntimeVersion != runtime || m.CreatedAt != "2026-10-17T09:00:00Z" {
		t.Errorf("manifest head = %s %s %s", m.ID, m.RuntimeVersion, m.CreatedAt)
	}
	if len(m.Assets) != 1 || m.Assets[0].ContentType != "image/png" || m.Assets[0].FileExtension != ".png" {
		t.Errorf("assets = %+v", m.Assets)
	}
	if _, ok := m.Extra["expoClient"]; !ok {
		t.Error("extra.expoClient missing")
	}
	if _, err := u.Manifest("android", "http://h"); !errors.Is(err, ErrNotFound) {
		t.Errorf("android manifest err = %v", err)
	}
}

func TestFileServesOnlyListedFiles(t *testing.T) {
	dir := t.TempDir()
	writeUpdate(t, dir, runtime, idOld, time.Now(), "bundle")
	if err := os.WriteFile(filepath.Join(dir, "secret"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := Store{Dir: dir}.Open(runtime, idOld)
	if err != nil {
		t.Fatal(err)
	}
	defer u.Close()
	f, ct, err := u.File("assets/0f1e2d")
	if err != nil || ct != "image/png" {
		t.Fatalf("File(asset) = %v, %q", err, ct)
	}
	f.Close()
	for _, p := range []string{"update.json", "metadata.json", "../../secret", "assets/../update.json", "/etc/passwd"} {
		if _, _, err := u.File(p); !errors.Is(err, ErrNotFound) {
			t.Errorf("File(%q) err = %v, want ErrNotFound", p, err)
		}
	}
}

func TestOpenRejectsBadIDsAndMismatchedUpdateJSON(t *testing.T) {
	dir := t.TempDir()
	writeUpdate(t, dir, runtime, idOld, time.Now(), "bundle")
	s := Store{Dir: dir}
	for _, id := range []string{"..", "../" + idOld, "not-a-uuid", ""} {
		if _, err := s.Open(runtime, id); !errors.Is(err, ErrNotFound) {
			t.Errorf("Open(%q) err = %v", id, err)
		}
	}
	// A copied folder whose update.json names another update is refused.
	if err := os.Rename(filepath.Join(dir, runtime, idOld), filepath.Join(dir, runtime, idNew)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(runtime, idNew); err == nil {
		t.Error("Open accepted an update.json naming another id")
	}
}
