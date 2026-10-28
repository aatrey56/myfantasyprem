// Command api runs the MyFantasyPrem HTTP API.
package main

import (
	"context"
	"errors"
	"expvar"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/assets"
	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/config"
	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/demo/midseason"
	"github.com/aatrey56/myfantasyprem/server/internal/history"
	"github.com/aatrey56/myfantasyprem/server/internal/httpapi"
	"github.com/aatrey56/myfantasyprem/server/internal/idp"
	"github.com/aatrey56/myfantasyprem/server/internal/jobs"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications/webpush"
	"github.com/aatrey56/myfantasyprem/server/internal/poller"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
	"github.com/aatrey56/myfantasyprem/server/internal/realtime"
	"github.com/aatrey56/myfantasyprem/server/internal/scoring"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
	"github.com/aatrey56/myfantasyprem/server/internal/tags"
	"github.com/aatrey56/myfantasyprem/server/internal/watchlist"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		// Restore default signal handling once shutdown starts, so a second
		// interrupt kills the process instead of waiting out the timeout.
		<-ctx.Done()
		stop()
	}()

	if err := run(ctx, os.Getenv, os.Stderr, nil); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run serves the API until ctx is cancelled, then shuts down gracefully and
// returns nil. It binds the listen address before logging "listening", so a
// bad or busy address fails fast with an error. ready, if not nil, is called
// with the bound address once the server accepts connections.
func run(ctx context.Context, getenv func(string) string, logOut io.Writer, ready func(net.Addr)) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	if err := checkEmailProvider(cfg, getenv("APP_ENV"), authRateLimited); err != nil {
		return err
	}

	handlerOpts := &slog.HandlerOptions{Level: cfg.LogLevel}
	var logHandler slog.Handler = slog.NewTextHandler(logOut, handlerOpts)
	if cfg.LogJSON {
		logHandler = slog.NewJSONHandler(logOut, handlerOpts)
	}
	logger := slog.New(logHandler)
	// River logs routine progress at info; keep it to warnings and up.
	riverOpts := &slog.HandlerOptions{Level: max(cfg.LogLevel, slog.LevelWarn)}
	var riverHandler slog.Handler = slog.NewTextHandler(logOut, riverOpts)
	if cfg.LogJSON {
		riverHandler = slog.NewJSONHandler(logOut, riverOpts)
	}
	slog.SetDefault(logger)

	sender := newSender(cfg, logger)

	var appleTokens *idp.AppleTokens
	if cfg.AppleRevocationConfigured() {
		if appleTokens, err = idp.NewAppleTokens(cfg.AppleTeamID, cfg.AppleKeyID, cfg.ApplePrivateKey, cfg.AppleTokenKey); err != nil {
			return fmt.Errorf("APPLE_PRIVATE_KEY: %w", err)
		}
	} else if len(cfg.AppleClientIDs) > 0 {
		logger.Error("Sign in with Apple is on but Apple token revocation is NOT configured: deleted accounts keep their Apple authorization. " +
			"Set APPLE_TEAM_ID, APPLE_KEY_ID, APPLE_PRIVATE_KEY and APPLE_TOKEN_KEY; required before App Store submission (docs/RUNBOOK.md)")
	}

	opts := httpapi.Options{
		AppleTokens:     appleTokens,
		Logger:          logger,
		Version:         version,
		AllowedOrigins:  cfg.CORSAllowedOrigins,
		Sender:          sender,
		RealtimeURL:     cfg.RealtimeURL,
		AppURL:          cfg.AppURL,
		Apple:           idp.Apple(cfg.AppleClientIDs, nil),
		Google:          idp.Google(cfg.GoogleClientIDs, nil),
		TrustedProxies:  cfg.TrustedProxies,
		Production:      cfg.Production(),
		ClaimLinkSignIn: cfg.ClaimLinkSignIn,

		ResyncDisabledLeagues: cfg.ResyncDisabledLeagues,
	}
	histSource, histProvider := historySource(cfg, logger.With("component", "history"))
	opts.HistoryProvider = histProvider
	if cfg.ClaimLinkSignIn && cfg.Production() {
		logger.Warn("CLAIM_LINK_SIGNIN is on in production: anyone holding a seat's claim link gets an account and that seat " +
			"without an email (testing phase, docs/adr/0013); turn it off once email, Google or Apple sign-in works")
	}
	// The pieces the jobs and the realtime listener need besides opts.
	var (
		pool       *pgxpool.Pool
		pgLeagues  *league.Postgres
		memLeagues *league.Memory
		rtStore    realtime.Store
		pruneSnaps func(context.Context, int) (int, error)
		pruneIdem  func(context.Context, time.Time) (int, error)
		mirrors    mirror.Store
	)
	if cfg.DatabaseURL != "" {
		if cfg.MigrateOnStart {
			if err := store.Migrate(ctx, cfg.DatabaseURL); err != nil {
				return err
			}
		}
		pool, err = store.Open(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		defer pool.Close()
		pgLeagues = league.NewPostgres(pool)
		pgStats := stats.NewPostgres(pool)
		rtStore = realtime.NewPostgres(pool, time.Now)
		opts.Auth = auth.NewPostgres(pool, time.Now)
		opts.Watchlist = watchlist.NewPostgres(pool, time.Now)
		opts.Tags = tags.NewPostgres(pool, time.Now)
		opts.Notifications = notifications.NewPostgres(pool, time.Now)
		opts.History = history.NewPostgres(pool)
		opts.Leagues = pgLeagues
		opts.Stats = pgStats
		pgIdem := httpapi.NewPostgresIdempotency(pool)
		opts.Idempotency = pgIdem
		pruneIdem = pgIdem.Prune
		opts.Realtime = rtStore
		opts.RealtimeListen = true
		pruneSnaps = pgStats.PruneSnapshots
		mirrors = mirror.NewPostgresStore(pool)
		opts.HealthCheck = pool.Ping
	} else {
		logger.Warn("DATABASE_URL is not set: every store is in memory and is lost on restart (development only)")
		memLeagues = league.NewMemory()
		rtStore = realtime.NewMemory(time.Now)
		opts.Auth = auth.NewMemory(time.Now)
		opts.Notifications = notifications.NewMemory(time.Now)
		opts.History = history.NewMemory()
		opts.Leagues = memLeagues
		opts.Stats = stats.NewMemory()
		opts.Realtime = rtStore
		mirrors = mirror.NewMemoryStore()
		if cfg.SeedDemo {
			deps := demo.Deps{Auth: opts.Auth, Leagues: league.NewService(opts.Leagues, time.Now), Stats: opts.Stats,
				History: opts.History, Log: logger}
			res, err := demo.Seed(ctx, deps)
			if err != nil {
				return fmt.Errorf("seed demo: %w", err)
			}
			if _, err := midseason.Seed(ctx, deps, res.UserIDs); err != nil {
				return fmt.Errorf("seed mid-season demo league: %w", err)
			}
		}
	}
	// Importing and syncing FPL Draft leagues (docs/adr/0012): FPL's public
	// league endpoints, read by the server only (FPL_BASE_URL points it at a
	// fixture server in the end-to-end tests). The jobs and the notifier are
	// set once they exist.
	mir := &mirror.Service{
		Source: fpl.New(fpl.Options{BaseURL: cfg.FPLBaseURL, UserAgent: cfg.FPLUserAgent,
			Logger: logger.With("component", "mirror-fpl")}),
		Leagues: opts.Leagues, Stats: opts.Stats, Store: mirrors, Log: logger.With("component", "mirror"),
		ShadowUntil: cfg.FPLShadowUntil,
	}
	opts.Mirror = mir
	if opts.Assets, err = newAssets(cfg, opts.Stats, logger); err != nil {
		return err
	}
	// Notifications: every one lands in the inbox; pushes go to browsers
	// over Web Push when VAPID keys are set. Native (Expo) push stays off
	// until the iOS app ships.
	deliverers := map[string]notifications.Deliverer{notifications.KindExpo: notifications.Expo{}}
	if cfg.VAPIDPublicKey != "" {
		wp, err := webpush.New(webpush.VAPID{
			PublicKey: cfg.VAPIDPublicKey, PrivateKey: cfg.VAPIDPrivateKey, Subject: cfg.VAPIDSubject})
		if err != nil {
			return err
		}
		deliverers[notifications.KindWebPush] = wp
		opts.WebPushPublicKey = wp.PublicKey()
	} else {
		logger.Info("VAPID keys are not set: Web Push is off (notifications still reach the inbox)")
	}
	notifier := notifications.NewNotifier(opts.Notifications)
	mir.Alert = notifier
	opts.UpdatesPublicURL = cfg.UpdatesPublicURL
	if opts.UpdatesDir = cfg.UpdatesDir; opts.UpdatesDir != "" {
		logger.Info("over-the-air app updates are served from /updates/", "dir", opts.UpdatesDir, "public_url", opts.UpdatesPublicURL)
	}
	api := httpapi.NewAPI(opts)
	mir.Notify = api.Notifier

	// Bind first, so a busy address fails before any goroutine starts.
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	defer wg.Wait()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Jobs (ADR 0010): River on Postgres, timers in memory. Every instance
	// inserts jobs; JOBS_ENABLED instances work them.
	runner := &jobs.Runner{
		Leagues: opts.Leagues, Stats: opts.Stats, Now: time.Now, Notify: api.Notifier,
		Log: logger.With("component", "jobs"), PruneRealtime: rtStore.Prune,
		PruneSnapshots: pruneSnaps, KeepSnapshots: cfg.SnapshotsKeep,
		PruneAuth: opts.Auth.Prune, PruneIdempotency: pruneIdem, Mirror: mir,
		Reminders: (&notifications.Changes{Leagues: opts.Leagues, Stats: opts.Stats, Notifier: notifier,
			Log: logger.With("component", "notifications")}).Deadlines,
	}
	if pool != nil {
		runner.History = &history.Fetcher{Source: histSource, Store: opts.History, Now: time.Now,
			Codes: history.CodesFrom(opts.Stats, histProvider), Log: logger.With("component", "history")}
		riv, err := jobs.NewRiver(pool, runner, jobs.RiverConfig{TickEvery: cfg.JobsTick}, slog.New(riverHandler).With("component", "river"))
		if err != nil {
			return err
		}
		pgLeagues.SetScheduler(riv)
		mir.Sched = riv
		if cfg.JobsEnabled {
			if err := riv.Start(ctx); err != nil {
				return fmt.Errorf("start jobs: %w", err)
			}
			defer func() {
				stopCtx, done := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
				defer done()
				_ = riv.Stop(stopCtx)
			}()
		}
		// Realtime events from every instance (and cmd/admin) arrive here.
		wg.Add(1)
		go func() {
			defer wg.Done()
			realtime.Listen(ctx, pool, rtStore, api.Hub, logger.With("component", "realtime"))
		}()
	} else {
		mem := jobs.NewMemory(ctx, runner)
		memLeagues.SetScheduler(mem)
		mir.Sched = mem
		if cfg.JobsEnabled {
			wg.Add(1)
			go func() {
				defer wg.Done()
				mem.Run(ctx, cfg.JobsTick)
			}()
		}
	}

	if cfg.JobsEnabled {
		// The push outbox; on Postgres every instance may drain it
		// (deliveries are claimed with SKIP LOCKED).
		d := &notifications.Dispatcher{Store: opts.Notifications, Deliverers: deliverers, Now: time.Now,
			Log: logger.With("component", "push"), Batch: 100}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = d.Run(ctx, 5*time.Second)
		}()
	}
	if cfg.PollerEnabled {
		p := newPoller(cfg, opts, api.Notifier, notifier, logger)
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Run(ctx)
		}()
	}
	if cfg.MetricsAddr != "" {
		mln, err := net.Listen("tcp", cfg.MetricsAddr)
		if err != nil {
			return fmt.Errorf("METRICS_ADDR: %w", err)
		}
		poller.PublishedMetrics()
		msrv := &http.Server{Handler: expvar.Handler(), ReadHeaderTimeout: 5 * time.Second}
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = msrv.Serve(mln)
		}()
		go func() {
			<-ctx.Done()
			_ = msrv.Close()
		}()
		logger.Info("metrics listening", "addr", mln.Addr().String(), "path", "/debug/vars")
	}

	srv := &http.Server{
		Handler:           api.Handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		// WriteTimeout bounds the whole response, per request. The
		// WebSocket handler (GET /v1/realtime) clears it, and the read
		// deadline, for its hijacked connection.
		WriteTimeout:   15 * time.Second,
		IdleTimeout:    60 * time.Second,
		MaxHeaderBytes: 1 << 16,
	}
	// Shutdown does not wait for hijacked connections: close the sockets.
	srv.RegisterOnShutdown(api.Close)

	logger.Info("listening", "addr", ln.Addr().String(), "version", version, "env", cfg.AppEnv)
	if ready != nil {
		ready(ln.Addr())
	}
	return serve(ctx, srv, ln, cfg.ShutdownTimeout, logger)
}

// historySource is where previous seasons come from, and the provider
// whose player ids key them: FPL with its own polite client (one request
// at a time, 2 a second), or the demo world.
func historySource(cfg config.Config, log *slog.Logger) (history.Source, string) {
	if cfg.PollerSource == config.SourceDemo {
		return demo.Source(log), demo.ProviderName
	}
	return fpl.New(fpl.Options{BaseURL: cfg.FPLBaseURL, UserAgent: cfg.FPLUserAgent, Logger: log}), cfg.PollerSource
}

// newPoller builds the stats poller on the configured source. The demo
// source reads the embedded demo world and never calls FPL. Score moves
// and corrections go to notify (realtime).
func newPoller(cfg config.Config, opts httpapi.Options, notify league.Notifier, n *notifications.Notifier, logger *slog.Logger) *poller.Poller {
	log := logger.With("component", "poller", "source", cfg.PollerSource)
	var src provider.StatsSource
	providerName := cfg.PollerSource
	if cfg.PollerSource == config.SourceDemo {
		src = demo.Source(log)
		providerName = demo.ProviderName
	} else {
		src = fpl.New(fpl.Options{BaseURL: cfg.FPLBaseURL, UserAgent: cfg.FPLUserAgent, Logger: log})
	}
	scorer := &league.Scorer{Leagues: opts.Leagues, Stats: opts.Stats, Rules: scoring.StandardLineup(), Log: log, Notify: notify,
		Live: notifications.NewLive(n, log)}
	p := poller.New(src, stats.NewIngester(opts.Stats, providerName, time.Now), opts.Stats, scorer, poller.Config{}, log)
	p.News = &notifications.News{Leagues: opts.Leagues, Notifier: n, Log: log}
	p.Metrics = poller.PublishedMetrics()
	return p
}

// serve runs srv on ln until ctx is cancelled, then drains in-flight requests
// for up to shutdownTimeout. It returns nil after a clean shutdown.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, shutdownTimeout time.Duration, logger *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Serve(ln) }()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		logger.Info("shutting down", "timeout", shutdownTimeout)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	return nil
}

// newAssets is the branded-assets service (BRANDED_ASSETS): crests, shirts
// and player photos fetched upstream once, with FPL's user agent, and kept
// in ASSETS_CACHE_DIR. Off, nothing is ever fetched.
func newAssets(cfg config.Config, catalog assets.Catalog, logger *slog.Logger) (*assets.Service, error) {
	if !cfg.BrandedAssets {
		logger.Info("branded assets are off: /v1/assets answers 404")
		return nil, nil
	}
	ua := cfg.FPLUserAgent
	if ua == "" {
		ua = fpl.DefaultUserAgent
	}
	proxy, err := assets.NewProxy(assets.Options{
		Dir: cfg.AssetsCacheDir,
		URLs: map[assets.Kind][]string{
			assets.PlayerPhoto: cfg.AssetsPlayerPhotoURLs, assets.ClubCrest: cfg.AssetsClubCrestURLs,
			assets.ClubShirt: cfg.AssetsClubShirtURLs, assets.ClubGKShirt: cfg.AssetsClubGKShirtURLs,
		},
		UserAgent: ua,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("ASSETS_CACHE_DIR: %w", err)
	}
	logger.Info("branded assets are on: crests, shirts and player photos are served from /v1/assets")
	return &assets.Service{Enabled: true, Resolver: &assets.Resolver{Catalog: catalog}, Proxy: proxy}, nil
}
