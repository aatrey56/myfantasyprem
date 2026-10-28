package fpl

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// testdata/element-summary/{1,3}.json are real history_past rows (FPL,
// 2026-10-17): element 1 (code 1001) carries Raya's five seasons, element 3
// (code 1003) Henderson's seventeen, with no 2024/25 (he played abroad).

func TestSeasonHistoryMapsTotalsAndLinksByCode(t *testing.T) {
	got, err := dirClient().SeasonHistory(context.Background(), "1001")
	if err != nil {
		t.Fatal(err)
	}
	// Raya: 2021/22 .. 2025/26, oldest first as FPL sends them.
	if len(got) != 5 || got[0].Season != "2021/22" || got[4].Season != "2025/26" {
		t.Fatalf("seasons = %+v", got)
	}
	last := got[4]
	if last.PlayerExternalID != "1001" {
		t.Errorf("external id = %q", last.PlayerExternalID)
	}
	// From the 2025/26 row: 162 pts, 3330 min, 19 CS, 11 bonus, 60 saves,
	// 37 starts, xA "0.07", ICT "57.5", 37 CBI, 304 recoveries, 1 tackle,
	// DC 0 (a keeper: tracked, genuinely zero).
	checkInt(t, "points", last.TotalPoints, 162)
	checkInt(t, "minutes", last.Minutes, 3330)
	checkInt(t, "clean sheets", last.CleanSheets, 19)
	checkInt(t, "bonus", last.Bonus, 11)
	checkInt(t, "saves", last.Saves, 60)
	checkInt(t, "starts", last.Starts, 37)
	checkInt(t, "cbi", last.ClearancesBlocksInterceptions, 37)
	checkInt(t, "recoveries", last.Recoveries, 304)
	checkInt(t, "tackles", last.Tackles, 1)
	checkInt(t, "dc", last.DefensiveContribution, 0)
	checkFloat(t, "xA", last.ExpectedAssists, 0.07)
	checkFloat(t, "ict", last.ICTIndex, 57.5)
}

func TestSeasonHistoryNullsStatsFPLDidNotTrack(t *testing.T) {
	got, err := dirClient().SeasonHistory(context.Background(), "1003")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]provider.PlayerSeason{}
	for _, s := range got {
		by[s.Season] = s
	}
	// 2008/09 .. 2025/26 is 18 seasons; Henderson has none for 2024/25.
	if len(got) != 17 {
		t.Fatalf("%d seasons, want 17", len(got))
	}
	if _, ok := by["2024/25"]; ok {
		t.Error("2024/25 present; Henderson played abroad")
	}
	// 2015/16: ICT, starts, xG and the defensive stats all untracked
	// (FPL sends 0); goals, assists and bonus are real (2, 3, 3).
	s := by["2015/16"]
	checkInt(t, "2015/16 goals", s.GoalsScored, 2)
	checkInt(t, "2015/16 assists", s.Assists, 3)
	checkInt(t, "2015/16 bonus", s.Bonus, 3)
	for name, v := range map[string]any{
		"ict": s.ICTIndex, "influence": s.Influence, "starts": s.Starts, "xG": s.ExpectedGoals,
		"xA": s.ExpectedAssists, "xGI": s.ExpectedGoalInvolvements, "xGC": s.ExpectedGoalsConceded,
		"dc": s.DefensiveContribution, "tackles": s.Tackles, "cbi": s.ClearancesBlocksInterceptions,
		"recoveries": s.Recoveries,
	} {
		if !isNil(v) {
			t.Errorf("2015/16 %s = %v, want nil", name, v)
		}
	}
	// 2016/17: ICT tracked (115.5); starts and xG not yet.
	s = by["2016/17"]
	checkFloat(t, "2016/17 ict", s.ICTIndex, 115.5)
	if s.Starts != nil || s.ExpectedGoals != nil {
		t.Errorf("2016/17 starts/xG = %v/%v, want nil", s.Starts, s.ExpectedGoals)
	}
	// 2022/23: starts 23, xG 1.35, xGI 4.93; DC still untracked.
	s = by["2022/23"]
	checkInt(t, "2022/23 starts", s.Starts, 23)
	checkFloat(t, "2022/23 xG", s.ExpectedGoals, 1.35)
	checkFloat(t, "2022/23 xGI", s.ExpectedGoalInvolvements, 4.93)
	if s.DefensiveContribution != nil || s.Tackles != nil {
		t.Error("2022/23 defensive stats tracked, want nil")
	}
	// 2025/26: DC 183, tackles 27, CBI 58, recoveries 98.
	s = by["2025/26"]
	checkInt(t, "2025/26 dc", s.DefensiveContribution, 183)
	checkInt(t, "2025/26 tackles", s.Tackles, 27)
	checkInt(t, "2025/26 cbi", s.ClearancesBlocksInterceptions, 58)
	checkInt(t, "2025/26 recoveries", s.Recoveries, 98)
}

func TestSeasonHistoryUnknownPlayerIsNotFound(t *testing.T) {
	_, err := dirClient().SeasonHistory(context.Background(), "999999")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	// Element 2 (code 1002) exists but has no recorded summary: a 404.
	_, err = dirClient().SeasonHistory(context.Background(), "1002")
	if !errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDecodeHistoryPastRejectsBadSeasons(t *testing.T) {
	for name, body := range map[string]string{
		"label":     `{"history_past":[{"season_name":"2021-22"}]}`,
		"years":     `{"history_past":[{"season_name":"2021/23"}]}`,
		"duplicate": `{"history_past":[{"season_name":"2021/22"},{"season_name":"2021/22"}]}`,
	} {
		if _, err := decodeHistoryPast([]byte(body), "1"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	// 1999/00 wraps the century.
	got, err := decodeHistoryPast([]byte(`{"history_past":[{"season_name":"1999/00","minutes":null}]}`), "1")
	if err != nil || len(got) != 1 || got[0].Minutes != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func checkInt(t *testing.T, name string, got *int, want int) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %d", name, deref(got), want)
	}
}

func checkFloat(t *testing.T, name string, got *float64, want float64) {
	t.Helper()
	if got == nil || *got != want {
		t.Errorf("%s = %v, want %v", name, deref(got), want)
	}
}

func deref[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

func isNil(v any) bool {
	switch p := v.(type) {
	case *int:
		return p == nil
	case *float64:
		return p == nil
	}
	return false
}

// testFS is the recorded testdata with some files replaced.
func testFS(t *testing.T, over map[string]string) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for _, p := range []string{"bootstrap-static.json", "element-summary/1.json", "element-summary/3.json"} {
		b, err := os.ReadFile("testdata/" + p)
		if err != nil {
			t.Fatal(err)
		}
		m[p] = &fstest.MapFile{Data: b}
	}
	for p, body := range over {
		m[p] = &fstest.MapFile{Data: []byte(body)}
	}
	return m
}

// After FPL's season rollover element ids are reassigned: a stale index
// fetches another player's summary. The rows' element_code gives it away,
// and nothing is returned for the wrong player.
func TestSeasonHistoryRejectsAnotherPlayersSummary(t *testing.T) {
	fsys := testFS(t, nil)
	// Element 1 now answers with Henderson's (code 1003) rows.
	fsys["element-summary/1.json"] = fsys["element-summary/3.json"]
	c := New(Options{Transport: DirTransport{FS: fsys}, Logger: quiet(), MinInterval: -1})
	got, err := c.SeasonHistory(ctx, "1001")
	if err == nil || errors.Is(err, provider.ErrNotFound) {
		t.Fatalf("got %d seasons, err %v; want an identity error", len(got), err)
	}
}

// RefreshIndex re-reads bootstrap, so the next lookup uses the new ids.
func TestRefreshIndexPicksUpReassignedIDs(t *testing.T) {
	fsys := testFS(t, nil)
	c := New(Options{Transport: DirTransport{FS: fsys}, Logger: quiet(), MinInterval: -1})
	if _, err := c.SeasonHistory(ctx, "1001"); err != nil {
		t.Fatal(err)
	}
	// Rollover: codes 1001 and 1003 swap element ids 1 and 3.
	boot := strings.Replace(string(fsys["bootstrap-static.json"].Data), `"code": 1001`, `"code": X`, 1)
	boot = strings.Replace(boot, `"code": 1003`, `"code": 1001`, 1)
	boot = strings.Replace(boot, `"code": X`, `"code": 1003`, 1)
	fsys["bootstrap-static.json"] = &fstest.MapFile{Data: []byte(boot)}
	fsys["element-summary/1.json"], fsys["element-summary/3.json"] = fsys["element-summary/3.json"], fsys["element-summary/1.json"]
	if err := c.RefreshIndex(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := c.SeasonHistory(ctx, "1001")
	// Raya's five seasons, now from element 3.
	if err != nil || len(got) != 5 {
		t.Fatalf("got %d seasons, %v", len(got), err)
	}
}

type countingTransport struct {
	inner http.RoundTripper
	paths []string
}

func (c *countingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	c.paths = append(c.paths, req.URL.Path)
	return c.inner.RoundTrip(req)
}

// A code no longer in FPL's bootstrap is not found from the cached index;
// it does not refetch bootstrap on every call (the batch refreshes it).
func TestSeasonHistoryUnknownCodeDoesNotRefetchBootstrap(t *testing.T) {
	tr := &countingTransport{inner: DirTransport{FS: os.DirFS("testdata")}}
	c := New(Options{Transport: tr, Logger: quiet(), MinInterval: -1})
	for range 3 {
		if _, err := c.SeasonHistory(ctx, "999999"); !errors.Is(err, provider.ErrNotFound) {
			t.Fatalf("err = %v", err)
		}
	}
	if len(tr.paths) != 1 {
		t.Fatalf("requests %v, want one bootstrap", tr.paths)
	}
}

// FPL relaunches in mid/late July: until then bootstrap is last season's
// game. testdata's gameweek 1 deadline is 2026-08-14.
func TestSeasonLiveReadsBootstrapsFirstDeadline(t *testing.T) {
	for since, want := range map[time.Time]bool{
		time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC): true,  // 2026/27 game is live
		time.Date(2027, 7, 1, 0, 0, 0, 0, time.UTC): false, // still the 2026/27 game
	} {
		got, first, err := dirClient().SeasonLive(ctx, since)
		if err != nil || got != want || !first.Equal(time.Date(2026, 8, 14, 17, 30, 0, 0, time.UTC)) {
			t.Errorf("SeasonLive(%v) = %v, %v, %v; want %v, 2026-08-14 17:30", since, got, first, err, want)
		}
	}
}
