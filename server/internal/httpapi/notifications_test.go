package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/demo"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
)

// A trade offered over the API lands in the receiver's inbox, and is
// queued for push only while their trade_offered switch is on.
func TestTradeOfferNotifiesTheReceiver(t *testing.T) {
	srv, _ := newTestServer()
	ctx := context.Background()
	if _, err := demo.LoadStats(ctx, srv.stats, testOptions().Logger, srv.now); err != nil {
		t.Fatal(err)
	}
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	commish := userOf(t, srv, testToken)
	l, err := srv.leagues.Create(ctx, commish, "Notify League", 4, "One", league.SettingsInput{})
	if err != nil {
		t.Fatal(err)
	}
	inv, _ := srv.leagues.CreateInvite(ctx, commish, l.ID, nil, nil)
	tokens := []string{testToken}
	for i := range 3 {
		tok := signIn(srv, "Player", "n"+string(rune('a'+i))+"@example.com")
		tokens = append(tokens, tok)
		if _, _, err := srv.leagues.AcceptInvite(ctx, userOf(t, srv, tok), inv.Code, "Side "+string(rune('A'+i))); err != nil {
			t.Fatal(err)
		}
	}
	for _, tok := range tokens {
		_, _ = srv.drafts().SetAutopick(ctx, userOf(t, srv, tok), l.ID, true)
	}
	if _, err := srv.drafts().Start(ctx, commish, l.ID); err != nil {
		t.Fatal(err)
	}
	st, _ := srv.leagues.Repo().State(ctx, l.ID)
	teams, gw := st.League.Teams, st.Game.Rosters[0].FromGW
	receiver := userOf(t, srv, tokens[1])
	if teams[1].OwnerID != receiver {
		t.Fatalf("team order: %+v", teams)
	}

	// The receiver registers a device and keeps trade offers on.
	if rec := doAs(h, tokens[1], http.MethodPost, "/v1/me/push-subscriptions", webPushBody); rec.Code != http.StatusCreated {
		t.Fatalf("subscribe = %d %s", rec.Code, rec.Body)
	}
	offer := func(i int) {
		t.Helper()
		body := `{"receivingTeamId":"` + teams[1].ID + `","offeredPlayerIds":["` + st.Game.RosterAt(teams[0].ID, gw)[i] +
			`"],"requestedPlayerIds":["` + st.Game.RosterAt(teams[1].ID, gw)[i] + `"]}`
		if rec := do(h, http.MethodPost, "/v1/leagues/"+l.ID+"/trades", body); rec.Code != http.StatusCreated {
			t.Fatalf("propose = %d %s", rec.Code, rec.Body)
		}
	}
	offer(0)

	var inbox NotificationInboxResponse
	rec := doAs(h, tokens[1], http.MethodGet, "/v1/me/notifications", "")
	if err := json.Unmarshal(rec.Body.Bytes(), &inbox); err != nil || rec.Code != 200 {
		t.Fatalf("inbox = %d %s", rec.Code, rec.Body)
	}
	if inbox.Data.UnreadCount != 1 || inbox.Data.Items[0].Type != string(notifications.TradeOffered) ||
		inbox.Data.Items[0].LeagueId == nil || *inbox.Data.Items[0].LeagueId != l.ID {
		t.Fatalf("receiver inbox = %+v", inbox.Data)
	}
	// The proposer and the rest of the league hear nothing.
	for _, tok := range []string{tokens[0], tokens[2]} {
		_ = json.Unmarshal(doAs(h, tok, http.MethodGet, "/v1/me/notifications", "").Body.Bytes(), &inbox)
		if inbox.Data.UnreadCount != 0 {
			t.Fatalf("bystander inbox = %+v", inbox.Data)
		}
	}
	due, _ := srv.notifs.Due(ctx, time.Now().Add(time.Hour), time.Minute, 10)
	if len(due) != 1 || due[0].Notification.UserID != receiver {
		t.Fatalf("push queue = %+v", due)
	}

	// Trade offers off in this league: the next offer is inbox only.
	body := `{"leagueId":"` + l.ID + `","settings":[{"type":"trade_offered","enabled":false}]}`
	var prefs NotificationPreferencesResponse
	rec = doAs(h, tokens[1], http.MethodPut, "/v1/me/notifications/preferences", body)
	if err := json.Unmarshal(rec.Body.Bytes(), &prefs); err != nil || rec.Code != 200 {
		t.Fatalf("prefs = %d %s", rec.Code, rec.Body)
	}
	if s := prefs.Data.Leagues[0].Settings[0]; s.Type != "trade_offered" || s.Enabled {
		t.Fatalf("setting = %+v", s)
	}
	offer(1)
	if due, _ := srv.notifs.Due(ctx, time.Now().Add(2*time.Hour), time.Minute, 10); len(due) != 1 {
		t.Fatalf("pushed with the switch off: %d queued", len(due))
	}
	_ = json.Unmarshal(doAs(h, tokens[1], http.MethodGet, "/v1/me/notifications", "").Body.Bytes(), &inbox)
	if inbox.Data.UnreadCount != 2 {
		t.Fatalf("inbox after second offer = %d", inbox.Data.UnreadCount)
	}

	// Mark one read, then all.
	rec = doAs(h, tokens[1], http.MethodPost, "/v1/me/notifications/read", `{"ids":["`+inbox.Data.Items[0].Id+`"]}`)
	_ = json.Unmarshal(doAs(h, tokens[1], http.MethodGet, "/v1/me/notifications?unread=true", "").Body.Bytes(), &inbox)
	if rec.Code != 204 || inbox.Data.UnreadCount != 1 || len(inbox.Data.Items) != 1 {
		t.Fatalf("after one read: %d %+v", rec.Code, inbox.Data)
	}
	doAs(h, tokens[1], http.MethodPost, "/v1/me/notifications/read", `{"all":true}`)
	_ = json.Unmarshal(doAs(h, tokens[1], http.MethodGet, "/v1/me/notifications", "").Body.Bytes(), &inbox)
	if inbox.Data.UnreadCount != 0 || inbox.Data.Items[0].ReadAt == nil {
		t.Fatalf("after all read: %+v", inbox.Data)
	}
}

func TestAppConfigWebPushKey(t *testing.T) {
	srv, _ := newTestServer()
	h := newHandler(testOptions(), newRoutes(testOptions(), srv))
	var cfg AppConfigResponse
	_ = json.Unmarshal(doAnon(h, http.MethodGet, "/v1/config", "").Body.Bytes(), &cfg)
	if cfg.Data.WebPushPublicKey != nil {
		t.Fatalf("key without Web Push: %v", *cfg.Data.WebPushPublicKey)
	}
	srv.webPushKey = "BKey"
	_ = json.Unmarshal(doAnon(h, http.MethodGet, "/v1/config", "").Body.Bytes(), &cfg)
	if cfg.Data.WebPushPublicKey == nil || *cfg.Data.WebPushPublicKey != "BKey" {
		t.Fatalf("key = %v", cfg.Data.WebPushPublicKey)
	}
}
