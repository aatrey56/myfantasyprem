// Package webpush delivers notifications to browsers and home-screen web
// apps: RFC 8291 message encryption (aes128gcm, RFC 8188) and RFC 8292
// VAPID authentication, on the standard library. It is an upstream HTTP
// client (the browsers' push services), like idp.
package webpush

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
)

var _ notifications.Deliverer = (*WebPush)(nil)

// VAPID is the application server's key pair (RFC 8292): the private key
// as a base64url 32-byte scalar, the public key as base64url uncompressed
// point (what browsers take as applicationServerKey), and a contact
// subject (mailto: or https:).
type VAPID struct {
	PublicKey  string
	PrivateKey string
	Subject    string
}

// GenerateVAPID returns a fresh key pair.
func GenerateVAPID() (public, private string, err error) {
	k, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	b64 := base64.RawURLEncoding
	return b64.EncodeToString(k.PublicKey().Bytes()), b64.EncodeToString(k.Bytes()), nil
}

// WebPush delivers to browsers: RFC 8291 message encryption (aes128gcm,
// RFC 8188) and RFC 8292 VAPID authentication.
type WebPush struct {
	key     *ecdsa.PrivateKey
	public  string
	subject string
	Client  *http.Client
	Now     func() time.Time
	// TTL is how long the push service keeps an undelivered message.
	TTL time.Duration
}

// New checks the VAPID keys and returns a WebPush.
func New(v VAPID) (*WebPush, error) {
	d, err := base64.RawURLEncoding.DecodeString(v.PrivateKey)
	if err != nil {
		return nil, fmt.Errorf("webpush: private key: %w", err)
	}
	key, err := ecdsa.ParseRawPrivateKey(elliptic.P256(), d)
	if err != nil {
		return nil, fmt.Errorf("webpush: private key: %w", err)
	}
	pub, err := key.PublicKey.Bytes()
	if err != nil {
		return nil, err
	}
	if got := base64.RawURLEncoding.EncodeToString(pub); got != v.PublicKey {
		return nil, errors.New("webpush: the public key does not match the private key")
	}
	if v.Subject == "" {
		return nil, errors.New("webpush: a subject (mailto: or https: URL) is required")
	}
	return &WebPush{key: key, public: v.PublicKey, subject: v.Subject,
		Client: &http.Client{Timeout: 15 * time.Second}, Now: time.Now, TTL: 24 * time.Hour}, nil
}

// PublicKey is the applicationServerKey for browsers.
func (w *WebPush) PublicKey() string { return w.public }

func (w *WebPush) Deliver(ctx context.Context, sub notifications.Subscription, p notifications.Payload) error {
	u, err := url.Parse(sub.Endpoint)
	if err != nil || u.Scheme != "https" {
		return notifications.ErrGone
	}
	plain, err := json.Marshal(p)
	if err != nil {
		return err
	}
	body, err := Encrypt(plain, sub.P256dh, sub.Auth)
	if err != nil {
		// Keys the browser gave us that do not work will never work.
		return fmt.Errorf("%w: %v", notifications.ErrGone, err)
	}
	jwt, err := w.jwt(u.Scheme + "://" + u.Host)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Encoding", "aes128gcm")
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("TTL", strconv.Itoa(int(w.TTL.Seconds())))
	req.Header.Set("Urgency", "high")
	req.Header.Set("Authorization", "vapid t="+jwt+", k="+w.public)
	res, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 4096))
	switch {
	case res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone:
		return notifications.ErrGone
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return nil
	}
	return fmt.Errorf("webpush: push service answered %d", res.StatusCode)
}

// jwt signs the VAPID token (ES256) for the push service's origin.
func (w *WebPush) jwt(aud string) (string, error) {
	b64 := base64.RawURLEncoding
	head := b64.EncodeToString([]byte(`{"typ":"JWT","alg":"ES256"}`))
	claims, err := json.Marshal(map[string]any{"aud": aud, "exp": w.Now().Add(12 * time.Hour).Unix(), "sub": w.subject})
	if err != nil {
		return "", err
	}
	signing := head + "." + b64.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	r, s, err := ecdsa.Sign(rand.Reader, w.key, sum[:])
	if err != nil {
		return "", err
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return signing + "." + b64.EncodeToString(sig), nil
}

// recordSize is the aes128gcm record size; one record holds a push.
const recordSize = 4096

// Encrypt encrypts plain for a subscription's p256dh and auth (base64url)
// as RFC 8291 describes, in one aes128gcm record.
func Encrypt(plain []byte, p256dh, auth string) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	as, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return encrypt(plain, p256dh, auth, salt, as)
}

func decodeKey(s string) ([]byte, error) {
	if b, err := base64.RawURLEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.URLEncoding.DecodeString(s)
}

func encrypt(plain []byte, p256dh, auth string, salt []byte, as *ecdh.PrivateKey) ([]byte, error) {
	uaRaw, err := decodeKey(p256dh)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	secret, err := decodeKey(auth)
	if err != nil || len(secret) != 16 {
		return nil, errors.New("auth: want 16 bytes")
	}
	ua, err := ecdh.P256().NewPublicKey(uaRaw)
	if err != nil {
		return nil, fmt.Errorf("p256dh: %w", err)
	}
	if len(plain)+1+16 > recordSize-86 {
		return nil, errors.New("payload too large")
	}
	shared, err := as.ECDH(ua)
	if err != nil {
		return nil, err
	}
	asPub := as.PublicKey().Bytes()
	cek, nonce, err := keys(shared, secret, uaRaw, asPub, salt)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// The last (only) record ends with the 0x02 delimiter.
	sealed := gcm.Seal(nil, nonce, append(append([]byte{}, plain...), 2), nil)
	out := make([]byte, 0, 86+len(sealed))
	out = append(out, salt...)
	out = binary.BigEndian.AppendUint32(out, recordSize)
	out = append(out, byte(len(asPub)))
	out = append(out, asPub...)
	return append(out, sealed...), nil
}

// keys derives the content encryption key and nonce (RFC 8291 section 3.4).
func keys(shared, auth, uaPub, asPub, salt []byte) (cek, nonce []byte, err error) {
	prkKey, err := hkdf.Extract(sha256.New, shared, auth)
	if err != nil {
		return nil, nil, err
	}
	info := "WebPush: info\x00" + string(uaPub) + string(asPub)
	ikm, err := hkdf.Expand(sha256.New, prkKey, info, 32)
	if err != nil {
		return nil, nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, nil, err
	}
	if cek, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16); err != nil {
		return nil, nil, err
	}
	nonce, err = hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	return cek, nonce, err
}
