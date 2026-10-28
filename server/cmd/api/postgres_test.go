package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/aatrey56/myfantasyprem/server/internal/auth"
	"github.com/aatrey56/myfantasyprem/server/internal/store/storetest"
)

// On Postgres the server runs River (the tick job appears) and the
// realtime channel works end to end through LISTEN/NOTIFY.
func TestRunOnPostgresWithJobsAndRealtime(t *testing.T) {
	pool, url := storetest.NewWithURL(t)
	base := startRun(t, map[string]string{"HTTP_ADDR": "127.0.0.1:0", "DATABASE_URL": url, "JOBS_TICK": "1s"})
	ctx := context.Background()

	u, err := auth.NewPostgres(pool, time.Now).CreateUser(ctx, "Sam", "sam@example.com")
	if err != nil {
		t.Fatal(err)
	}
	tok, err := auth.NewPostgres(pool, time.Now).Issue(ctx, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	req, _ := http.NewRequest("POST", base+"/v1/realtime/tickets", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	var tk struct{ Data struct{ URL string } }
	_ = json.NewDecoder(res.Body).Decode(&tk)
	res.Body.Close()
	if res.StatusCode != 201 || !strings.HasPrefix(tk.Data.URL, "ws://127.0.0.1:") {
		t.Fatalf("ticket = %d %+v", res.StatusCode, tk)
	}
	dctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(dctx, tk.Data.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if err := c.Write(dctx, websocket.MessageText, []byte(`{"op":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	if _, data, err := c.Read(dctx); err != nil || !strings.Contains(string(data), `"type":"pong"`) {
		t.Fatalf("pong = %s %v", data, err)
	}

	deadline := time.Now().Add(10 * time.Second)
	for {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'tick' AND state = 'completed'`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the tick job never completed")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
