// Package history keeps each player's totals for finished Premier League
// seasons (FPL element-summary history_past), keyed by FPL code, and
// fetches them politely: every player once per season, and a new player
// on first sight. The player card's "Previous seasons" reads it.
package history

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"time"

	"github.com/aatrey56/myfantasyprem/server/internal/provider"
)

// Season is one player-season. A nil stat was not tracked that season.
type Season = provider.PlayerSeason

// Store persists player-seasons. Memory and Postgres implement it.
type Store interface {
	// Save upserts code's seasons and records the fetch at `at`, in one
	// transaction. Saving the same seasons again changes nothing else.
	Save(ctx context.Context, code string, seasons []Season, at time.Time) error
	// Seasons returns code's seasons, newest first; none is an empty slice.
	Seasons(ctx context.Context, code string) ([]Season, error)
	// Fetched returns when each code's history was last saved.
	Fetched(ctx context.Context) (map[string]time.Time, error)
}

// Source is the upstream: fpl.Client.
type Source interface {
	SeasonHistory(ctx context.Context, playerCode string) ([]provider.PlayerSeason, error)
}

// IndexRefresher is a Source whose id lookups come from a cached index
// that can go stale (fpl.Client: element ids are reassigned at FPL's
// season rollover). Run refreshes it before each batch.
type IndexRefresher interface {
	RefreshIndex(ctx context.Context) error
}

// SeasonGate is a Source that knows whether the new season's game is live
// upstream (fpl.Client). Before that, history_past still lacks the season
// just finished, so Run fetches nothing.
type SeasonGate interface {
	SeasonLive(ctx context.Context, since time.Time) (live bool, firstDeadline time.Time, err error)
}

// Fetcher fetches histories for the current players. Requests go one at a
// time, so the source's own pacing (fpl MinInterval) is the request rate.
type Fetcher struct {
	Source Source
	Store  Store
	// Codes lists the current players' FPL codes.
	Codes func(ctx context.Context) ([]string, error)
	Now   func() time.Time
	Log   *slog.Logger
}

// Result counts one run.
type Result struct {
	Fetched, Skipped, Failed int
	// Missing players have no history upstream (provider.ErrNotFound).
	// With an IndexRefresher source they are recorded as fetched (tried
	// again next season); otherwise they are tried again on the next run.
	Missing int
	// Held players wait for FPL's relaunch (a SeasonGate source not live
	// yet); FirstDeadline is the first deadline of the game FPL serves.
	Held          int
	FirstDeadline time.Time
}

// SeasonStart is 1 July (UTC) of the season that `now` falls in: the
// previous season is finished by then. FPL rolls history_past only when it
// relaunches (mid/late July); a SeasonGate source holds the batch till then.
func SeasonStart(now time.Time) time.Time {
	now = now.UTC()
	y := now.Year()
	if now.Month() < time.July {
		y--
	}
	return time.Date(y, time.July, 1, 0, 0, 0, 0, time.UTC)
}

// Run fetches every code not saved since `since`: with SeasonStart, each
// player once per season plus any player seen for the first time. A zero
// `since` fetches only codes never fetched. A failure for one player is
// logged and counted, and the run goes on; the context ending stops it.
func (f *Fetcher) Run(ctx context.Context, since time.Time) (Result, error) {
	var res Result
	codes, err := f.Codes(ctx)
	if err != nil {
		return res, err
	}
	done, err := f.Store.Fetched(ctx)
	if err != nil {
		return res, err
	}
	slices.Sort(codes)
	log := f.Log
	if log == nil {
		log = slog.Default()
	}
	refreshed, gated := false, false
	for _, code := range codes {
		if at, ok := done[code]; ok && !at.Before(since) {
			res.Skipped++
			continue
		}
		if r, ok := f.Source.(IndexRefresher); ok && !refreshed {
			if err := r.RefreshIndex(ctx); err != nil {
				return res, err
			}
			refreshed = true
		}
		if g, ok := f.Source.(SeasonGate); ok && !gated {
			// Asked about the season under way, not `since`: a refetch of
			// everyone passes a moment ahead of now.
			season := SeasonStart(f.Now())
			if since.Before(season) {
				season = since
			}
			live, first, err := g.SeasonLive(ctx, season)
			if err != nil {
				return res, err
			}
			if !live {
				log.InfoContext(ctx, "player history: new season not live upstream yet", "first_deadline", first)
				held := Result{FirstDeadline: first}
				for _, c := range codes {
					if at, ok := done[c]; ok && !at.Before(since) {
						held.Skipped++
					} else {
						held.Held++
					}
				}
				return held, nil
			}
			gated = true
		}
		seasons, err := f.Source.SeasonHistory(ctx, code)
		if err == nil {
			err = f.Store.Save(ctx, code, seasons, f.Now())
		}
		switch {
		case err == nil:
		case ctx.Err() != nil:
			return res, ctx.Err()
		case errors.Is(err, provider.ErrNotFound):
			res.Missing++
			log.DebugContext(ctx, "no player history upstream", "code", code)
			// Looked up in a just-refreshed index: record the fetch, so a
			// player gone from FPL is not looked up again until next season.
			if refreshed {
				if err := f.Store.Save(ctx, code, nil, f.Now()); err != nil {
					log.WarnContext(ctx, "player history: record missing", "code", code, "err", err)
				}
			}
			continue
		default:
			res.Failed++
			log.WarnContext(ctx, "player history fetch failed", "code", code, "err", err)
			continue
		}
		res.Fetched++
	}
	return res, nil
}

// IDSource lists upstream ids (stats.Repo).
type IDSource interface {
	ExternalIDs(ctx context.Context, provider, entity string) (map[string]string, error)
}

// CodesFrom lists the current players' upstream codes for provider.
func CodesFrom(ids IDSource, provider string) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		m, err := ids.ExternalIDs(ctx, provider, "player")
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(m))
		for code := range m {
			out = append(out, code)
		}
		return out, nil
	}
}
