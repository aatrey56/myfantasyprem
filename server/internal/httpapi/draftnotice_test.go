package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
)

// A missed clock over HTTP: the pick says missedClock, the activity feed
// has a draft_autopick entry naming the team and player, and a manager can
// turn "autopick when my clock runs out" off.
func TestMissedClockNoticeOverHTTP(t *testing.T) {
	f := newDemoFixture(t)
	ctx := context.Background()
	l, err := f.srv.leagues.Create(ctx, f.users[0], "Sleepy Drafters", 4, "Snoozers", league.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	inv, err := f.srv.leagues.CreateInvite(ctx, f.users[0], l.ID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i, u := range f.users[1:4] {
		if _, _, err := f.srv.leagues.AcceptInvite(ctx, u, inv.Code, fmt.Sprintf("Team %d", i+2)); err != nil {
			t.Fatal(err)
		}
	}
	base := "/v1/leagues/" + l.ID + "/draft"

	var q DraftQueueResponse
	if rec := f.get(t, f.token, base+"/queue", &q); rec.Code != 200 || !q.Data.AutopickOnTimeout || q.Data.Autopick {
		t.Fatalf("default queue = %d %s", rec.Code, rec.Body)
	}
	rec := doAs(f.h, f.token, http.MethodPut, base+"/autopick", `{"onTimeout": false}`)
	if rec.Code != 200 {
		t.Fatalf("switch off = %d %s", rec.Code, rec.Body)
	}
	if rec := doAs(f.h, f.token, http.MethodPut, base+"/autopick", `{}`); rec.Code != 422 && rec.Code != 400 {
		t.Errorf("empty autopick body = %d %s", rec.Code, rec.Body)
	}
	if _, err := f.srv.drafts().Start(ctx, f.users[0], l.ID); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(61 * time.Second)
	if changed, err := f.srv.drafts().Expire(ctx, l.ID); err != nil || !changed {
		t.Fatalf("expire: %v %v", changed, err)
	}

	var d DraftResponse
	if rec := f.get(t, f.token, base, &d); rec.Code != 200 || len(d.Data.Picks) != 1 {
		t.Fatalf("draft = %d %s", rec.Code, rec.Body)
	}
	pk := d.Data.Picks[0]
	if !pk.MissedClock || pk.Team.Name != "Snoozers" {
		t.Errorf("pick = %+v, want a missed clock by Snoozers", pk)
	}
	if d.Data.Order[0].Autopick {
		t.Error("autopick turned on although the switch is off")
	}
	if d.Data.CurrentPick == nil || d.Data.CurrentPick.Overall != 2 {
		t.Errorf("clock = %+v", d.Data.CurrentPick)
	}

	var feed TransactionListResponse
	if rec := f.get(t, f.token, "/v1/leagues/"+l.ID+"/transactions?kind=draft_autopick", &feed); rec.Code != 200 || len(feed.Data) != 1 {
		t.Fatalf("feed = %d %s", rec.Code, rec.Body)
	}
	x, err := feed.Data[0].AsDraftAutopickTransaction()
	if err != nil {
		t.Fatal(err)
	}
	if x.Team.Name != "Snoozers" || x.Player.Id != pk.Player.Id || x.Overall != 1 || x.Round != 1 ||
		x.Method != pk.Method || x.Gameweek != 8 { // GW8's deadline (12:30) is after the pick (12:01)
		t.Errorf("feed entry = %+v", x)
	}
}
