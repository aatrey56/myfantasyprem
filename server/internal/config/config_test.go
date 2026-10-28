package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != ":8080" || cfg.LogLevel != slog.LevelInfo || cfg.ShutdownTimeout != 10*time.Second ||
		cfg.PollerEnabled || cfg.PollerSource != SourceFPL || !cfg.SeedDemo || cfg.MigrateOnStart || cfg.AppURL != "http://localhost:8081" {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
}

func TestLoadOverrides(t *testing.T) {
	cfg, err := Load(env(map[string]string{
		"HTTP_ADDR":            "127.0.0.1:9000",
		"LOG_LEVEL":            "debug",
		"LOG_FORMAT":           "json",
		"CORS_ALLOWED_ORIGINS": "https://a.example, https://b.example",
		"SHUTDOWN_TIMEOUT":     "3s",
		"AUTH_DEV_MAGIC_LINKS": "true",
		"POLLER_ENABLED":       "true",
		"POLLER_SOURCE":        "demo",
		"MIGRATE_ON_START":     "1",
		"SEED_DEMO":            "false",
		"APP_URL":              "https://app.example/",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Addr != "127.0.0.1:9000" || cfg.LogLevel != slog.LevelDebug || !cfg.LogJSON ||
		len(cfg.CORSAllowedOrigins) != 2 || cfg.ShutdownTimeout != 3*time.Second || !cfg.DevMagicLinks ||
		!cfg.PollerEnabled || cfg.PollerSource != SourceDemo || !cfg.MigrateOnStart || cfg.SeedDemo || cfg.AppURL != "https://app.example" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	for _, m := range []map[string]string{
		{"LOG_LEVEL": "loud"},
		{"SHUTDOWN_TIMEOUT": "soon"},
		{"SHUTDOWN_TIMEOUT": "-1s"},
		{"POLLER_ENABLED": "maybe"},
		{"POLLER_SOURCE": "scraper"},
		{"JOBS_ENABLED": "sometimes"},
		{"JOBS_TICK": "100ms"},
		{"SNAPSHOTS_KEEP": "0"},
		{"APP_URL": "app.example"},
		{"APP_URL": "ftp://app.example"},
		{"APP_URL": "https://"},
		{"APP_URL": "https://app.example/?x=1"},
		{"UPDATES_PUBLIC_URL": "api.example"},
		{"UPDATES_PUBLIC_URL": "ftp://api.example"},
		{"UPDATES_PUBLIC_URL": "https://"},
		{"UPDATES_PUBLIC_URL": "https://api.example/?x=1"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("want error for %v", m)
		}
	}
}

// UPDATES_PUBLIC_URL is the absolute base for OTA update urls; a trailing
// slash is dropped, and unset leaves the request-derived default.
func TestLoadUpdatesPublicURL(t *testing.T) {
	cfg, err := Load(env(map[string]string{"UPDATES_PUBLIC_URL": " https://api.example.com/mfp/ "}))
	if err != nil || cfg.UpdatesPublicURL != "https://api.example.com/mfp" {
		t.Fatalf("UpdatesPublicURL = %q, %v", cfg.UpdatesPublicURL, err)
	}
	if cfg, err := Load(env(nil)); err != nil || cfg.UpdatesPublicURL != "" {
		t.Fatalf("unset: %q, %v", cfg.UpdatesPublicURL, err)
	}
}

// prodEnv is a complete production configuration.
func prodEnv() map[string]string {
	return map[string]string{
		"APP_ENV":              "production",
		"DATABASE_URL":         "postgres://db/app",
		"REALTIME_URL":         "wss://api.example/v1/realtime",
		"CORS_ALLOWED_ORIGINS": "https://app.example",
		"APP_URL":              "https://app.example",
		"BRANDED_ASSETS":       "false",
	}
}

func TestLoadAppEnv(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.AppEnv != EnvDevelopment || cfg.Production() {
		t.Fatalf("default env = %q %v", cfg.AppEnv, err)
	}
	cfg, err = Load(env(prodEnv()))
	if err != nil || !cfg.Production() {
		t.Fatalf("production = %+v %v", cfg, err)
	}
	if _, err := Load(env(map[string]string{"APP_ENV": "staging"})); err == nil {
		t.Error("want an error for an unknown APP_ENV")
	}
}

// Production refuses to start on development settings.
func TestLoadRefusesUnsafeProduction(t *testing.T) {
	for name, change := range map[string]func(m map[string]string){
		"dev magic links":  func(m map[string]string) { m["AUTH_DEV_MAGIC_LINKS"] = "true" },
		"no database":      func(m map[string]string) { delete(m, "DATABASE_URL") },
		"no realtime url":  func(m map[string]string) { delete(m, "REALTIME_URL") },
		"no cors origins":  func(m map[string]string) { delete(m, "CORS_ALLOWED_ORIGINS") },
		"no app url":       func(m map[string]string) { delete(m, "APP_URL") },
		"wildcard origins": func(m map[string]string) { m["CORS_ALLOWED_ORIGINS"] = "https://app.example,*" },
		"logged email":     func(m map[string]string) { m["EMAIL_PROVIDER"] = "log" },
		"implicit assets":  func(m map[string]string) { delete(m, "BRANDED_ASSETS") },
	} {
		m := prodEnv()
		change(m)
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: production config accepted", name)
		}
		// The same settings are fine in development.
		m["APP_ENV"] = "development"
		if _, err := Load(env(m)); err != nil {
			t.Errorf("%s: development refused: %v", name, err)
		}
	}
}

// Apple token revocation needs all four settings or none.
func TestLoadAppleTokenRevocation(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || cfg.AppleRevocationConfigured() {
		t.Fatalf("default = %v %v", cfg.AppleRevocationConfigured(), err)
	}
	all := map[string]string{
		"APPLE_TEAM_ID": "TEAM123", "APPLE_KEY_ID": "KEY123",
		"APPLE_PRIVATE_KEY": `-----BEGIN PRIVATE KEY-----\nMIG...\n-----END PRIVATE KEY-----`,
		"APPLE_TOKEN_KEY":   "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8=",
	}
	cfg, err = Load(env(all))
	if err != nil || !cfg.AppleRevocationConfigured() || cfg.AppleTeamID != "TEAM123" || len(cfg.AppleTokenKey) != 32 ||
		!strings.Contains(string(cfg.ApplePrivateKey), "\nMIG...\n") {
		t.Fatalf("configured = %+v %v", cfg, err)
	}
	for _, missing := range []string{"APPLE_TEAM_ID", "APPLE_KEY_ID", "APPLE_PRIVATE_KEY", "APPLE_TOKEN_KEY"} {
		m := map[string]string{}
		for k, v := range all {
			if k != missing {
				m[k] = v
			}
		}
		if _, err := Load(env(m)); err == nil {
			t.Errorf("without %s: accepted", missing)
		}
	}
	all["APPLE_TOKEN_KEY"] = "c2hvcnQ="
	if _, err := Load(env(all)); err == nil {
		t.Error("a short APPLE_TOKEN_KEY was accepted")
	}
}

// Production runs with EMAIL_PROVIDER=none until email sign-in is set up.
func TestLoadProductionAcceptsEmailNone(t *testing.T) {
	m := prodEnv()
	m["EMAIL_PROVIDER"] = "none"
	cfg, err := Load(env(m))
	if err != nil || cfg.EmailProvider != EmailNone {
		t.Fatalf("provider = %q, %v", cfg.EmailProvider, err)
	}
}

func TestLoadResyncDisabledLeagues(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || len(cfg.ResyncDisabledLeagues) != 0 {
		t.Fatalf("default = %v %v", cfg.ResyncDisabledLeagues, err)
	}
	cfg, err = Load(env(map[string]string{"FPL_RESYNC_DISABLED_LEAGUES": " lg_01k7m2xq8rfv3c9a4t6wbn5hdz, lg_01k7m2xq8rfv3c9a4t6wbn5hdy ,"}))
	if err != nil || strings.Join(cfg.ResyncDisabledLeagues, "|") != "lg_01k7m2xq8rfv3c9a4t6wbn5hdz|lg_01k7m2xq8rfv3c9a4t6wbn5hdy" {
		t.Fatalf("leagues = %q %v", cfg.ResyncDisabledLeagues, err)
	}
	// A typo would silently leave re-sync on: refuse to start.
	for _, bad := range []string{"lg_A", "12345", "tm_01k7m2xq8rfv3c9a4t6wbn5hdz", "lg_01k7m2xq8rfv3c9a4t6wbn5hdz;lg_x"} {
		if _, err := Load(env(map[string]string{"FPL_RESYNC_DISABLED_LEAGUES": bad})); err == nil || !strings.Contains(err.Error(), "FPL_RESYNC_DISABLED_LEAGUES") {
			t.Errorf("%q: err = %v", bad, err)
		}
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	cfg, err := Load(env(map[string]string{"TRUSTED_PROXIES": "10.0.0.0/8, 192.0.2.7, fd00::/8"}))
	if err != nil || len(cfg.TrustedProxies) != 3 || cfg.TrustedProxies[1].String() != "192.0.2.7/32" {
		t.Fatalf("trusted proxies = %v %v", cfg.TrustedProxies, err)
	}
	if _, err := Load(env(map[string]string{"TRUSTED_PROXIES": "the load balancer"})); err == nil {
		t.Error("want an error for a non-address")
	}
}

func TestLoadJobsAndRealtime(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || !cfg.JobsEnabled || cfg.JobsTick != 15*time.Second || cfg.SnapshotsKeep != 50 || cfg.RealtimeURL != "" {
		t.Fatalf("defaults: %+v %v", cfg, err)
	}
	cfg, err = Load(env(map[string]string{"JOBS_ENABLED": "false", "JOBS_TICK": "5s", "SNAPSHOTS_KEEP": "10",
		"REALTIME_URL": "wss://api.example/v1/realtime", "APPLE_CLIENT_IDS": "com.example.app, com.example.web",
		"GOOGLE_CLIENT_IDS": "123.apps.googleusercontent.com"}))
	if err != nil || cfg.JobsEnabled || cfg.JobsTick != 5*time.Second || cfg.SnapshotsKeep != 10 || cfg.RealtimeURL != "wss://api.example/v1/realtime" ||
		len(cfg.AppleClientIDs) != 2 || cfg.AppleClientIDs[1] != "com.example.web" || len(cfg.GoogleClientIDs) != 1 {
		t.Fatalf("overrides: %+v %v", cfg, err)
	}
}

// CLAIM_LINK_SIGNIN is on by default except in production, where it may
// be turned on (cmd/api then warns at startup).
func TestLoadClaimLinkSignIn(t *testing.T) {
	for _, c := range []struct {
		env, value string
		want       bool
	}{
		{"", "", true},
		{"development", "", true},
		{"test", "", true},
		{"development", "false", false},
		{"production", "", false},
		{"production", "true", true},
	} {
		m := map[string]string{"APP_ENV": c.env, "CLAIM_LINK_SIGNIN": c.value}
		if c.env == "production" {
			m = prodEnv()
			m["CLAIM_LINK_SIGNIN"] = c.value
		}
		cfg, err := Load(env(m))
		if err != nil || cfg.ClaimLinkSignIn != c.want {
			t.Errorf("APP_ENV=%q CLAIM_LINK_SIGNIN=%q: %v %v, want %v", c.env, c.value, cfg.ClaimLinkSignIn, err, c.want)
		}
	}
	if _, err := Load(env(map[string]string{"CLAIM_LINK_SIGNIN": "maybe"})); err == nil {
		t.Error("want an error for CLAIM_LINK_SIGNIN=maybe")
	}
}

// BRANDED_ASSETS defaults on, except production must choose explicitly.
func TestLoadBrandedAssets(t *testing.T) {
	cfg, err := Load(env(nil))
	if err != nil || !cfg.BrandedAssets || cfg.AssetsCacheDir != "" || cfg.AssetsPlayerPhotoURLs != nil {
		t.Fatalf("default = %+v %v", cfg, err)
	}
	for _, v := range []string{"true", "false"} {
		m := prodEnv()
		m["BRANDED_ASSETS"] = v
		cfg, err := Load(env(m))
		if err != nil || cfg.BrandedAssets != (v == "true") {
			t.Errorf("production BRANDED_ASSETS=%s: %v %v", v, cfg.BrandedAssets, err)
		}
	}
	cfg, err = Load(env(map[string]string{
		"ASSETS_CACHE_DIR":          "/var/cache/assets",
		"ASSETS_PLAYER_PHOTO_URLS":  "https://img.example/p/{code}.png, https://alt.example/{code}.png",
		"ASSETS_CLUB_GK_SHIRT_URLS": "https://img.example/s/{code}_1.png",
	}))
	if err != nil || cfg.AssetsCacheDir != "/var/cache/assets" || len(cfg.AssetsPlayerPhotoURLs) != 2 ||
		cfg.AssetsPlayerPhotoURLs[1] != "https://alt.example/{code}.png" || len(cfg.AssetsClubGKShirtURLs) != 1 {
		t.Fatalf("overrides = %+v %v", cfg, err)
	}
	for _, m := range []map[string]string{
		{"BRANDED_ASSETS": "perhaps"},
		{"ASSETS_CLUB_CREST_URLS": "https://img.example/crest.png"},
		{"ASSETS_CLUB_SHIRT_URLS": "img.example/{code}.png"},
	} {
		if _, err := Load(env(m)); err == nil {
			t.Errorf("want error for %v", m)
		}
	}
}
