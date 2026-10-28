package assets

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/stats"
)

// Provider is the id map the upstream codes come from: FPL's element and
// team `code` values (stats ingests them as external ids). Players and
// clubs of any other provider (the demo world) have no image.
const Provider = "fpl"

// Catalog is the part of stats.Repo the Resolver reads.
type Catalog interface {
	Clubs(ctx context.Context) ([]stats.Club, error)
	ExternalIDs(ctx context.Context, provider, entity string) (map[string]string, error)
}

// Resolver maps our ids to upstream codes: a player id (plr_) to its FPL
// element code, a club's short code (ARS) to its FPL team code. It keeps
// the maps for TTL and reloads them sooner, at most once a minute, when an
// id is unknown (a new signing). Safe for concurrent use.
type Resolver struct {
	Catalog Catalog
	TTL     time.Duration // default 10m
	Now     func() time.Time

	mu       sync.Mutex
	loadedAt time.Time
	players  map[string]string // plr_ id -> element code
	clubs    map[string]string // short code (upper case) -> team code
}

// retryUnknown is how soon an unknown id may reload the maps.
const retryUnknown = time.Minute

func (r *Resolver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

// lookup returns m[key] from fresh maps.
func (r *Resolver) lookup(ctx context.Context, pick func(*Resolver) map[string]string, key string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ttl := r.TTL
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	if r.players == nil || r.now().Sub(r.loadedAt) >= ttl {
		if err := r.load(ctx); err != nil {
			return "", err
		}
	}
	if code, ok := pick(r)[key]; ok {
		return code, nil
	}
	if r.now().Sub(r.loadedAt) >= retryUnknown {
		if err := r.load(ctx); err != nil {
			return "", err
		}
		if code, ok := pick(r)[key]; ok {
			return code, nil
		}
	}
	return "", ErrNotFound
}

// load rebuilds both maps. Called with r.mu held.
func (r *Resolver) load(ctx context.Context) error {
	playerExt, err := r.Catalog.ExternalIDs(ctx, Provider, "player")
	if err != nil {
		return err
	}
	clubExt, err := r.Catalog.ExternalIDs(ctx, Provider, "club")
	if err != nil {
		return err
	}
	clubs, err := r.Catalog.Clubs(ctx)
	if err != nil {
		return err
	}
	players := make(map[string]string, len(playerExt))
	for code, id := range playerExt {
		players[id] = code
	}
	teamCode := make(map[string]string, len(clubExt))
	for code, id := range clubExt {
		teamCode[id] = code
	}
	byShort := make(map[string]string, len(clubs))
	for _, c := range clubs {
		if code, ok := teamCode[c.ID]; ok {
			byShort[strings.ToUpper(c.Code)] = code
		}
	}
	r.players, r.clubs, r.loadedAt = players, byShort, r.now()
	return nil
}

// PlayerCode returns the FPL element code of a player id.
func (r *Resolver) PlayerCode(ctx context.Context, playerID string) (string, error) {
	return r.lookup(ctx, func(r *Resolver) map[string]string { return r.players }, playerID)
}

// ClubCode returns the FPL team code of a club's short code.
func (r *Resolver) ClubCode(ctx context.Context, short string) (string, error) {
	return r.lookup(ctx, func(r *Resolver) map[string]string { return r.clubs }, strings.ToUpper(short))
}

// Service answers the /v1/assets operations. A nil *Service, or one not
// Enabled, is the switch off: every image is ErrNotFound and nothing is
// resolved or fetched.
type Service struct {
	Enabled  bool
	Resolver *Resolver
	Proxy    *Proxy
}

// On reports whether branded images are served.
func (s *Service) On() bool { return s != nil && s.Enabled && s.Resolver != nil && s.Proxy != nil }

// PlayerPhoto returns a player's photo.
func (s *Service) PlayerPhoto(ctx context.Context, playerID string) (*Image, error) {
	if !s.On() {
		return nil, ErrNotFound
	}
	code, err := s.Resolver.PlayerCode(ctx, playerID)
	if err != nil {
		return nil, err
	}
	return s.Proxy.Get(ctx, PlayerPhoto, code)
}

// ClubCrest returns a club's crest by its short code.
func (s *Service) ClubCrest(ctx context.Context, short string) (*Image, error) {
	return s.club(ctx, ClubCrest, short)
}

// ClubShirt returns a club's shirt, or its goalkeeper shirt.
func (s *Service) ClubShirt(ctx context.Context, short string, goalkeeper bool) (*Image, error) {
	if goalkeeper {
		return s.club(ctx, ClubGKShirt, short)
	}
	return s.club(ctx, ClubShirt, short)
}

func (s *Service) club(ctx context.Context, kind Kind, short string) (*Image, error) {
	if !s.On() {
		return nil, ErrNotFound
	}
	code, err := s.Resolver.ClubCode(ctx, short)
	if err != nil {
		return nil, err
	}
	return s.Proxy.Get(ctx, kind, code)
}
