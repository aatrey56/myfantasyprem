package league

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func nullInt(n int) *int {
	if n == 0 {
		return nil
	}
	return &n
}

// loadState reads a league's State; lock takes the league row FOR UPDATE.
func loadState(ctx context.Context, db store.DB, leagueID string, lock bool) (*State, error) {
	l, err := load(ctx, db, leagueID, lock)
	if err != nil || l == nil {
		return nil, err
	}
	g, err := loadGame(ctx, db, leagueID)
	if err != nil {
		return nil, err
	}
	s := &State{League: l, Game: g, Runs: map[RunKey]time.Time{}}
	if s.Draft, err = loadDraft(ctx, db, leagueID); err != nil {
		return nil, fmt.Errorf("load draft: %w", err)
	}
	if s.Priority, err = idList(ctx, db,
		`SELECT team_id FROM waiver_priority WHERE league_id = $1 ORDER BY rank`, leagueID); err != nil {
		return nil, err
	}
	if s.Claims, err = loadClaims(ctx, db, `league_id = $1 AND status = 'pending'`, leagueID); err != nil {
		return nil, fmt.Errorf("load claims: %w", err)
	}
	if s.Trades, err = loadTrades(ctx, db, `league_id = $1 AND status IN ('offered', 'accepted', 'in_review')`, leagueID); err != nil {
		return nil, fmt.Errorf("load trades: %w", err)
	}
	rows, err := db.Query(ctx, `SELECT player_id, until_gw, reason FROM waiver_locks WHERE league_id = $1 ORDER BY player_id`, leagueID)
	if err != nil {
		return nil, err
	}
	var lk WaiverLock
	if _, err := pgx.ForEachRow(rows, []any{&lk.PlayerID, &lk.UntilGW, &lk.Reason}, func() error {
		s.Locks = append(s.Locks, lk)
		return nil
	}); err != nil {
		return nil, err
	}
	rows, err = db.Query(ctx,
		`SELECT team_id, player_id, note, wanted, added_at FROM trade_block WHERE league_id = $1 ORDER BY added_at, player_id`, leagueID)
	if err != nil {
		return nil, err
	}
	var be BlockEntry
	if _, err := pgx.ForEachRow(rows, []any{&be.TeamID, &be.PlayerID, &be.Note, &be.Wanted, &be.AddedAt}, func() error {
		be.AddedAt = be.AddedAt.UTC()
		s.Block = append(s.Block, BlockEntry{TeamID: be.TeamID, PlayerID: be.PlayerID, Note: be.Note,
			Wanted: slices.Clone(be.Wanted), AddedAt: be.AddedAt})
		return nil
	}); err != nil {
		return nil, err
	}
	rows, err = db.Query(ctx, `SELECT kind, gameweek, ran_at FROM league_runs WHERE league_id = $1`, leagueID)
	if err != nil {
		return nil, err
	}
	var (
		k  RunKey
		at time.Time
	)
	if _, err := pgx.ForEachRow(rows, []any{&k.Kind, &k.Gameweek, &at}, func() error {
		s.Runs[k] = at.UTC()
		return nil
	}); err != nil {
		return nil, err
	}
	s.Refs = map[RefKey]string{}
	rows, err = db.Query(ctx, `SELECT entity, provider, external_id, internal_id FROM league_refs WHERE league_id = $1`, leagueID)
	if err != nil {
		return nil, err
	}
	var (
		rk       RefKey
		internal string
	)
	if _, err := pgx.ForEachRow(rows, []any{&rk.Entity, &rk.Provider, &rk.ExternalID, &internal}, func() error {
		s.Refs[rk] = internal
		return nil
	}); err != nil {
		return nil, err
	}
	s.snapshot()
	return s, nil
}

func loadDraft(ctx context.Context, db store.DB, leagueID string) (*Draft, error) {
	d := &Draft{Autopick: map[string]bool{}, StayManual: map[string]bool{}, Queues: map[string][]string{}}
	err := db.QueryRow(ctx,
		`SELECT id, status, pick_timer_s, clock_started_at, paused_at, completed_at FROM drafts WHERE league_id = $1`, leagueID).
		Scan(&d.ID, &d.Status, &d.PickTimerS, &d.ClockStartedAt, &d.PausedAt, &d.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.ClockStartedAt, d.PausedAt, d.CompletedAt = utcPtr(d.ClockStartedAt), utcPtr(d.PausedAt), utcPtr(d.CompletedAt)
	if d.Order, err = idList(ctx, db, `SELECT team_id FROM draft_order WHERE draft_id = $1 ORDER BY slot`, d.ID); err != nil {
		return nil, err
	}
	auto, err := idList(ctx, db, `SELECT team_id FROM draft_autopick WHERE draft_id = $1`, d.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range auto {
		d.Autopick[id] = true
	}
	manual, err := idList(ctx, db, `SELECT team_id FROM draft_stay_manual WHERE draft_id = $1`, d.ID)
	if err != nil {
		return nil, err
	}
	for _, id := range manual {
		d.StayManual[id] = true
	}
	rows, err := db.Query(ctx,
		`SELECT id, overall, team_id, player_id, method, picked_at, missed_clock FROM draft_picks WHERE draft_id = $1 ORDER BY overall`, d.ID)
	if err != nil {
		return nil, err
	}
	var p Pick
	if _, err := pgx.ForEachRow(rows, []any{&p.ID, &p.Overall, &p.TeamID, &p.PlayerID, &p.Method, &p.PickedAt, &p.MissedClock}, func() error {
		p.PickedAt = p.PickedAt.UTC()
		d.Picks = append(d.Picks, p)
		return nil
	}); err != nil {
		return nil, err
	}
	rows, err = db.Query(ctx,
		`SELECT q.team_id, q.player_id FROM draft_queue q JOIN teams t ON t.id = q.team_id
		 WHERE t.league_id = $1 ORDER BY q.team_id, q.rank`, leagueID)
	if err != nil {
		return nil, err
	}
	var team, player string
	if _, err := pgx.ForEachRow(rows, []any{&team, &player}, func() error {
		d.Queues[team] = append(d.Queues[team], player)
		return nil
	}); err != nil {
		return nil, err
	}
	return d, nil
}

func loadClaims(ctx context.Context, db store.DB, where string, args ...any) ([]Claim, error) {
	rows, err := db.Query(ctx,
		`SELECT id, team_id, add_player_id, drop_player_id, pref_order, gameweek, status,
		        coalesce(failure_reason, ''), processed_at, created_at
		 FROM waiver_claims WHERE `+where+` ORDER BY team_id, pref_order, created_at, id`, args...)
	if err != nil {
		return nil, err
	}
	var (
		out []Claim
		c   Claim
	)
	_, err = pgx.ForEachRow(rows, []any{&c.ID, &c.TeamID, &c.AddPlayerID, &c.DropPlayerID, &c.Order, &c.Gameweek,
		&c.Status, &c.FailureReason, &c.ProcessedAt, &c.CreatedAt}, func() error {
		x := c
		x.ProcessedAt, x.CreatedAt = utcPtr(c.ProcessedAt), c.CreatedAt.UTC()
		out = append(out, x)
		return nil
	})
	return out, err
}

func loadTrades(ctx context.Context, db store.DB, where string, args ...any) ([]Trade, error) {
	rows, err := db.Query(ctx,
		`SELECT id, offering_team_id, receiving_team_id, status, coalesce(message, ''), created_at, expires_at,
		        responded_at, review_ends_at, processed_at, coalesce(review_mode, ''), coalesce(decided_by, ''),
		        coalesce(decision_reason, ''), coalesce(failure_reason, ''), coalesce(effective_gw, 0)
		 FROM trades WHERE `+where+` ORDER BY created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	var (
		out     []Trade
		t       Trade
		expires *time.Time
	)
	if _, err := pgx.ForEachRow(rows, []any{&t.ID, &t.OfferingTeamID, &t.ReceivingTeamID, &t.Status, &t.Message,
		&t.CreatedAt, &expires, &t.RespondedAt, &t.ReviewEndsAt, &t.ProcessedAt, &t.ReviewMode, &t.DecidedBy,
		&t.DecisionReason, &t.FailureReason, &t.EffectiveGW}, func() error {
		x := t
		x.CreatedAt = t.CreatedAt.UTC()
		if expires != nil {
			x.ExpiresAt = expires.UTC()
		}
		x.RespondedAt, x.ReviewEndsAt, x.ProcessedAt = utcPtr(t.RespondedAt), utcPtr(t.ReviewEndsAt), utcPtr(t.ProcessedAt)
		x.Votes = map[string]string{}
		out = append(out, x)
		return nil
	}); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, nil
	}
	byID := map[string]*Trade{}
	ids := make([]string, len(out))
	for i := range out {
		byID[out[i].ID] = &out[i]
		ids[i] = out[i].ID
	}
	rows, err = db.Query(ctx,
		`SELECT i.trade_id, i.player_id, i.from_team_id = t.offering_team_id
		 FROM trade_items i JOIN trades t ON t.id = i.trade_id
		 WHERE i.trade_id = ANY($1) ORDER BY i.trade_id, i.ord`, ids)
	if err != nil {
		return nil, err
	}
	var (
		tradeID, playerID string
		offered           bool
	)
	if _, err := pgx.ForEachRow(rows, []any{&tradeID, &playerID, &offered}, func() error {
		x := byID[tradeID]
		if offered {
			x.Offered = append(x.Offered, playerID)
		} else {
			x.Requested = append(x.Requested, playerID)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	rows, err = db.Query(ctx, `SELECT trade_id, team_id, vote FROM trade_votes WHERE trade_id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	var team, vote string
	_, err = pgx.ForEachRow(rows, []any{&tradeID, &team, &vote}, func() error {
		byID[tradeID].Votes[team] = vote
		return nil
	})
	return out, err
}

func (p *Postgres) State(ctx context.Context, leagueID string) (*State, error) {
	return loadState(ctx, p.pool, leagueID, false)
}

func (p *Postgres) LeaguesWithStatus(ctx context.Context, status string) ([]string, error) {
	return idList(ctx, p.pool, `SELECT id FROM leagues WHERE status = $1 ORDER BY id`, status)
}

func (p *Postgres) Claims(ctx context.Context, leagueID, teamID string) ([]Claim, error) {
	return loadClaims(ctx, p.pool, `league_id = $1 AND team_id = $2`, leagueID, teamID)
}

func (p *Postgres) Trades(ctx context.Context, leagueID string) ([]Trade, error) {
	return loadTrades(ctx, p.pool, `league_id = $1`, leagueID)
}

func (p *Postgres) Transactions(ctx context.Context, leagueID string) ([]Txn, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT id, kind, gameweek, occurred_at, payload FROM transactions
		 WHERE league_id = $1 ORDER BY occurred_at DESC, id DESC`, leagueID)
	if err != nil {
		return nil, err
	}
	var (
		out []Txn
		t   Txn
		raw []byte
	)
	_, err = pgx.ForEachRow(rows, []any{&t.ID, &t.Kind, &t.Gameweek, &t.OccurredAt, &raw}, func() error {
		var x Txn
		if err := json.Unmarshal(raw, &x); err != nil {
			return err
		}
		x.ID, x.Kind, x.Gameweek, x.OccurredAt = t.ID, t.Kind, t.Gameweek, t.OccurredAt.UTC()
		out = append(out, x)
		return nil
	})
	return out, err
}

func (t pgTx) LoadState(ctx context.Context, leagueID string) (*State, error) {
	return loadState(ctx, t.tx, leagueID, true)
}

// SaveState writes the difference between s and what LoadState read.
func (t pgTx) SaveState(ctx context.Context, s *State) error {
	o := s.orig
	if o == nil {
		return errors.New("league: SaveState without LoadState")
	}
	id := s.League.ID
	if s.DropLocalMoves && !o.DropLocalMoves {
		if err := t.dropLocalMoves(ctx, id); err != nil {
			return err
		}
	}
	if err := t.Save(ctx, s.League); err != nil {
		return err
	}
	s.pruneBlock()
	steps := []func() error{
		func() error { return t.saveRosters(ctx, id, o.Game.Rosters, s.Game.Rosters) },
		func() error { return t.saveLineups(ctx, o.Game.Lineups, s.Game.Lineups) },
		func() error { return t.saveDraft(ctx, id, s.League.Settings, o.Draft, s.Draft) },
		func() error {
			if slices.Equal(o.Priority, s.Priority) {
				return nil
			}
			if _, err := t.tx.Exec(ctx, `DELETE FROM waiver_priority WHERE league_id = $1`, id); err != nil {
				return err
			}
			for i, team := range s.Priority {
				if _, err := t.tx.Exec(ctx,
					`INSERT INTO waiver_priority (league_id, team_id, rank, as_of) VALUES ($1, $2, $3, now())`, id, team, i+1); err != nil {
					return err
				}
			}
			return nil
		},
		func() error { return t.saveClaims(ctx, id, o.Claims, s.Claims) },
		func() error { return t.saveTrades(ctx, id, o.Trades, s.Trades) },
		func() error {
			if reflect.DeepEqual(o.Locks, s.Locks) {
				return nil
			}
			if _, err := t.tx.Exec(ctx, `DELETE FROM waiver_locks WHERE league_id = $1`, id); err != nil {
				return err
			}
			for _, l := range s.Locks {
				if _, err := t.tx.Exec(ctx,
					`INSERT INTO waiver_locks (league_id, player_id, until_gw, reason) VALUES ($1, $2, $3, $4)`,
					id, l.PlayerID, l.UntilGW, l.Reason); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			if reflect.DeepEqual(o.Block, s.Block) {
				return nil
			}
			if _, err := t.tx.Exec(ctx, `DELETE FROM trade_block WHERE league_id = $1`, id); err != nil {
				return err
			}
			for _, e := range s.Block {
				wanted := e.Wanted
				if wanted == nil {
					wanted = []string{}
				}
				if _, err := t.tx.Exec(ctx,
					`INSERT INTO trade_block (league_id, team_id, player_id, note, wanted, added_at) VALUES ($1, $2, $3, $4, $5, $6)`,
					id, e.TeamID, e.PlayerID, e.Note, wanted, e.AddedAt); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			for k, at := range s.Runs {
				if _, ok := o.Runs[k]; ok {
					continue
				}
				if _, err := t.tx.Exec(ctx,
					`INSERT INTO league_runs (league_id, gameweek, kind, ran_at) VALUES ($1, $2, $3, $4)
					 ON CONFLICT DO NOTHING`, id, k.Gameweek, k.Kind, at); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			for _, x := range s.NewTxns[len(o.NewTxns):] {
				raw, err := json.Marshal(x)
				if err != nil {
					return err
				}
				if _, err := t.tx.Exec(ctx,
					`INSERT INTO transactions (id, league_id, kind, gameweek, payload, occurred_at) VALUES ($1, $2, $3, $4, $5, $6)`,
					x.ID, id, x.Kind, x.Gameweek, raw, x.OccurredAt); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			for _, m := range s.NewMatchups[len(o.NewMatchups):] {
				if _, err := t.tx.Exec(ctx,
					`INSERT INTO matchups (id, league_id, gameweek, home_team_id, away_team_id, home_points, away_points, state)
					 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
					m.ID, id, m.Gameweek, nullable(m.HomeTeamID), nullable(m.AwayTeamID), m.HomePoints, m.AwayPoints, string(m.State)); err != nil {
					return err
				}
			}
			return nil
		},
	}
	steps = append(steps, func() error {
		for k, internal := range s.Refs {
			if _, ok := o.Refs[k]; ok {
				continue
			}
			if _, err := t.tx.Exec(ctx,
				`INSERT INTO league_refs (league_id, entity, provider, external_id, internal_id) VALUES ($1, $2, $3, $4, $5)`,
				id, k.Entity, k.Provider, k.ExternalID, internal); err != nil {
				return fmt.Errorf("ref %s %s: %w", k.Entity, k.ExternalID, err)
			}
		}
		return nil
	})
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	s.Game.Matchups = append(s.Game.Matchups, s.NewMatchups[len(o.NewMatchups):]...)
	s.snapshot()
	return nil
}

// saveRosters deletes, ends and inserts slots in that order, so the
// one-owner exclusion constraint holds after every statement.
// dropLocalMoves deletes the league's closed claims, closed trades and the
// feed entries that no ref names (State.DropLocalMoves). An imported
// trade's feed entry is named through its trade.
func (t pgTx) dropLocalMoves(ctx context.Context, leagueID string) error {
	for _, q := range []string{
		`DELETE FROM waiver_claims WHERE league_id = $1 AND status <> 'pending'`,
		`DELETE FROM trades t WHERE t.league_id = $1 AND t.status NOT IN ('offered', 'accepted', 'in_review')
		   AND NOT EXISTS (SELECT 1 FROM league_refs r WHERE r.league_id = $1 AND r.entity = 'trade' AND r.internal_id = t.id)`,
		`DELETE FROM transactions x WHERE x.league_id = $1
		   AND NOT EXISTS (SELECT 1 FROM league_refs r WHERE r.league_id = $1 AND r.entity = 'transaction' AND r.internal_id = x.id)
		   AND NOT EXISTS (SELECT 1 FROM league_refs r WHERE r.league_id = $1 AND r.entity = 'trade'
		                   AND r.internal_id = x.payload->>'tradeId')`,
	} {
		if _, err := t.tx.Exec(ctx, q, leagueID); err != nil {
			return err
		}
	}
	return nil
}

func (t pgTx) saveRosters(ctx context.Context, leagueID string, before, after []RosterSlot) error {
	kept := map[int64]RosterSlot{}
	for _, r := range after {
		if r.ID != 0 {
			kept[r.ID] = r
		}
	}
	for _, r := range before {
		if _, ok := kept[r.ID]; !ok {
			if _, err := t.tx.Exec(ctx, `DELETE FROM roster_slots WHERE id = $1 AND league_id = $2`, r.ID, leagueID); err != nil {
				return err
			}
		}
	}
	for _, r := range before {
		if n, ok := kept[r.ID]; ok && n != r {
			if _, err := t.tx.Exec(ctx,
				`UPDATE roster_slots SET from_gw = $3, to_gw = $4 WHERE id = $1 AND league_id = $2`,
				r.ID, leagueID, n.FromGW, nullInt(n.ToGW)); err != nil {
				return err
			}
		}
	}
	for i := range after {
		r := &after[i]
		if r.ID != 0 {
			continue
		}
		if err := t.tx.QueryRow(ctx,
			`INSERT INTO roster_slots (league_id, team_id, player_id, from_gw, to_gw, acquired_via)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			leagueID, r.TeamID, r.PlayerID, r.FromGW, nullInt(r.ToGW), r.AcquiredVia).Scan(&r.ID); err != nil {
			return fmt.Errorf("roster slot %s/%s from %d: %w", r.TeamID, r.PlayerID, r.FromGW, err)
		}
	}
	return nil
}

func (t pgTx) saveLineups(ctx context.Context, before, after []Lineup) error {
	type key struct {
		team string
		gw   int
	}
	now := map[key]Lineup{}
	for _, l := range after {
		now[key{l.TeamID, l.FromGW}] = l
	}
	was := map[key]Lineup{}
	for _, l := range before {
		k := key{l.TeamID, l.FromGW}
		was[k] = l
		if _, ok := now[k]; !ok {
			if _, err := t.tx.Exec(ctx, `DELETE FROM lineups WHERE team_id = $1 AND from_gw = $2`, l.TeamID, l.FromGW); err != nil {
				return err
			}
		}
	}
	for k, l := range now {
		if old, ok := was[k]; ok && slices.Equal(old.Slots, l.Slots) && old.UpdatedAt.Equal(l.UpdatedAt) {
			continue
		}
		if _, err := t.tx.Exec(ctx,
			`INSERT INTO lineups (team_id, from_gw, slots, updated_at) VALUES ($1, $2, $3, $4)
			 ON CONFLICT (team_id, from_gw) DO UPDATE SET slots = EXCLUDED.slots, updated_at = EXCLUDED.updated_at`,
			l.TeamID, l.FromGW, l.Slots, l.UpdatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (t pgTx) saveDraft(ctx context.Context, leagueID string, st Settings, before, after *Draft) error {
	if after == nil {
		return nil
	}
	if before == nil {
		before = &Draft{}
	}
	if _, err := t.tx.Exec(ctx,
		`INSERT INTO drafts (id, league_id, status, starts_at, pick_timer_s, clock_started_at, paused_at, completed_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		 ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, starts_at = EXCLUDED.starts_at,
		   pick_timer_s = EXCLUDED.pick_timer_s, clock_started_at = EXCLUDED.clock_started_at,
		   paused_at = EXCLUDED.paused_at, completed_at = EXCLUDED.completed_at`,
		after.ID, leagueID, after.Status, st.DraftStartsAt, after.PickTimerS, after.ClockStartedAt, after.PausedAt, after.CompletedAt); err != nil {
		return err
	}
	if !slices.Equal(before.Order, after.Order) {
		if _, err := t.tx.Exec(ctx, `DELETE FROM draft_order WHERE draft_id = $1`, after.ID); err != nil {
			return err
		}
		for i, team := range after.Order {
			if _, err := t.tx.Exec(ctx, `INSERT INTO draft_order (draft_id, slot, team_id) VALUES ($1, $2, $3)`, after.ID, i+1, team); err != nil {
				return err
			}
		}
	}
	if !reflect.DeepEqual(before.Autopick, after.Autopick) {
		if _, err := t.tx.Exec(ctx, `DELETE FROM draft_autopick WHERE draft_id = $1`, after.ID); err != nil {
			return err
		}
		for team, on := range after.Autopick {
			if !on {
				continue
			}
			if _, err := t.tx.Exec(ctx, `INSERT INTO draft_autopick (draft_id, team_id) VALUES ($1, $2)`, after.ID, team); err != nil {
				return err
			}
		}
	}
	if !reflect.DeepEqual(before.StayManual, after.StayManual) {
		if _, err := t.tx.Exec(ctx, `DELETE FROM draft_stay_manual WHERE draft_id = $1`, after.ID); err != nil {
			return err
		}
		for team, on := range after.StayManual {
			if !on {
				continue
			}
			if _, err := t.tx.Exec(ctx, `INSERT INTO draft_stay_manual (draft_id, team_id) VALUES ($1, $2)`, after.ID, team); err != nil {
				return err
			}
		}
	}
	for _, p := range after.Picks[min(len(before.Picks), len(after.Picks)):] {
		if _, err := t.tx.Exec(ctx,
			`INSERT INTO draft_picks (id, draft_id, overall, team_id, player_id, method, picked_at, missed_clock)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			p.ID, after.ID, p.Overall, p.TeamID, p.PlayerID, p.Method, p.PickedAt, p.MissedClock); err != nil {
			return fmt.Errorf("pick %d: %w", p.Overall, err)
		}
	}
	for team, q := range after.Queues {
		if slices.Equal(before.Queues[team], q) {
			continue
		}
		if err := t.saveQueue(ctx, team, q); err != nil {
			return err
		}
	}
	for team := range before.Queues {
		if _, ok := after.Queues[team]; !ok {
			if err := t.saveQueue(ctx, team, nil); err != nil {
				return err
			}
		}
	}
	return nil
}

func (t pgTx) saveQueue(ctx context.Context, teamID string, q []string) error {
	if _, err := t.tx.Exec(ctx, `DELETE FROM draft_queue WHERE team_id = $1`, teamID); err != nil {
		return err
	}
	for i, p := range q {
		if _, err := t.tx.Exec(ctx, `INSERT INTO draft_queue (team_id, rank, player_id) VALUES ($1, $2, $3)`, teamID, i+1, p); err != nil {
			return err
		}
	}
	return nil
}

func (t pgTx) saveClaims(ctx context.Context, leagueID string, before, after []Claim) error {
	was := map[string]Claim{}
	for _, c := range before {
		was[c.ID] = c
	}
	for _, c := range after {
		if old, ok := was[c.ID]; ok && reflect.DeepEqual(old, c) {
			continue
		}
		if _, err := t.tx.Exec(ctx,
			`INSERT INTO waiver_claims (id, league_id, team_id, add_player_id, drop_player_id, pref_order, gameweek,
			                            status, failure_reason, processed_at, created_at)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			 ON CONFLICT (id) DO UPDATE SET drop_player_id = EXCLUDED.drop_player_id, pref_order = EXCLUDED.pref_order,
			   gameweek = EXCLUDED.gameweek, status = EXCLUDED.status, failure_reason = EXCLUDED.failure_reason,
			   processed_at = EXCLUDED.processed_at`,
			c.ID, leagueID, c.TeamID, c.AddPlayerID, c.DropPlayerID, c.Order, c.Gameweek, c.Status,
			nullable(c.FailureReason), c.ProcessedAt, c.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (t pgTx) saveTrades(ctx context.Context, leagueID string, before, after []Trade) error {
	was := map[string]Trade{}
	for _, tr := range before {
		was[tr.ID] = tr
	}
	for _, tr := range after {
		old, existed := was[tr.ID]
		if existed && reflect.DeepEqual(old, tr) {
			continue
		}
		if _, err := t.tx.Exec(ctx,
			`INSERT INTO trades (id, league_id, offering_team_id, receiving_team_id, status, message, created_at, expires_at,
			                     responded_at, review_ends_at, processed_at, review_mode, decided_by, decision_reason,
			                     failure_reason, effective_gw)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)
			 ON CONFLICT (id) DO UPDATE SET status = EXCLUDED.status, responded_at = EXCLUDED.responded_at,
			   review_ends_at = EXCLUDED.review_ends_at, processed_at = EXCLUDED.processed_at,
			   review_mode = EXCLUDED.review_mode, decided_by = EXCLUDED.decided_by,
			   decision_reason = EXCLUDED.decision_reason, failure_reason = EXCLUDED.failure_reason,
			   effective_gw = EXCLUDED.effective_gw, expires_at = EXCLUDED.expires_at`,
			tr.ID, leagueID, tr.OfferingTeamID, tr.ReceivingTeamID, tr.Status, nullable(tr.Message), tr.CreatedAt, tr.ExpiresAt,
			tr.RespondedAt, tr.ReviewEndsAt, tr.ProcessedAt, nullable(tr.ReviewMode), nullable(tr.DecidedBy),
			nullable(tr.DecisionReason), nullable(tr.FailureReason), nullInt(tr.EffectiveGW)); err != nil {
			return err
		}
		if !existed {
			for i, p := range tr.Offered {
				if _, err := t.tx.Exec(ctx, `INSERT INTO trade_items (trade_id, player_id, from_team_id, ord) VALUES ($1, $2, $3, $4)`,
					tr.ID, p, tr.OfferingTeamID, i); err != nil {
					return err
				}
			}
			for i, p := range tr.Requested {
				if _, err := t.tx.Exec(ctx, `INSERT INTO trade_items (trade_id, player_id, from_team_id, ord) VALUES ($1, $2, $3, $4)`,
					tr.ID, p, tr.ReceivingTeamID, i); err != nil {
					return err
				}
			}
		}
		for team, vote := range tr.Votes {
			if old.Votes[team] == vote {
				continue
			}
			if _, err := t.tx.Exec(ctx,
				`INSERT INTO trade_votes (trade_id, team_id, vote) VALUES ($1, $2, $3)
				 ON CONFLICT (trade_id, team_id) DO UPDATE SET vote = EXCLUDED.vote, voted_at = now()`, tr.ID, team, vote); err != nil {
				return err
			}
		}
	}
	return nil
}
