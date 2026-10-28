package webpush

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
)

// browser is a fake user agent: its keys and a decrypt for RFC 8291.
type browser struct {
	key  *ecdh.PrivateKey
	auth []byte
}

func newBrowser(t *testing.T) *browser {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	auth := make([]byte, 16)
	_, _ = rand.Read(auth)
	return &browser{key: k, auth: auth}
}

func (b *browser) sub(endpoint string) notifications.Subscription {
	e := base64.RawURLEncoding
	return notifications.Subscription{ID: "psub_x", Kind: notifications.KindWebPush, Endpoint: endpoint,
		P256dh: e.EncodeToString(b.key.PublicKey().Bytes()), Auth: e.EncodeToString(b.auth)}
}

func (b *browser) decrypt(t *testing.T, body []byte) []byte {
	t.Helper()
	salt, rs, idlen := body[:16], binary.BigEndian.Uint32(body[16:20]), int(body[20])
	if rs != recordSize || idlen != 65 {
		t.Fatalf("header rs %d idlen %d", rs, idlen)
	}
	asRaw := body[21 : 21+idlen]
	as, err := ecdh.P256().NewPublicKey(asRaw)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := b.key.ECDH(as)
	if err != nil {
		t.Fatal(err)
	}
	cek, nonce, err := keys(shared, b.auth, b.key.PublicKey().Bytes(), asRaw, salt)
	if err != nil {
		t.Fatal(err)
	}
	block, _ := aes.NewCipher(cek)
	gcm, _ := cipher.NewGCM(block)
	plain, err := gcm.Open(nil, nonce, body[21+idlen:], nil)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if plain[len(plain)-1] != 2 {
		t.Fatalf("no last-record delimiter")
	}
	return plain[:len(plain)-1]
}

func testVAPID(t *testing.T) VAPID {
	pub, priv, err := GenerateVAPID()
	if err != nil {
		t.Fatal(err)
	}
	return VAPID{PublicKey: pub, PrivateKey: priv, Subject: "mailto:owner@example.com"}
}

func TestWebPushEncryptsAndSignsForThePushService(t *testing.T) {
	b := newBrowser(t)
	v := testVAPID(t)
	var got struct {
		body []byte
		h    http.Header
	}
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.body, _ = io.ReadAll(r.Body)
		got.h = r.Header
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	wp, err := New(v)
	if err != nil {
		t.Fatal(err)
	}
	wp.Client = srv.Client()
	p := notifications.Payload{ID: "ntf_1", Type: notifications.PlayerGoal, Title: "Goal", Body: "Saka scored.", URL: "/league/x"}
	if err := wp.Deliver(context.Background(), b.sub(srv.URL+"/push/abc"), p); err != nil {
		t.Fatal(err)
	}
	var back notifications.Payload
	if err := json.Unmarshal(b.decrypt(t, got.body), &back); err != nil || back != p {
		t.Fatalf("payload %+v %v", back, err)
	}
	if got.h.Get("Content-Encoding") != "aes128gcm" || got.h.Get("TTL") == "" {
		t.Fatalf("headers %v", got.h)
	}
	// Authorization: vapid t=<jwt>, k=<public key>; the JWT verifies with k.
	auth := strings.TrimPrefix(got.h.Get("Authorization"), "vapid t=")
	jwt, k, ok := strings.Cut(auth, ", k=")
	if !ok || k != v.PublicKey {
		t.Fatalf("authorization %q", got.h.Get("Authorization"))
	}
	parts := strings.Split(jwt, ".")
	claims, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Aud string `json:"aud"`
		Sub string `json:"sub"`
		Exp int64  `json:"exp"`
	}
	_ = json.Unmarshal(claims, &c)
	if c.Aud != srv.URL || c.Sub != v.Subject || c.Exp <= time.Now().Unix() {
		t.Fatalf("claims %+v", c)
	}
	raw, _ := base64.RawURLEncoding.DecodeString(k)
	pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), raw)
	if err != nil {
		t.Fatal(err)
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	var r, s = new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])
	if !ecdsa.Verify(pub, sum[:], r, s) {
		t.Fatal("VAPID signature does not verify")
	}
}

func TestWebPushRejectsMismatchedKeys(t *testing.T) {
	a, b := testVAPID(t), testVAPID(t)
	a.PublicKey = b.PublicKey
	if _, err := New(a); err == nil {
		t.Fatal("mismatched keys accepted")
	}
}
