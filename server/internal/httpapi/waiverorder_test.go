package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// settings.waivers.order: reverse standings by default, set at creation
// or later by the commissioner, shown on the waivers screen; an unknown
// rule is refused.
func TestWaiverOrderSetting(t *testing.T) {
	eachStore(t, func(t *testing.T, srv *Server, h http.Handler) {
		send := func(method, target, body string, want int, out any) {
			t.Helper()
			rec := do(h, method, target, body)
			if rec.Code != want {
				t.Fatalf("%s %s %s = %d %s", method, target, body, rec.Code, rec.Body)
			}
			if out != nil {
				if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
					t.Fatal(err)
				}
			}
		}
		var lr LeagueResponse
		send("POST", "/v1/leagues", `{"name":"Order","teamCount":4,"teamName":"A"}`, 201, &lr)
		if got := lr.Data.Settings.Waivers.Order; got != ReverseStandings {
			t.Fatalf("default order %q", got)
		}
		path := "/v1/leagues/" + lr.Data.League.Id
		send("PATCH", path, `{"settings":{"waivers":{"order":"rolling"}}}`, 200, &lr)
		if got := lr.Data.Settings.Waivers.Order; got != Rolling {
			t.Fatalf("after PATCH: %q", got)
		}
		var ws WaiverStateResponse
		send("GET", path+"/waivers", "", 200, &ws)
		if ws.Data.Order != Rolling || ws.Data.NextRunGameweek != nil || len(ws.Data.Priority) != 1 {
			t.Fatalf("waivers = %+v", ws.Data)
		}
		send("PATCH", path, `{"settings":{"waivers":{"order":"fifo"}}}`, 422, nil)

		send("POST", "/v1/leagues", `{"name":"Rolling","teamCount":4,"teamName":"A","settings":{"waivers":{"order":"rolling"}}}`, 201, &lr)
		if got := lr.Data.Settings.Waivers.Order; got != Rolling {
			t.Fatalf("created with %q", got)
		}
	})
}
