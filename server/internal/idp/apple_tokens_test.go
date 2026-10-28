package idp_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"math/big"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/idp"
	"github.com/aatrey56/myfantasyprem/server/internal/idp/idptest"
)

func appleKeyPEM(t *testing.T) (*ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
}

// checkClientSecret verifies the ES256 client secret Apple would check.
func checkClientSecret(t *testing.T, secret string, pub *ecdsa.PublicKey, clientID string, now time.Time) {
	t.Helper()
	parts := strings.Split(secret, ".")
	if len(parts) != 3 {
		t.Fatalf("client_secret is not a JWT: %q", secret)
	}
	var header struct{ Alg, Kid string }
	var claims struct {
		Iss, Sub, Aud string
		Iat, Exp      int64
	}
	h, _ := base64.RawURLEncoding.DecodeString(parts[0])
	c, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if json.Unmarshal(h, &header) != nil || json.Unmarshal(c, &claims) != nil {
		t.Fatalf("client_secret header/claims: %s %s", h, c)
	}
	if header.Alg != "ES256" || header.Kid != "KEY123" {
		t.Errorf("header = %+v", header)
	}
	if claims.Iss != "TEAM123" || claims.Sub != clientID || claims.Aud != "https://appleid.apple.com" ||
		claims.Iat != now.Unix() || claims.Exp <= claims.Iat || claims.Exp > claims.Iat+15777000 {
		t.Errorf("claims = %+v", claims)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if len(sig) != 64 {
		t.Fatalf("signature is %d bytes, want raw r||s (64)", len(sig))
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(pub, sum[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
		t.Error("client_secret signature does not verify")
	}
}

func TestAppleTokensExchangeAndRevoke(t *testing.T) {
	key, keyPEM := appleKeyPEM(t)
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	fake := idptest.NewAppleTokenServer(t)
	fake.RefreshToken = "r.refresh-123"
	sealKey := make([]byte, 32)
	a, err := idp.NewAppleTokens("TEAM123", "KEY123", keyPEM, sealKey)
	if err != nil {
		t.Fatal(err)
	}
	a.BaseURL, a.Now = fake.URL, func() time.Time { return now }

	rt, idToken, err := a.Exchange(ctx, "com.example.app", "c0de")
	if err != nil || rt != "r.refresh-123" || idToken != fake.IDToken {
		t.Fatalf("exchange = %q %q %v", rt, idToken, err)
	}
	ex := fake.Requests[0]
	if ex.Path != "/auth/token" || ex.Form.Get("grant_type") != "authorization_code" || ex.Form.Get("code") != "c0de" ||
		ex.Form.Get("client_id") != "com.example.app" {
		t.Errorf("token request = %+v", ex)
	}
	checkClientSecret(t, ex.Form.Get("client_secret"), &key.PublicKey, "com.example.app", now)

	if err := a.Revoke(ctx, "com.example.app", rt); err != nil {
		t.Fatal(err)
	}
	rv := fake.Requests[1]
	if rv.Path != "/auth/revoke" || rv.Form.Get("token") != rt || rv.Form.Get("token_type_hint") != "refresh_token" ||
		rv.Form.Get("client_id") != "com.example.app" {
		t.Errorf("revoke request = %+v", rv)
	}
	checkClientSecret(t, rv.Form.Get("client_secret"), &key.PublicKey, "com.example.app", now)

	// Apple refusing is an error the caller logs.
	fake.Status = http.StatusBadRequest
	if _, _, err := a.Exchange(ctx, "com.example.app", "used-code"); err == nil {
		t.Error("a refused exchange returned no error")
	}
}

func TestAppleTokensSealing(t *testing.T) {
	_, keyPEM := appleKeyPEM(t)
	sealKey := make([]byte, 32)
	sealKey[0] = 7
	a, err := idp.NewAppleTokens("TEAM123", "KEY123", keyPEM, sealKey)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := a.Seal("com.example.app", "r.refresh-123")
	if err != nil || strings.Contains(sealed, "refresh-123") || strings.Contains(sealed, "com.example") {
		t.Fatalf("sealed = %q %v", sealed, err)
	}
	client, token, err := a.Open(sealed)
	if err != nil || client != "com.example.app" || token != "r.refresh-123" {
		t.Fatalf("open = %q %q %v", client, token, err)
	}
	tampered := sealed[:len(sealed)-2] + "AA"
	if _, _, err := a.Open(tampered); err == nil {
		t.Error("a tampered token opened")
	}
	other, _ := idp.NewAppleTokens("TEAM123", "KEY123", keyPEM, make([]byte, 32))
	if _, _, err := other.Open(sealed); err == nil {
		t.Error("another key opened the token")
	}
	if _, err := idp.NewAppleTokens("TEAM123", "KEY123", keyPEM, make([]byte, 16)); err == nil {
		t.Error("a 16-byte sealing key was accepted")
	}
	if _, err := idp.NewAppleTokens("TEAM123", "KEY123", []byte("not a key"), sealKey); err == nil {
		t.Error("a bad private key was accepted")
	}
	var nilTokens *idp.AppleTokens
	if nilTokens.Configured() {
		t.Error("nil AppleTokens configured")
	}
}
