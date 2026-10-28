// Package realtime is the WebSocket hub's core, without the transport
// (ADR 0011, docs/realtime.md): per-league event sequences and the
// five-minute replay buffer (Store: Memory, Postgres), single-use
// connection tickets, and the Hub that keeps each connection's league
// subscriptions and fans events out to them in seq order, replaying missed
// ones on subscribe. Several API instances share events through Postgres
// LISTEN/NOTIFY (Listen). The HTTP upgrade and the socket loop live in
// httpapi; the events themselves are rendered there too, so this package
// only moves opaque JSON. It never imports net/http.
//
// Delivery is at most once per socket: a slow client (full send buffer)
// is disconnected, and a client that sees a gap in seq refetches the REST
// resources. REST stays the source of truth.
package realtime
