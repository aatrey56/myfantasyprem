package httpapi

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// failingHealth answers /healthz with err, as a handler whose store call
// failed would.
type failingHealth struct {
	*Server
	err error
}

func (f failingHealth) GetHealth(context.Context, GetHealthRequestObject) (GetHealthResponseObject, error) {
	return nil, f.err
}

// A client that goes away cancels the request context. The handler's
// context.Canceled is not a server fault: no problem+json 500, no error
// log, and the access log records 499.
func TestClientCanceledIsNot500(t *testing.T) {
	srv, _ := newTestServer()
	var logs bytes.Buffer
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h := newHandler(opts, newRoutes(opts, failingHealth{srv, fmt.Errorf("load: %w", context.Canceled)}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "internal") || rec.Header().Get("Content-Type") == "application/problem+json" {
		t.Errorf("wrote a problem to a client that left: %d %s", rec.Code, rec.Body)
	}
	l := logs.String()
	if strings.Contains(l, "level=ERROR") || strings.Contains(l, "status=500") {
		t.Errorf("logged as a server error:\n%s", l)
	}
	if !strings.Contains(l, `msg="client canceled"`) || !strings.Contains(l, "status=499") {
		t.Errorf("access log lacks the 499:\n%s", l)
	}
}

// context.Canceled from somewhere else (the request itself still live) is
// still a server fault: 500 and an error log.
func TestCanceledWithoutClientIs500(t *testing.T) {
	srv, _ := newTestServer()
	var logs bytes.Buffer
	opts := testOptions()
	opts.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	h := newHandler(opts, newRoutes(opts, failingHealth{srv, context.Canceled}))

	rec := doAnon(h, http.MethodGet, "/healthz", "")
	decodeProblem(t, rec, http.StatusInternalServerError)
	if l := logs.String(); !strings.Contains(l, "level=ERROR") || !strings.Contains(l, "status=500") {
		t.Errorf("log lacks the error:\n%s", l)
	}
}
