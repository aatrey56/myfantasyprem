package httpapi

import (
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider/fpl"
)

// overlayFS serves top's files over base's.
type overlayFS struct{ top, base fs.FS }

func (o overlayFS) Open(name string) (fs.File, error) {
	if f, err := o.top.Open(name); err == nil {
		return f, nil
	}
	return o.base.Open(name)
}

// The real league with trade 343149 accepted on FPL and waiting for its
// commissioner (testdata/real-gw6-pending): the sync shows it pending
// (not failing), the league sees it in review with no actions, and the
// commissioner cannot review it here.
func TestImportWithATradePendingOnFPL(t *testing.T) {
	srv, clock := newTestServer()
	realStats(t, srv, clock)
	clock.Set(time.Date(2026, 10, 14, 20, 0, 0, 0, time.UTC))
	srv.mirror.Source = fpl.New(fpl.Options{Transport: fpl.DirTransport{FS: overlayFS{
		top: os.DirFS("../provider/fpl/testdata/real-gw6-pending"), base: os.DirFS("../provider/fpl/testdata/real-gw6")}},
		Logger: testOptions().Logger, MinInterval: -1})
	testToken = signIn(srv, "Sam P.", "sam-pending@example.com")
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))

	rec := do(h, http.MethodPost, "/v1/imports", `{"fplLeagueId":90001,"seat":3}`)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("POST status = %d: %s", rec.Code, rec.Body)
	}
	id := decodeImport(t, rec).Data.Id
	if err := srv.mirror.RunImport(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	done := decodeImport(t, do(h, http.MethodGet, "/v1/imports/"+id, "")).Data
	if done.Status != ImportStatusSucceeded || done.LeagueId == nil {
		t.Fatalf("import = %+v", done)
	}
	leagueID := *done.LeagueId

	var sync LeagueSyncResponse
	rec = do(h, http.MethodGet, "/v1/leagues/"+leagueID+"/sync", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &sync); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("sync = %d %s", rec.Code, rec.Body)
	}
	v := sync.Data.Verification
	if v == nil || v.Status != SyncReportStatusClean || v.Counts.Mismatch != 0 || v.Counts.Unverified != 0 || v.Counts.Pending != 1 ||
		len(v.Failing) != 0 || v.Unexplained != 0 || len(v.Pending) != 1 {
		t.Fatalf("verification = %s", rec.Body)
	}
	if p := v.Pending[0]; p.Status != SyncItemStatusPending || p.Kind != SyncItemKindPendingTrade || p.Team == nil || p.Note == nil ||
		!strings.HasPrefix(*p.Note, "Wait for FPL to process or veto the pending trade between ") || p.Ours != p.Theirs {
		t.Errorf("pending item = %+v", p)
	}

	var list TradeListResponse
	rec = do(h, http.MethodGet, "/v1/leagues/"+leagueID+"/trades", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("trades = %d %s", rec.Code, rec.Body)
	}
	var pending []Trade
	for _, tr := range list.Data {
		if tr.Status == TradeStatusInReview {
			pending = append(pending, tr)
			if len(tr.Offered) != 5 || len(tr.Requested) != 5 || len(tr.ViewerActions) != 0 ||
				tr.Review == nil || tr.Review.EndsAt != nil {
				t.Errorf("pending trade = %+v", tr)
			}
		}
	}
	if len(pending) != 1 {
		t.Fatalf("%d trades in review, want 1: %s", len(pending), rec.Body)
	}

	// FPL decides it: the commissioner cannot review it here.
	p := decodeProblem(t, do(h, http.MethodPost, "/v1/leagues/"+leagueID+"/trades/"+pending[0].Id+"/review",
		`{"decision":"approve"}`), http.StatusConflict)
	if problemType(p) != problemTypeBase+"pending-on-fpl" {
		t.Errorf("review = %q", problemType(p))
	}

}
