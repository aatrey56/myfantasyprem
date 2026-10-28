package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

// Invite links open the web app's join route on APP_URL (Options.AppURL),
// and on the Expo web dev server when none is set.
func TestInviteURLsUseTheAppURL(t *testing.T) {
	for _, c := range []struct{ appURL, want string }{
		{"https://play.example", "https://play.example/join/"},
		{"", "http://localhost:8081/join/"},
	} {
		opts := testOptions()
		opts.AppURL = c.appURL
		api := NewAPI(opts)
		testToken = signIn(api.srv, "Sam P.", "sam@example.com")
		rec := do(api.Handler, http.MethodPost, "/v1/leagues", `{"name":"Links","teamCount":4,"teamName":"Heroes"}`)
		var created LeagueResponse
		_ = json.Unmarshal(rec.Body.Bytes(), &created)
		path := "/v1/leagues/" + created.Data.League.Id + "/invites"
		var inv InviteResponse
		_ = json.Unmarshal(do(api.Handler, http.MethodPost, path, "").Body.Bytes(), &inv)
		if inv.Data.Url != c.want+inv.Data.Code {
			t.Errorf("AppURL %q: created invite url = %q, want %q", c.appURL, inv.Data.Url, c.want+inv.Data.Code)
		}
		var list InviteListResponse
		_ = json.Unmarshal(do(api.Handler, http.MethodGet, path, "").Body.Bytes(), &list)
		if len(list.Data) != 1 || list.Data[0].Url != c.want+inv.Data.Code {
			t.Errorf("AppURL %q: listed invites = %+v", c.appURL, list.Data)
		}
	}
}
