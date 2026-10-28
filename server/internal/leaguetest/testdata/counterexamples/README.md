# TestRosterInvariants counterexamples

rapid fail files found on 2026-10-13, all the late-trade bug through a waiver
claim made before the trade (see docs/testing.md, "Known bugs pinned by
skipped tests"). `...183209-81822.fail` is the shrunk one: 4 teams, no trade
review; a claim drops a goalkeeper, a 2-for-2 goalkeeper trade with him is
accepted after the trade deadline, and the waiver run leaves the team with 3
goalkeepers in gameweek 9.

They replay only against the test as of the commit that added them (later
commits steer the generator around the bug, so rapid reports them as "no
longer valid"):

    cd server/internal/leaguetest
    go test -run TestRosterInvariants -rapid.failfile=testdata/counterexamples/<file>
