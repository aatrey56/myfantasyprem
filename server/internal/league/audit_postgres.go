package league

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// auditDetails is the jsonb of an entry's before and after values.
type auditDetails struct {
	Before map[string]any `json:"before,omitempty"`
	After  map[string]any `json:"after,omitempty"`
}

func (t pgTx) AddAudit(ctx context.Context, e *AuditEntry) error {
	var details []byte
	if e.Before != nil || e.After != nil {
		var err error
		if details, err = json.Marshal(auditDetails{e.Before, e.After}); err != nil {
			return err
		}
	}
	teams := e.TeamIDs
	if teams == nil {
		teams = []string{}
	}
	// The league's row lock first (held already when the action loaded the
	// league): the identity is then drawn in commit order within the
	// league, so a member's seen-up-to marker never passes an entry that
	// commits later. Without it an insert draws its number, then waits for
	// the lock in its foreign key check while a later entry commits.
	var one int
	if err := t.tx.QueryRow(ctx, `SELECT 1 FROM leagues WHERE id = $1 FOR UPDATE`, e.LeagueID).Scan(&one); err != nil {
		return err
	}
	return t.tx.QueryRow(ctx,
		`INSERT INTO commissioner_moves (id, league_id, actor_id, action, summary, team_ids, details, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING seq`,
		e.ID, e.LeagueID, nullable(e.ActorID), e.Action, e.Summary, teams, details, e.At).Scan(&e.Seq)
}

func (t pgTx) StartAuditSeen(ctx context.Context, leagueID, userID string) error {
	_, err := t.tx.Exec(ctx,
		`INSERT INTO commissioner_moves_seen (league_id, user_id, seen_seq, updated_at)
		 SELECT $1, $2, max(seq), now() FROM commissioner_moves WHERE league_id = $1 HAVING max(seq) IS NOT NULL
		 ON CONFLICT (league_id, user_id) DO UPDATE
		   SET seen_seq = greatest(commissioner_moves_seen.seen_seq, EXCLUDED.seen_seq), updated_at = now()`,
		leagueID, userID)
	return err
}

const auditColumns = `seq, id, league_id, coalesce(actor_id, ''), action, summary, team_ids, details, created_at`

func scanAudit(rows pgx.Rows) ([]AuditEntry, error) {
	var out []AuditEntry
	for rows.Next() {
		var (
			e   AuditEntry
			raw []byte
		)
		if err := rows.Scan(&e.Seq, &e.ID, &e.LeagueID, &e.ActorID, &e.Action, &e.Summary, &e.TeamIDs, &raw, &e.At); err != nil {
			return nil, err
		}
		e.At = e.At.UTC()
		if raw != nil {
			var d auditDetails
			if err := json.Unmarshal(raw, &d); err != nil {
				return nil, err
			}
			e.Before, e.After = d.Before, d.After
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (p *Postgres) Audit(ctx context.Context, leagueID string, offset, limit int) ([]AuditEntry, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+auditColumns+` FROM commissioner_moves WHERE league_id = $1 ORDER BY seq DESC OFFSET $2 LIMIT $3`,
		leagueID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanAudit(rows)
}

func (p *Postgres) AuditSeenSeq(ctx context.Context, leagueID, userID string) (int64, error) {
	var seen int64
	err := p.pool.QueryRow(ctx,
		`SELECT coalesce((SELECT seen_seq FROM commissioner_moves_seen WHERE league_id = $1 AND user_id = $2), 0)`,
		leagueID, userID).Scan(&seen)
	return seen, err
}

func (p *Postgres) AuditSummary(ctx context.Context, leagueID, userID string, since time.Time) (AuditSummary, error) {
	return read(ctx, p, func(db store.DB) (AuditSummary, error) {
		var s AuditSummary
		const unseen = `FROM commissioner_moves m WHERE m.league_id = $1
		  AND m.seq > coalesce((SELECT seen_seq FROM commissioner_moves_seen WHERE league_id = $1 AND user_id = $2), 0)
		  AND m.actor_id IS DISTINCT FROM $2 AND m.created_at >= $3`
		if err := db.QueryRow(ctx, `SELECT count(*) `+unseen, leagueID, userID, since).Scan(&s.Unseen); err != nil || s.Unseen == 0 {
			return s, err
		}
		rows, err := db.Query(ctx, `SELECT `+auditColumns+` `+unseen+` ORDER BY m.seq DESC LIMIT 1`, leagueID, userID, since)
		if err != nil {
			return s, err
		}
		defer rows.Close()
		es, err := scanAudit(rows)
		if len(es) > 0 {
			s.Latest = &es[0]
		}
		return s, err
	})
}

func (p *Postgres) MarkAuditSeen(ctx context.Context, leagueID, userID, throughID string) (bool, error) {
	var through *int64
	err := p.pool.QueryRow(ctx,
		`SELECT CASE WHEN $2 = '' THEN (SELECT max(seq) FROM commissioner_moves WHERE league_id = $1)
		             ELSE (SELECT seq FROM commissioner_moves WHERE league_id = $1 AND id = $2) END`,
		leagueID, throughID).Scan(&through)
	if err != nil {
		return false, err
	}
	if through == nil {
		// No entry yet, or an id that is not this league's.
		return throughID == "", nil
	}
	_, err = p.pool.Exec(ctx,
		`INSERT INTO commissioner_moves_seen (league_id, user_id, seen_seq, updated_at) VALUES ($1, $2, $3, now())
		 ON CONFLICT (league_id, user_id) DO UPDATE
		   SET seen_seq = greatest(commissioner_moves_seen.seen_seq, EXCLUDED.seen_seq), updated_at = now()`,
		leagueID, userID, *through)
	return err == nil, err
}
