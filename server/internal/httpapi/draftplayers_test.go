package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"testing"
)

// fullLeague creates a full league of n teams over HTTP as testToken's
// user and returns its path and the other managers' tokens.
func fullLeague(t *testing.T, srv *Server, h http.Handler, n int) (string, []string) {
	t.Helper()
	var lr LeagueResponse
	if rec := do(h, "POST", "/v1/leagues", fmt.Sprintf(`{"name":"Draft Board","teamCount":%d,"teamName":"A"}`, n)); rec.Code != 201 {
		t.Fatalf("create = %d %s", rec.Code, rec.Body)
	} else {
		_ = json.Unmarshal(rec.Body.Bytes(), &lr)
	}
	path := "/v1/leagues/" + lr.Data.League.Id
	var inv InviteResponse
	_ = json.Unmarshal(do(h, "POST", path+"/invites", `{}`).Body.Bytes(), &inv)
	var others []string
	for i := 1; i < n; i++ {
		tok := signIn(srv, fmt.Sprintf("M%d", i), fmt.Sprintf("dp%d@example.com", i))
		if rec := doAs(h, tok, "POST", "/v1/invites/"+inv.Data.Code+"/accept", fmt.Sprintf(`{"teamName":"T%d"}`, i)); rec.Code != 201 {
			t.Fatalf("join = %d %s", rec.Code, rec.Body)
		}
		others = append(others, tok)
	}
	return path, others
}

// While the draft runs, drafted players are taken: not available, listed
// as rostered with their team (rosters only exist once it completes).
func TestPlayersDuringDraft(t *testing.T) {
	eachStore(t, func(t *testing.T, srv *Server, h http.Handler) {
		me := testToken
		path, _ := fullLeague(t, srv, h, 4)
		if rec := doAs(h, me, "POST", path+"/draft/start", ""); rec.Code != 200 {
			t.Fatalf("start = %d %s", rec.Code, rec.Body)
		}
		list := func(q string) PlayerListResponse {
			t.Helper()
			rec := doAs(h, me, "GET", path+"/players?"+q, "")
			if rec.Code != 200 {
				t.Fatalf("players %s = %d %s", q, rec.Code, rec.Body)
			}
			var out PlayerListResponse
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			return out
		}
		top := list("status=available&sort=draft_rank&limit=1").Data[0].Player.Id
		var pick DraftPickResponse
		if rec := doAs(h, me, "POST", path+"/draft/picks", `{"playerId":"`+top+`"}`); rec.Code != 201 {
			t.Fatalf("pick = %d %s", rec.Code, rec.Body)
		} else {
			_ = json.Unmarshal(rec.Body.Bytes(), &pick)
		}
		for _, p := range list("status=available&sort=draft_rank&limit=100").Data {
			if p.Player.Id == top {
				t.Fatalf("drafted player %s is listed as available (%s)", top, p.RosterStatus)
			}
		}
		rostered := list("status=rostered")
		if len(rostered.Data) != 1 || rostered.Data[0].Player.Id != top || rostered.Data[0].Owner == nil ||
			rostered.Data[0].Owner.Id != pick.Data.Team.Id {
			t.Fatalf("rostered = %+v", rostered.Data)
		}
		if byTeam := list("teamId=" + pick.Data.Team.Id); len(byTeam.Data) != 1 {
			t.Fatalf("teamId filter = %d players", len(byTeam.Data))
		}
		var one PlayerDetailResponse
		_ = json.Unmarshal(doAs(h, me, "GET", path+"/players/"+top, "").Body.Bytes(), &one)
		if one.Data.RosterStatus != RosterStatusRostered {
			t.Fatalf("player detail status = %s", one.Data.RosterStatus)
		}
	})
}

// A draft finishing on autopick makes dozens of picks in one commit; a
// subscribed client that is not reading at that moment must not overflow
// its send buffer and be dropped: the burst comes as one draft.updated
// with the whole board, not a frame per pick.
func TestAutopickBurstDoesNotDropClients(t *testing.T) {
	eachStore(t, func(t *testing.T, srv *Server, h http.Handler) {
		me := testToken
		path, others := fullLeague(t, srv, h, 6)
		for _, tok := range others {
			if rec := doAs(h, tok, "PUT", path+"/draft/autopick", `{"enabled":true}`); rec.Code != 200 {
				t.Fatalf("autopick = %d %s", rec.Code, rec.Body)
			}
		}
		if rec := doAs(h, me, "POST", path+"/draft/start", ""); rec.Code != 200 {
			t.Fatalf("start = %d %s", rec.Code, rec.Body)
		}
		kicked := ""
		client := srv.hub.Connect(userOf(t, srv, me), "", func(_ int, reason string) { kicked = reason })
		if err := srv.hub.Subscribe(context.Background(), client, path[len("/v1/leagues/"):], nil); err != nil {
			t.Fatal(err)
		}
		if rec := doAs(h, me, "PUT", path+"/draft/autopick", `{"enabled":true}`); rec.Code != 200 {
			t.Fatalf("autopick = %d %s", rec.Code, rec.Body)
		}
		var d DraftResponse
		_ = json.Unmarshal(doAs(h, me, "GET", path+"/draft", "").Body.Bytes(), &d)
		if d.Data.Status != DraftStatusComplete || len(d.Data.Picks) != 90 {
			t.Fatalf("draft %s with %d picks", d.Data.Status, len(d.Data.Picks))
		}
		n := len(client.Send)
		var types []string
		for len(client.Send) > 0 {
			var m struct{ Type string }
			_ = json.Unmarshal(<-client.Send, &m)
			types = append(types, m.Type)
		}
		if kicked != "" || n > 4 {
			t.Fatalf("client dropped (%q) after %d frames", kicked, n)
		}
		if !slices.Contains(types, "draft.updated") || !slices.Contains(types, "league.updated") {
			t.Fatalf("heard %v", types)
		}
	})
}
