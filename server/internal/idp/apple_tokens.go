package idp

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AppleBaseURL is Apple's token endpoint host.
const AppleBaseURL = "https://appleid.apple.com"

// AppleTokens talks to Apple's REST API for Sign in with Apple tokens
// (App Store guideline 5.1.1(v): deleting an account revokes its Apple
// token). At sign-in the authorization code is exchanged for a refresh
// token; at account deletion that token is revoked. Each request carries
// a client secret: a short-lived ES256 JWT signed with the team's Sign in
// with Apple key. Refresh tokens are stored sealed (AES-256-GCM) with a
// key of our own, so a database dump does not hold usable tokens.
//
// A nil *AppleTokens is "not configured".
type AppleTokens struct {
	TeamID, KeyID string
	// BaseURL is Apple's host (tests point it at a fake).
	BaseURL string
	Client  *http.Client
	Now     func() time.Time

	key  *ecdsa.PrivateKey
	aead cipher.AEAD
}

// NewAppleTokens parses the .p8 private key (PEM, PKCS#8, P-256) and the
// 32-byte sealing key.
func NewAppleTokens(teamID, keyID string, keyPEM, sealKey []byte) (*AppleTokens, error) {
	if teamID == "" || keyID == "" {
		return nil, errors.New("idp: apple tokens need a team id and a key id")
	}
	block, _ := pem.Decode(keyPEM)
	if block == nil {
		return nil, errors.New("idp: apple private key: not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("idp: apple private key: %w", err)
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok || key.Curve.Params().BitSize != 256 {
		return nil, errors.New("idp: apple private key: want a P-256 EC key")
	}
	if len(sealKey) != 32 {
		return nil, errors.New("idp: apple token sealing key: want 32 bytes")
	}
	block2, err := aes.NewCipher(sealKey)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block2)
	if err != nil {
		return nil, err
	}
	return &AppleTokens{TeamID: teamID, KeyID: keyID, BaseURL: AppleBaseURL, key: key, aead: aead}, nil
}

// Configured reports whether token exchange and revocation are set up.
func (a *AppleTokens) Configured() bool { return a != nil && a.key != nil }

func (a *AppleTokens) now() time.Time {
	if a.Now == nil {
		return time.Now()
	}
	return a.Now()
}

// clientSecret is the ES256 JWT Apple wants as client_secret.
func (a *AppleTokens) clientSecret(clientID string) (string, error) {
	enc := func(v any) string {
		b, _ := json.Marshal(v)
		return base64.RawURLEncoding.EncodeToString(b)
	}
	now := a.now()
	signing := enc(map[string]string{"alg": "ES256", "kid": a.KeyID}) + "." + enc(map[string]any{
		"iss": a.TeamID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"aud": AppleIssuer, "sub": clientID,
	})
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, a.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func (a *AppleTokens) post(ctx context.Context, path, clientID string, form url.Values) ([]byte, error) {
	secret, err := a.clientSecret(clientID)
	if err != nil {
		return nil, err
	}
	form.Set("client_id", clientID)
	form.Set("client_secret", secret)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(a.BaseURL, "/")+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("idp: apple %s: %w", path, err)
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("idp: apple %s: %w", path, err)
	}
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(body, &e)
		return nil, fmt.Errorf("idp: apple %s: status %d %s", path, res.StatusCode, e.Error)
	}
	return body, nil
}

// Exchange trades a Sign in with Apple authorization code (single use,
// valid five minutes) for a refresh token, and returns the id_token of
// the answer too: the identity the code was issued to, which the caller
// verifies is the one signing in before keeping the refresh token.
// clientID is the identity token's audience: the app's bundle id or the
// web Services ID.
func (a *AppleTokens) Exchange(ctx context.Context, clientID, code string) (refreshToken, idToken string, err error) {
	body, err := a.post(ctx, "/auth/token", clientID, url.Values{"grant_type": {"authorization_code"}, "code": {code}})
	if err != nil {
		return "", "", err
	}
	var out struct {
		RefreshToken string `json:"refresh_token"`
		IDToken      string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &out); err != nil || out.RefreshToken == "" {
		return "", "", errors.New("idp: apple /auth/token: no refresh_token in the answer")
	}
	return out.RefreshToken, out.IDToken, nil
}

// Revoke revokes a refresh token (and the user's authorization of the app).
func (a *AppleTokens) Revoke(ctx context.Context, clientID, refreshToken string) error {
	_, err := a.post(ctx, "/auth/revoke", clientID, url.Values{"token": {refreshToken}, "token_type_hint": {"refresh_token"}})
	return err
}

type sealedToken struct {
	ClientID string `json:"c"`
	Token    string `json:"t"`
}

// Seal encrypts a refresh token and the client id it belongs to for
// storage.
func (a *AppleTokens) Seal(clientID, refreshToken string) (string, error) {
	plain, _ := json.Marshal(sealedToken{ClientID: clientID, Token: refreshToken})
	nonce := make([]byte, a.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return "v1." + base64.RawURLEncoding.EncodeToString(a.aead.Seal(nonce, nonce, plain, []byte("apple-refresh-token"))), nil
}

// Open decrypts what Seal made.
func (a *AppleTokens) Open(sealed string) (clientID, refreshToken string, err error) {
	raw, ok := strings.CutPrefix(sealed, "v1.")
	if !ok {
		return "", "", errors.New("idp: sealed apple token: unknown format")
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(b) < a.aead.NonceSize() {
		return "", "", errors.New("idp: sealed apple token: bad encoding")
	}
	plain, err := a.aead.Open(nil, b[:a.aead.NonceSize()], b[a.aead.NonceSize():], []byte("apple-refresh-token"))
	if err != nil {
		return "", "", errors.New("idp: sealed apple token: cannot decrypt (wrong APPLE_TOKEN_KEY?)")
	}
	var t sealedToken
	if err := json.Unmarshal(plain, &t); err != nil {
		return "", "", err
	}
	return t.ClientID, t.Token, nil
}
