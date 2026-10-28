package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// Start with one manager is refused with a problem that says at least 2
// are needed, and the draft does not start; with 2 of 6 it starts, and
// the 4 empty places are open seats ("Open seat") on autopick.
func TestStartDraftNeedsTwoManagersAndOpensSeats(t *testing.T) {
	f := newDemoFixture(t)
	ctx := context.Background()
	l, err := f.srv.leagues.Create(ctx, f.users[0], "Half Empty", 6, "Early Birds", league.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	rec := doAs(f.h, f.token, http.MethodPost, "/v1/leagues/"+l.ID+"/draft/start", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("start = %d %s", rec.Code, rec.Body)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/problem+json") {
		t.Errorf("content type %q", ct)
	}
	var p struct{ Type, Title, Detail string }
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(p.Type, "/not-enough-managers") || p.Title != "Not enough managers" ||
		!strings.Contains(p.Detail, "1 of 6 teams") || !strings.Contains(p.Detail, "at least 2") {
		t.Errorf("problem = %+v", p)
	}
	var d DraftResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+l.ID+"/draft", &d); rec.Code != 200 || d.Data.Status != DraftStatusScheduled {
		t.Errorf("draft after refused start = %d %s", rec.Code, d.Data.Status)
	}

	inv, err := f.srv.leagues.CreateInvite(ctx, f.users[0], l.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.srv.leagues.AcceptInvite(ctx, f.users[1], inv.Code, "Second"); err != nil {
		t.Fatal(err)
	}
	rec = doAs(f.h, f.token, http.MethodPost, "/v1/leagues/"+l.ID+"/draft/start", "")
	d = DraftResponse{}
	if err := json.Unmarshal(rec.Body.Bytes(), &d); err != nil || rec.Code != http.StatusOK || d.Data.Status != DraftStatusInProgress ||
		len(d.Data.Order) != 6 {
		t.Fatalf("start with 2 of 6 = %d %s", rec.Code, rec.Body)
	}
	for _, slot := range d.Data.Order[2:] {
		if slot.Team.ManagerName != "Open seat" || !strings.HasPrefix(slot.Team.Name, "Open seat ") || !slot.Autopick {
			t.Errorf("open seat = %+v", slot)
		}
	}
}
