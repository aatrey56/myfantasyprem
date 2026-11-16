// Package pollertest helps tests drive a poller.
package pollertest

import (
	"context"
	"testing"

	"github.com/aatrey56/myfantasyprem/server/internal/poller"
)

// maxTicks bounds TickUntilIdle: a season has 38 gameweeks.
const maxTicks = 50

// TickUntilIdle polls until the catch-up is done: no earlier gameweek is
// queued, or every queued one failed since the last that was fetched
// (they are retried by later polls). It returns the polls made.
func TickUntilIdle(t testing.TB, ctx context.Context, p *poller.Poller) int {
	t.Helper()
	p.Tick(ctx) // the first poll plans the catch-up
	stuck := 0  // polls in a row that fetched no queued gameweek
	for n := 1; n < maxTicks; n++ {
		before := p.Pending()
		if before == 0 || stuck >= before {
			return n
		}
		p.Tick(ctx)
		if p.Pending() < before {
			stuck = 0
		} else {
			stuck++
		}
	}
	t.Fatalf("pollertest: still catching up after %d polls (%d gameweeks queued)", maxTicks, p.Pending())
	return maxTicks
}
