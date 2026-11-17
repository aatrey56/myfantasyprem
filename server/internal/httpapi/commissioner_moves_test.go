package httpapi

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// The commissioner's log over HTTP: entries newest first with who and what
// changed, a member's unseen count and its marker (per member, never going
// back), paging, and who may read it.
func TestCommissionerMovesEndpoints(t *testing.T) {
	srv, clock := newTestServer()
	opts := testOptions()
	h := newHandler(opts, newRoutes(opts, srv))
	commish := userOf(t, srv, testToken)
	memberTok := signIn(srv, "Priya N.", "priya@example.com")
	outsiderTok := signIn(srv, "Olu R.", "olu@example.com")
	l := importedLeague(t, srv, commish, userOf(t, srv, memberTok), true)
	base := "/v1/leagues/" + l.ID + "/commissioner-moves"

	patch := func(body string) {
		t.Helper()
		clock.Set(clock.Now().Add(time.Minute)) // after the managers joined
		if rec := do(h, http.MethodPatch, "/v1/leagues/"+l.ID, body); rec.Code != http.StatusOK {
			t.Fatalf("PATCH %s = %d %s", body, rec.Code, rec.Body)
		}
	}
	patch(`{"settings":{"trades":{"reviewHours":36}}}`)
	patch(`{"name":"Renamed League"}`)

	list := func(tok, query string) CommissionerMoveListResponse {
		t.Helper()
		rec := doAs(h, tok, http.MethodGet, base+query, "")
		var out CommissionerMoveListResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d %s", query, rec.Code, rec.Body)
		}
		return out
	}
	summary := func(tok string) CommissionerMovesSummary {
		t.Helper()
		rec := doAs(h, tok, http.MethodGet, base+"/summary", "")
		var out CommissionerMovesSummaryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("summary = %d %s", rec.Code, rec.Body)
		}
		return out.Data
	}
	seen := func(tok, body string) CommissionerMovesSummary {
		t.Helper()
		rec := doAs(h, tok, http.MethodPost, base+"/seen", body)
		var out CommissionerMovesSummaryResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("seen %s = %d %s", body, rec.Code, rec.Body)
		}
		return out.Data
	}

	got := list(memberTok, "")
	if len(got.Data) != 2 || got.Meta.NextCursor != nil {
		t.Fatalf("list = %+v", got)
	}
	newest, older := got.Data[0], got.Data[1]
	if newest.Action != "league_renamed" || older.Action != "settings_changed" ||
		newest.Actor.Id != commish || newest.Actor.DisplayName == "" || !newest.Unseen || !older.Unseen ||
		older.Details == nil || older.Details.After == nil || (*older.Details.After)["reviewHours"] != float64(36) ||
		newest.Summary != "Renamed the league from “Imported” to “Renamed League”." {
		t.Fatalf("entries = %+v", got.Data)
	}

	// The member has two unseen; the commissioner's own moves are not unseen.
	if s := summary(memberTok); s.UnseenCount != 2 || s.Latest == nil || s.Latest.Id != newest.Id {
		t.Fatalf("member summary = %+v", s)
	}
	if s := summary(testToken); s.UnseenCount != 0 || s.Latest != nil {
		t.Fatalf("commissioner summary = %+v", s)
	}
	if own := list(testToken, ""); own.Data[0].Unseen {
		t.Fatal("the commissioner's own move is unseen")
	}

	// Seen up to the older one leaves the newer; the marker is the member's own.
	if s := seen(memberTok, `{"throughId":"`+older.Id+`"}`); s.UnseenCount != 1 || s.Latest == nil || s.Latest.Id != newest.Id {
		t.Fatalf("after marking the older = %+v", s)
	}
	if list(memberTok, "").Data[1].Unseen {
		t.Fatal("a seen move is still unseen")
	}
	if s := seen(memberTok, ``); s.UnseenCount != 0 {
		t.Fatalf("after marking all = %+v", s)
	}
	if s := seen(memberTok, `{"throughId":"`+older.Id+`"}`); s.UnseenCount != 0 {
		t.Fatalf("an older id moved the marker back: %+v", s)
	}

	// Paging, newest first.
	first := list(memberTok, "?limit=1")
	if len(first.Data) != 1 || first.Data[0].Id != newest.Id || first.Meta.NextCursor == nil {
		t.Fatalf("first page = %+v", first)
	}
	next := list(memberTok, "?limit=1&cursor="+url.QueryEscape(*first.Meta.NextCursor))
	if len(next.Data) != 1 || next.Data[0].Id != older.Id || next.Meta.NextCursor != nil {
		t.Fatalf("second page = %+v", next)
	}

	// Outsiders get nothing; an id that is not a move is a 404.
	for _, path := range []string{"", "/summary"} {
		if rec := doAs(h, outsiderTok, http.MethodGet, base+path, ""); rec.Code != http.StatusForbidden {
			t.Errorf("outsider GET %s = %d", path, rec.Code)
		}
	}
	if rec := doAs(h, outsiderTok, http.MethodPost, base+"/seen", ""); rec.Code != http.StatusForbidden {
		t.Errorf("outsider seen = %d", rec.Code)
	}
	if rec := doAs(h, memberTok, http.MethodPost, base+"/seen", `{"throughId":"cmv_01k7m2xq800000000000000401"}`); rec.Code != http.StatusNotFound {
		t.Errorf("unknown move = %d", rec.Code)
	}
}
