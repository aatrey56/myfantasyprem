package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// eachStore runs test on a server with the memory stores and, when
// TEST_DATABASE_URL is set, the Postgres ones.
func eachStore(t *testing.T, test func(t *testing.T, srv *Server, h http.Handler)) {
	run := func(t *testing.T, pool *pgxpool.Pool) {
		srv, _ := newTestServerOn(pool)
		if _, err := demo.LoadStats(context.Background(), srv.stats, testOptions().Logger, srv.now); err != nil {
			t.Fatal(err)
		}
		test(t, srv, newHandler(testOptions(), newRoutes(testOptions(), srv)))
	}
	t.Run("memory", func(t *testing.T) { run(t, nil) })
	t.Run("postgres", func(t *testing.T) { run(t, storetest.New(t)) })
}

// In a PATCH an omitted nullable field keeps its value and an explicit
// null clears it: the draft start (league settings and the draft), the
// trade deadline and the avatar colour.
func TestPatchNullClears(t *testing.T) {
	eachStore(t, func(t *testing.T, srv *Server, h http.Handler) {
		send := func(method, target, body string, out any) {
			t.Helper()
			rec := do(h, method, target, body)
			if rec.Code/100 != 2 {
				t.Fatalf("%s %s %s = %d %s", method, target, body, rec.Code, rec.Body)
			}
			if out != nil {
				if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
					t.Fatal(err)
				}
			}
		}
		var lr LeagueResponse
		send("POST", "/v1/leagues", `{"name":"Nulls","teamCount":4,"teamName":"A","settings":{"draft":{"startsAt":"2026-11-01T18:00:00Z"},"trades":{"deadlineGameweek":30}}}`, &lr)
		path := "/v1/leagues/" + lr.Data.League.Id
		set := func(l LeagueResponse) (bool, bool) {
			return l.Data.Settings.Draft.StartsAt != nil, l.Data.Settings.Trades.DeadlineGameweek != nil
		}
		if s, d := set(lr); !s || !d {
			t.Fatalf("created without start %v or deadline %v", s, d)
		}
		send("PATCH", path, `{"name":"Nulls Renamed","settings":{"draft":{"pickTimerSeconds":90},"trades":{}}}`, &lr)
		if s, d := set(lr); !s || !d {
			t.Fatalf("omitted fields changed: start %v deadline %v", s, d)
		}
		send("PATCH", path, `{"settings":{"trades":{"deadlineGameweek":null}}}`, &lr)
		if s, d := set(lr); !s || d {
			t.Fatalf("after deadline null: start %v deadline %v", s, d)
		}
		send("PATCH", path, `{"settings":{"draft":{"startsAt":null}}}`, &lr)
		if s, _ := set(lr); s {
			t.Fatal("settings.draft.startsAt: null did not clear the start")
		}

		var dr DraftResponse
		send("PATCH", path+"/draft", `{"startsAt":"2026-11-02T18:00:00Z"}`, &dr)
		if dr.Data.StartsAt == nil {
			t.Fatal("draft start not set")
		}
		send("PATCH", path+"/draft", `{"pickTimerSeconds":45}`, &dr)
		if dr.Data.StartsAt == nil {
			t.Fatal("omitted startsAt cleared the start")
		}
		send("PATCH", path+"/draft", `{"startsAt":null}`, &dr)
		if dr.Data.StartsAt != nil {
			t.Fatalf("draft startsAt: null left %v", dr.Data.StartsAt)
		}
		send("GET", path, "", nil)

		var me UserResponse
		send("PATCH", "/v1/me", `{"avatarColor":"#2f6fed"}`, &me)
		if me.Data.AvatarColor == nil {
			t.Fatal("colour not set")
		}
		send("PATCH", "/v1/me", `{"displayName":"Sam"}`, &me)
		if me.Data.AvatarColor == nil {
			t.Fatal("omitted avatarColor cleared it")
		}
		send("PATCH", "/v1/me", `{"avatarColor":null}`, &me)
		if me.Data.AvatarColor != nil {
			t.Fatalf("avatarColor: null left %q", *me.Data.AvatarColor)
		}
	})
}
