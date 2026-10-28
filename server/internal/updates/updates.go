// Package updates reads over-the-air JavaScript updates for the native app
// from a directory and builds Expo Updates protocol manifests from them
// (https://docs.expo.dev/technical-specs/expo-updates-1/). The HTTP side
// (headers, the multipart body, serving files) is httpapi's updates.go.
//
// `npm run updates:publish` (scripts/updates-publish.mjs) writes the
// directory, one folder per update:
//
//	<dir>/<runtimeVersion>/<updateId>/update.json   {id, createdAt, runtimeVersion}
//	<dir>/<runtimeVersion>/<updateId>/metadata.json from `expo export`
//	<dir>/<runtimeVersion>/<updateId>/expoConfig.json the public app config
//	<dir>/<runtimeVersion>/<updateId>/_expo/..., assets/... the exported files
//
// An update reaches only apps whose runtime version (the native
// fingerprint) is the folder's name, so JavaScript never lands on a binary
// it was not built for. Code signing is not used.
package updates

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrNotFound is an unknown runtime version, update or file.
var ErrNotFound = errors.New("updates: not found")

var (
	runtimeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)
	idRe      = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
)

// ValidRuntime reports whether v can name a runtime version folder.
func ValidRuntime(v string) bool { return runtimeRe.MatchString(v) && !strings.Contains(v, "..") }

// ValidID reports whether v is an update id (a lowercase UUID).
func ValidID(v string) bool { return idRe.MatchString(v) }

// ValidPlatform reports whether p is a platform the app exports.
func ValidPlatform(p string) bool { return p == "ios" || p == "android" }

// Store reads updates from Dir.
type Store struct{ Dir string }

// Update is one published update.
type Update struct {
	ID             string    `json:"id"`
	CreatedAt      time.Time `json:"createdAt"`
	RuntimeVersion string    `json:"runtimeVersion"`

	root       *os.Root
	meta       exportMetadata
	expoConfig json.RawMessage
}

// Close releases the update's folder.
func (u *Update) Close() error { return u.root.Close() }

// exportMetadata is `expo export`'s metadata.json.
type exportMetadata struct {
	FileMetadata map[string]struct {
		Bundle string `json:"bundle"`
		Assets []struct {
			Path string `json:"path"`
			Ext  string `json:"ext"`
		} `json:"assets"`
	} `json:"fileMetadata"`
}

// Latest is the newest update for runtime that has a bundle for platform,
// or ErrNotFound. Folders without a readable update.json (one being
// written) are skipped.
func (s Store) Latest(runtime, platform string) (*Update, error) {
	if !ValidRuntime(runtime) || !ValidPlatform(platform) {
		return nil, ErrNotFound
	}
	entries, err := os.ReadDir(filepath.Join(s.Dir, runtime))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	var best *Update
	for _, e := range entries {
		if !e.IsDir() || !ValidID(e.Name()) {
			continue
		}
		u, err := s.Open(runtime, e.Name())
		if err != nil {
			continue
		}
		if _, ok := u.meta.FileMetadata[platform]; !ok || (best != nil && !u.CreatedAt.After(best.CreatedAt)) {
			u.Close()
			continue
		}
		if best != nil {
			best.Close()
		}
		best = u
	}
	if best == nil {
		return nil, ErrNotFound
	}
	return best, nil
}

// Open reads one update. The caller closes it.
func (s Store) Open(runtime, id string) (*Update, error) {
	if !ValidRuntime(runtime) || !ValidID(id) {
		return nil, ErrNotFound
	}
	root, err := os.OpenRoot(filepath.Join(s.Dir, runtime, id))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	u := &Update{root: root}
	if err := readJSON(root, "update.json", u); err != nil {
		root.Close()
		return nil, err
	}
	if u.ID != id || u.RuntimeVersion != runtime {
		root.Close()
		return nil, fmt.Errorf("updates: %s/%s: update.json names %s/%s", runtime, id, u.RuntimeVersion, u.ID)
	}
	if err := readJSON(root, "metadata.json", &u.meta); err != nil {
		root.Close()
		return nil, err
	}
	if b, err := root.ReadFile("expoConfig.json"); err == nil && json.Valid(b) {
		u.expoConfig = b
	}
	return u, nil
}

func readJSON(root *os.Root, name string, v any) error {
	b, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Manifest is the protocol's update manifest.
type Manifest struct {
	ID             string         `json:"id"`
	CreatedAt      string         `json:"createdAt"`
	RuntimeVersion string         `json:"runtimeVersion"`
	LaunchAsset    Asset          `json:"launchAsset"`
	Assets         []Asset        `json:"assets"`
	Metadata       map[string]any `json:"metadata"`
	Extra          map[string]any `json:"extra"`
}

// Asset is one file of a manifest. Hash is the base64url SHA-256 of the
// file, which the client checks after downloading it.
type Asset struct {
	Hash          string `json:"hash"`
	Key           string `json:"key"`
	ContentType   string `json:"contentType"`
	FileExtension string `json:"fileExtension,omitempty"`
	URL           string `json:"url"`
}

// bundleType is the launch asset's content type (a Hermes or JS bundle).
const bundleType = "application/javascript"

// Manifest builds the manifest for platform. assetBase is the absolute URL
// that File's paths are served under, for example
// http://host:8090/updates/assets.
func (u *Update) Manifest(platform, assetBase string) (Manifest, error) {
	fm, ok := u.meta.FileMetadata[platform]
	if !ok || fm.Bundle == "" {
		return Manifest{}, ErrNotFound
	}
	base := strings.TrimRight(assetBase, "/") + "/" + u.RuntimeVersion + "/" + u.ID + "/"
	asset := func(p, contentType, ext string) (Asset, error) {
		f, err := u.root.Open(filepath.FromSlash(p))
		if err != nil {
			return Asset{}, err
		}
		defer f.Close()
		sha, md := sha256.New(), md5.New()
		if _, err := io.Copy(io.MultiWriter(sha, md), f); err != nil {
			return Asset{}, err
		}
		return Asset{
			Hash:          base64.RawURLEncoding.EncodeToString(sha.Sum(nil)),
			Key:           hex.EncodeToString(md.Sum(nil)),
			ContentType:   contentType,
			FileExtension: ext,
			URL:           base + escapePath(p),
		}, nil
	}
	m := Manifest{
		ID:             u.ID,
		CreatedAt:      u.CreatedAt.UTC().Format(time.RFC3339Nano),
		RuntimeVersion: u.RuntimeVersion,
		Assets:         []Asset{},
		Metadata:       map[string]any{},
		Extra:          map[string]any{},
	}
	if u.expoConfig != nil {
		// expo-constants reads Constants.expoConfig from here when an
		// update is running.
		m.Extra["expoClient"] = u.expoConfig
	}
	var err error
	if m.LaunchAsset, err = asset(fm.Bundle, bundleType, ".bundle"); err != nil {
		return Manifest{}, err
	}
	for _, a := range fm.Assets {
		x, err := asset(a.Path, contentType(a.Ext), "."+a.Ext)
		if err != nil {
			return Manifest{}, err
		}
		m.Assets = append(m.Assets, x)
	}
	return m, nil
}

// File opens a file of the update by its path in a manifest url. Only the
// bundles and assets metadata.json lists are served, and only from inside
// the update's folder.
func (u *Update) File(p string) (*os.File, string, error) {
	for _, fm := range u.meta.FileMetadata {
		ct := ""
		if p == fm.Bundle {
			ct = bundleType
		}
		for _, a := range fm.Assets {
			if p == a.Path {
				ct = contentType(a.Ext)
			}
		}
		if ct == "" {
			continue
		}
		f, err := u.root.Open(filepath.FromSlash(p))
		if errors.Is(err, fs.ErrNotExist) {
			return nil, "", ErrNotFound
		}
		return f, ct, err
	}
	return nil, "", ErrNotFound
}

func contentType(ext string) string {
	if t := mime.TypeByExtension("." + ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

func escapePath(p string) string {
	parts := strings.Split(path.Clean(p), "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}
