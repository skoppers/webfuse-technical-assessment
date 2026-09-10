-- +goose Up
CREATE TABLE sessions (
  session_id        text PRIMARY KEY,
  space_id          text NOT NULL DEFAULT '',
  status            text NOT NULL CHECK (status IN ('live','ended')),
  started_at        timestamptz NOT NULL,
  ended_at          timestamptz,
  source            text NOT NULL CHECK (source IN ('webhook','event')),
  participant_count int  NOT NULL DEFAULT 0,
  metadata          jsonb NOT NULL DEFAULT '{}'::jsonb,
  last_webhook_seq  bigint NOT NULL DEFAULT 0,   -- highest webhook sequence_id applied (dedup/order)
  created_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE events (
  id          bigserial PRIMARY KEY,
  session_id  text NOT NULL REFERENCES sessions(session_id),
  seq         int  NOT NULL,
  type        text NOT NULL,
  ts          timestamptz NOT NULL,
  received_at timestamptz NOT NULL DEFAULT now(),
  data        jsonb NOT NULL DEFAULT '{}'::jsonb,
  UNIQUE (session_id, seq)
);

CREATE INDEX events_session_ts ON events (session_id, ts);
CREATE INDEX sessions_status ON sessions (status);

-- +goose Down
DROP TABLE events;
DROP TABLE sessions;
