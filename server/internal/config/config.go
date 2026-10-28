// Package config loads server settings from environment variables.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
)

// Poller sources.
const (
	SourceFPL  = "fpl"  // FPL Draft's public API
	SourceDemo = "demo" // the embedded demo world (internal/demo); never calls FPL
)

// Environments (APP_ENV).
const (
	EnvDevelopment = "development"
	EnvTest        = "test"
	EnvProduction  = "production"
)

// DevAppURL is the Expo web dev server, the default APP_URL.
const DevAppURL = "http://localhost:8081"

// Config is the process configuration. Every field has a safe local default.
type Config struct {
	// AppEnv is development, test or production, from APP_ENV (default
	// development). Production refuses to start on settings only safe
	// locally (see Load).
	AppEnv string
	// Addr is the listen address, from HTTP_ADDR. Default ":8080".
	Addr string
	// LogLevel is from LOG_LEVEL (debug, info, warn, error). Default info.
	LogLevel slog.Level
	// LogJSON switches the log handler to JSON, from LOG_FORMAT=json.
	LogJSON bool
	// CORSAllowedOrigins is a comma separated list from CORS_ALLOWED_ORIGINS.
	// Default allows the Expo web dev server.
	CORSAllowedOrigins []string
	// ShutdownTimeout bounds graceful shutdown, from SHUTDOWN_TIMEOUT.
	ShutdownTimeout time.Duration
	// DatabaseURL is from DATABASE_URL. Empty runs every store in memory
	// (development only: data is lost on restart).
	DatabaseURL string
	// MigrateOnStart applies pending migrations at startup, from
	// MIGRATE_ON_START=true. Otherwise run `go run ./cmd/migrate up`.
	MigrateOnStart bool
	// SeedDemo loads the demo world and league into the in-memory stores
	// when DATABASE_URL is empty, from SEED_DEMO (default true). With a
	// database, run `go run ./cmd/seed` instead.
	SeedDemo bool
	// DevMagicLinks logs email sign-in tokens instead of sending mail, from
	// AUTH_DEV_MAGIC_LINKS=true. Development only: anyone who can read the
	// logs can sign in as anyone.
	DevMagicLinks bool
	// ClaimLinkSignIn lets a seat's claim link sign a signed-out person in
	// with only a display name (docs/adr/0013), from CLAIM_LINK_SIGNIN.
	// Default on, except with APP_ENV=production, where it must be set to
	// true explicitly and cmd/api logs a warning at startup: anyone holding
	// a seat's link gets an account and that seat, without an email.
	ClaimLinkSignIn bool
	// AppURL is the web app's public origin, from APP_URL: magic links open
	// its /sign-in and invite links its /join/{code}. Default DevAppURL;
	// required in production.
	AppURL string
	// PollerEnabled runs the stats poller in this process, from
	// POLLER_ENABLED=true. Run it in exactly one process.
	PollerEnabled bool
	// PollerSource is fpl or demo, from POLLER_SOURCE. Default fpl.
	PollerSource string
	// FPLBaseURL and FPLUserAgent override the FPL client's defaults, from
	// FPL_BASE_URL and FPL_USER_AGENT.
	FPLBaseURL   string
	FPLUserAgent string
	// FPLShadowUntil ends the scheduled read-only checks of imported
	// leagues against FPL (the switch-over), from FPL_SHADOW_UNTIL as a
	// date (2027-01-01) or RFC 3339 time. Zero: they run all season.
	FPLShadowUntil time.Time
	// ResyncDisabledLeagues are the league ids (lg_...) whose commissioner
	// may not re-sync from FPL, from FPL_RESYNC_DISABLED_LEAGUES, comma
	// separated. Empty: every imported league may.
	ResyncDisabledLeagues []string
	// MetricsAddr serves expvar metrics (/debug/vars) on a separate
	// listener, from METRICS_ADDR, for example 127.0.0.1:9090. Empty: off.
	MetricsAddr string
	// JobsEnabled works the scheduled jobs (draft clocks, waiver runs,
	// trade windows, the tick) in this process, from JOBS_ENABLED (default
	// true). On Postgres any number of instances may; every instance
	// schedules jobs either way.
	JobsEnabled bool
	// JobsTick is the periodic tick that catches every due deadline, from
	// JOBS_TICK (default 15s).
	JobsTick time.Duration
	// RealtimeURL is the WebSocket URL realtime tickets point at, from
	// REALTIME_URL (for example wss://api.example.com/v1/realtime). Empty:
	// derived from each ticket request's host.
	RealtimeURL string
	// SnapshotsKeep is how many raw stat snapshots to keep per kind and
	// gameweek (final ones are always kept), from SNAPSHOTS_KEEP (default 50).
	SnapshotsKeep int
	// AppleClientIDs are the accepted audiences of Sign in with Apple
	// identity tokens (the iOS bundle id, the web Services ID), from
	// APPLE_CLIENT_IDS, comma separated. Empty: Apple sign-in answers 503.
	AppleClientIDs []string
	// GoogleClientIDs are the accepted OAuth client ids of Google ID
	// tokens, from GOOGLE_CLIENT_IDS. Empty: Google sign-in answers 503.
	GoogleClientIDs []string
	// Apple token revocation (App Store guideline 5.1.1(v)): the Sign in
	// with Apple key that signs client secrets, from APPLE_TEAM_ID,
	// APPLE_KEY_ID and APPLE_PRIVATE_KEY (the .p8 PEM; literal \n allowed),
	// and the 32-byte key that seals stored refresh tokens, from
	// APPLE_TOKEN_KEY (base64). All four or none; none disables code
	// exchange and revocation (required before App Store submission).
	AppleTeamID, AppleKeyID string
	ApplePrivateKey         []byte
	AppleTokenKey           []byte
	// TrustedProxies are the CIDRs (or single addresses) of the reverse
	// proxies in front of the API, from TRUSTED_PROXIES, comma separated.
	// Only a connection from one of them has X-Forwarded-For read for the
	// client address that rate limits key on. Empty: never read.
	TrustedProxies []netip.Prefix
	// EmailProvider delivers magic links, from EMAIL_PROVIDER: resend
	// (Resend's API), log (log them; development only) or none. Default:
	// log with AUTH_DEV_MAGIC_LINKS=true, otherwise none.
	EmailProvider string
	// ResendAPIKey is from RESEND_API_KEY; EmailFrom (EMAIL_FROM) is the
	// sender on a domain verified in Resend. Both required with resend.
	ResendAPIKey string
	EmailFrom    string
	// BrandedAssets serves official club crests, kit images and player
	// photos through /v1/assets (fetched upstream once, then cached), from
	// BRANDED_ASSETS. Default on, except with APP_ENV=production, where it
	// must be set explicitly: off for App Store and other public builds
	// (Apple's intellectual-property guideline).
	BrandedAssets bool
	// VAPIDPublicKey, VAPIDPrivateKey and VAPIDSubject turn on Web Push
	// (RFC 8292), from VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY (base64url;
	// `npm run admin -- vapid-keys` makes a pair) and VAPID_SUBJECT (a
	// mailto: or https: contact for push services). All three or none.
	VAPIDPublicKey  string
	VAPIDPrivateKey string
	VAPIDSubject    string
	// AssetsCacheDir is where fetched images are kept, from
	// ASSETS_CACHE_DIR. Default: a directory under the OS temp dir.
	AssetsCacheDir string
	// Upstream image URL templates, each a comma separated list tried in
	// order; {code} is the FPL element or team code. From
	// ASSETS_PLAYER_PHOTO_URLS, ASSETS_CLUB_CREST_URLS,
	// ASSETS_CLUB_SHIRT_URLS and ASSETS_CLUB_GK_SHIRT_URLS. Empty: the
	// assets package defaults.
	AssetsPlayerPhotoURLs, AssetsClubCrestURLs []string
	AssetsClubShirtURLs, AssetsClubGKShirtURLs []string
	// UpdatesDir is the folder `npm run updates:publish` writes the native
	// app's over-the-air updates to, served under /updates/ (docs/IOS.md),
	// from UPDATES_DIR. Empty: no updates endpoint.
	UpdatesDir string
	// UpdatesPublicURL is the absolute base URL (scheme, host, optional
	// path) the update manifest's asset urls start with, from
	// UPDATES_PUBLIC_URL; needed behind a TLS-terminating proxy in
	// production, where forwarded headers are not trusted. Empty: the
	// request's own scheme and host (the tailnet setup).
	UpdatesPublicURL string
}

// AppleRevocationConfigured reports whether the Apple token settings are set.
func (c Config) AppleRevocationConfigured() bool { return c.AppleTeamID != "" }

// loadApple reads the Apple token revocation settings: all or none.
func loadApple(cfg *Config, getenv func(string) string) error {
	vals := map[string]string{}
	set := 0
	for _, k := range []string{"APPLE_TEAM_ID", "APPLE_KEY_ID", "APPLE_PRIVATE_KEY", "APPLE_TOKEN_KEY"} {
		if vals[k] = strings.TrimSpace(getenv(k)); vals[k] != "" {
			set++
		}
	}
	if set == 0 {
		return nil
	}
	if set != 4 {
		return errors.New("APPLE_TEAM_ID, APPLE_KEY_ID, APPLE_PRIVATE_KEY and APPLE_TOKEN_KEY: set all four or none")
	}
	key, err := base64.StdEncoding.DecodeString(vals["APPLE_TOKEN_KEY"])
	if err != nil || len(key) != 32 {
		return errors.New("APPLE_TOKEN_KEY: want 32 random bytes, base64 (openssl rand -base64 32)")
	}
	cfg.AppleTeamID, cfg.AppleKeyID, cfg.AppleTokenKey = vals["APPLE_TEAM_ID"], vals["APPLE_KEY_ID"], key
	cfg.ApplePrivateKey = []byte(strings.ReplaceAll(vals["APPLE_PRIVATE_KEY"], `\n`, "\n"))
	return nil
}

// Production reports APP_ENV=production.
func (c Config) Production() bool { return c.AppEnv == EnvProduction }

// Email providers.
const (
	EmailResend = "resend"
	EmailLog    = "log"
	EmailNone   = "none"
)

// list splits a comma-separated variable, dropping blanks.
func list(v string) []string {
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func boolEnv(getenv func(string) string, key string, def bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(getenv(key))) {
	case "":
		return def, nil
	case "true", "1", "yes":
		return true, nil
	case "false", "0", "no":
		return false, nil
	}
	return false, fmt.Errorf("%s: want true or false, got %q", key, getenv(key))
}

// Load reads configuration through getenv (os.Getenv in production).
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		Addr:               ":8080",
		LogLevel:           slog.LevelInfo,
		CORSAllowedOrigins: []string{DevAppURL},
		ShutdownTimeout:    10 * time.Second,
		DatabaseURL:        getenv("DATABASE_URL"),
		AppURL:             DevAppURL,
		PollerSource:       SourceFPL,
		FPLBaseURL:         getenv("FPL_BASE_URL"),
		FPLUserAgent:       getenv("FPL_USER_AGENT"),
		MetricsAddr:        getenv("METRICS_ADDR"),
		UpdatesDir:         strings.TrimSpace(getenv("UPDATES_DIR")),
	}
	if v := getenv("HTTP_ADDR"); v != "" {
		cfg.Addr = v
	}
	if v := getenv("LOG_LEVEL"); v != "" {
		if err := cfg.LogLevel.UnmarshalText([]byte(v)); err != nil {
			return Config{}, fmt.Errorf("LOG_LEVEL: %w", err)
		}
	}
	cfg.LogJSON = strings.EqualFold(getenv("LOG_FORMAT"), "json")
	cfg.DevMagicLinks = getenv("AUTH_DEV_MAGIC_LINKS") == "true"
	if v := strings.TrimSpace(getenv("APP_URL")); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return Config{}, fmt.Errorf("APP_URL: want the web app's http(s) origin, like https://app.example.com, got %q", v)
		}
		cfg.AppURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(getenv("UPDATES_PUBLIC_URL")); v != "" {
		u, err := url.Parse(v)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" {
			return Config{}, fmt.Errorf("UPDATES_PUBLIC_URL: want the API's absolute http(s) base URL, like https://api.example.com, got %q", v)
		}
		cfg.UpdatesPublicURL = strings.TrimRight(v, "/")
	}
	if v := getenv("CORS_ALLOWED_ORIGINS"); v != "" {
		cfg.CORSAllowedOrigins = nil
		for _, o := range strings.Split(v, ",") {
			if o = strings.TrimSpace(o); o != "" {
				cfg.CORSAllowedOrigins = append(cfg.CORSAllowedOrigins, o)
			}
		}
	}
	if v := strings.TrimSpace(getenv("FPL_SHADOW_UNTIL")); v != "" {
		t, err := time.Parse(time.RFC3339, v)
		if err != nil {
			if t, err = time.Parse(time.DateOnly, v); err != nil {
				return Config{}, fmt.Errorf("FPL_SHADOW_UNTIL: want a date or RFC 3339 time, got %q", v)
			}
		}
		cfg.FPLShadowUntil = t.UTC()
	}
	cfg.ResyncDisabledLeagues = list(getenv("FPL_RESYNC_DISABLED_LEAGUES"))
	for _, id := range cfg.ResyncDisabledLeagues {
		if !ids.Valid("lg", id) {
			return Config{}, fmt.Errorf("FPL_RESYNC_DISABLED_LEAGUES: want comma-separated lg_ ids, got %q", id)
		}
	}
	if v := getenv("SHUTDOWN_TIMEOUT"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return Config{}, fmt.Errorf("SHUTDOWN_TIMEOUT: want a positive duration, got %q", v)
		}
		cfg.ShutdownTimeout = d
	}
	var err error
	if cfg.MigrateOnStart, err = boolEnv(getenv, "MIGRATE_ON_START", false); err != nil {
		return Config{}, err
	}
	if cfg.SeedDemo, err = boolEnv(getenv, "SEED_DEMO", true); err != nil {
		return Config{}, err
	}
	if cfg.PollerEnabled, err = boolEnv(getenv, "POLLER_ENABLED", false); err != nil {
		return Config{}, err
	}
	if cfg.JobsEnabled, err = boolEnv(getenv, "JOBS_ENABLED", true); err != nil {
		return Config{}, err
	}
	cfg.JobsTick = 15 * time.Second
	if v := getenv("JOBS_TICK"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < time.Second {
			return Config{}, fmt.Errorf("JOBS_TICK: want a duration of at least 1s, got %q", v)
		}
		cfg.JobsTick = d
	}
	cfg.RealtimeURL = strings.TrimSpace(getenv("REALTIME_URL"))
	cfg.AppleClientIDs = list(getenv("APPLE_CLIENT_IDS"))
	cfg.GoogleClientIDs = list(getenv("GOOGLE_CLIENT_IDS"))
	if err := loadApple(&cfg, getenv); err != nil {
		return Config{}, err
	}
	for _, s := range list(getenv("TRUSTED_PROXIES")) {
		p, err := netip.ParsePrefix(s)
		if err != nil {
			a, aerr := netip.ParseAddr(s)
			if aerr != nil {
				return Config{}, fmt.Errorf("TRUSTED_PROXIES: want CIDRs or addresses, got %q", s)
			}
			p = netip.PrefixFrom(a, a.BitLen())
		}
		cfg.TrustedProxies = append(cfg.TrustedProxies, p.Masked())
	}
	cfg.SnapshotsKeep = 50
	if v := getenv("SNAPSHOTS_KEEP"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			return Config{}, fmt.Errorf("SNAPSHOTS_KEEP: want a positive integer, got %q", v)
		}
		cfg.SnapshotsKeep = n
	}
	if v := getenv("POLLER_SOURCE"); v != "" {
		if v != SourceFPL && v != SourceDemo {
			return Config{}, fmt.Errorf("POLLER_SOURCE: want fpl or demo, got %q", v)
		}
		cfg.PollerSource = v
	}
	if err := loadEmail(getenv, &cfg); err != nil {
		return Config{}, err
	}
	cfg.AppEnv = EnvDevelopment
	if v := strings.TrimSpace(getenv("APP_ENV")); v != "" {
		if v != EnvDevelopment && v != EnvTest && v != EnvProduction {
			return Config{}, fmt.Errorf("APP_ENV: want development, test or production, got %q", v)
		}
		cfg.AppEnv = v
	}
	if cfg.ClaimLinkSignIn, err = boolEnv(getenv, "CLAIM_LINK_SIGNIN", cfg.AppEnv != EnvProduction); err != nil {
		return Config{}, err
	}
	if cfg.BrandedAssets, err = boolEnv(getenv, "BRANDED_ASSETS", true); err != nil {
		return Config{}, err
	}
	if err := loadAssets(getenv, &cfg); err != nil {
		return Config{}, err
	}
	cfg.VAPIDPublicKey = strings.TrimSpace(getenv("VAPID_PUBLIC_KEY"))
	cfg.VAPIDPrivateKey = strings.TrimSpace(getenv("VAPID_PRIVATE_KEY"))
	cfg.VAPIDSubject = strings.TrimSpace(getenv("VAPID_SUBJECT"))
	if set := cfg.VAPIDPublicKey != "" || cfg.VAPIDPrivateKey != "" || cfg.VAPIDSubject != ""; set &&
		(cfg.VAPIDPublicKey == "" || cfg.VAPIDPrivateKey == "" || cfg.VAPIDSubject == "") {
		return Config{}, fmt.Errorf("VAPID_PUBLIC_KEY, VAPID_PRIVATE_KEY and VAPID_SUBJECT: set all three for Web Push, or none")
	}
	if cfg.Production() {
		if err := productionReady(cfg, getenv("CORS_ALLOWED_ORIGINS"), getenv("APP_URL"), getenv("BRANDED_ASSETS")); err != nil {
			return Config{}, err
		}
	}
	return cfg, nil
}

// productionReady refuses the settings that are only safe locally.
func productionReady(cfg Config, corsEnv, appURLEnv, brandedEnv string) error {
	var problems []string
	if strings.TrimSpace(brandedEnv) == "" {
		problems = append(problems, "BRANDED_ASSETS must be set to true or false (false for App Store and other public builds)")
	}
	if cfg.DevMagicLinks {
		problems = append(problems, "AUTH_DEV_MAGIC_LINKS must be off (it logs sign-in links: anyone with the logs could sign in as anyone)")
	}
	if cfg.EmailProvider == EmailLog {
		problems = append(problems, "EMAIL_PROVIDER must not be log (it logs sign-in links: anyone with the logs could sign in as anyone)")
	}
	if cfg.DatabaseURL == "" {
		problems = append(problems, "DATABASE_URL is required (memory mode loses everything on restart)")
	}
	if cfg.RealtimeURL == "" {
		problems = append(problems, "REALTIME_URL is required (the ticket URL must not come from request headers)")
	}
	if strings.TrimSpace(appURLEnv) == "" {
		problems = append(problems, "APP_URL is required (sign-in and invite links would point at the local dev server)")
	}
	if strings.TrimSpace(corsEnv) == "" {
		problems = append(problems, "CORS_ALLOWED_ORIGINS is required (the default is the local dev server)")
	} else if slices.Contains(cfg.CORSAllowedOrigins, "*") {
		problems = append(problems, "CORS_ALLOWED_ORIGINS must list origins, not *")
	}
	if len(problems) > 0 {
		return fmt.Errorf("APP_ENV=production refuses to start: %s", strings.Join(problems, "; "))
	}
	return nil
}

// loadAssets reads the image cache directory and upstream URL templates.
func loadAssets(getenv func(string) string, cfg *Config) error {
	cfg.AssetsCacheDir = strings.TrimSpace(getenv("ASSETS_CACHE_DIR"))
	for key, dst := range map[string]*[]string{
		"ASSETS_PLAYER_PHOTO_URLS":  &cfg.AssetsPlayerPhotoURLs,
		"ASSETS_CLUB_CREST_URLS":    &cfg.AssetsClubCrestURLs,
		"ASSETS_CLUB_SHIRT_URLS":    &cfg.AssetsClubShirtURLs,
		"ASSETS_CLUB_GK_SHIRT_URLS": &cfg.AssetsClubGKShirtURLs,
	} {
		for _, t := range list(getenv(key)) {
			u, err := url.Parse(strings.ReplaceAll(t, "{code}", "1"))
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || !strings.Contains(t, "{code}") {
				return fmt.Errorf("%s: want http(s) URL templates containing {code}, got %q", key, t)
			}
			*dst = append(*dst, t)
		}
	}
	return nil
}

func loadEmail(getenv func(string) string, cfg *Config) error {
	cfg.ResendAPIKey = strings.TrimSpace(getenv("RESEND_API_KEY"))
	cfg.EmailFrom = strings.TrimSpace(getenv("EMAIL_FROM"))
	switch v := strings.ToLower(strings.TrimSpace(getenv("EMAIL_PROVIDER"))); v {
	case "":
		cfg.EmailProvider = EmailNone
		if cfg.DevMagicLinks {
			cfg.EmailProvider = EmailLog
		}
	case EmailResend:
		if cfg.ResendAPIKey == "" || cfg.EmailFrom == "" {
			return fmt.Errorf("EMAIL_PROVIDER=resend needs RESEND_API_KEY and EMAIL_FROM")
		}
		cfg.EmailProvider = v
	case EmailLog, EmailNone:
		cfg.EmailProvider = v
	default:
		return fmt.Errorf("EMAIL_PROVIDER: want resend, log or none, got %q", v)
	}
	return nil
}
