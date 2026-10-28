package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/store"
)

// Postgres is the Service on Postgres. Times come from now, not the
// database clock, so tests can move time.
type Postgres struct {
	pool *pgxpool.Pool
	now  func() time.Time
}

var _ Service = (*Postgres)(nil)

// NewPostgres returns a Service on pool.
func NewPostgres(pool *pgxpool.Pool, now func() time.Time) *Postgres {
	return &Postgres{pool: pool, now: now}
}

func (p *Postgres) tx(ctx context.Context, fn func(pgx.Tx) error) error {
	return store.InTx(ctx, p.pool, pgx.ReadCommitted, fn)
}

func (p *Postgres) StartEmail(ctx context.Context, email string) (EmailSignIn, error) {
	token, code := randomToken(), randomCode()
	now := p.now()
	expiresAt := now.Add(MagicLinkTTL)
	h := hash(token)
	_, err := p.pool.Exec(ctx,
		`INSERT INTO magic_links (token_sha256, email, expires_at, created_at, code_sha256) VALUES ($1, $2, $3, $4, $5)`,
		h, normalizeEmail(email), expiresAt, now, codeHash(h, code))
	if err != nil {
		return EmailSignIn{}, err
	}
	return EmailSignIn{Token: token, Code: code, ExpiresAt: expiresAt}, nil
}

func (p *Postgres) VerifyEmailCode(ctx context.Context, email, code string) (Tokens, *User, bool, error) {
	now := p.now()
	email = normalizeEmail(email)
	var (
		t       Tokens
		userID  string
		created bool
		// A wrong code must commit its count although the caller gets an
		// error, so the transaction returns nil and sets these instead.
		wrong   bool
		limited *CodeLimitError
	)
	err := p.tx(ctx, func(tx pgx.Tx) error {
		wrong, limited = false, nil
		// One code check per address at a time, so concurrent guesses
		// cannot all slip under the limits.
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('sign-in-code:' || $1, 0))`, email); err != nil {
			return err
		}
		var n int
		var oldest *time.Time
		if err := tx.QueryRow(ctx,
			`SELECT count(*), min(failed_at) FROM sign_in_code_failures WHERE email = $1 AND failed_at > $2`,
			email, now.Add(-CodeFailureWindow)).Scan(&n, &oldest); err != nil {
			return err
		}
		if n >= CodeFailuresPerEmail && oldest != nil {
			limited = codeRetryAfter(*oldest, now)
			return nil
		}
		// The address's latest sign-in.
		var (
			tokenHash string
			stored    *string
			expiresAt time.Time
			usedAt    *time.Time
		)
		err := tx.QueryRow(ctx,
			`SELECT token_sha256, code_sha256, expires_at, used_at FROM magic_links
			 WHERE email = $1 ORDER BY seq DESC LIMIT 1 FOR UPDATE`, email).
			Scan(&tokenHash, &stored, &expiresAt, &usedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		if usedAt != nil || !now.Before(expiresAt) {
			return ErrInvalidToken
		}
		if stored == nil || !codeMatches(*stored, tokenHash, code) {
			wrong = true
			if _, err := tx.Exec(ctx, `INSERT INTO sign_in_code_failures (email, failed_at) VALUES ($1, $2)`, email, now); err != nil {
				return err
			}
			// The last allowed wrong code kills the sign-in, link and all.
			_, err := tx.Exec(ctx,
				`UPDATE magic_links SET code_failures = code_failures + 1,
				        used_at = CASE WHEN code_failures + 1 >= $2 THEN $3::timestamptz ELSE used_at END
				 WHERE token_sha256 = $1`, tokenHash, CodeAttempts, now)
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE magic_links SET used_at = $2 WHERE token_sha256 = $1`, tokenHash, now); err != nil {
			return err
		}
		userID, created, err = userForEmail(ctx, tx, email, now)
		if err != nil {
			return err
		}
		t, err = issue(ctx, tx, userID, now)
		return err
	})
	switch {
	case err != nil:
		return Tokens{}, nil, false, err
	case limited != nil:
		return Tokens{}, nil, false, limited
	case wrong:
		return Tokens{}, nil, false, ErrInvalidToken
	}
	u, err := p.User(ctx, userID)
	return t, u, created, err
}

func (p *Postgres) VerifyEmail(ctx context.Context, token string) (Tokens, *User, bool, error) {
	now := p.now()
	var (
		t       Tokens
		userID  string
		created bool
	)
	err := p.tx(ctx, func(tx pgx.Tx) error {
		var email string
		var expiresAt time.Time
		err := tx.QueryRow(ctx,
			`UPDATE magic_links SET used_at = $2 WHERE token_sha256 = $1 AND used_at IS NULL
			 RETURNING email, expires_at`, hash(token), now).Scan(&email, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		if !now.Before(expiresAt) {
			return ErrInvalidToken
		}
		userID, created, err = userForEmail(ctx, tx, email, now)
		if err != nil {
			return err
		}
		t, err = issue(ctx, tx, userID, now)
		return err
	})
	if err != nil {
		return Tokens{}, nil, false, err
	}
	u, err := p.User(ctx, userID)
	return t, u, created, err
}

// userForEmail finds or creates the user with this email and records the
// email identity.
func userForEmail(ctx context.Context, tx pgx.Tx, email string, now time.Time) (string, bool, error) {
	var id string
	created := true
	err := tx.QueryRow(ctx,
		`INSERT INTO users (id, display_name, email, created_at) VALUES ($1, $2, $3, $4)
		 ON CONFLICT (email) DO NOTHING RETURNING id`,
		ids.New("usr"), defaultName(email), email, now).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		created = false
		err = tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id)
	}
	if err != nil {
		return "", false, err
	}
	_, err = tx.Exec(ctx,
		`INSERT INTO auth_identities (provider, subject, user_id, created_at) VALUES ('email', $1, $2, $3)
		 ON CONFLICT (provider, subject) DO NOTHING`, email, id, now)
	return id, created, err
}

func issue(ctx context.Context, db store.DB, userID string, now time.Time) (Tokens, error) {
	t := newTokens(ids.ULID(), now)
	_, err := db.Exec(ctx,
		`INSERT INTO sessions (id, user_id, access_sha256, access_expires_at, refresh_sha256, refresh_expires_at, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		t.SessionID, userID, hash(t.AccessToken), t.AccessExpiresAt, hash(t.RefreshToken), t.RefreshExpiresAt, now)
	return t, err
}

func (p *Postgres) SignInWithIdentity(ctx context.Context, id Identity) (Tokens, *User, bool, error) {
	now := p.now()
	email := normalizeEmail(id.Email)
	var (
		t       Tokens
		userID  string
		created bool
	)
	err := p.tx(ctx, func(tx pgx.Tx) error {
		created = false
		err := tx.QueryRow(ctx, `SELECT user_id FROM auth_identities WHERE provider = $1 AND subject = $2`,
			id.Provider, id.Subject).Scan(&userID)
		switch {
		case err == nil:
		case !errors.Is(err, pgx.ErrNoRows):
			return err
		default:
			userID = ""
			if id.EmailVerified && email != "" {
				err := tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&userID)
				if err != nil && !errors.Is(err, pgx.ErrNoRows) {
					return err
				}
			}
			if userID == "" {
				var mail *string
				if id.EmailVerified && email != "" {
					mail = &email
				}
				userID, created = ids.New("usr"), true
				if _, err := tx.Exec(ctx,
					`INSERT INTO users (id, display_name, email, created_at) VALUES ($1, $2, $3, $4)`,
					userID, id.newName(), mail, now); err != nil {
					return err
				}
			}
			if _, err := tx.Exec(ctx,
				`INSERT INTO auth_identities (provider, subject, user_id, created_at) VALUES ($1, $2, $3, $4)`,
				id.Provider, id.Subject, userID, now); err != nil {
				return err
			}
		}
		if id.AppleRefreshToken != "" {
			if _, err := tx.Exec(ctx,
				`UPDATE auth_identities SET apple_refresh_token = $3 WHERE provider = $1 AND subject = $2`,
				id.Provider, id.Subject, id.AppleRefreshToken); err != nil {
				return err
			}
		}
		t, err = issue(ctx, tx, userID, now)
		return err
	})
	if store.IsUniqueViolation(err) && !id.retried {
		// Two first sign-ins at once: the other one created the account.
		id.retried = true
		return p.SignInWithIdentity(ctx, id)
	}
	if err != nil {
		return Tokens{}, nil, false, err
	}
	u, err := p.User(ctx, userID)
	return t, u, created, err
}

func (p *Postgres) CreateUser(ctx context.Context, displayName, email string) (*User, error) {
	var id string
	err := p.tx(ctx, func(tx pgx.Tx) error {
		var err error
		id, _, err = userForEmail(ctx, tx, normalizeEmail(email), p.now())
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE users SET display_name = $2 WHERE id = $1`, id, displayName)
		return err
	})
	if err != nil {
		return nil, err
	}
	return p.User(ctx, id)
}

func (p *Postgres) CreateAccount(ctx context.Context, displayName string) (Tokens, *User, error) {
	var (
		id string
		t  Tokens
	)
	err := p.tx(ctx, func(tx pgx.Tx) error {
		var err error
		id, t, err = CreateAccountIn(ctx, tx, displayName, p.now())
		return err
	})
	if err != nil {
		return Tokens{}, nil, err
	}
	u, err := p.User(ctx, id)
	return t, u, err
}

// CreateAccountIn is CreateAccount in the caller's transaction, so the
// account commits with what it is made for (the claimed seat) or not at
// all. It returns the new user's id and session.
func CreateAccountIn(ctx context.Context, db store.DB, displayName string, now time.Time) (string, Tokens, error) {
	id := ids.New("usr")
	if _, err := db.Exec(ctx, `INSERT INTO users (id, display_name, created_at) VALUES ($1, $2, $3)`,
		id, Identity{Name: displayName}.newName(), now); err != nil {
		return "", Tokens{}, err
	}
	t, err := issue(ctx, db, id, now)
	return id, t, err
}

func (p *Postgres) Issue(ctx context.Context, userID string) (Tokens, error) {
	t, err := issue(ctx, p.pool, userID, p.now())
	if err != nil && store.IsForeignKeyViolation(err) {
		return Tokens{}, ErrNoUser
	}
	return t, err
}

func (p *Postgres) Authenticate(ctx context.Context, accessToken string) (string, string, error) {
	var userID, sessionID string
	err := p.pool.QueryRow(ctx,
		`SELECT user_id, id FROM sessions WHERE access_sha256 = $1 AND access_expires_at > $2`,
		hash(accessToken), p.now()).Scan(&userID, &sessionID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", "", ErrInvalidToken
	}
	return userID, sessionID, err
}

func (p *Postgres) SessionActive(ctx context.Context, sessionID string) (bool, error) {
	var ok bool
	err := p.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM sessions WHERE id = $1)`, sessionID).Scan(&ok)
	return ok, err
}

func (p *Postgres) Refresh(ctx context.Context, refreshToken string) (Tokens, *User, error) {
	now := p.now()
	h := hash(refreshToken)
	var (
		t      Tokens
		userID string
	)
	// A revoke on reuse or expiry must commit even though the caller gets
	// an error, so the transaction returns nil and sets revoked instead.
	revoked := ""
	err := p.tx(ctx, func(tx pgx.Tx) error {
		revoked = ""
		var sessionID string
		var expiresAt time.Time
		err := tx.QueryRow(ctx,
			`SELECT id, user_id, refresh_expires_at FROM sessions WHERE refresh_sha256 = $1 FOR UPDATE`, h).
			Scan(&sessionID, &userID, &expiresAt)
		if errors.Is(err, pgx.ErrNoRows) {
			// A rotated-out token: someone has a copy. Revoke the session.
			err := tx.QueryRow(ctx,
				`DELETE FROM sessions WHERE id = (SELECT session_id FROM used_refresh_tokens WHERE refresh_sha256 = $1)
				 RETURNING id`, h).Scan(&revoked)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrInvalidToken
			}
			return err
		}
		if err != nil {
			return err
		}
		if !now.Before(expiresAt) {
			revoked = sessionID
			_, err := tx.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
			return err
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO used_refresh_tokens (refresh_sha256, session_id) VALUES ($1, $2)`, h, sessionID); err != nil {
			return err
		}
		t = newTokens(sessionID, now)
		_, err = tx.Exec(ctx,
			`UPDATE sessions SET access_sha256 = $2, access_expires_at = $3, refresh_sha256 = $4, refresh_expires_at = $5
			 WHERE id = $1`,
			sessionID, hash(t.AccessToken), t.AccessExpiresAt, hash(t.RefreshToken), t.RefreshExpiresAt)
		return err
	})
	switch {
	case err != nil:
		return Tokens{}, nil, err
	case revoked != "":
		return Tokens{}, nil, &RevokedError{SessionID: revoked}
	}
	u, err := p.User(ctx, userID)
	if err != nil {
		return Tokens{}, nil, err
	}
	return t, u, nil
}

func (p *Postgres) Revoke(ctx context.Context, sessionID string) error {
	_, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, sessionID)
	return err
}

func (p *Postgres) User(ctx context.Context, id string) (*User, error) {
	var (
		u            User
		email, color *string
	)
	err := p.pool.QueryRow(ctx,
		`SELECT u.id, u.display_name, u.email, u.avatar_color, u.created_at,
		        coalesce(array_agg(i.provider ORDER BY i.created_at, i.provider) FILTER (WHERE i.provider IS NOT NULL), '{}')
		 FROM users u LEFT JOIN auth_identities i ON i.user_id = u.id
		 WHERE u.id = $1 GROUP BY u.id`, id).
		Scan(&u.ID, &u.DisplayName, &email, &color, &u.CreatedAt, &u.Providers)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoUser
	}
	if err != nil {
		return nil, err
	}
	if email != nil {
		u.Email = *email
	}
	if color != nil {
		u.AvatarColor = *color
	}
	return &u, nil
}

func (p *Postgres) DisplayNames(ctx context.Context, userIDs []string) (map[string]string, error) {
	rows, err := p.pool.Query(ctx, `SELECT id, display_name FROM users WHERE id = ANY($1)`, userIDs)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	var id, name string
	_, err = pgx.ForEachRow(rows, []any{&id, &name}, func() error {
		out[id] = name
		return nil
	})
	return out, err
}

func (p *Postgres) UpdateUser(ctx context.Context, id string, displayName, avatarColor *string) (*User, error) {
	tag, err := p.pool.Exec(ctx,
		`UPDATE users SET display_name = coalesce($2, display_name), avatar_color = CASE WHEN $3::text IS NULL THEN avatar_color ELSE nullif($3::text, '') END
		 WHERE id = $1`, id, displayName, avatarColor)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNoUser
	}
	return p.User(ctx, id)
}

func (p *Postgres) DeleteUser(ctx context.Context, id string) error {
	// Sessions and identities cascade; teams are orphaned (owner_id SET NULL).
	return p.tx(ctx, func(tx pgx.Tx) error { return DeleteUserIn(ctx, tx, id) })
}

// DeleteUserIn deletes the user and the magic links sent to their email
// in db (a transaction the caller may share with other work, see
// httpapi's account deletion). Sessions, identities and the rest cascade.
func DeleteUserIn(ctx context.Context, db store.DB, id string) error {
	for _, q := range []string{
		`DELETE FROM magic_links WHERE email = (SELECT email FROM users WHERE id = $1)`,
		`DELETE FROM sign_in_code_failures WHERE email = (SELECT email FROM users WHERE id = $1)`,
	} {
		if _, err := db.Exec(ctx, q, id); err != nil {
			return err
		}
	}
	_, err := db.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	return err
}

func (p *Postgres) AppleRefreshTokens(ctx context.Context, userID string) ([]string, error) {
	rows, err := p.pool.Query(ctx,
		`SELECT apple_refresh_token FROM auth_identities
		 WHERE user_id = $1 AND provider = 'apple' AND apple_refresh_token IS NOT NULL`, userID)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

func (p *Postgres) Prune(ctx context.Context) (int, error) {
	now := p.now()
	links, err := p.pool.Exec(ctx, `DELETE FROM magic_links WHERE expires_at <= $1 OR used_at IS NOT NULL`, now)
	if err != nil {
		return 0, err
	}
	sessions, err := p.pool.Exec(ctx, `DELETE FROM sessions WHERE refresh_expires_at <= $1`, now)
	if err != nil {
		return int(links.RowsAffected()), err
	}
	failures, err := p.pool.Exec(ctx, `DELETE FROM sign_in_code_failures WHERE failed_at <= $1`, now.Add(-CodeFailureWindow))
	if err != nil {
		return int(links.RowsAffected() + sessions.RowsAffected()), err
	}
	signIn, err := p.pool.Exec(ctx, `DELETE FROM signin_links WHERE expires_at <= $1`, now)
	if err != nil {
		return int(links.RowsAffected() + sessions.RowsAffected() + failures.RowsAffected()), err
	}
	return int(links.RowsAffected() + sessions.RowsAffected() + failures.RowsAffected() + signIn.RowsAffected()), nil
}
