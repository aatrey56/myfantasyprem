package auth

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestResendSenderPostsEmail(t *testing.T) {
	var got resendEmail
	var hdr http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/emails" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		hdr = r.Header.Clone()
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Error(err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"id":"email_123"}`)
	}))
	defer srv.Close()

	s := ResendSender{APIKey: "re_test", From: "App <login@example.test>", AppURL: "https://app.example.test",
		BaseURL: srv.URL, Log: slog.New(slog.DiscardHandler)}
	m := MagicLink{Email: "a@b.test", Token: "tok+/=", Code: "042917", Next: "/join/DERBY7XQ", ExpiresAt: time.Now().Add(15 * time.Minute)}
	if err := s.SendMagicLink(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	if hdr.Get("Authorization") != "Bearer re_test" || hdr.Get("Idempotency-Key") == "" ||
		strings.Contains(hdr.Get("Idempotency-Key"), "tok") {
		t.Fatalf("headers: %v", hdr)
	}
	// The link carries where to go after signing in.
	link := "https://app.example.test/sign-in?token=tok%2B%2F%3D&amp;next=%2Fjoin%2FDERBY7XQ"
	if got.From != s.From || len(got.To) != 1 || got.To[0] != "a@b.test" ||
		!strings.Contains(got.Text, strings.ReplaceAll(link, "&amp;", "&")) ||
		!strings.Contains(got.HTML, link) || !strings.Contains(got.Text, "15 minutes") || got.Subject == "" {
		t.Fatalf("email: %+v", got)
	}
	// The code is in both bodies, for typing in on another device.
	if !strings.Contains(got.Text, "042917") || !strings.Contains(got.HTML, "042917") {
		t.Fatalf("no code in the email: %+v", got)
	}
}

// The link without a destination has no next parameter.
func TestMagicLinkURL(t *testing.T) {
	m := MagicLink{Token: "a b"}
	if got := m.URL("https://x.test"); got != "https://x.test/sign-in?token=a+b" {
		t.Errorf("url = %q", got)
	}
	m.Next = "/leagues/lg_1"
	if got := m.URL("https://x.test"); got != "https://x.test/sign-in?token=a+b&next=%2Fleagues%2Flg_1" {
		t.Errorf("url with next = %q", got)
	}
}

// The development sender logs the link and the code (never the address).
func TestLogSenderLogsLinkAndCode(t *testing.T) {
	var logs strings.Builder
	s := LogSender{Log: slog.New(slog.NewTextHandler(&logs, nil)), AppURL: "https://x.test"}
	m := MagicLink{Email: "a@b.test", Token: "tok", Code: "042917", Next: "/join/DERBY7XQ", ExpiresAt: time.Now()}
	if err := s.SendMagicLink(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	out := logs.String()
	if !strings.Contains(out, "code=042917") || !strings.Contains(out, "next=%2Fjoin%2FDERBY7XQ") ||
		strings.Contains(out, "a@b.test") {
		t.Errorf("log = %s", out)
	}
}

func TestResendSenderRetriesOnceThenFails(t *testing.T) {
	var calls atomic.Int32
	keys := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		keys <- r.Header.Get("Idempotency-Key")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, `{"message":"boom"}`)
	}))
	defer srv.Close()
	s := ResendSender{APIKey: "k", From: "f@x.test", AppURL: "https://x.test", BaseURL: srv.URL}
	err := s.SendMagicLink(context.Background(), MagicLink{Email: "a@b.test", Token: "t", Code: "123456", ExpiresAt: time.Now().Add(time.Minute)})
	if err == nil || !strings.Contains(err.Error(), "500") || calls.Load() != 2 {
		t.Fatalf("err %v after %d calls", err, calls.Load())
	}
	if a, b := <-keys, <-keys; a != b {
		t.Fatalf("idempotency keys differ: %q %q", a, b)
	}
}

func TestResendSenderDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, `{"message":"domain not verified"}`)
	}))
	defer srv.Close()
	s := ResendSender{APIKey: "k", From: "f@x.test", AppURL: "https://x.test", BaseURL: srv.URL}
	err := s.SendMagicLink(context.Background(), MagicLink{Email: "a@b.test", Token: "t", Code: "123456", ExpiresAt: time.Now().Add(time.Minute)})
	if err == nil || !strings.Contains(err.Error(), "domain not verified") || calls.Load() != 1 {
		t.Fatalf("err %v after %d calls", err, calls.Load())
	}
}

// hangingResend blocks each request until the client gives up, or answers
// 200 once hang reaches 0.
func hangingResend(t *testing.T, hang int32, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body) // lets the server notice the client hanging up
		if calls.Add(1) <= hang {
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{"id":"email_1"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestResendSenderBoundsTheWholeSend(t *testing.T) {
	var calls atomic.Int32
	srv := hangingResend(t, 2, &calls)
	s := ResendSender{APIKey: "k", From: "f@x.test", AppURL: "https://x.test", BaseURL: srv.URL,
		Timeout: 400 * time.Millisecond}
	start := time.Now()
	err := s.SendMagicLink(context.Background(), MagicLink{Email: "a@b.test", Token: "t", Code: "123456", ExpiresAt: time.Now().Add(time.Minute)})
	if took := time.Since(start); err == nil || took > 600*time.Millisecond {
		t.Fatalf("err %v after %v; want an error within the 400ms budget", err, took)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d, want 2 (the retry fits in the budget)", calls.Load())
	}
}

func TestResendSenderRetriesAfterAHungAttempt(t *testing.T) {
	var calls atomic.Int32
	srv := hangingResend(t, 1, &calls)
	s := ResendSender{APIKey: "k", From: "f@x.test", AppURL: "https://x.test", BaseURL: srv.URL,
		Timeout: time.Second}
	if err := s.SendMagicLink(context.Background(), MagicLink{Email: "a@b.test", Token: "t", Code: "123456", ExpiresAt: time.Now().Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls %d, want 2", calls.Load())
	}
}

// The default budget, retry and backoff included, must stay under the API
// server's 15 second WriteTimeout (cmd/api) with room for the rest of the
// request.
func TestResendTimeoutFitsTheWriteTimeout(t *testing.T) {
	if ResendTimeout > 8*time.Second {
		t.Fatalf("ResendTimeout %v; keep it at most 8s", ResendTimeout)
	}
}
