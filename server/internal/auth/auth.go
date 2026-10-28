// Package auth owns accounts and sessions: users, opaque bearer tokens,
// refresh-token rotation and email magic links. Sign in with Apple and
// Google verify the provider's identity token first (package idp, in
// httpapi; SignInWithIdentity links or creates the account); after that
// every method ends in the same session issue.
//
// Service has two implementations with the same rules: Postgres (the
// server) and Memory (unit tests and the database-less dev server). A
// shared test suite runs against both. Tokens are random 256-bit strings;
// only their SHA-256 is kept, so a dump of the store does not leak usable
// tokens (docs/ARCHITECTURE.md, tables users, sessions, magic_links).
//
// An email sign-in (one magic link) also carries a 6-digit code for
// typing in on another device: the link or the code, whichever comes
// first, consumes it. Guessing is bounded per sign-in (CodeAttempts) and
// per address (CodeFailuresPerEmail in CodeFailureWindow); see
// docs/ARCHITECTURE.md "Security" for the numbers.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

// Lifetimes.
const (
	AccessTTL    = time.Hour
	RefreshTTL   = 60 * 24 * time.Hour
	MagicLinkTTL = 15 * time.Minute
)

// Sign-in code limits. A code is 6 digits (10^6 values), so a guess
// succeeds with probability 1e-6: an address takes at most
// CodeFailuresPerEmail wrong guesses per CodeFailureWindow, so at most a
// 1e-5 chance per day of attack, each guess against a code just mailed to
// the address's owner.
const (
	// CodeAttempts is how many wrong codes kill a sign-in (its link too).
	CodeAttempts = 5
	// CodeFailuresPerEmail wrong codes in CodeFailureWindow lock code
	// sign-in for an address (the link keeps working).
	CodeFailuresPerEmail = 10
	CodeFailureWindow    = 24 * time.Hour
)

var (
	// ErrInvalidToken: unknown, expired, revoked or already used.
	ErrInvalidToken = errors.New("auth: invalid or expired token")
	// ErrNoUser: the user does not exist (deleted).
	ErrNoUser = errors.New("auth: no such user")
	// ErrTooManyCodes: the address has used up its wrong sign-in codes
	// (CodeLimitError says for how long).
	ErrTooManyCodes = errors.New("auth: too many wrong sign-in codes")
)

// CodeLimitError is VerifyEmailCode's error while the address is over
// CodeFailuresPerEmail. It is an ErrTooManyCodes.
type CodeLimitError struct{ RetryAfter time.Duration }

func (e *CodeLimitError) Error() string { return ErrTooManyCodes.Error() }
func (e *CodeLimitError) Unwrap() error { return ErrTooManyCodes }

// EmailSignIn is a started email sign-in: the link's token and the code,
// both single use, both ending at ExpiresAt.
type EmailSignIn struct {
	Token     string
	Code      string
	ExpiresAt time.Time
}

// RevokedError is Refresh's error when it revoked the session: the
// refresh token was a rotated-out copy (reuse) or expired. It is an
// ErrInvalidToken; the caller ends what else the session holds (its
// realtime sockets).
type RevokedError struct{ SessionID string }

func (e *RevokedError) Error() string { return "auth: session revoked" }
func (e *RevokedError) Unwrap() error { return ErrInvalidToken }

// User is an account.
type User struct {
	ID          string
	DisplayName string
	Email       string // "" when unknown
	AvatarColor string // "" for the default
	Providers   []string
	CreatedAt   time.Time
}

// Tokens is a freshly issued session.
type Tokens struct {
	SessionID        string
	AccessToken      string
	AccessExpiresAt  time.Time
	RefreshToken     string
	RefreshExpiresAt time.Time
}

// Service is the account and session store.
type Service interface {
	// StartEmail creates a single-use magic-link token and sign-in code for
	// email. The caller sends them (see Sender). It succeeds for any
	// address, so the endpoint cannot reveal whether an account exists.
	StartEmail(ctx context.Context, email string) (EmailSignIn, error)
	// VerifyEmail consumes a magic-link token and signs the user in,
	// creating the account on first use. created reports a new account.
	VerifyEmail(ctx context.Context, token string) (t Tokens, u *User, created bool, err error)
	// VerifyEmailCode signs in with the code of the address's latest email
	// sign-in, consuming it like its link. A wrong code counts against
	// the sign-in (CodeAttempts, then it is dead) and the address
	// (CodeFailuresPerEmail per CodeFailureWindow, then a CodeLimitError
	// before any code is compared). Anything else is ErrInvalidToken.
	VerifyEmailCode(ctx context.Context, email, code string) (t Tokens, u *User, created bool, err error)
	// SignInWithIdentity signs in with a verified provider identity
	// (Apple, Google): the user who has it, else the user with this email
	// when the provider verified it (the identity is linked), else a new
	// user. created reports a new account.
	SignInWithIdentity(ctx context.Context, id Identity) (t Tokens, u *User, created bool, err error)
	// CreateAccount creates an account with only a display name (no email,
	// no provider identity) and signs it in: a seat's claim link signing a
	// signed-out person in (docs/adr/0013). The name is trimmed.
	CreateAccount(ctx context.Context, displayName string) (Tokens, *User, error)
	// IssueSignInLink makes a single-use link that signs userID in on any
	// device within SignInLinkTTL, landing on leagueID, and deletes the
	// user's older unused links (docs/adr/0013, "Sign-in links"). Only the
	// code's hash is kept, with issuerID.
	IssueSignInLink(ctx context.Context, userID, leagueID, issuerID string) (SignInLink, error)
	// UseSignInLink consumes a sign-in link and, once check accepts its
	// grant, starts a session for its user; it returns the link's league.
	// Unknown, used, replaced or expired links are ErrInvalidToken; a
	// refused grant returns check's error, and the link is spent either way.
	UseSignInLink(ctx context.Context, code string, check func(context.Context, SignInLinkGrant) error) (t Tokens, u *User, leagueID string, err error)
	// RevokeSignInLinks deletes the unused sign-in links issued in
	// leagueID: all of them, or only userID's when it is set.
	RevokeSignInLinks(ctx context.Context, leagueID, userID string) error
	// CreateUser finds or creates the user with this email and sets the
	// display name (tests and seeding).
	CreateUser(ctx context.Context, displayName, email string) (*User, error)
	// Issue starts a session for userID (tests and provider sign-ins).
	Issue(ctx context.Context, userID string) (Tokens, error)
	// Authenticate resolves a bearer access token to its user and session.
	Authenticate(ctx context.Context, accessToken string) (userID, sessionID string, err error)
	// SessionActive reports whether the session exists (not logged out,
	// revoked or deleted with its user).
	SessionActive(ctx context.Context, sessionID string) (bool, error)
	// Refresh rotates a session's tokens. Reusing a rotated-out refresh
	// token revokes the session (someone else has a copy).
	Refresh(ctx context.Context, refreshToken string) (Tokens, *User, error)
	// Revoke ends one session.
	Revoke(ctx context.Context, sessionID string) error
	// User returns the user or ErrNoUser.
	User(ctx context.Context, id string) (*User, error)
	// DisplayNames returns the display name of every existing user in ids.
	DisplayNames(ctx context.Context, ids []string) (map[string]string, error)
	// UpdateUser changes the display name and/or avatar colour (nil = keep;
	// an empty colour goes back to the default).
	UpdateUser(ctx context.Context, id string, displayName, avatarColor *string) (*User, error)
	// DeleteUser removes the account, revokes all its sessions and deletes
	// the magic links sent to its email. Revoking the Apple token and
	// leaving leagues is the caller's job.
	DeleteUser(ctx context.Context, id string) error
	// AppleRefreshTokens returns the sealed Apple refresh tokens of the
	// user's Apple identities (to revoke them at account deletion).
	AppleRefreshTokens(ctx context.Context, userID string) ([]string, error)
	// Prune deletes expired or used magic links and sessions past their
	// refresh expiry (the prune job) and returns how many rows went.
	Prune(ctx context.Context) (int, error)
}

// Identity is a provider-verified sign-in.
type Identity struct {
	Provider      string // apple | google
	Subject       string // the provider's stable user id
	Email         string
	EmailVerified bool
	// Name is the display name for a new account ("" derives one).
	Name string
	// AppleRefreshToken is the sealed Apple refresh token from this
	// sign-in's authorization code (idp.AppleTokens.Seal), kept to revoke
	// at account deletion; "" keeps the one stored.
	AppleRefreshToken string

	retried bool
}

// newName is the display name of an account created from id.
func (id Identity) newName() string {
	name := strings.TrimSpace(id.Name)
	if name == "" && id.Email != "" {
		name = defaultName(normalizeEmail(id.Email))
	}
	if name == "" {
		name = "Manager"
	}
	if r := []rune(name); len(r) > 32 {
		name = string(r[:32])
	}
	return name
}

func randomToken() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("auth: crypto/rand: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b[:])
}

// randomCode is a uniformly random 6-digit code.
func randomCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic("auth: crypto/rand: " + err.Error())
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// codeHash is the stored form of a sign-in code, salted with its link's
// token hash. Six digits are quick to brute-force from a hash, so this
// keeps codes out of plain sight in a dump; what protects a code is its
// 15 minutes, single use and the attempt limits.
func codeHash(tokenHash, code string) string { return hash(tokenHash + ":" + code) }

// codeMatches compares in constant time.
func codeMatches(stored, tokenHash, code string) bool {
	return stored != "" && subtle.ConstantTimeCompare([]byte(stored), []byte(codeHash(tokenHash, code))) == 1
}

// codeRetryAfter is how long until the oldest failure in the window leaves it.
func codeRetryAfter(oldest, now time.Time) *CodeLimitError {
	return &CodeLimitError{RetryAfter: max(time.Second, oldest.Add(CodeFailureWindow).Sub(now))}
}

func hash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }

// defaultName derives a display name from an email address.
func defaultName(email string) string {
	name, _, _ := strings.Cut(email, "@")
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}

func newTokens(sessionID string, now time.Time) Tokens {
	return Tokens{
		SessionID:        sessionID,
		AccessToken:      randomToken(),
		AccessExpiresAt:  now.Add(AccessTTL),
		RefreshToken:     randomToken(),
		RefreshExpiresAt: now.Add(RefreshTTL),
	}
}
