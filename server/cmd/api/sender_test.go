package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/config"
)

func TestNewSender(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	if _, ok := newSender(config.Config{EmailProvider: config.EmailNone}, log).(auth.DisabledSender); !ok {
		t.Error("none: want DisabledSender")
	}
	if _, ok := newSender(config.Config{EmailProvider: config.EmailLog}, log).(auth.LogSender); !ok {
		t.Error("log: want LogSender")
	}
	s, ok := newSender(config.Config{EmailProvider: config.EmailResend, ResendAPIKey: "re_x", EmailFrom: "a@b.test",
		AppURL: "https://app.example"}, log).(auth.ResendSender)
	if !ok || s.APIKey != "re_x" || s.From != "a@b.test" || s.AppURL != "https://app.example" {
		t.Errorf("resend: got %#v", s)
	}
}

func TestCheckEmailProvider(t *testing.T) {
	resend := config.Config{EmailProvider: config.EmailResend}
	for _, tc := range []struct {
		cfg         config.Config
		env         string
		rateLimited bool
		refuse      bool
	}{
		{resend, "production", false, true},
		{resend, "production", true, false},
		{resend, "", false, false},
		{resend, "development", false, false},
		{config.Config{EmailProvider: config.EmailNone}, "production", false, false},
		{config.Config{EmailProvider: config.EmailLog}, "production", false, false},
	} {
		err := checkEmailProvider(tc.cfg, tc.env, tc.rateLimited)
		if got := errors.Is(err, errUnthrottledEmail); got != tc.refuse {
			t.Errorf("%s in %q, rate limited %v: err %v, want refusal %v", tc.cfg.EmailProvider, tc.env,
				tc.rateLimited, err, tc.refuse)
		}
	}
}

func TestRunRefusesUnthrottledResendInProduction(t *testing.T) {
	if authRateLimited {
		t.Skip("this build rate limits /v1/auth/email")
	}
	env := map[string]string{"APP_ENV": "production", "EMAIL_PROVIDER": "resend", "RESEND_API_KEY": "re_x",
		"EMAIL_FROM": "a@b.test", "HTTP_ADDR": "127.0.0.1:0"}
	err := run(context.Background(), func(k string) string { return env[k] }, io.Discard, nil)
	if !errors.Is(err, errUnthrottledEmail) {
		t.Fatalf("run: %v, want errUnthrottledEmail", err)
	}
}
