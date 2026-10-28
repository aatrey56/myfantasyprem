package idptest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
)

// AppleTokenServer fakes Apple's /auth/token and /auth/revoke, recording
// every request. Tests never call Apple.
type AppleTokenServer struct {
	URL string
	// RefreshToken is what a code exchange returns.
	RefreshToken string
	// IDToken is the id_token a code exchange returns: the identity the
	// code belongs to (a Provider's Token in tests that check it).
	IDToken string
	// Status, when set, is the answer to every request (an Apple refusal).
	Status int

	mu       sync.Mutex
	Requests []AppleRequest
}

// AppleRequest is one recorded request.
type AppleRequest struct {
	Path string
	Form url.Values
}

// NewAppleTokenServer starts a fake that lives until the test ends.
func NewAppleTokenServer(t testing.TB) *AppleTokenServer {
	t.Helper()
	a := &AppleTokenServer{RefreshToken: "r.fake-refresh-token", IDToken: "x.y.z"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		a.mu.Lock()
		a.Requests = append(a.Requests, AppleRequest{Path: r.URL.Path, Form: r.PostForm})
		status, refresh, idToken := a.Status, a.RefreshToken, a.IDToken
		a.mu.Unlock()
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/x-www-form-urlencoded" {
			status = http.StatusBadRequest
		}
		if status != 0 && status != http.StatusOK {
			w.WriteHeader(status)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_grant"})
			return
		}
		switch r.URL.Path {
		case "/auth/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "a.fake", "token_type": "Bearer", "expires_in": 3600,
				"refresh_token": refresh, "id_token": idToken,
			})
		case "/auth/revoke":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	a.URL = srv.URL
	return a
}

// Revoked returns the refresh tokens revoked so far.
func (a *AppleTokenServer) Revoked() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, r := range a.Requests {
		if r.Path == "/auth/revoke" {
			out = append(out, r.Form.Get("token"))
		}
	}
	return out
}
