package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strings"

	"github.com/aatrey56/myfantasyprem/server/internal/updates"
)

// Over-the-air updates for the native app (Expo Updates protocol 1,
// https://docs.expo.dev/technical-specs/expo-updates-1/), mounted beside
// the API when UPDATES_DIR is set:
//
//	GET /updates/manifest                           the newest update for the app's runtime
//	GET /updates/assets/{runtime}/{id}/{path...}    its bundle and assets
//
// They are public (the app asks before anyone signs in) and outside the
// API's {data, meta} contract: the protocol fixes the shapes.
const (
	updatesManifestPath = "/updates/manifest"
	updatesAssetsPath   = "/updates/assets"
)

// newUpdatesHandler serves store's updates. Asset urls are absolute: on
// publicURL when set, else on the host the app asked (trustForwarded is
// as for withOrigin).
func newUpdatesHandler(store updates.Store, trustForwarded bool, publicURL string, log *slog.Logger) http.Handler {
	h := &updatesHandler{store: store, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+updatesManifestPath, h.manifest)
	mux.HandleFunc("GET "+updatesAssetsPath+"/{runtime}/{id}/{path...}", h.asset)
	if publicURL != "" {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mux.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), originKey{}, publicURL)))
		})
	}
	return withOrigin(trustForwarded, mux)
}

type updatesHandler struct {
	store updates.Store
	log   *slog.Logger
}

func (h *updatesHandler) manifest(w http.ResponseWriter, r *http.Request) {
	protocol := r.Header.Get("expo-protocol-version")
	if protocol != "" && protocol != "0" && protocol != "1" {
		http.Error(w, "unsupported expo-protocol-version", http.StatusBadRequest)
		return
	}
	v1 := protocol == "1"
	platform := headerOrQuery(r, "expo-platform", "platform")
	runtime := headerOrQuery(r, "expo-runtime-version", "runtime-version")
	if !updates.ValidPlatform(platform) {
		http.Error(w, "expo-platform must be ios or android", http.StatusBadRequest)
		return
	}
	if !updates.ValidRuntime(runtime) {
		http.Error(w, "expo-runtime-version is missing or invalid", http.StatusBadRequest)
		return
	}
	w.Header().Set("expo-protocol-version", "1")
	w.Header().Set("expo-sfv-version", "0")
	w.Header().Set("Cache-Control", "private, max-age=0")
	multipartOK := acceptsMultipart(r.Header.Get("Accept"))

	u, err := h.store.Latest(runtime, platform)
	if err != nil && !errors.Is(err, updates.ErrNotFound) {
		h.fail(w, r, err)
		return
	}
	if u != nil {
		defer u.Close()
	}
	// No update for this runtime, or the app already runs the newest.
	if u == nil || (v1 && r.Header.Get("expo-current-update-id") == u.ID) {
		switch {
		case v1 && multipartOK:
			writeParts(w, h, r, part{"directive", map[string]string{"type": "noUpdateAvailable"}})
		case v1:
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "no update available", http.StatusNotFound)
		}
		return
	}
	origin, _ := r.Context().Value(originKey{}).(string)
	m, err := u.Manifest(platform, origin+updatesAssetsPath)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !multipartOK {
		w.Header().Set("Content-Type", "application/expo+json")
		_ = json.NewEncoder(w).Encode(m)
		return
	}
	writeParts(w, h, r,
		part{"manifest", m},
		part{"extensions", map[string]any{"assetRequestHeaders": map[string]any{}}})
}

func (h *updatesHandler) asset(w http.ResponseWriter, r *http.Request) {
	u, err := h.store.Open(r.PathValue("runtime"), r.PathValue("id"))
	if errors.Is(err, updates.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	defer u.Close()
	f, ct, err := u.File(r.PathValue("path"))
	if errors.Is(err, updates.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		h.fail(w, r, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", ct)
	// An update's files never change: its id is in the url.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeContent(w, r, "", st.ModTime(), f)
}

func (h *updatesHandler) fail(w http.ResponseWriter, r *http.Request, err error) {
	h.log.ErrorContext(r.Context(), "updates", "path", r.URL.Path, "err", err)
	http.Error(w, "internal error", http.StatusInternalServerError)
}

// part is one form-data part of a multipart/mixed response.
type part struct {
	name string
	body any
}

// writeParts writes the protocol's multipart/mixed body.
func writeParts(w http.ResponseWriter, h *updatesHandler, r *http.Request, parts ...part) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for _, p := range parts {
		pw, err := mw.CreatePart(textproto.MIMEHeader{
			"Content-Disposition": {`form-data; name="` + p.name + `"`},
			"Content-Type":        {"application/json; charset=utf-8"},
		})
		if err == nil {
			err = json.NewEncoder(pw).Encode(p.body)
		}
		if err != nil {
			h.fail(w, r, err)
			return
		}
	}
	if err := mw.Close(); err != nil {
		h.fail(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "multipart/mixed; boundary="+mw.Boundary())
	_, _ = w.Write(buf.Bytes())
}

func headerOrQuery(r *http.Request, header, query string) string {
	if v := r.Header.Get(header); v != "" {
		return v
	}
	return r.URL.Query().Get(query)
}

// acceptsMultipart: no Accept header, */* or multipart/mixed.
func acceptsMultipart(accept string) bool {
	if strings.TrimSpace(accept) == "" {
		return true
	}
	for _, a := range strings.Split(accept, ",") {
		mt := strings.TrimSpace(strings.SplitN(a, ";", 2)[0])
		if mt == "*/*" || mt == "multipart/*" || mt == "multipart/mixed" {
			return true
		}
	}
	return false
}
