// Package idp verifies identity tokens from Sign in with Apple and Google
// (OpenID Connect ID tokens, RS256) against the provider's published keys
// (JWKS), which it fetches over HTTPS and caches. It is an upstream HTTP
// client like provider/fpl, so it may import net/http (AGENTS.md); nothing
// else in it does I/O. Tests serve their own keys from httptest and never
// call Apple or Google.
package idp

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Errors. ErrInvalid wraps every reason a token is refused.
var (
	ErrInvalid       = errors.New("idp: invalid identity token")
	ErrNotConfigured = errors.New("idp: provider not configured")
)

// Published endpoints.
const (
	AppleIssuer   = "https://appleid.apple.com"
	AppleJWKS     = "https://appleid.apple.com/auth/keys"
	GoogleJWKS    = "https://www.googleapis.com/oauth2/v3/certs"
	googleIssuer1 = "https://accounts.google.com"
	googleIssuer2 = "accounts.google.com"
)

// Claims are the identity a verified token carries.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Nonce         string
	// Audience is the accepted client id the token was issued to.
	Audience string
}

// Verifier checks ID tokens of one provider.
type Verifier struct {
	// Issuers are the accepted `iss` values; Audiences the accepted
	// client ids (`aud`). No audiences means not configured.
	Issuers   []string
	Audiences []string
	// JWKSURL is where the provider publishes its keys.
	JWKSURL string
	Client  *http.Client
	Now     func() time.Time
	// Leeway tolerates clock skew on exp and iat (default 60 s).
	Leeway time.Duration

	mu        sync.Mutex
	keys      map[string]*rsa.PublicKey
	expires   time.Time
	lastFetch time.Time
	inflight  *fetchCall // the running JWKS fetch, if any
}

// Apple returns the Sign in with Apple verifier for these client ids (the
// app's bundle id and the web Services ID).
func Apple(clientIDs []string, client *http.Client) *Verifier {
	return &Verifier{Issuers: []string{AppleIssuer}, Audiences: clientIDs, JWKSURL: AppleJWKS, Client: client}
}

// Google returns the Google verifier for these OAuth client ids.
func Google(clientIDs []string, client *http.Client) *Verifier {
	return &Verifier{Issuers: []string{googleIssuer1, googleIssuer2}, Audiences: clientIDs, JWKSURL: GoogleJWKS, Client: client}
}

// Configured reports whether any client id is set.
func (v *Verifier) Configured() bool { return v != nil && len(v.Audiences) > 0 }

func (v *Verifier) now() time.Time {
	if v.Now == nil {
		return time.Now()
	}
	return v.Now()
}

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// payload is the claims we read. Apple sends email_verified as a string.
type payload struct {
	Iss           string          `json:"iss"`
	Sub           string          `json:"sub"`
	Aud           json.RawMessage `json:"aud"`
	Exp           json.Number     `json:"exp"`
	Iat           json.Number     `json:"iat"`
	Nbf           json.Number     `json:"nbf"`
	Email         string          `json:"email"`
	EmailVerified any             `json:"email_verified"`
	Name          string          `json:"name"`
	Nonce         string          `json:"nonce"`
}

func b64(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// Verify checks the token's signature against the provider's keys and its
// issuer, audience, expiry and issue time, and returns its claims.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	if !v.Configured() {
		return Claims{}, ErrNotConfigured
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return Claims{}, invalid("not a JWT")
	}
	rawHeader, err := b64(parts[0])
	if err != nil {
		return Claims{}, invalid("header encoding")
	}
	var header struct {
		Alg string `json:"alg"`
		Kid string `json:"kid"`
	}
	if err := json.Unmarshal(rawHeader, &header); err != nil {
		return Claims{}, invalid("header")
	}
	if header.Alg != "RS256" {
		return Claims{}, invalid("alg %q", header.Alg)
	}
	sig, err := b64(parts[2])
	if err != nil {
		return Claims{}, invalid("signature encoding")
	}
	key, err := v.key(ctx, header.Kid)
	if err != nil {
		return Claims{}, err
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], sig); err != nil {
		return Claims{}, invalid("signature")
	}
	rawPayload, err := b64(parts[1])
	if err != nil {
		return Claims{}, invalid("payload encoding")
	}
	var p payload
	dec := json.NewDecoder(strings.NewReader(string(rawPayload)))
	dec.UseNumber()
	if err := dec.Decode(&p); err != nil {
		return Claims{}, invalid("payload")
	}
	if !slices.Contains(v.Issuers, p.Iss) {
		return Claims{}, invalid("issuer %q", p.Iss)
	}
	aud := audience(p.Aud, v.Audiences)
	if aud == "" {
		return Claims{}, invalid("audience")
	}
	leeway := v.Leeway
	if leeway == 0 {
		leeway = time.Minute
	}
	now := v.now()
	exp, err := p.Exp.Int64()
	if err != nil || now.After(time.Unix(exp, 0).Add(leeway)) {
		return Claims{}, invalid("expired")
	}
	if iat, err := p.Iat.Int64(); err == nil && time.Unix(iat, 0).After(now.Add(leeway)) {
		return Claims{}, invalid("issued in the future")
	}
	if nbf, err := p.Nbf.Int64(); err == nil && time.Unix(nbf, 0).After(now.Add(leeway)) {
		return Claims{}, invalid("not yet valid")
	}
	if p.Sub == "" {
		return Claims{}, invalid("no subject")
	}
	c := Claims{Subject: p.Sub, Email: strings.ToLower(strings.TrimSpace(p.Email)), Name: p.Name, Nonce: p.Nonce, Audience: aud}
	switch ev := p.EmailVerified.(type) {
	case bool:
		c.EmailVerified = ev
	case string:
		c.EmailVerified = ev == "true"
	}
	return c, nil
}

// NonceMatches reports whether a token's nonce claim is the SHA-256 (hex)
// of the raw nonce the client sent (Apple's scheme), or equal to it.
func NonceMatches(claim, raw string) bool {
	sum := sha256.Sum256([]byte(raw))
	return claim == hex.EncodeToString(sum[:]) || claim == raw
}

// audience returns the first of the token's audiences that is allowed,
// or "" when none is.
func audience(raw json.RawMessage, allowed []string) string {
	var many []string
	var one string
	if json.Unmarshal(raw, &one) == nil {
		many = []string{one}
	} else if json.Unmarshal(raw, &many) != nil {
		return ""
	}
	for _, a := range many {
		if slices.Contains(allowed, a) {
			return a
		}
	}
	return ""
}

// key returns the public key with kid, fetching the JWKS when the cache
// is cold or expired, or once a minute at most when the kid is unknown
// (the provider rotated its keys).
//
// The HTTP fetch runs without the lock, so verifications the cache can
// answer never wait for it, and concurrent callers share one fetch.
func (v *Verifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.Lock()
	now := v.now()
	if k, ok := v.keys[kid]; ok && now.Before(v.expires) {
		v.mu.Unlock()
		return k, nil
	}
	call := v.inflight
	if call == nil && (v.keys == nil || !now.Before(v.expires) || now.Sub(v.lastFetch) >= time.Minute) {
		call = &fetchCall{done: make(chan struct{})}
		v.inflight, v.lastFetch = call, now
		go v.runFetch(context.WithoutCancel(ctx), call, now)
	}
	v.mu.Unlock()

	var fetchErr error
	if call != nil {
		select {
		case <-call.done:
			fetchErr = call.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if k, ok := v.keys[kid]; ok {
		return k, nil // a stale key is served while the provider is down
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	return nil, invalid("unknown key %q", kid)
}

// fetchCall is one JWKS fetch that callers wait on.
type fetchCall struct {
	done chan struct{}
	err  error
}

func (v *Verifier) runFetch(ctx context.Context, call *fetchCall, now time.Time) {
	keys, ttl, err := v.fetch(ctx)
	v.mu.Lock()
	if err == nil {
		v.keys, v.expires = keys, now.Add(ttl)
	}
	v.inflight = nil
	call.err = err
	v.mu.Unlock()
	close(call.done)
}

// fetch downloads the provider's keys and how long to cache them.
func (v *Verifier) fetch(ctx context.Context) (map[string]*rsa.PublicKey, time.Duration, error) {
	client := v.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, v.JWKSURL, nil)
	if err != nil {
		return nil, 0, err
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("idp: fetch keys: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("idp: fetch keys: status %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, 0, fmt.Errorf("idp: fetch keys: %w", err)
	}
	var set struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &set); err != nil {
		return nil, 0, fmt.Errorf("idp: keys: %w", err)
	}
	keys := map[string]*rsa.PublicKey{}
	for _, k := range set.Keys {
		if k.Kty != "RSA" {
			continue
		}
		n, err1 := b64(k.N)
		e, err2 := b64(k.E)
		if err1 != nil || err2 != nil || len(e) > 4 {
			continue
		}
		exp := 0
		for _, b := range e {
			exp = exp<<8 | int(b)
		}
		keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: exp}
	}
	return keys, maxAge(res.Header.Get("Cache-Control")), nil
}

// maxAge reads Cache-Control max-age, within 5 minutes and 24 hours
// (default 1 hour).
func maxAge(cc string) time.Duration {
	for _, part := range strings.Split(cc, ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(part), "max-age="); ok {
			if s, err := strconv.Atoi(v); err == nil {
				return min(max(time.Duration(s)*time.Second, 5*time.Minute), 24*time.Hour)
			}
		}
	}
	return time.Hour
}
