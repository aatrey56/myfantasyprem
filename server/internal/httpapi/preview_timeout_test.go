package httpapi

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// stalledLeague is an FPL that never answers the league lookup until the
// caller gives up (the mirror's rate-paced client queued behind a sync).
type stalledLeague struct{ provider.LeagueMirrorSource }

func (stalledLeague) LeagueDetails(ctx context.Context, _ string) (*provider.LeagueDetails, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}

// A preview stuck on FPL stops at its own bound and answers 502 "FPL
// unavailable", inside the response deadline, instead of running on until
// the connection dies.
func TestPreviewImportBoundedWhenFPLStalls(t *testing.T) {
	old := previewTimeout
	previewTimeout = 50 * time.Millisecond
	t.Cleanup(func() { previewTimeout = old })
	srv, _ := newTestServer()
	srv.mirror.Source = stalledLeague{srv.mirror.Source}
	testToken = signIn(srv, "Sam P.", "sam-stall@example.com")
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))

	start := time.Now()
	rec := do(h, http.MethodGet, "/v1/imports/preview?fplLeagueId=90001", "")
	if p := decodeProblem(t, rec, http.StatusBadGateway); problemType(p) != problemTypeBase+"upstream-error" {
		t.Fatalf("preview = %+v", p)
	}
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("preview took %v, want about the 50ms bound", took)
	}
}
