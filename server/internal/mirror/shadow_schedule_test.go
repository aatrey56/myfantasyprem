package mirror_test

import (
	"testing"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/mirror"
	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

func at(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

func kick(s string) *time.Time { t := at(s); return &t }

// A hand-worked weekend: GW9's waivers Thursday 23:30 BST (22:30 UTC),
// deadline Friday 18:30 UTC, a Friday 19:00 match, two Saturday 14:00
// matches sharing every slot, and a postponed match with no date.
func TestShadowTimes(t *testing.T) {
	gws := []stats.Gameweek{{Number: 9, WaiverDeadlineAt: at("2026-10-22T22:30:00Z"), DeadlineAt: at("2026-10-23T18:30:00Z")}}
	fx := []stats.Fixture{
		{ID: "fx1", Gameweek: 9, KickoffAt: kick("2026-10-23T19:00:00Z")},
		{ID: "fx2", Gameweek: 9, KickoffAt: kick("2026-10-24T14:00:00Z")},
		{ID: "fx3", Gameweek: 9, KickoffAt: kick("2026-10-24T14:00:00Z")},
		{ID: "fx4", Gameweek: 0},
	}
	got := mirror.ShadowTimes(gws, fx, at("2026-10-20T00:00:00Z"), at("2026-10-27T00:00:00Z"))
	want := []mirror.ShadowSlot{
		{At: at("2026-10-22T22:40:00Z"), Reason: mirror.ReasonAfterWaivers},
		{At: at("2026-10-23T18:15:00Z"), Reason: mirror.ReasonBeforeDeadline},
		{At: at("2026-10-23T18:40:00Z"), Reason: mirror.ReasonAfterDeadline},
		{At: at("2026-10-23T19:00:00Z"), Reason: mirror.ReasonKickoff},
		{At: at("2026-10-23T19:55:00Z"), Reason: mirror.ReasonHalfTime},
		{At: at("2026-10-23T20:55:00Z"), Reason: mirror.ReasonFullTime},
		{At: at("2026-10-23T21:55:00Z"), Reason: mirror.ReasonBonus},
		{At: at("2026-10-24T14:00:00Z"), Reason: mirror.ReasonKickoff},
		{At: at("2026-10-24T14:55:00Z"), Reason: mirror.ReasonHalfTime},
		{At: at("2026-10-24T15:55:00Z"), Reason: mirror.ReasonFullTime},
		{At: at("2026-10-24T16:55:00Z"), Reason: mirror.ReasonBonus},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d slots, want %d: %+v", len(got), len(want), got)
	}
	for i := range want {
		if !got[i].At.Equal(want[i].At) || got[i].Reason != want[i].Reason {
			t.Errorf("slot %d = %v %s, want %v %s", i, got[i].At, got[i].Reason, want[i].At, want[i].Reason)
		}
	}

	// The window is half-open: only Saturday's slots from 14:55.
	got = mirror.ShadowTimes(gws, fx, at("2026-10-24T14:55:00Z"), at("2026-10-24T16:55:00Z"))
	if len(got) != 2 || got[0].Reason != mirror.ReasonHalfTime || got[1].Reason != mirror.ReasonFullTime {
		t.Errorf("window = %+v", got)
	}
}

func TestDataCheckedSlot(t *testing.T) {
	gws := []stats.Gameweek{
		{Number: 8, DeadlineAt: at("2026-10-16T17:30:00Z"), DataChecked: true},
		{Number: 9, DeadlineAt: at("2026-10-23T18:30:00Z")},
	}
	fx := []stats.Fixture{{Gameweek: 8, KickoffAt: kick("2026-10-19T19:00:00Z")}, {Gameweek: 8, KickoffAt: kick("2026-10-18T14:00:00Z")}}
	s, ok := mirror.DataCheckedSlot(gws, fx, at("2026-10-21T09:00:00Z"))
	if !ok || !s.At.Equal(at("2026-10-19T19:00:00Z")) || s.Reason != mirror.ReasonDataChecked {
		t.Errorf("slot = %+v %v", s, ok)
	}
	// After the next deadline it is no longer due.
	if _, ok := mirror.DataCheckedSlot(gws, fx, at("2026-10-23T18:31:00Z")); ok {
		t.Error("data_checked slot after the next deadline")
	}
	gws[0].DataChecked = false
	if _, ok := mirror.DataCheckedSlot(gws, fx, at("2026-10-21T09:00:00Z")); ok {
		t.Error("data_checked slot before FPL checked the data")
	}
}
