package httpapi

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/idp"
	"github.com/aatrey56/myfantasyprem/server/internal/idp/idptest"
)

// Sign in with Apple exchanges the authorization code for a refresh
// token (stored sealed) and account deletion revokes it with Apple
// (guideline 5.1.1(v)). Apple is a local fake.
func TestAppleTokenRevokedAtAccountDeletion(t *testing.T) {
	srv, _ := newTestServer()
	ctx := context.Background()
	keys := idptest.New(t)
	srv.apple = idp.Apple([]string{"com.example.app"}, nil)
	srv.apple.JWKSURL = keys.URL
	fake := idptest.NewAppleTokenServer(t)
	fake.RefreshToken = "r.refresh-123"
	// The code's own identity, as Apple's token endpoint returns it.
	fake.IDToken = keys.Token(t, map[string]any{"iss": idp.AppleIssuer, "aud": "com.example.app", "sub": "001234.apple"})
	ec, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, _ := x509.MarshalPKCS8PrivateKey(ec)
	tokens, err := idp.NewAppleTokens("TEAM123", "KEY123", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	tokens.BaseURL = fake.URL
	srv.appleTokens = tokens
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))

	sum := sha256.Sum256([]byte("n0nce"))
	identity := keys.Token(t, map[string]any{"iss": idp.AppleIssuer, "aud": "com.example.app", "sub": "001234.apple",
		"email": "sam@privaterelay.example", "email_verified": "true", "nonce": hex.EncodeToString(sum[:])})
	rec := doAnon(h, http.MethodPost, "/v1/auth/apple", `{"identityToken":"`+identity+`","authorizationCode":"c0de","nonce":"n0nce"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("sign in = %d %s", rec.Code, rec.Body)
	}
	var sess SessionResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &sess)
	if len(fake.Requests) != 1 || fake.Requests[0].Path != "/auth/token" || fake.Requests[0].Form.Get("code") != "c0de" ||
		fake.Requests[0].Form.Get("client_id") != "com.example.app" {
		t.Fatalf("apple requests = %+v", fake.Requests)
	}
	stored, err := srv.auth.AppleRefreshTokens(ctx, sess.Data.User.Id)
	if err != nil || len(stored) != 1 || strings.Contains(stored[0], "refresh-123") {
		t.Fatalf("stored = %v %v (want one sealed token)", stored, err)
	}

	if rec := doAs(h, sess.Data.AccessToken, http.MethodDelete, "/v1/me", ""); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d %s", rec.Code, rec.Body)
	}
	if got := fake.Revoked(); len(got) != 1 || got[0] != "r.refresh-123" {
		t.Fatalf("revoked = %v", got)
	}
}

// When Apple refuses the code, sign-in still works (the token just
// cannot be revoked later; it is logged).
func TestAppleSignInSurvivesAFailedExchange(t *testing.T) {
	srv, _ := newTestServer()
	keys := idptest.New(t)
	srv.apple = idp.Apple([]string{"com.example.app"}, nil)
	srv.apple.JWKSURL = keys.URL
	fake := idptest.NewAppleTokenServer(t)
	fake.Status = http.StatusBadRequest
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(ec)
	tokens, _ := idp.NewAppleTokens("TEAM123", "KEY123", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), make([]byte, 32))
	tokens.BaseURL = fake.URL
	srv.appleTokens = tokens
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	identity := keys.Token(t, map[string]any{"iss": idp.AppleIssuer, "aud": "com.example.app", "sub": "001235.apple", "nonce": "n0nce"})
	if rec := doAnon(h, http.MethodPost, "/v1/auth/apple", `{"identityToken":"`+identity+`","authorizationCode":"c0de","nonce":"n0nce"}`); rec.Code != http.StatusOK {
		t.Fatalf("sign in = %d %s", rec.Code, rec.Body)
	}
}

// The authorization code is exchanged for a token of whichever Apple
// account it was issued to. A code of another account (B's, sent along
// A's identity token) must not be stored under A: deleting A would
// revoke B's authorization of the app.
func TestAppleExchangeOfAnotherAccountsCodeIsNotStored(t *testing.T) {
	srv, _ := newTestServer()
	ctx := context.Background()
	keys := idptest.New(t)
	srv.apple = idp.Apple([]string{"com.example.app"}, nil)
	srv.apple.JWKSURL = keys.URL
	fake := idptest.NewAppleTokenServer(t)
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	der, _ := x509.MarshalPKCS8PrivateKey(ec)
	tokens, _ := idp.NewAppleTokens("TEAM123", "KEY123", pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), make([]byte, 32))
	tokens.BaseURL = fake.URL
	srv.appleTokens = tokens
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	sum := sha256.Sum256([]byte("n0nce"))
	nonce := hex.EncodeToString(sum[:])
	claims := func(sub, aud, nonce string) map[string]any {
		return map[string]any{"iss": idp.AppleIssuer, "aud": aud, "sub": sub, "nonce": nonce}
	}
	signIn := func(exchanged string) []string {
		t.Helper()
		fake.IDToken = exchanged
		identity := keys.Token(t, claims("001300.alice", "com.example.app", nonce))
		rec := doAnon(h, http.MethodPost, "/v1/auth/apple", `{"identityToken":"`+identity+`","authorizationCode":"c0de","nonce":"n0nce"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("sign in = %d %s", rec.Code, rec.Body)
		}
		var sess SessionResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &sess)
		stored, err := srv.auth.AppleRefreshTokens(ctx, sess.Data.User.Id)
		if err != nil {
			t.Fatal(err)
		}
		return stored
	}
	for name, exchanged := range map[string]string{
		"another subject":  keys.Token(t, claims("001301.bob", "com.example.app", nonce)),
		"another audience": keys.Token(t, claims("001300.alice", "com.example.other", nonce)),
		"another nonce":    keys.Token(t, claims("001300.alice", "com.example.app", "other")),
		"not signed":       "x.y.z",
	} {
		if stored := signIn(exchanged); len(stored) != 0 {
			t.Errorf("%s: stored %d Apple tokens under the signed-in account", name, len(stored))
		}
	}
	// Apple's id_token from the exchange may carry no nonce.
	if stored := signIn(keys.Token(t, claims("001300.alice", "com.example.app", ""))); len(stored) != 1 {
		t.Errorf("matching exchange: stored %d tokens, want 1", len(stored))
	}
}
