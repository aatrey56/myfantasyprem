package httpapi

import (
	"context"
	"net/url"
	"strings"

	"github.com/aatrey56/myfantasyprem/server/internal/ids"
	"github.com/aatrey56/myfantasyprem/server/internal/league"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications"
	"github.com/aatrey56/myfantasyprem/server/internal/notifications/webpush"
)

func (s *Server) ListNotifications(ctx context.Context, r ListNotificationsRequestObject) (ListNotificationsResponseObject, error) {
	limit := 50
	if r.Params.Limit != nil {
		if *r.Params.Limit < 1 || *r.Params.Limit > 100 {
			return ListNotifications400ApplicationProblemPlusJSONResponse{badRequest(ctx, "limit must be between 1 and 100.")}, nil
		}
		limit = *r.Params.Limit
	}
	unread := r.Params.Unread != nil && *r.Params.Unread
	items, count, err := s.notifs.List(ctx, userFrom(ctx), limit, unread)
	if err != nil {
		return nil, err
	}
	out := make([]Notification, 0, len(items))
	for _, n := range items {
		x := Notification{Id: n.ID, Type: string(n.Type), Title: n.Title, Body: n.Body, CreatedAt: n.CreatedAt, ReadAt: n.ReadAt}
		if n.LeagueID != "" {
			x.LeagueId = ptr(n.LeagueID)
		}
		if n.URL != "" {
			x.Url = ptr(n.URL)
		}
		out = append(out, x)
	}
	return ListNotifications200JSONResponse{
		Data: NotificationInbox{Items: out, UnreadCount: count}, Meta: metaAt(s.now()),
	}, nil
}

func (s *Server) MarkNotificationsRead(ctx context.Context, r MarkNotificationsReadRequestObject) (MarkNotificationsReadResponseObject, error) {
	if r.Body == nil {
		return MarkNotificationsRead400ApplicationProblemPlusJSONResponse{badRequest(ctx, "A JSON body is required.")}, nil
	}
	var list []string
	if r.Body.Ids != nil {
		if len(*r.Body.Ids) > 100 {
			return MarkNotificationsRead400ApplicationProblemPlusJSONResponse{badRequest(ctx, "At most 100 ids.")}, nil
		}
		for _, id := range *r.Body.Ids {
			if d := badID("ids", "ntf", id); d != "" {
				return MarkNotificationsRead400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
			}
			list = append(list, id)
		}
	}
	all := r.Body.All != nil && *r.Body.All
	if err := s.notifs.MarkRead(ctx, userFrom(ctx), list, all); err != nil {
		return nil, err
	}
	return MarkNotificationsRead204Response{}, nil
}

// notificationPrefs renders the caller's settings in each of their leagues.
func (s *Server) notificationPrefs(ctx context.Context) (NotificationPreferences, error) {
	out := NotificationPreferences{Types: []NotificationTypeInfo{}, Leagues: []LeagueNotificationPreferences{}}
	for _, i := range notifications.Catalog {
		out.Types = append(out.Types, NotificationTypeInfo{Type: string(i.Type), Group: string(i.Group), Label: i.Label})
	}
	ls, err := s.leagues.ForUser(ctx, userFrom(ctx))
	if err != nil {
		return out, err
	}
	for _, l := range ls {
		off, err := s.notifs.Disabled(ctx, userFrom(ctx), l.ID)
		if err != nil {
			return out, err
		}
		lp := LeagueNotificationPreferences{LeagueId: l.ID, LeagueName: l.Name, Settings: []NotificationSetting{}}
		for _, i := range notifications.Catalog {
			lp.Settings = append(lp.Settings, NotificationSetting{Type: string(i.Type), Enabled: !off[i.Type]})
		}
		out.Leagues = append(out.Leagues, lp)
	}
	return out, nil
}

func (s *Server) GetNotificationPreferences(ctx context.Context, _ GetNotificationPreferencesRequestObject) (GetNotificationPreferencesResponseObject, error) {
	p, err := s.notificationPrefs(ctx)
	if err != nil {
		return nil, err
	}
	return GetNotificationPreferences200JSONResponse{Data: p, Meta: metaAt(s.now())}, nil
}

func (s *Server) UpdateNotificationPreferences(ctx context.Context, r UpdateNotificationPreferencesRequestObject) (UpdateNotificationPreferencesResponseObject, error) {
	b := r.Body
	if b == nil {
		return UpdateNotificationPreferences400ApplicationProblemPlusJSONResponse{badRequest(ctx, "A JSON body is required.")}, nil
	}
	settings := map[notifications.Type]bool{}
	if b.Settings != nil {
		if len(*b.Settings) > 100 {
			return UpdateNotificationPreferences400ApplicationProblemPlusJSONResponse{badRequest(ctx, "At most 100 settings.")}, nil
		}
		for _, x := range *b.Settings {
			t := notifications.Type(x.Type)
			if !notifications.Known(t) {
				return fail[UpdateNotificationPreferencesdefaultApplicationProblemPlusJSONResponse](newProblem(422,
					"unknown-notification-type", "Unknown notification type",
					"No notification type is called "+x.Type+".", RequestIDFrom(ctx))), nil
			}
			settings[t] = x.Enabled
		}
	}
	var leagueIDs []string
	if b.LeagueId != nil {
		if d := badID("leagueId", "lg", *b.LeagueId); d != "" {
			return UpdateNotificationPreferences400ApplicationProblemPlusJSONResponse{badRequest(ctx, d)}, nil
		}
		if _, err := s.leagues.Get(ctx, userFrom(ctx), *b.LeagueId); err != nil {
			return failLeague[UpdateNotificationPreferencesdefaultApplicationProblemPlusJSONResponse](ctx, err)
		}
		leagueIDs = []string{*b.LeagueId}
	} else {
		ls, err := s.leagues.ForUser(ctx, userFrom(ctx))
		if err != nil {
			return nil, err
		}
		for _, l := range ls {
			leagueIDs = append(leagueIDs, l.ID)
		}
	}
	for _, id := range leagueIDs {
		if err := s.notifs.SetPreferences(ctx, userFrom(ctx), id, b.All, settings); err != nil {
			return nil, err
		}
	}
	p, err := s.notificationPrefs(ctx)
	if err != nil {
		return nil, err
	}
	return UpdateNotificationPreferences200JSONResponse{Data: p, Meta: metaAt(s.now())}, nil
}

func (s *Server) CreatePushSubscription(ctx context.Context, r CreatePushSubscriptionRequestObject) (CreatePushSubscriptionResponseObject, error) {
	b := r.Body
	if b == nil {
		return CreatePushSubscription400ApplicationProblemPlusJSONResponse{badRequest(ctx, "A JSON body is required.")}, nil
	}
	invalid := func(detail string) (CreatePushSubscriptionResponseObject, error) {
		return CreatePushSubscription422ApplicationProblemPlusJSONResponse{UnprocessableApplicationProblemPlusJSONResponse(
			newProblem(422, "invalid-subscription", "Invalid push subscription", detail, RequestIDFrom(ctx)))}, nil
	}
	sub := notifications.Subscription{UserID: userFrom(ctx), Kind: b.Kind}
	switch b.Kind {
	case notifications.KindWebPush:
		if b.Endpoint == nil || b.Keys == nil {
			return invalid("A webpush subscription needs endpoint and keys.")
		}
		u, err := url.Parse(*b.Endpoint)
		if err != nil || u.Scheme != "https" || u.Host == "" || len(*b.Endpoint) > 2048 {
			return invalid("endpoint must be the push service's https URL.")
		}
		if len(b.Keys.P256dh) > 200 || len(b.Keys.Auth) > 100 {
			return invalid("keys are too long.")
		}
		sub.Endpoint, sub.P256dh, sub.Auth = *b.Endpoint, b.Keys.P256dh, b.Keys.Auth
		// Keys that cannot encrypt would only fail every push.
		if _, err := webpush.Encrypt([]byte("{}"), sub.P256dh, sub.Auth); err != nil {
			return invalid("keys are not a valid P-256 key and 16-byte auth secret.")
		}
	case notifications.KindExpo:
		if b.ExpoToken == nil || *b.ExpoToken == "" || len(*b.ExpoToken) > 200 ||
			!strings.HasPrefix(*b.ExpoToken, "ExponentPushToken[") && !strings.HasPrefix(*b.ExpoToken, "ExpoPushToken[") {
			return invalid("An expo subscription needs expoToken (ExponentPushToken[...]).")
		}
		sub.Endpoint = *b.ExpoToken
	default:
		return invalid("kind must be webpush or expo.")
	}
	got, err := s.notifs.Subscribe(ctx, sub)
	if err != nil {
		return nil, err
	}
	return CreatePushSubscription201JSONResponse{
		Data: PushSubscription{Id: got.ID, Kind: got.Kind, CreatedAt: got.CreatedAt}, Meta: metaAt(s.now()),
	}, nil
}

func (s *Server) DeletePushSubscription(ctx context.Context, r DeletePushSubscriptionRequestObject) (DeletePushSubscriptionResponseObject, error) {
	if !ids.Valid("psub", r.SubscriptionId) {
		return DeletePushSubscription400ApplicationProblemPlusJSONResponse{badRequest(ctx, "subscriptionId must be a psub_ id.")}, nil
	}
	if err := s.notifs.Unsubscribe(ctx, userFrom(ctx), r.SubscriptionId); err != nil {
		return nil, err
	}
	return DeletePushSubscription204Response{}, nil
}

// notifyChanges turns committed league changes into notifications.
func (s *Server) notifyChanges(ctx context.Context, changes ...league.Change) {
	c := &notifications.Changes{Leagues: s.leagues.Repo(), Stats: s.stats, Notifier: notifications.NewNotifier(s.notifs), Log: s.log}
	c.Notify(ctx, changes...)
}

// nullString is s, or nil when empty.
func nullString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
