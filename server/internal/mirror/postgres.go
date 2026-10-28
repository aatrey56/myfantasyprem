package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/provider"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// Postgres is the Store on the mirror tables (migration 11).
type Postgres struct{ pool *pgxpool.Pool }

var _ Store = (*Postgres)(nil)

// NewPostgresStore returns a Store on pool.
func NewPostgresStore(pool *pgxpool.Pool) *Postgres { return &Postgres{pool: pool} }

const importCols = `id, user_id, fpl_league_id, seat, status, done, total, error, league_id, created_at, updated_at`

func scanImport(row pgx.Row) (*Import, error) {
	var (
		imp Import
		raw []byte
	)
	err := row.Scan(&imp.ID, &imp.UserID, &imp.FPLLeagueID, &imp.Seat, &imp.Status, &imp.Done, &imp.Total, &raw,
		&imp.LeagueID, &imp.CreatedAt, &imp.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(raw) > 0 {
		imp.Error = &Problem{}
		if err := json.Unmarshal(raw, imp.Error); err != nil {
			return nil, err
		}
	}
	imp.CreatedAt, imp.UpdatedAt = imp.CreatedAt.UTC(), imp.UpdatedAt.UTC()
	return &imp, nil
}

func problemJSON(p *Problem) ([]byte, error) {
	if p == nil {
		return nil, nil
	}
	return json.Marshal(p)
}

func (p *Postgres) CreateImport(ctx context.Context, imp Import, sched league.Scheduler, job league.JobArgs) (Import, error) {
	var existing *Import
	err := store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		existing = nil
		raw, err := problemJSON(imp.Error)
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `INSERT INTO imports (`+importCols+`) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (user_id, fpl_league_id) WHERE status IN ('queued', 'running', 'succeeded') DO NOTHING`,
			imp.ID, imp.UserID, imp.FPLLeagueID, imp.Seat, imp.Status, imp.Done, imp.Total, raw, imp.LeagueID,
			imp.CreatedAt, imp.UpdatedAt)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			existing = &Import{}
			return nil
		}
		if sched != nil {
			return sched.ScheduleTx(ctx, tx, job, imp.CreatedAt)
		}
		return nil
	})
	if err != nil {
		return Import{}, err
	}
	if existing != nil {
		e, err := p.LatestImport(ctx, imp.UserID, imp.FPLLeagueID)
		if err != nil || e == nil {
			return Import{}, errors.Join(err, errors.New("mirror: the existing import vanished"))
		}
		return *e, ErrImportExists
	}
	return imp, nil
}

func (p *Postgres) Import(ctx context.Context, id string) (*Import, error) {
	return scanImport(p.pool.QueryRow(ctx, `SELECT `+importCols+` FROM imports WHERE id = $1`, id))
}

func (p *Postgres) LatestImport(ctx context.Context, userID string, fplLeagueID int64) (*Import, error) {
	return scanImport(p.pool.QueryRow(ctx,
		`SELECT `+importCols+` FROM imports WHERE user_id = $1 AND fpl_league_id = $2 ORDER BY created_at DESC, id DESC LIMIT 1`,
		userID, fplLeagueID))
}

func (p *Postgres) SaveImport(ctx context.Context, imp Import) error {
	raw, err := problemJSON(imp.Error)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx,
		`UPDATE imports SET status = $2, done = $3, total = $4, error = $5, updated_at = $6 WHERE id = $1`,
		imp.ID, imp.Status, imp.Done, imp.Total, raw, imp.UpdatedAt)
	return err
}

func (p *Postgres) Picks(ctx context.Context, prov, entryID string, gw int) (*provider.EntryPicks, error) {
	var raw []byte
	err := p.pool.QueryRow(ctx, `SELECT picks FROM mirror_picks WHERE provider = $1 AND entry_id = $2 AND gameweek = $3`,
		prov, entryID, gw).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out provider.EntryPicks
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (p *Postgres) SavePicks(ctx context.Context, prov string, ep *provider.EntryPicks) error {
	raw, err := json.Marshal(ep)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx,
		`INSERT INTO mirror_picks (provider, entry_id, gameweek, picks) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (provider, entry_id, gameweek) DO UPDATE SET picks = EXCLUDED.picks`,
		prov, ep.EntryID, ep.Gameweek, raw)
	return err
}

func (p *Postgres) Sync(ctx context.Context, leagueID string) (*SyncRecord, error) {
	var r SyncRecord
	err := p.pool.QueryRow(ctx,
		`SELECT league_id, last_attempt_at, last_synced_at, coalesce(last_error, ''), coalesce(pending, ''), requested_at,
		        coalesce(note, '')
		 FROM mirror_sync WHERE league_id = $1`, leagueID).
		Scan(&r.LeagueID, &r.LastAttemptAt, &r.LastSyncedAt, &r.LastError, &r.Pending, &r.RequestedAt, &r.Note)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.LastAttemptAt, r.LastSyncedAt, r.RequestedAt = utc(r.LastAttemptAt), utc(r.LastSyncedAt), utc(r.RequestedAt)
	return &r, nil
}

func utc(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (p *Postgres) SaveSync(ctx context.Context, r SyncRecord) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO mirror_sync (league_id, last_attempt_at, last_synced_at, last_error, pending, requested_at, note)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (league_id) DO UPDATE SET last_attempt_at = EXCLUDED.last_attempt_at,
		   last_synced_at = EXCLUDED.last_synced_at, last_error = EXCLUDED.last_error, pending = EXCLUDED.pending,
		   requested_at = EXCLUDED.requested_at, note = EXCLUDED.note`,
		r.LeagueID, r.LastAttemptAt, r.LastSyncedAt, nullable(r.LastError), nullable(r.Pending), r.RequestedAt, nullable(r.Note))
	return err
}

func (p *Postgres) Request(ctx context.Context, leagueID, kind string, at, staleBefore time.Time) (bool, error) {
	tag, err := p.pool.Exec(ctx,
		`INSERT INTO mirror_sync (league_id, pending, requested_at) VALUES ($1, $2, $3)
		 ON CONFLICT (league_id) DO UPDATE SET pending = EXCLUDED.pending, requested_at = EXCLUDED.requested_at
		 WHERE mirror_sync.pending IS NULL OR mirror_sync.requested_at < $4`,
		leagueID, kind, at, staleBefore)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (p *Postgres) SaveReport(ctx context.Context, leagueID string, r Report) error {
	raw, err := json.Marshal(r.Items)
	if err != nil {
		return err
	}
	return store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO mirror_reports (league_id, checked_at, status, items) VALUES ($1, $2, $3, $4)`,
			leagueID, r.CheckedAt, r.Status, raw); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`DELETE FROM mirror_reports WHERE league_id = $1 AND id NOT IN (
			   SELECT id FROM mirror_reports WHERE league_id = $1 ORDER BY checked_at DESC, id DESC LIMIT $2)`,
			leagueID, keepReports)
		return err
	})
}

func (p *Postgres) LatestReport(ctx context.Context, leagueID string) (*Report, error) {
	var (
		r   Report
		raw []byte
	)
	err := p.pool.QueryRow(ctx,
		`SELECT checked_at, status, items FROM mirror_reports WHERE league_id = $1 ORDER BY checked_at DESC, id DESC LIMIT 1`,
		leagueID).Scan(&r.CheckedAt, &r.Status, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.CheckedAt = r.CheckedAt.UTC()
	if err := json.Unmarshal(raw, &r.Items); err != nil {
		return nil, err
	}
	return &r, nil
}

func (p *Postgres) Explanations(ctx context.Context, leagueID string) ([]Explanation, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT key, ours, theirs, note, coalesce(explained_by, ''), explained_at FROM mirror_explanations
		 WHERE league_id = $1 ORDER BY key`, leagueID)
	if err != nil {
		return nil, err
	}
	var (
		out []Explanation
		e   Explanation
	)
	_, err = pgx.ForEachRow(rows, []any{&e.Key, &e.Ours, &e.Theirs, &e.Note, &e.ExplainedBy, &e.ExplainedAt}, func() error {
		x := e
		x.ExplainedAt = e.ExplainedAt.UTC()
		out = append(out, x)
		return nil
	})
	return out, err
}

func (p *Postgres) SaveExplanation(ctx context.Context, leagueID string, e Explanation) error {
	_, err := p.pool.Exec(ctx,
		`INSERT INTO mirror_explanations (league_id, key, ours, theirs, note, explained_by, explained_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)
		 ON CONFLICT (league_id, key) DO UPDATE SET ours = EXCLUDED.ours, theirs = EXCLUDED.theirs, note = EXCLUDED.note,
		   explained_by = EXCLUDED.explained_by, explained_at = EXCLUDED.explained_at`,
		leagueID, e.Key, e.Ours, e.Theirs, e.Note, nullable(e.ExplainedBy), e.ExplainedAt)
	return err
}

func (p *Postgres) SaveShadow(ctx context.Context, leagueID string, r ShadowReport) error {
	raw, err := json.Marshal(r.Items)
	if err != nil {
		return err
	}
	return store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `INSERT INTO mirror_shadow_checks (league_id, checked_at, reason, gameweek, status, items)
			VALUES ($1, $2, $3, $4, $5, $6)`, leagueID, r.CheckedAt, r.Reason, r.Gameweek, r.Status, raw); err != nil {
			return err
		}
		_, err := tx.Exec(ctx,
			`DELETE FROM mirror_shadow_checks WHERE league_id = $1 AND id NOT IN (
			   SELECT id FROM mirror_shadow_checks WHERE league_id = $1 ORDER BY checked_at DESC, id DESC LIMIT $2)`,
			leagueID, keepShadows)
		return err
	})
}

func (p *Postgres) LatestShadow(ctx context.Context, leagueID string) (*ShadowReport, error) {
	var (
		r   ShadowReport
		raw []byte
	)
	err := p.pool.QueryRow(ctx,
		`SELECT checked_at, reason, gameweek, status, items FROM mirror_shadow_checks WHERE league_id = $1
		 ORDER BY checked_at DESC, id DESC LIMIT 1`,
		leagueID).Scan(&r.CheckedAt, &r.Reason, &r.Gameweek, &r.Status, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.CheckedAt = r.CheckedAt.UTC()
	if err := json.Unmarshal(raw, &r.Items); err != nil {
		return nil, err
	}
	return &r, nil
}
