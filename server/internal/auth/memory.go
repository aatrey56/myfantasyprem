package auth

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
)

type memSession struct {
	id, userID       string
	accessHash       string
	accessExpiresAt  time.Time
	refreshHash      string
	refreshExpiresAt time.Time
	// usedRefresh holds hashes of rotated-out refresh tokens. Presenting one
	// again means the token was copied: the whole session is revoked.
	usedRefresh map[string]bool
}

type magicLink struct {
	email     string
	expiresAt time.Time
	used      bool
	seq       int // creation order
	codeHash  string
	failures  int
}

// Memory is the in-memory Service. Safe for concurrent use.
type Memory struct {
	now func() time.Time

	mu        sync.Mutex
	users     map[string]*User       // by id
	byEmail   map[string]string      // lowercased email -> user id
	sessions  map[string]*memSession // by id
	byAccess  map[string]string      // access hash -> session id
	byRefresh map[string]string      // current or used refresh hash -> session id
	links     map[string]magicLink
	linkSeq   int
	// codeFailures are the times of wrong sign-in codes by address.
	codeFailures map[string][]time.Time
	idents       map[string]string // provider + "\x00" + subject -> user id
	// appleTokens are the sealed Apple refresh tokens by identity key.
	appleTokens map[string]string
	// signInLinks are commissioner-issued sign-in links by code hash.
	signInLinks map[string]memSignInLink
}

func (s *Memory) AppleRefreshTokens(_ context.Context, userID string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for key, uid := range s.idents {
		if t := s.appleTokens[key]; uid == userID && t != "" {
			out = append(out, t)
		}
	}
	return out, nil
}

var _ Service = (*Memory)(nil)

func (s *Memory) SignInWithIdentity(_ context.Context, id Identity) (Tokens, *User, bool, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.idents == nil {
		s.idents = map[string]string{}
	}
	key := id.Provider + "\x00" + id.Subject
	if id.AppleRefreshToken != "" {
		defer func() { s.appleTokens[key] = id.AppleRefreshToken }() // runs under the lock
	}
	if uid, ok := s.idents[key]; ok {
		if u, ok := s.users[uid]; ok {
			return s.issueLocked(u.ID, now), copyUser(u), false, nil
		}
	}
	email := normalizeEmail(id.Email)
	if uid, ok := s.byEmail[email]; ok && id.EmailVerified && email != "" {
		u := s.users[uid]
		if !slices.Contains(u.Providers, id.Provider) {
			u.Providers = append(u.Providers, id.Provider)
		}
		s.idents[key] = uid
		return s.issueLocked(uid, now), copyUser(u), false, nil
	}
	u := &User{ID: ids.New("usr"), DisplayName: id.newName(), Providers: []string{id.Provider}, CreatedAt: now}
	if id.EmailVerified && email != "" {
		u.Email = email
		s.byEmail[email] = u.ID
	}
	s.users[u.ID] = u
	s.idents[key] = u.ID
	return s.issueLocked(u.ID, now), copyUser(u), true, nil
}

// NewMemory returns an empty store using clock now.
func NewMemory(now func() time.Time) *Memory {
	return &Memory{
		now:          now,
		users:        map[string]*User{},
		byEmail:      map[string]string{},
		sessions:     map[string]*memSession{},
		byAccess:     map[string]string{},
		byRefresh:    map[string]string{},
		links:        map[string]magicLink{},
		codeFailures: map[string][]time.Time{},
		appleTokens:  map[string]string{},
	}
}

func (s *Memory) StartEmail(_ context.Context, email string) (EmailSignIn, error) {
	token, code := randomToken(), randomCode()
	expiresAt := s.now().Add(MagicLinkTTL)
	h := hash(token)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.linkSeq++
	s.links[h] = magicLink{email: normalizeEmail(email), expiresAt: expiresAt, seq: s.linkSeq, codeHash: codeHash(h, code)}
	return EmailSignIn{Token: token, Code: code, ExpiresAt: expiresAt}, nil
}

func (s *Memory) VerifyEmailCode(_ context.Context, email, code string) (Tokens, *User, bool, error) {
	now := s.now()
	email = normalizeEmail(email)
	s.mu.Lock()
	defer s.mu.Unlock()
	// Failures still in the window; older ones no longer count.
	recent := s.codeFailures[email][:0:0]
	for _, at := range s.codeFailures[email] {
		if at.After(now.Add(-CodeFailureWindow)) {
			recent = append(recent, at)
		}
	}
	s.codeFailures[email] = recent
	if len(recent) >= CodeFailuresPerEmail {
		return Tokens{}, nil, false, codeRetryAfter(recent[0], now)
	}
	// The address's latest sign-in.
	var h string
	var link magicLink
	for k, l := range s.links {
		if l.email == email && l.seq > link.seq {
			h, link = k, l
		}
	}
	if h == "" || link.used || !now.Before(link.expiresAt) {
		return Tokens{}, nil, false, ErrInvalidToken
	}
	if !codeMatches(link.codeHash, h, code) {
		s.codeFailures[email] = append(recent, now)
		link.failures++
		link.used = link.failures >= CodeAttempts
		s.links[h] = link
		return Tokens{}, nil, false, ErrInvalidToken
	}
	link.used = true
	s.links[h] = link
	u, created := s.userForEmailLocked(link.email, "email", now)
	return s.issueLocked(u.ID, now), copyUser(u), created, nil
}

func (s *Memory) VerifyEmail(_ context.Context, token string) (Tokens, *User, bool, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	link, ok := s.links[hash(token)]
	if !ok || link.used || !now.Before(link.expiresAt) {
		return Tokens{}, nil, false, ErrInvalidToken
	}
	// Kept, used, until Prune (as on Postgres).
	link.used = true
	s.links[hash(token)] = link
	u, created := s.userForEmailLocked(link.email, "email", now)
	return s.issueLocked(u.ID, now), copyUser(u), created, nil
}

// userForEmailLocked finds or creates the user with this email.
func (s *Memory) userForEmailLocked(email, provider string, now time.Time) (*User, bool) {
	if id, ok := s.byEmail[email]; ok {
		u := s.users[id]
		if !slices.Contains(u.Providers, provider) {
			u.Providers = append(u.Providers, provider)
		}
		return u, false
	}
	u := &User{ID: ids.New("usr"), DisplayName: defaultName(email), Email: email, Providers: []string{provider}, CreatedAt: now}
	s.users[u.ID] = u
	s.byEmail[email] = u.ID
	return u, true
}

func (s *Memory) CreateUser(_ context.Context, displayName, email string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, _ := s.userForEmailLocked(normalizeEmail(email), "email", s.now())
	u.DisplayName = displayName
	return copyUser(u), nil
}

func (s *Memory) CreateAccount(_ context.Context, displayName string) (Tokens, *User, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	u := &User{ID: ids.New("usr"), DisplayName: Identity{Name: displayName}.newName(), CreatedAt: now}
	s.users[u.ID] = u
	return s.issueLocked(u.ID, now), copyUser(u), nil
}

func (s *Memory) Issue(_ context.Context, userID string) (Tokens, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[userID]; !ok {
		return Tokens{}, ErrNoUser
	}
	return s.issueLocked(userID, s.now()), nil
}

func (s *Memory) issueLocked(userID string, now time.Time) Tokens {
	t := newTokens(ids.ULID(), now)
	sess := &memSession{
		id: t.SessionID, userID: userID,
		accessHash: hash(t.AccessToken), accessExpiresAt: t.AccessExpiresAt,
		refreshHash: hash(t.RefreshToken), refreshExpiresAt: t.RefreshExpiresAt,
		usedRefresh: map[string]bool{},
	}
	s.sessions[sess.id] = sess
	s.byAccess[sess.accessHash] = sess.id
	s.byRefresh[sess.refreshHash] = sess.id
	return t
}

func (s *Memory) Authenticate(_ context.Context, accessToken string) (string, string, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[s.byAccess[hash(accessToken)]]
	if !ok || !now.Before(sess.accessExpiresAt) {
		return "", "", ErrInvalidToken
	}
	if _, ok := s.users[sess.userID]; !ok {
		return "", "", ErrInvalidToken
	}
	return sess.userID, sess.id, nil
}

func (s *Memory) SessionActive(_ context.Context, sessionID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[sessionID]
	if !ok {
		return false, nil
	}
	_, ok = s.users[sess.userID]
	return ok, nil
}

func (s *Memory) Refresh(_ context.Context, refreshToken string) (Tokens, *User, error) {
	now := s.now()
	h := hash(refreshToken)
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[s.byRefresh[h]]
	if !ok {
		return Tokens{}, nil, ErrInvalidToken
	}
	if sess.usedRefresh[h] {
		s.revokeLocked(sess)
		return Tokens{}, nil, &RevokedError{SessionID: sess.id}
	}
	u, ok := s.users[sess.userID]
	if !ok || !now.Before(sess.refreshExpiresAt) {
		s.revokeLocked(sess)
		return Tokens{}, nil, &RevokedError{SessionID: sess.id}
	}
	delete(s.byAccess, sess.accessHash)
	sess.usedRefresh[h] = true
	t := newTokens(sess.id, now)
	sess.accessHash, sess.accessExpiresAt = hash(t.AccessToken), t.AccessExpiresAt
	sess.refreshHash, sess.refreshExpiresAt = hash(t.RefreshToken), t.RefreshExpiresAt
	s.byAccess[sess.accessHash] = sess.id
	s.byRefresh[sess.refreshHash] = sess.id
	return t, copyUser(u), nil
}

func (s *Memory) Revoke(_ context.Context, sessionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[sessionID]; ok {
		s.revokeLocked(sess)
	}
	return nil
}

func (s *Memory) revokeLocked(sess *memSession) {
	delete(s.byAccess, sess.accessHash)
	delete(s.byRefresh, sess.refreshHash)
	for h := range sess.usedRefresh {
		delete(s.byRefresh, h)
	}
	delete(s.sessions, sess.id)
}

func (s *Memory) User(_ context.Context, id string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNoUser
	}
	return copyUser(u), nil
}

func (s *Memory) DisplayNames(_ context.Context, ids []string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(ids))
	for _, id := range ids {
		if u, ok := s.users[id]; ok {
			out[id] = u.DisplayName
		}
	}
	return out, nil
}

func (s *Memory) UpdateUser(_ context.Context, id string, displayName, avatarColor *string) (*User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil, ErrNoUser
	}
	if displayName != nil {
		u.DisplayName = *displayName
	}
	if avatarColor != nil {
		u.AvatarColor = *avatarColor
	}
	return copyUser(u), nil
}

func (s *Memory) DeleteUser(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.users[id]
	if !ok {
		return nil
	}
	delete(s.byEmail, u.Email)
	delete(s.users, id)
	if u.Email != "" {
		for h, l := range s.links {
			if l.email == u.Email {
				delete(s.links, h)
			}
		}
		delete(s.codeFailures, u.Email)
	}
	for key, uid := range s.idents {
		if uid == id {
			delete(s.idents, key)
			delete(s.appleTokens, key)
		}
	}
	for _, sess := range s.sessions {
		if sess.userID == id {
			s.revokeLocked(sess)
		}
	}
	for h, l := range s.signInLinks {
		if l.UserID == id || l.IssuerID == id {
			delete(s.signInLinks, h)
		}
	}
	return nil
}

func copyUser(u *User) *User {
	c := *u
	c.Providers = append([]string(nil), u.Providers...)
	return &c
}

func (s *Memory) Prune(context.Context) (int, error) {
	now := s.now()
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for h, l := range s.links {
		if l.used || !now.Before(l.expiresAt) {
			delete(s.links, h)
			n++
		}
	}
	for h, l := range s.signInLinks {
		if !now.Before(l.expiresAt) {
			delete(s.signInLinks, h)
			n++
		}
	}
	for _, sess := range s.sessions {
		if !now.Before(sess.refreshExpiresAt) {
			s.revokeLocked(sess)
			n++
		}
	}
	for email, times := range s.codeFailures {
		keep := times[:0]
		for _, at := range times {
			if at.After(now.Add(-CodeFailureWindow)) {
				keep = append(keep, at)
			} else {
				n++
			}
		}
		if len(keep) == 0 {
			delete(s.codeFailures, email)
		} else {
			s.codeFailures[email] = keep
		}
	}
	return n, nil
}
