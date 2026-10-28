package auth

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ResendBaseURL is Resend's API origin (https://resend.com/docs/api-reference).
const ResendBaseURL = "https://api.resend.com"

// ResendSender mails magic links through Resend's HTTP API
// (EMAIL_PROVIDER=resend). The From address must be on a domain verified
// in Resend.
type ResendSender struct {
	// APIKey is a Resend API key with sending access (RESEND_API_KEY).
	APIKey string
	// From is the sender, for example "MyFantasyPrem <login@example.com>".
	From string
	// AppURL is the web app's origin; the link opens /sign-in?token=... there.
	AppURL string
	// AppName names the app in the subject and body. Default "MyFantasyPrem".
	AppName string
	// BaseURL overrides ResendBaseURL (tests point it at httptest).
	BaseURL string
	// Client defaults to http.DefaultClient; Timeout bounds every send.
	Client *http.Client
	// Timeout bounds the whole send, retry and backoff included, so the
	// sign-in request answers well inside the API's 15 second
	// WriteTimeout. Default ResendTimeout.
	Timeout time.Duration
	Log     *slog.Logger
}

type resendEmail struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

// ResendTimeout is the default bound on one SendMagicLink call.
const ResendTimeout = 8 * time.Second

// SendMagicLink posts one email to Resend. It retries once on 429, 5xx and
// transport errors (with the same Idempotency-Key, so Resend never sends
// twice) and returns an error otherwise. The first attempt gets half of
// Timeout, the retry what is left. Neither the token, the code nor the link is logged.
func (s ResendSender) SendMagicLink(ctx context.Context, m MagicLink) error {
	email, token := m.Email, m.Token
	name := s.AppName
	if name == "" {
		name = "MyFantasyPrem"
	}
	link := m.URL(s.AppURL)
	mins := max(1, int(time.Until(m.ExpiresAt).Round(time.Minute)/time.Minute))
	body, err := json.Marshal(resendEmail{
		From:    s.From,
		To:      []string{email},
		Subject: "Your " + name + " sign-in link and code",
		Text: fmt.Sprintf("Sign in to %s:\n\n%s\n\nOr enter this code in the app: %s\n\n"+
			"The link and the code work once and expire in %d minutes. "+
			"If you did not ask for them, ignore this email.\n", name, link, m.Code, mins),
		HTML: fmt.Sprintf(`<p>Sign in to %s:</p><p><a href="%s">Sign in</a></p>`+
			`<p>Or enter this code in the app:</p>`+
			`<p style="font-size:28px;font-weight:bold;letter-spacing:6px;font-family:monospace">%s</p>`+
			`<p>The link and the code work once and expire in %d minutes. If you did not ask for them, ignore this email.</p>`,
			html.EscapeString(name), html.EscapeString(link), html.EscapeString(m.Code), mins),
	})
	if err != nil {
		return err
	}
	// The key identifies this token, not the token itself.
	sum := sha256.Sum256([]byte(token))
	key := "magic-link-" + hex.EncodeToString(sum[:12])

	base := strings.TrimRight(s.BaseURL, "/")
	if base == "" {
		base = ResendBaseURL
	}
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	budget := s.Timeout
	if budget <= 0 {
		budget = ResendTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, budget)
	defer cancel()
	deadline, _ := ctx.Deadline()
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return s.failed(ctx, last)
			case <-time.After(min(time.Second, budget/8)):
			}
		}
		attemptTimeout := time.Until(deadline)
		if attempt == 0 {
			attemptTimeout /= 2
		}
		status, msg, err := s.post(ctx, client, base+"/emails", key, body, attemptTimeout)
		if err != nil {
			last = fmt.Errorf("resend: %w", err)
			continue
		}
		if status >= 200 && status < 300 {
			if s.Log != nil {
				var ok struct {
					ID string `json:"id"`
				}
				_ = json.Unmarshal(msg, &ok)
				s.Log.InfoContext(ctx, "magic link sent", "provider", "resend", "email_id", ok.ID)
			}
			return nil
		}
		last = fmt.Errorf("resend: status %d: %s", status, strings.TrimSpace(string(msg)))
		if status != http.StatusTooManyRequests && status < 500 {
			break
		}
	}
	return s.failed(ctx, last)
}

// failed logs and returns the last error of a send.
func (s ResendSender) failed(ctx context.Context, last error) error {
	if last == nil {
		last = fmt.Errorf("resend: %w", ctx.Err())
	}
	if s.Log != nil {
		s.Log.ErrorContext(ctx, "magic link not sent", "provider", "resend", "err", last)
	}
	return last
}

// post makes one attempt, bounded by timeout.
func (s ResendSender) post(ctx context.Context, client *http.Client, u, key string, body []byte,
	timeout time.Duration) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+s.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", key)
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	return resp.StatusCode, msg, nil
}
