package idp_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/idp"
	"github.com/aatrey56/myfantasyprem/server/internal/idp/idptest"
)

var ctx = context.Background()

func verifier(p *idptest.Provider) *idp.Verifier {
	v := idp.Google([]string{"web-client", "ios-client"}, nil)
	v.JWKSURL = p.URL
	return v
}

func claims(extra map[string]any) map[string]any {
	c := map[string]any{"iss": "https://accounts.google.com", "aud": "web-client", "sub": "1234567890",
		"email": "Sam@Example.com", "email_verified": true, "name": "Sam Parker"}
	for k, v := range extra {
		c[k] = v
	}
	return c
}

func TestVerifyGood(t *testing.T) {
	p := idptest.New(t)
	v := verifier(p)
	c, err := v.Verify(ctx, p.Token(t, claims(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "1234567890" || c.Email != "sam@example.com" || !c.EmailVerified || c.Name != "Sam Parker" {
		t.Fatalf("claims = %+v", c)
	}
	// Audience as an array; the other issuer spelling; Apple's string
	// email_verified.
	c, err = v.Verify(ctx, p.Token(t, claims(map[string]any{"aud": []string{"x", "ios-client"}, "iss": "accounts.google.com", "email_verified": "true"})))
	if err != nil || !c.EmailVerified || c.Audience != "ios-client" {
		t.Fatalf("variant: %+v %v", c, err)
	}
	// The keys are cached.
	if p.Fetches != 1 {
		t.Fatalf("fetches = %d", p.Fetches)
	}
}

func TestVerifyRefusals(t *testing.T) {
	p := idptest.New(t)
	v := verifier(p)
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	for name, tok := range map[string]string{
		"wrong audience":  p.Token(t, claims(map[string]any{"aud": "someone-else"})),
		"wrong issuer":    p.Token(t, claims(map[string]any{"iss": "https://evil.example"})),
		"expired":         p.Token(t, claims(map[string]any{"exp": time.Now().Add(-time.Hour).Unix()})),
		"from the future": p.Token(t, claims(map[string]any{"iat": time.Now().Add(time.Hour).Unix()})),
		"no subject":      p.Token(t, claims(map[string]any{"sub": ""})),
		"bad signature":   p.TokenWith(t, other, p.Kid, claims(nil)),
		"unknown key":     p.TokenWith(t, other, "rotated", claims(nil)),
		"not a jwt":       "abc.def",
		"alg none":        "eyJhbGciOiJub25lIn0.e30.",
	} {
		if _, err := v.Verify(ctx, tok); !errors.Is(err, idp.ErrInvalid) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
	// Tampering with the payload breaks the signature.
	tok := p.Token(t, claims(nil))
	parts := strings.Split(tok, ".")
	forged := parts[0] + "." + strings.Replace(parts[1], "A", "B", 1) + "." + parts[2]
	if _, err := v.Verify(ctx, forged); !errors.Is(err, idp.ErrInvalid) {
		t.Errorf("forged: %v", err)
	}
	if _, err := idp.Apple(nil, nil).Verify(ctx, tok); !errors.Is(err, idp.ErrNotConfigured) {
		t.Errorf("unconfigured: %v", err)
	}
}

// A rotated key is fetched once; an unknown one does not hammer the JWKS.
func TestKeyRotation(t *testing.T) {
	p := idptest.New(t)
	now := time.Now()
	v := verifier(p)
	v.Now = func() time.Time { return now }
	if _, err := v.Verify(ctx, p.Token(t, claims(nil))); err != nil {
		t.Fatal(err)
	}
	newKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	p.Key, p.Kid = newKey, "test-key-2"
	now = now.Add(2 * time.Minute)
	if _, err := v.Verify(ctx, p.Token(t, claims(nil))); err != nil {
		t.Fatalf("rotated key: %v", err)
	}
	if p.Fetches != 2 {
		t.Fatalf("fetches = %d", p.Fetches)
	}
	// Unknown kids refetch at most once a minute.
	for range 5 {
		_, _ = v.Verify(ctx, p.TokenWith(t, newKey, "never-published", claims(nil)))
	}
	if p.Fetches != 2 {
		t.Fatalf("fetches after unknown kids = %d, want none more within a minute", p.Fetches)
	}
	now = now.Add(2 * time.Minute)
	for range 5 {
		_, _ = v.Verify(ctx, p.TokenWith(t, newKey, "never-published", claims(nil)))
	}
	if p.Fetches != 3 {
		t.Fatalf("fetches = %d, want one more after a minute", p.Fetches)
	}
}

// A slow JWKS fetch does not hold up verifications that the cache can
// answer, and concurrent callers share one fetch.
func TestSlowKeyFetchDoesNotBlock(t *testing.T) {
	p := idptest.New(t)
	var (
		mu      sync.Mutex
		fetches int
	)
	started, release := make(chan struct{}, 10), make(chan struct{})
	gated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		fetches++
		n := fetches
		mu.Unlock()
		if n > 1 {
			started <- struct{}{}
			<-release
		}
		res, err := http.Get(p.URL)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		defer res.Body.Close()
		w.Header().Set("Cache-Control", res.Header.Get("Cache-Control"))
		_, _ = io.Copy(w, res.Body)
	}))
	defer gated.Close()
	var clock atomic.Int64
	clock.Store(time.Now().UnixNano())
	v := verifier(p)
	v.JWKSURL = gated.URL
	v.Now = func() time.Time { return time.Unix(0, clock.Load()) }
	good := p.Token(t, claims(nil))
	if _, err := v.Verify(ctx, good); err != nil {
		t.Fatal(err)
	}
	// Two minutes on, unknown kids trigger a refetch, which hangs.
	clock.Add(int64(2 * time.Minute))
	other, _ := rsa.GenerateKey(rand.Reader, 2048)
	unknown := p.TokenWith(t, other, "rotated", claims(nil))
	var wg sync.WaitGroup
	for range 5 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = v.Verify(ctx, unknown)
		}()
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("no refetch")
	}
	// A cached key verifies meanwhile.
	done := make(chan error, 1)
	go func() {
		_, err := v.Verify(ctx, good)
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		close(release)
		t.Fatal("a cached-key verification waited for the JWKS fetch")
	}
	close(release)
	wg.Wait()
	mu.Lock()
	defer mu.Unlock()
	if fetches != 2 {
		t.Fatalf("fetches = %d, want 2 (concurrent callers share one)", fetches)
	}
}

func TestNonce(t *testing.T) {
	sum := sha256.Sum256([]byte("raw-nonce"))
	if !idp.NonceMatches(hex.EncodeToString(sum[:]), "raw-nonce") || idp.NonceMatches("x", "raw-nonce") {
		t.Fatal("nonce check")
	}
}
