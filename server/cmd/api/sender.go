package main

import (
	"errors"
	"log/slog"
	"reflect"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/config"
	"github.com/aatrey56/myfantasyprem/server/internal/httpapi"
)

// newSender picks the magic-link sender from EMAIL_PROVIDER.
func newSender(cfg config.Config, logger *slog.Logger) auth.Sender {
	switch cfg.EmailProvider {
	case config.EmailResend:
		return auth.ResendSender{APIKey: cfg.ResendAPIKey, From: cfg.EmailFrom, AppURL: cfg.AppURL, Log: logger}
	case config.EmailLog:
		return auth.LogSender{Log: logger, AppURL: cfg.AppURL}
	}
	return auth.DisabledSender{Log: logger}
}

// authRateLimited reports whether this build rate limits POST
// /v1/auth/email. The limiter arrives with the fix/security branch, which
// adds httpapi.Options.RateLimits; looking for that field means merging
// the branch lifts checkEmailProvider's refusal without another edit.
var authRateLimited = func() bool {
	_, ok := reflect.TypeFor[httpapi.Options]().FieldByName("RateLimits")
	return ok
}()

// errUnthrottledEmail is why production refuses real email without rate limits.
var errUnthrottledEmail = errors.New("EMAIL_PROVIDER=resend with APP_ENV=production needs rate limiting " +
	"on POST /v1/auth/email, and this build has none: anyone could spend the Resend quota and mail " +
	"any address. Merge fix/security first, or set EMAIL_PROVIDER=none (docs/DEPLOY.md)")

// checkEmailProvider refuses to send real email from an unthrottled
// production server.
func checkEmailProvider(cfg config.Config, appEnv string, rateLimited bool) error {
	if cfg.EmailProvider == config.EmailResend && appEnv == "production" && !rateLimited {
		return errUnthrottledEmail
	}
	return nil
}
