package stats

import "testing"

// mark marks gw and returns its mark as a rescore starting now would read it.
func mark(t *testing.T, repo Repo, gw int) RescoreMark {
	t.Helper()
	if err := repo.Apply(ctx, Batch{Rescore: []int{gw}}); err != nil {
		t.Fatal(err)
	}
	marks := pending(t, repo)
	if len(marks) != 1 || marks[0].Gameweek != gw {
		t.Fatalf("pending = %+v, want gameweek %d", marks, gw)
	}
	return marks[0]
}

func pending(t *testing.T, repo Repo) []RescoreMark {
	t.Helper()
	got, err := repo.RescorePending(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// A rescore clears the mark it started from.
func TestRescoreClearsItsMark(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		m := mark(t, repo, 2) // the rescore starts
		// The rescore finishes.
		if err := repo.ClearRescore(ctx, m); err != nil {
			t.Fatal(err)
		}
		if got := pending(t, repo); len(got) != 0 {
			t.Errorf("pending after rescore = %+v, want none", got)
		}
	})
}

// A gameweek marked again while its rescore runs (another poller ingested
// newer stats) stays marked, with the newer version, when that rescore
// finishes.
func TestRescoreKeepsMarkSetDuringRescore(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		v1 := mark(t, repo, 2) // the rescore starts
		v2 := mark(t, repo, 2) // a concurrent ingest
		if v2.Version <= v1.Version {
			t.Fatalf("version after a second mark = %d, want > %d", v2.Version, v1.Version)
		}
		if err := repo.ClearRescore(ctx, v1); err != nil { // the rescore finishes
			t.Fatal(err)
		}
		if got := pending(t, repo); len(got) != 1 || got[0] != v2 {
			t.Errorf("pending after rescore = %+v, want [%+v]: the newer mark was lost", got, v2)
		}
		// The next rescore read v2 and clears it.
		if err := repo.ClearRescore(ctx, v2); err != nil {
			t.Fatal(err)
		}
		if got := pending(t, repo); len(got) != 0 {
			t.Errorf("pending after second rescore = %+v, want none", got)
		}
	})
}

// A version is never reused: a stale rescore cannot clear a mark set after
// the one it read was cleared.
func TestRescoreVersionNotReusedAfterClear(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		v1 := mark(t, repo, 2)
		if err := repo.ClearRescore(ctx, v1); err != nil {
			t.Fatal(err)
		}
		v2 := mark(t, repo, 2)
		if err := repo.ClearRescore(ctx, v1); err != nil { // a late, stale clear
			t.Fatal(err)
		}
		if got := pending(t, repo); len(got) != 1 || got[0] != v2 {
			t.Errorf("pending = %+v, want [%+v]", got, v2)
		}
	})
}

// A gameweek marked as checked for starts stays marked; marking twice is
// one mark.
func TestStartsChecked(t *testing.T) {
	impls(t, func(t *testing.T, repo Repo) {
		for _, gw := range []int{3, 3, 1} {
			if err := repo.MarkStartsChecked(ctx, gw); err != nil {
				t.Fatal(err)
			}
		}
		got, err := repo.StartsChecked(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || !got[1] || !got[3] {
			t.Errorf("checked = %v, want gameweeks 1 and 3", got)
		}
	})
}
