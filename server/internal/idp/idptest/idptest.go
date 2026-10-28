// Package idptest is a fake identity provider for tests: a locally
// generated RSA key, its JWKS served by httptest, and signed ID tokens.
// Tests never call Apple or Google.
package idptest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// Provider is a fake issuer.
type Provider struct {
	Key *rsa.PrivateKey
	Kid string
	// URL serves the JWKS; Fetches counts requests to it.
	URL     string
	Fetches int
}

// sharedKey is generated once per test process: RSA key generation is
// slow and every provider may use the same key.
var sharedKey = sync.OnceValue(func() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
})

// New starts a provider whose JWKS lives until the test ends.
func New(t testing.TB) *Provider {
	t.Helper()
	p := &Provider{Key: sharedKey(), Kid: "test-key-1"}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		p.Fetches++
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA", "kid": p.Kid, "alg": "RS256", "use": "sig",
			"n": base64.RawURLEncoding.EncodeToString(p.Key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(p.Key.E)).Bytes()),
		}}})
	}))
	t.Cleanup(srv.Close)
	p.URL = srv.URL
	return p
}

// Token signs claims (iat and exp default to now and an hour on).
func (p *Provider) Token(t testing.TB, claims map[string]any) string {
	t.Helper()
	return p.TokenWith(t, p.Key, p.Kid, claims)
}

// TokenWith signs with any key and kid (to test bad signatures).
func (p *Provider) TokenWith(t testing.TB, key *rsa.PrivateKey, kid string, claims map[string]any) string {
	t.Helper()
	if _, ok := claims["iat"]; !ok {
		claims["iat"] = time.Now().Unix()
	}
	if _, ok := claims["exp"]; !ok {
		claims["exp"] = time.Now().Add(time.Hour).Unix()
	}
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}
