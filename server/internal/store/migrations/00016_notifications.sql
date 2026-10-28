-- Notifications (internal/notifications).
--
-- notification_prefs: switches a user turned away from the default. Every
-- type is on unless a row says enabled = false, so types added later start
-- on. "All on" deletes the user's rows in the league.
--
-- notifications: the inbox (the FEED tab). dedupe_key is unique per user so
-- a re-poll or a retried job never stores, or pushes, the same news twice.
--
-- push_subscriptions: devices registered for push. endpoint is the Web Push
-- URL or the Expo token; one device belongs to one user at a time.
--
-- push_outbox: one queued push per notification and subscription, retried
-- with backoff and dropped with its subscription.

-- +goose Up
CREATE TABLE notification_prefs (
    user_id   text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    league_id text NOT NULL REFERENCES leagues (id) ON DELETE CASCADE,
    type      text NOT NULL,
    enabled   boolean NOT NULL,
    PRIMARY KEY (user_id, league_id, type)
);

CREATE TABLE notifications (
    id         text PRIMARY KEY CHECK (id ~ '^ntf_[0-9a-hjkmnp-tv-z]{26}$'),
    user_id    text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    league_id  text REFERENCES leagues (id) ON DELETE CASCADE,
    type       text NOT NULL,
    title      text NOT NULL,
    body       text NOT NULL,
    url        text NOT NULL DEFAULT '',
    dedupe_key text NOT NULL,
    created_at timestamptz NOT NULL,
    read_at    timestamptz,
    UNIQUE (user_id, dedupe_key)
);
CREATE INDEX notifications_user_idx ON notifications (user_id, created_at DESC, id DESC);

CREATE TABLE push_subscriptions (
    id         text PRIMARY KEY CHECK (id ~ '^psub_[0-9a-hjkmnp-tv-z]{26}$'),
    user_id    text NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text NOT NULL,
    endpoint   text NOT NULL,
    p256dh     text NOT NULL DEFAULT '',
    auth       text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL,
    UNIQUE (kind, endpoint)
);
CREATE INDEX push_subscriptions_user_idx ON push_subscriptions (user_id);

CREATE TABLE push_outbox (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    notification_id text NOT NULL REFERENCES notifications (id) ON DELETE CASCADE,
    subscription_id text NOT NULL REFERENCES push_subscriptions (id) ON DELETE CASCADE,
    attempts        integer NOT NULL DEFAULT 0,
    next_attempt_at timestamptz NOT NULL
);
CREATE INDEX push_outbox_due_idx ON push_outbox (next_attempt_at);

-- +goose Down
DROP TABLE push_outbox;
DROP TABLE push_subscriptions;
DROP TABLE notifications;
DROP TABLE notification_prefs;
