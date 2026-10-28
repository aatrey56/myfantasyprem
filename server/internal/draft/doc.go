// Package draft runs snake drafts: the commissioner schedules, orders,
// starts, pauses and resumes the draft; managers pick on a server-side
// clock; autopick takes the team's queue and then the best available player
// by draft rank at a position the squad still needs; the last pick creates
// the rosters, first lineups, the H2H schedule and the waiver order and puts
// the league in season.
//
// Every change is one transaction on the league's State (league.Tx), with
// the league row locked, so two picks for one slot queue up and exactly one
// wins (the unique (draft, overall) and (draft, player) constraints back it
// up). The pick clock is durable: its deadline is derived from
// drafts.clock_started_at, a ClockArgs job is scheduled in the same
// transaction (River on Postgres) and the periodic tick (package jobs)
// catches any clock that ran out while no job ran. Changes are reported to
// a league.Notifier after commit.
package draft
