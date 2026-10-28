package httpapi

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// leagueInputCases is api/testdata/league-inputs.json, shared with the app's
// parser tests (apps/app/src/api/league-input.test.ts).
type leagueInputCases struct {
	Messages map[string]string `json:"messages"`
	Cases    []struct {
		Input string `json:"input"`
		ID    int64  `json:"id"`
		Error string `json:"error"`
	} `json:"cases"`
}

func loadLeagueInputCases(t *testing.T) leagueInputCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "api", "testdata", "league-inputs.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c leagueInputCases
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if len(c.Cases) == 0 {
		t.Fatal("no cases")
	}
	return c
}

func TestParseLeagueInputSharedCases(t *testing.T) {
	c := loadLeagueInputCases(t)
	for _, tt := range c.Cases {
		t.Run(tt.Input, func(t *testing.T) {
			got, err := parseLeagueInput(tt.Input)
			switch tt.Error {
			case "":
				if err != nil || got != tt.ID {
					t.Fatalf("got (%d, %v), want (%d, nil)", got, err, tt.ID)
				}
			case "classic":
				if !errors.Is(err, errClassicLeagueURL) {
					t.Fatalf("got (%d, %v), want the classic-link error", got, err)
				}
			default:
				want, ok := c.Messages[tt.Error]
				if !ok {
					t.Fatalf("unknown error key %q", tt.Error)
				}
				if err == nil || err.Error() != want {
					t.Fatalf("got (%d, %v), want %q", got, err, want)
				}
			}
		})
	}
}

// The messages reach the import screen as problem details: plain English,
// never a request field name.
func TestLeagueInputMessagesArePlainEnglish(t *testing.T) {
	c := loadLeagueInputCases(t)
	for key, msg := range map[string]string{"empty": msgLeagueInputEmpty, "invalid": msgLeagueInputInvalid, "missingId": msgLeagueInputMissingID} {
		if c.Messages[key] != msg {
			t.Errorf("%s: server says %q, shared cases say %q", key, msg, c.Messages[key])
		}
	}
	for _, tc := range []struct {
		url *string
		id  *int64
	}{{nil, nil}, {ptr("1"), ptr(int64(1))}, {nil, ptr(int64(0))}, {ptr("https://example.com/"), nil}} {
		_, err := resolveLeagueRef(tc.url, tc.id)
		if err == nil {
			t.Fatalf("resolveLeagueRef(%v, %v) = nil error", tc.url, tc.id)
		}
		for _, field := range []string{"draftLeagueUrl", "fplLeagueId"} {
			if strings.Contains(err.Error(), field) {
				t.Errorf("message %q names the field %s", err, field)
			}
		}
	}
}
