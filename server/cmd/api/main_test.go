package main

import (
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func testEnv(addr string) func(string) string {
	env := map[string]string{"HTTP_ADDR": addr, "SHUTDOWN_TIMEOUT": "5s"}
	return func(k string) string { return env[k] }
}

func TestRunServesAndShutsDownGracefully(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, testEnv("127.0.0.1:0"), io.Discard, func(a net.Addr) { addrCh <- a })
	}()

	var addr net.Addr
	select {
	case addr = <-addrCh:
	case err := <-done:
		t.Fatalf("run returned early: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not become ready")
	}

	resp, err := http.Get("http://" + addr.String() + "/healthz")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("healthz status = %d", resp.StatusCode)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run after cancel = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("run did not return after cancel")
	}

	// The listener is closed: nothing accepts connections any more.
	if conn, err := net.DialTimeout("tcp", addr.String(), time.Second); err == nil {
		conn.Close()
		t.Fatal("listener still accepting after shutdown")
	}
}

func TestRunFailsOnBadAddress(t *testing.T) {
	// Occupy a port so the second listen on it fails.
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer busy.Close()

	for name, addr := range map[string]string{
		"invalid": "256.0.0.1:99999",
		"in use":  busy.Addr().String(),
		"no port": "127.0.0.1",
	} {
		t.Run(name, func(t *testing.T) {
			ready := false
			err := run(context.Background(), testEnv(addr), io.Discard, func(net.Addr) { ready = true })
			if err == nil {
				t.Fatal("run returned nil, want a listen error")
			}
			if ready {
				t.Fatal("ready called although listen failed")
			}
		})
	}
}

func TestRunFailsOnBadConfig(t *testing.T) {
	env := func(k string) string {
		if k == "LOG_LEVEL" {
			return "loud"
		}
		return ""
	}
	if err := run(context.Background(), env, io.Discard, nil); err == nil {
		t.Fatal("run returned nil, want a config error")
	}
}

// Production never falls back to memory mode or dev magic links.
func TestRunRefusesUnsafeProduction(t *testing.T) {
	env := map[string]string{
		"APP_ENV": "production", "HTTP_ADDR": "127.0.0.1:0", "AUTH_DEV_MAGIC_LINKS": "true",
		"REALTIME_URL": "wss://api.example/v1/realtime", "CORS_ALLOWED_ORIGINS": "https://app.example",
	}
	err := run(context.Background(), func(k string) string { return env[k] }, io.Discard, func(net.Addr) {
		t.Error("an unsafe production server started")
	})
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "AUTH_DEV_MAGIC_LINKS") {
		t.Fatalf("err = %v", err)
	}
}

// An in-flight request must finish while the server shuts down, and the
// shutdown must wait for it.
func TestServeDrainsInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		_, _ = io.WriteString(w, "done")
	})}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan error, 1)
	go func() {
		serveDone <- serve(ctx, srv, ln, 5*time.Second, slog.New(slog.NewTextHandler(io.Discard, nil)))
	}()

	type result struct {
		body string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		resp, err := http.Get("http://" + ln.Addr().String() + "/")
		if err != nil {
			resCh <- result{err: err}
			return
		}
		defer resp.Body.Close()
		b, err := io.ReadAll(resp.Body)
		resCh <- result{body: string(b), err: err}
	}()

	<-started
	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("serve returned (%v) while a request was still in flight", err)
	case <-time.After(200 * time.Millisecond):
	}

	close(release)
	res := <-resCh
	if res.err != nil || !strings.Contains(res.body, "done") {
		t.Fatalf("in-flight request = %q, %v", res.body, res.err)
	}
	if err := <-serveDone; err != nil {
		t.Fatalf("serve = %v, want nil", err)
	}
}

// startRun runs the server with env and returns its base URL; it stops
// when the test ends.
func startRun(t *testing.T, env map[string]string) string {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	addrCh := make(chan net.Addr, 1)
	done := make(chan error, 1)
	go func() {
		done <- run(ctx, func(k string) string { return env[k] }, io.Discard, func(a net.Addr) { addrCh <- a })
	}()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("run = %v", err)
		}
	})
	select {
	case a := <-addrCh:
		return "http://" + a.String()
	case err := <-done:
		t.Fatalf("run returned early: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("server did not become ready")
	}
	return ""
}

// Without a database the server seeds the demo world in memory, and the
// poller can run on the demo source (never FPL).
func TestRunInMemoryWithDemoAndPoller(t *testing.T) {
	base := startRun(t, map[string]string{
		"HTTP_ADDR": "127.0.0.1:0", "POLLER_ENABLED": "true", "POLLER_SOURCE": "demo", "METRICS_ADDR": "127.0.0.1:0",
	})
	for target, want := range map[string]string{
		// The current gameweek follows the wall clock against the demo
		// calendar, so only its presence is checked here.
		"/v1/gameweeks/current": `"current":{`,
		"/v1/clubs":             `"code":"NOR"`,
	} {
		resp, err := http.Get(base + target)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), want) {
			t.Fatalf("%s = %d %s", target, resp.StatusCode, body)
		}
	}
}
