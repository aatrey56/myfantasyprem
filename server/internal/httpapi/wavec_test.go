package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"testing"
	"time"
)

// rosterOf returns team's players now, in id order.
func (f demoFixture) rosterOf(t *testing.T, teamID string) []string {
	t.Helper()
	var page PlayerListResponse
	f.get(t, f.token, "/v1/leagues/"+f.league+"/players?status=rostered&teamId="+teamID+"&sort=name&limit=100", &page)
	out := listIDs(page)
	slices.Sort(out)
	return out
}

// The trade block: a manager puts own players on it, every member sees it
// (oldest first), only his team takes him off, and each change sends
// trade_block.updated to the league.
func TestTradeBlockAPI(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		base := "/v1/leagues/" + f.league
		mine, theirs := f.rosterOf(t, f.mine.ID), f.rosterOf(t, f.theirs.ID)
		a, b := mine[0], mine[1]
		before, _ := f.srv.rt.Since(context.Background(), f.league, 0)

		var put TradeBlockEntryResponse
		rec := doAs(f.h, f.token, http.MethodPut, base+"/trade-block/"+a, `{"note":" Open to offers ","wanted":["FWD","MID","FWD"]}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("put = %d %s", rec.Code, rec.Body)
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &put); err != nil {
			t.Fatal(err)
		}
		if e := put.Data; e.Player.Id != a || e.Team.Id != f.mine.ID || e.Note != "Open to offers" ||
			!slices.Equal(e.Wanted, []Position{"MID", "FWD"}) || !e.AddedAt.Equal(f.clock.t.UTC().Truncate(time.Microsecond)) {
			t.Fatalf("entry = %+v", e)
		}
		f.clock.t = f.clock.t.Add(time.Minute)
		if rec := doAs(f.h, f.token, http.MethodPut, base+"/trade-block/"+b, `{}`); rec.Code != http.StatusOK {
			t.Fatalf("put b = %d %s", rec.Code, rec.Body)
		}
		// Another team's player cannot go on my block.
		decodeProblem(t, doAs(f.h, f.token, http.MethodPut, base+"/trade-block/"+theirs[0], `{}`), http.StatusConflict)

		// The other manager sees both, oldest first.
		var block TradeBlockResponse
		f.get(t, f.other, base+"/trade-block", &block)
		var got []string
		for _, e := range block.Data {
			got = append(got, e.Player.Id)
		}
		if !slices.Equal(got, []string{a, b}) {
			t.Fatalf("block = %v, want %v", got, []string{a, b})
		}
		decodeProblem(t, doAs(f.h, f.other, http.MethodDelete, base+"/trade-block/"+a, ""), http.StatusForbidden)
		if rec := doAs(f.h, f.token, http.MethodDelete, base+"/trade-block/"+a, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("delete = %d", rec.Code)
		}
		f.get(t, f.other, base+"/trade-block", &block)
		if len(block.Data) != 1 || block.Data[0].Player.Id != b {
			t.Fatalf("block after delete = %+v", block.Data)
		}

		// Two puts and a delete: three events (the refused ones send none).
		after, _ := f.srv.rt.Since(context.Background(), f.league, 0)
		n := 0
		for _, ev := range after[len(before):] {
			if ev.Type == "trade_block.updated" {
				n++
			}
		}
		if n != 3 {
			t.Fatalf("%d trade_block.updated events, want 3", n)
		}
	})
}

// News is newest first. The demo world's five notes (demo/data
// bootstrap-static.json): Osei 15 Oct 12:15 (suspended), Tanaka 14 Oct
// 09:30 (doubtful, 75%), Thorne 13 Oct 19:00 (left), Fairbairn 12 Oct 17:00
// (injured), Castellan 10 Oct 08:45 (doubtful, 50%).
func TestPlayerNewsAPI(t *testing.T) {
	f := newDemoFixture(t)
	base := "/v1/leagues/" + f.league
	var news PlayerNewsResponse
	if rec := f.get(t, f.token, base+"/news", &news); rec.Code != http.StatusOK {
		t.Fatalf("news = %d %s", rec.Code, rec.Body)
	}
	type row struct {
		name   string
		chance int
	}
	var got []row
	for _, it := range news.Data {
		c := -1
		if it.ChanceOfPlaying != nil {
			c = *it.ChanceOfPlaying
		}
		if it.NewsAt == nil || it.Player.NewsAt == nil || !it.NewsAt.Equal(*it.Player.NewsAt) {
			t.Errorf("%s: newsAt %v, player newsAt %v", it.Player.DisplayName, it.NewsAt, it.Player.NewsAt)
		}
		got = append(got, row{it.Player.DisplayName, c})
	}
	want := []row{{"C. Osei", 0}, {"A. Tanaka", 75}, {"E. Thorne", 0}, {"B. Fairbairn", 0}, {"D. Castellan", 50}}
	if !slices.Equal(got, want) {
		t.Fatalf("news = %v, want %v", got, want)
	}
	if want := time.Date(2026, 10, 15, 12, 15, 0, 0, time.UTC); !news.Data[0].NewsAt.Equal(want) {
		t.Errorf("first newsAt = %v, want %v", news.Data[0].NewsAt, want)
	}

	// Filtered to a team, only its players with news; limit cuts the list.
	owned := map[string]string{}
	for _, it := range news.Data {
		if it.Owner != nil {
			owned[it.Player.Id] = it.Owner.Id
		}
	}
	var mine PlayerNewsResponse
	f.get(t, f.token, base+"/news?teamId="+f.mine.ID, &mine)
	for _, it := range mine.Data {
		if owned[it.Player.Id] != f.mine.ID {
			t.Errorf("team news has %s, owned by %q", it.Player.DisplayName, owned[it.Player.Id])
		}
	}
	n := 0
	for _, team := range owned {
		if team == f.mine.ID {
			n++
		}
	}
	if len(mine.Data) != n {
		t.Errorf("team news has %d items, want %d", len(mine.Data), n)
	}
	var two PlayerNewsResponse
	f.get(t, f.token, base+"/news?limit=2", &two)
	if len(two.Data) != 2 || two.Data[1].Player.DisplayName != "A. Tanaka" {
		t.Errorf("limit 2 = %+v", two.Data)
	}
}

// Tags are private, one per player, and filter the players list.
func TestPlayerTagsAPI(t *testing.T) {
	eachDemo(t, func(t *testing.T, f demoFixture) {
		base := "/v1/leagues/" + f.league
		var top PlayerListResponse
		f.get(t, f.token, base+"/players?status=all&sort=draft_rank&limit=3", &top)
		a, b, c := top.Data[0].Player.Id, top.Data[1].Player.Id, top.Data[2].Player.Id
		for _, x := range []struct{ id, tag string }{{a, "hot"}, {b, "sell"}, {c, "hot"}, {b, "hot"}, {a, "avoid"}} {
			if rec := doAs(f.h, f.token, http.MethodPut, base+"/tags/"+x.id, `{"tag":"`+x.tag+`"}`); rec.Code != http.StatusOK {
				t.Fatalf("tag %s %s = %d %s", x.id, x.tag, rec.Code, rec.Body)
			}
			f.clock.t = f.clock.t.Add(time.Minute)
		}
		// a is avoid; b and c are hot.
		var hot PlayerListResponse
		f.get(t, f.token, base+"/players?status=all&tag=hot&sort=draft_rank", &hot)
		if got, want := listIDs(hot), []string{b, c}; !slices.Equal(got, want) {
			t.Fatalf("tag=hot = %v, want %v", got, want)
		}
		var list PlayerTagListResponse
		f.get(t, f.token, base+"/tags", &list)
		var got []string
		for _, e := range list.Data {
			got = append(got, e.Player.Id+"="+string(e.Tag))
		}
		if want := []string{a + "=avoid", b + "=hot", c + "=hot"}; !slices.Equal(got, want) {
			t.Fatalf("tags = %v, want newest first %v", got, want)
		}
		var theirs PlayerTagListResponse
		f.get(t, f.other, base+"/tags", &theirs)
		if len(theirs.Data) != 0 {
			t.Fatalf("other manager sees %+v", theirs.Data)
		}
		var none PlayerListResponse
		f.get(t, f.other, base+"/players?tag=hot", &none)
		if len(none.Data) != 0 {
			t.Fatalf("other manager's tag filter = %v", listIDs(none))
		}
		decodeProblem(t, doAs(f.h, f.token, http.MethodPut, base+"/tags/"+a, `{"tag":"🔥"}`), http.StatusUnprocessableEntity)
		if rec := doAs(f.h, f.token, http.MethodDelete, base+"/tags/"+a, ""); rec.Code != http.StatusNoContent {
			t.Fatalf("untag = %d", rec.Code)
		}
		f.get(t, f.token, base+"/tags", &list)
		if len(list.Data) != 2 {
			t.Fatalf("after untag = %+v", list.Data)
		}
	})
}
