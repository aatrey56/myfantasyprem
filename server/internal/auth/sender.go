package auth

import (
	"context"
	"log/slog"
	"net/url"
	"time"
)

// Sender delivers an email sign-in (link and code) to an email address:
// ResendSender in production (EMAIL_PROVIDER=resend), LogSender in
// development, DisabledSender when none is configured.
type Sender interface {
	SendMagicLink(ctx context.Context, m MagicLink) error
}

// MagicLink is one sign-in email.
type MagicLink struct {
	Email string
	// Token goes in the link, Code is typed in; either signs in once.
	Token, Code string
	// Next is where the app goes after signing in (a path checked by the
	// caller), carried in the link; "" for the default.
	Next      string
	ExpiresAt time.Time
}

// URL is the link that opens the app's sign-in screen with the token
// (and next).
func (m MagicLink) URL(appURL string) string {
	u := appURL + "/sign-in?token=" + url.QueryEscape(m.Token)
	if m.Next != "" {
		u += "&next=" + url.QueryEscape(m.Next)
	}
	return u
}

// LogSender logs the sign-in link instead of mailing it
// (AUTH_DEV_MAGIC_LINKS=true). Development only: anyone who can read the
// logs can sign in as anyone.
type LogSender struct {
	Log *slog.Logger
	// AppURL is the web app's origin; the link opens /sign-in?token=... there.
	AppURL string
}

// SendMagicLink logs the link, the raw token and the code (not the address).
func (s LogSender) SendMagicLink(ctx context.Context, m MagicLink) error {
	s.Log.InfoContext(ctx, "dev magic link: open the link, POST /v1/auth/email/verify with the token or type the code",
		"link", m.URL(s.AppURL), "token", m.Token, "code", m.Code, "expires_at", m.ExpiresAt)
	return nil
}

// DisabledSender sends nothing: no email provider is configured. Sign-in
// requests still answer 202 so the endpoint reveals nothing.
type DisabledSender struct{ Log *slog.Logger }

// SendMagicLink logs a warning without the token.
func (s DisabledSender) SendMagicLink(ctx context.Context, _ MagicLink) error {
	s.Log.WarnContext(ctx, "magic link not sent: no email provider configured (set AUTH_DEV_MAGIC_LINKS=true in development)")
	return nil
}
