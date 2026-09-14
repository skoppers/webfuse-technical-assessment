-- +goose Up
-- Events are deduped per client, not per session: every background boot of every
-- participant restarts seq at 1, so two participants (or one restarted background)
-- collided on (session_id, seq). Existing rows keep client_id '' and stay valid.
ALTER TABLE events ADD COLUMN client_id text NOT NULL DEFAULT '';
ALTER TABLE events DROP CONSTRAINT events_session_id_seq_key;
ALTER TABLE events ADD CONSTRAINT events_session_id_client_id_seq_key UNIQUE (session_id, client_id, seq);

-- +goose Down
ALTER TABLE events DROP CONSTRAINT events_session_id_client_id_seq_key;
-- Fails if any two clients stored the same seq for one session; those rows must be resolved first.
ALTER TABLE events ADD CONSTRAINT events_session_id_seq_key UNIQUE (session_id, seq);
ALTER TABLE events DROP COLUMN client_id;
