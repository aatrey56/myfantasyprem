package httpapi

import (
	"errors"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

const (
	draftHost   = "draft.premierleague.com"
	classicHost = "fantasy.premierleague.com"
)

// Messages for input that is not a league. They reach the import screen as
// problem details, so they are plain English and name no request field. The
// app says the same (apps/app/src/api/league-input.ts); both are checked
// against api/testdata/league-inputs.json.
const (
	msgLeagueInputEmpty   = "Paste your league link or league ID."
	msgLeagueInputInvalid = "That doesn't look like an FPL Draft league link. Paste the link from your league page, or just the league ID."
	// A draft.premierleague.com league page with no id in it (FPL Draft's own /league page).
	msgLeagueInputMissingID = "That page's link doesn't include your league ID. Type just your league's ID number instead."
)

// errClassicLeagueURL means the link points at classic FPL, not FPL Draft.
// Draft and classic league ids overlap, so importing such a link by its
// number would silently mirror the wrong league.
var errClassicLeagueURL = errors.New("classic FPL league link")

var (
	bareLeagueID = regexp.MustCompile(`^[0-9][0-9\s\p{Zs}]*$`)
	leagueID     = regexp.MustCompile(`^[0-9]{1,12}$`)
	hasScheme    = regexp.MustCompile(`(?i)^[a-z][a-z0-9+.-]*://`)
)

// toLeagueID is the id in s: 1 to 12 digits, above zero.
func toLeagueID(s string) (int64, bool) {
	if !leagueID.MatchString(s) {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

// parseLeagueInput extracts the numeric FPL Draft league id from what a user
// pasted: a bare id (spaces allowed), or a draft.premierleague.com link with
// or without www., the scheme, a locale segment (/en/), a trailing path,
// query or fragment. The id is the path segment after "league" or "leagues"
// (/league/12345/standings), or the "league" query parameter. A link inside
// other text (a shared message) is found by its host.
//
// It returns errClassicLeagueURL for fantasy.premierleague.com links. Every
// other error is safe to show to the client as a 400 detail.
func parseLeagueInput(raw string) (int64, error) {
	text := strings.TrimSpace(raw)
	if text == "" {
		return 0, errors.New(msgLeagueInputEmpty)
	}
	if bareLeagueID.MatchString(text) {
		if id, ok := toLeagueID(strings.Join(strings.Fields(text), "")); ok {
			return id, nil
		}
		return 0, errors.New(msgLeagueInputInvalid)
	}
	if fields := strings.Fields(text); len(fields) > 1 {
		text = ""
		for _, f := range fields {
			if strings.Contains(strings.ToLower(f), "premierleague.com") {
				text = f
				break
			}
		}
		if text == "" {
			return 0, errors.New(msgLeagueInputInvalid)
		}
	}
	if !hasScheme.MatchString(text) {
		text = "https://" + text
	}
	u, err := url.Parse(text)
	if err != nil || u.Host == "" {
		return 0, errors.New(msgLeagueInputInvalid)
	}
	host := strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
	if host == classicHost || strings.HasSuffix(host, "."+classicHost) {
		return 0, errClassicLeagueURL
	}
	if host != draftHost || (u.Scheme != "https" && u.Scheme != "http") {
		return 0, errors.New(msgLeagueInputInvalid)
	}

	segments := strings.FieldsFunc(u.Path, func(r rune) bool { return r == '/' })
	at := -1
	for i, seg := range segments {
		if seg = strings.ToLower(seg); seg == "league" || seg == "leagues" {
			at = i
			break
		}
	}
	if at >= 0 && at+1 < len(segments) {
		if id, ok := toLeagueID(segments[at+1]); ok {
			return id, nil
		}
	}
	if id, ok := toLeagueID(u.Query().Get("league")); ok {
		return id, nil
	}
	if at >= 0 && at+1 == len(segments) {
		return 0, errors.New(msgLeagueInputMissingID)
	}
	return 0, errors.New(msgLeagueInputInvalid)
}
