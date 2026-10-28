package realtime

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// Channel is the Postgres NOTIFY channel; the payload is "leagueID:seq".
const Channel = "realtime"

// ControlChannel carries revocations (a JSON Revocation) to every
// instance's hub.
const ControlChannel = "realtime_control"

// Postgres is the Store on Postgres: realtime_seq, realtime_events and
// realtime_tickets. Append notifies Channel in the same transaction, so
// other instances hear about an event only once it is committed.
type Postgres struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

var _ Store = (*Postgres)(nil)

// NewPostgres returns a Store on pool.
func NewPostgres(pool *pgxpool.Pool, now func() time.Time) *Postgres {
	return &Postgres{pool: pool, now: now}
}

func (p *Postgres) Append(ctx context.Context, ev Event) (Event, error) {
	ev.SentAt = p.now().UTC().Truncate(time.Microsecond)
	var perTeam []byte
	if ev.PerTeam != nil {
		b, err := json.Marshal(ev.PerTeam)
		if err != nil {
			return ev, err
		}
		perTeam = b
	}
	body := ev.Body
	if len(body) == 0 {
		body = json.RawMessage("{}")
	}
	err := store.InTx(ctx, p.pool, pgx.ReadCommitted, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx,
			`INSERT INTO realtime_seq (league_id, seq) VALUES ($1, 1)
			 ON CONFLICT (league_id) DO UPDATE SET seq = realtime_seq.seq + 1 RETURNING seq`, ev.LeagueID).Scan(&ev.Seq); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO realtime_events (league_id, seq, type, payload, sent_at, audience, per_team)
			 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			ev.LeagueID, ev.Seq, ev.Type, []byte(body), ev.SentAt, ev.Audience, perTeam); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, Channel, notification(ev.LeagueID, ev.Seq))
		return err
	})
	return ev, err
}

func scanEvents(rows pgx.Rows) ([]Event, error) {
	var (
		out     []Event
		ev      Event
		body    []byte
		perTeam []byte
	)
	_, err := pgx.ForEachRow(rows, []any{&ev.LeagueID, &ev.Seq, &ev.Type, &body, &ev.SentAt, &ev.Audience, &perTeam}, func() error {
		e := ev
		e.Body = json.RawMessage(append([]byte(nil), body...))
		e.SentAt = ev.SentAt.UTC()
		e.PerTeam = nil
		if perTeam != nil {
			if err := json.Unmarshal(perTeam, &e.PerTeam); err != nil {
				return err
			}
		}
		out = append(out, e)
		ev.Audience = nil
		return nil
	})
	return out, err
}

const eventColumns = `league_id, seq, type, payload, sent_at, audience, per_team`

func (p *Postgres) Since(ctx context.Context, leagueID string, afterSeq int64) ([]Event, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+eventColumns+` FROM realtime_events WHERE league_id = $1 AND seq > $2 ORDER BY seq`, leagueID, afterSeq)
	if err != nil {
		return nil, err
	}
	return scanEvents(rows)
}

func (p *Postgres) Get(ctx context.Context, leagueID string, seq int64) (Event, bool, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT `+eventColumns+` FROM realtime_events WHERE league_id = $1 AND seq = $2`, leagueID, seq)
	if err != nil {
		return Event{}, false, err
	}
	evs, err := scanEvents(rows)
	if err != nil || len(evs) == 0 {
		return Event{}, false, err
	}
	return evs[0], true, nil
}

func (p *Postgres) Prune(ctx context.Context, before time.Time) (int, error) {
	tag, err := p.pool.Exec(ctx, `DELETE FROM realtime_events WHERE sent_at < $1`, before)
	if err != nil {
		return 0, err
	}
	_, err = p.pool.Exec(ctx, `DELETE FROM realtime_tickets WHERE expires_at < $1`, p.now())
	return int(tag.RowsAffected()), err
}

func (p *Postgres) IssueTicket(ctx context.Context, tk Ticket, ttl time.Duration) (string, time.Time, error) {
	t := newTicket()
	exp := p.now().Add(ttl).UTC().Truncate(time.Microsecond)
	_, err := p.pool.Exec(ctx,
		`INSERT INTO realtime_tickets (ticket_sha256, user_id, session_id, league_id, expires_at) VALUES ($1, $2, $3, $4, $5)`,
		hashTicket(t), tk.UserID, nullable(tk.SessionID), nullable(tk.LeagueID), exp)
	return t, exp, err
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (p *Postgres) RedeemTicket(ctx context.Context, ticket string) (Ticket, error) {
	var (
		t               Ticket
		session, league *string
		expires         time.Time
	)
	// A ticket of a revoked session is gone already (ON DELETE CASCADE).
	err := p.pool.QueryRow(ctx,
		`UPDATE realtime_tickets SET used_at = $2 WHERE ticket_sha256 = $1 AND used_at IS NULL
		 RETURNING user_id, session_id, league_id, expires_at`, hashTicket(ticket), p.now()).Scan(&t.UserID, &session, &league, &expires)
	if errors.Is(err, pgx.ErrNoRows) {
		return Ticket{}, ErrBadTicket
	}
	if err != nil {
		return Ticket{}, err
	}
	if !p.now().Before(expires) {
		return Ticket{}, ErrBadTicket
	}
	if session != nil {
		t.SessionID = *session
	}
	if league != nil {
		t.LeagueID = *league
	}
	return t, nil
}

// Revoke deletes the unused tickets r covers and notifies ControlChannel,
// so every instance's Listen loop (this one's too) applies r to its hub.
func (p *Postgres) Revoke(ctx context.Context, r Revocation) error {
	var err error
	switch {
	case r.SessionID != "":
		_, err = p.pool.Exec(ctx, `DELETE FROM realtime_tickets WHERE session_id = $1`, r.SessionID)
	case r.UserID != "" && r.LeagueID == "":
		_, err = p.pool.Exec(ctx, `DELETE FROM realtime_tickets WHERE user_id = $1`, r.UserID)
	case r.UserID != "":
		_, err = p.pool.Exec(ctx, `DELETE FROM realtime_tickets WHERE user_id = $1 AND league_id = $2`, r.UserID, r.LeagueID)
	default:
		return nil
	}
	if err != nil {
		return err
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return err
	}
	_, err = p.pool.Exec(ctx, `SELECT pg_notify($1, $2)`, ControlChannel, string(payload))
	return err
}

// Listen delivers the events other instances (and this one) append to
// hub, through LISTEN on Channel, and applies the revocations notified
// on ControlChannel, until ctx ends. It reconnects with
// backoff; events notified while disconnected are not replayed here
// (clients see the seq gap and refetch).
func Listen(ctx context.Context, pool *pgxpool.Pool, st Store, hub *Hub, log *slog.Logger) {
	backoff := time.Second
	for ctx.Err() == nil {
		err := listenOnce(ctx, pool, st, hub)
		if ctx.Err() != nil {
			return
		}
		log.WarnContext(ctx, "realtime listener reconnecting", "err", err, "in", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, 30*time.Second)
	}
}

func listenOnce(ctx context.Context, pool *pgxpool.Pool, st Store, hub *Hub) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "LISTEN "+Channel+"; LISTEN "+ControlChannel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if n.Channel == ControlChannel {
			var r Revocation
			if json.Unmarshal([]byte(n.Payload), &r) == nil {
				hub.Revoke(r)
			}
			continue
		}
		league, seqText, ok := strings.Cut(n.Payload, ":")
		seq, perr := strconv.ParseInt(seqText, 10, 64)
		if !ok || perr != nil {
			continue
		}
		ev, found, err := st.Get(ctx, league, seq)
		if err != nil {
			return err
		}
		if found {
			hub.Publish(ev)
		}
	}
}
