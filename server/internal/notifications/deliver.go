package notifications

import (
	"context"
	"errors"
)

// ErrGone is a subscription the push service no longer knows (404, 410):
// drop it.
var ErrGone = errors.New("notifications: subscription gone")

// ErrDisabled is a deliverer that is switched off; the delivery is dropped.
var ErrDisabled = errors.New("notifications: deliverer disabled")

// Payload is what a push carries; the service worker shows it.
type Payload struct {
	ID    string `json:"id"`
	Type  Type   `json:"type"`
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
}

// PayloadOf returns n's push payload.
func PayloadOf(n Notification) Payload {
	return Payload{ID: n.ID, Type: n.Type, Title: n.Title, Body: n.Body, URL: n.URL}
}

// Deliverer pushes to one kind of subscription (webpush.WebPush for
// browsers, Expo for the native app).
type Deliverer interface {
	Deliver(ctx context.Context, sub Subscription, p Payload) error
}

// Expo is the native push deliverer (Expo push service). It stays off
// until the iOS app ships with an Apple developer account: Deliver then
// returns ErrDisabled, and the dispatcher drops the delivery.
type Expo struct {
	Enabled bool
}

func (e Expo) Deliver(context.Context, Subscription, Payload) error {
	if !e.Enabled {
		return ErrDisabled
	}
	return errors.New("expo: native push is not implemented yet")
}
