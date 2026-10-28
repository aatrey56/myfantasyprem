package ids

import (
	"regexp"
	"strings"
	"testing"
	"time"
)

// pattern is the id pattern from api/openapi.yaml with the prefix filled in.
var pattern = regexp.MustCompile(`^lg_[0-9a-hjkmnp-tv-z]{26}$`)

func TestNewMatchesContractPattern(t *testing.T) {
	seen := map[string]bool{}
	for range 1000 {
		id := New("lg")
		if !pattern.MatchString(id) {
			t.Fatalf("New = %q does not match the contract pattern", id)
		}
		if !Valid("lg", id) {
			t.Fatalf("Valid rejected %q", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestEncodeKnownValues(t *testing.T) {
	var zero [10]byte
	if got := encode(time.UnixMilli(0), zero); got != strings.Repeat("0", 26) {
		t.Fatalf("zero = %q", got)
	}
	var ones [10]byte
	for i := range ones {
		ones[i] = 0xff
	}
	// Maximum timestamp and randomness: the largest ULID.
	if got := encode(time.UnixMilli(1<<48-1), ones); got != "7"+strings.Repeat("z", 25) {
		t.Fatalf("max = %q", got)
	}
	// 2025-01-01T00:00:00Z is 1735689600000 ms; the timestamp is the first 10 chars.
	if got := encode(time.UnixMilli(1735689600000), zero); got[:10] != "01jgfjjz00" || got[10:] != strings.Repeat("0", 16) {
		t.Fatalf("2025 = %q", got)
	}
}

func TestIDsSortByTime(t *testing.T) {
	var r [10]byte
	a := encode(time.UnixMilli(1_000_000), r)
	b := encode(time.UnixMilli(1_000_001), r)
	if a >= b {
		t.Fatalf("%q should sort before %q", a, b)
	}
}

func TestValid(t *testing.T) {
	const ok = "01k7m2xq8rfv3c9a4t6wbn5hdy"
	tests := []struct {
		name, prefix, s string
		want            bool
	}{
		{"valid", "lg", "lg_" + ok, true},
		{"longer prefix", "plr", "plr_" + ok, true},
		{"wrong prefix", "tm", "lg_" + ok, false},
		{"prefix is not a prefix of another", "l", "lg_" + ok, false},
		{"empty", "lg", "", false},
		{"no separator", "lg", "lg" + ok, false},
		{"too short", "lg", "lg_" + ok[1:], false},
		{"too long", "lg", "lg_" + ok + "0", false},
		{"uppercase", "lg", "lg_" + strings.ToUpper(ok), false},
		{"excluded letter i", "lg", "lg_" + "i" + ok[1:], false},
		{"excluded letter l", "lg", "lg_" + "l" + ok[1:], false},
		{"excluded letter o", "lg", "lg_" + "o" + ok[1:], false},
		{"excluded letter u", "lg", "lg_" + "u" + ok[1:], false},
		{"fpl integer", "lg", "12345", false},
		{"old placeholder", "lg", "lg_1", false},
		{"trailing newline", "lg", "lg_" + ok + "\n", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Valid(tt.prefix, tt.s); got != tt.want {
				t.Fatalf("Valid(%q, %q) = %v, want %v", tt.prefix, tt.s, got, tt.want)
			}
		})
	}
}
