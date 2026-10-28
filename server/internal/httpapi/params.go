package httpapi

// Path and query checks. The generated code does not enforce `pattern`,
// so handlers check ids themselves and answer 400 with these details.

func lg(id string) string  { return badID("leagueId", "lg", id) }
func tm(id string) string  { return badID("teamId", "tm", id) }
func trd(id string) string { return badID("tradeId", "trd", id) }
func wv(id string) string  { return badID("claimId", "wv", id) }

func optTeam(id *string) string {
	if id == nil {
		return ""
	}
	return tm(*id)
}

func optGameweek(gw *int) string {
	if gw == nil {
		return ""
	}
	return badGameweek(*gw)
}
