package auth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// SignInLinkTTL is how long a commissioner-issued sign-in link works.
const SignInLinkTTL = 7 * 24 * time.Hour

// SignInLink is an issued sign-in link: Code goes in the link and is not
// kept (only its hash is). LeagueID is where the link lands.
type SignInLink struct {
	Code      string
	LeagueID  string
	ExpiresAt time.Time
}

// SignInLinkGrant is what a link was issued for: its user, league and
// issuer, which UseSignInLink's check confirms still hold.
type SignInLinkGrant struct {
	UserID, LeagueID, IssuerID string
}

type memSignInLink struct {
	SignInLinkGrant
	expiresAt time.Time
}

// IssueSignInLink makes a single-use link that signs userID in, valid for
// SignInLinkTTL, and deletes the user's older unused links.
func (s *Memory) IssueSignInLink(_ context.Context, userID, leagueID, issuerID string) (SignInLink, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; !ok {
		return SignInLink{}, ErrNoUser
	}
	if s.signInLinks == nil {
		s.signInLinks = map[string]memSignInLink{}
	}
	for h, l := range s.signInLinks {
		if l.UserID == userID {
			delete(s.signInLinks, h)
		}
	}
	code := randomToken()
	out := SignInLink{Code: code, LeagueID: leagueID, ExpiresAt: now.Add(SignInLinkTTL)}
	s.signInLinks[hash(code)] = memSignInLink{SignInLinkGrant{userID, leagueID, issuerID}, out.ExpiresAt}
	return out, nil
}

// UseSignInLink consumes a sign-in link and starts a session for its
// user once check accepts the grant. It returns the link's league.
// Unknown, used, replaced or expired links, and grants check refuses, are
// ErrInvalidToken.
func (s *Memory) UseSignInLink(ctx context.Context, code string, check func(context.Context, SignInLinkGrant) error) (Tokens, *User, string, error) {
	now := s.now()
	s.mu.Lock()
	h := hash(code)
	l, ok := s.signInLinks[h]
	delete(s.signInLinks, h)
	s.mu.Unlock()
	if !ok || !now.Before(l.expiresAt) {
		return Tokens{}, nil, "", ErrInvalidToken
	}
	if err := check(ctx, l.SignInLinkGrant); err != nil {
		return Tokens{}, nil, "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[l.UserID]
	if !ok {
		return Tokens{}, nil, "", ErrInvalidToken
	}
	return s.issueLocked(u.ID, now), copyUser(u), l.LeagueID, nil
}

// RevokeSignInLinks deletes the unused sign-in links issued in leagueID:
// all of them, or only userID's when it is set.
func (s *Memory) RevokeSignInLinks(_ context.Context, leagueID, userID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for h, l := range s.signInLinks {
		if l.LeagueID == leagueID && (userID == "" || l.UserID == userID) {
			delete(s.signInLinks, h)
		}
	}
	return nil
}

// SignInLinkCount counts userID's unused sign-in links (tests).
func (s *Memory) SignInLinkCount(userID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, l := range s.signInLinks {
		if l.UserID == userID {
			n++
		}
	}
	return n
}

func (p *Postgres) IssueSignInLink(ctx context.Context, userID, leagueID, issuerID string) (SignInLink, error) {
	now := p.now()
	code := randomToken()
	out := SignInLink{Code: code, LeagueID: leagueID, ExpiresAt: now.Add(SignInLinkTTL)}
	err := p.tx(ctx, func(tx pgx.Tx) error {
		// The user's row lock orders two issues for the same user.
		var id string
		err := tx.QueryRow(ctx, `SELECT id FROM users WHERE id = $1 FOR UPDATE`, userID).Scan(&id)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoUser
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM signin_links WHERE user_id = $1`, userID); err != nil {
			return err
		}
		_, err = tx.Exec(ctx,
			`INSERT INTO signin_links (code_sha256, user_id, league_id, issuer_id, expires_at, created_at) VALUES ($1, $2, $3, $4, $5, $6)`,
			hash(code), userID, leagueID, issuerID, out.ExpiresAt, now)
		return err
	})
	if err != nil {
		return SignInLink{}, err
	}
	return out, nil
}

func (p *Postgres) UseSignInLink(ctx context.Context, code string, check func(context.Context, SignInLinkGrant) error) (Tokens, *User, string, error) {
	now := p.now()
	var (
		g         SignInLinkGrant
		expiresAt time.Time
	)
	// Deleted first, in its own statement: the link is spent whatever the
	// outcome, and two uses cannot both get it.
	err := p.pool.QueryRow(ctx,
		`DELETE FROM signin_links WHERE code_sha256 = $1 RETURNING user_id, league_id, issuer_id, expires_at`,
		hash(code)).Scan(&g.UserID, &g.LeagueID, &g.IssuerID, &expiresAt)
	if errors.Is(err, pgx.ErrNoRows) || err == nil && !now.Before(expiresAt) {
		return Tokens{}, nil, "", ErrInvalidToken
	}
	if err != nil {
		return Tokens{}, nil, "", err
	}
	if err := check(ctx, g); err != nil {
		return Tokens{}, nil, "", err
	}
	var t Tokens
	err = p.tx(ctx, func(tx pgx.Tx) error {
		var err error
		t, err = issue(ctx, tx, g.UserID, now)
		return err
	})
	if err != nil {
		return Tokens{}, nil, "", err
	}
	u, err := p.User(ctx, g.UserID)
	if err != nil {
		return Tokens{}, nil, "", err
	}
	return t, u, g.LeagueID, nil
}

func (p *Postgres) RevokeSignInLinks(ctx context.Context, leagueID, userID string) error {
	_, err := p.pool.Exec(ctx,
		`DELETE FROM signin_links WHERE league_id = $1 AND ($2 = '' OR user_id = $2)`, leagueID, userID)
	return err
}
