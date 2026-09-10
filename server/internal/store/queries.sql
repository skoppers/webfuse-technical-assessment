-- Every SQL statement the server runs. Compiled into gen/ by sqlc; run
-- `make sqlc` after editing. Rows are exposed to other packages through the
-- Store methods in store.go, never through gen directly.

-- Sessions ------------------------------------------------------------------

-- name: InsertSessionIfAbsent :one
-- Inserts a live session and returns it. Returns no row when the session
-- already exists, whatever its status.
INSERT INTO sessions (session_id, space_id, status, started_at, source)
VALUES (@session_id, @space_id, 'live', @started_at, @source)
ON CONFLICT (session_id) DO NOTHING
RETURNING *;

-- name: GetSession :one
SELECT * FROM sessions
WHERE session_id = @session_id;

-- name: EnrichSessionFromWebhook :one
-- Applies the session.started webhook. Only a live session with a lower
-- last_webhook_seq is touched, so a stale or replayed webhook is ignored and
-- an ended session is never revived. Returns no row when nothing changed.
UPDATE sessions
SET space_id          = @space_id,
    started_at        = @started_at,
    participant_count = @participant_count,
    metadata          = @metadata,
    source            = 'webhook',
    last_webhook_seq  = @seq
WHERE session_id = @session_id
  AND status = 'live'
  AND last_webhook_seq < @seq
RETURNING *;

-- name: EndSession :one
-- Ends a live session. seq = 0 skips the ordering guard (the reaper has no
-- webhook sequence); otherwise a stale seq leaves the row alone. Returns no
-- row when the session is already ended or the seq is stale.
UPDATE sessions
SET status           = 'ended',
    ended_at         = @ended_at,
    last_webhook_seq = GREATEST(last_webhook_seq, @seq::bigint)
WHERE session_id = @session_id
  AND status = 'live'
  AND (@seq::bigint = 0 OR last_webhook_seq < @seq::bigint)
RETURNING *;

-- name: SetParticipantCount :one
-- Applies a participant_joined/left webhook with the same guard as
-- EnrichSessionFromWebhook. Returns no row when nothing changed.
UPDATE sessions
SET participant_count = @participant_count,
    last_webhook_seq  = @seq
WHERE session_id = @session_id
  AND status = 'live'
  AND last_webhook_seq < @seq
RETURNING *;

-- name: ListSessions :many
-- Live sessions first, newest first, each with how many of its stored
-- events are key events and the latest of them by ts then seq. The last_key_*
-- columns are zero values (id 0) when the session has no key event; sqlc
-- cannot see the LEFT JOIN LATERAL as nullable, so they are coalesced.
-- key_types is the comma-joined key event type list, passed in so the event
-- contract stays defined only in Go.
SELECT s.*,
       k.key_event_count,
       COALESCE(lk.id, 0)                  AS last_key_id,
       COALESCE(lk.seq, 0)                 AS last_key_seq,
       COALESCE(lk.type, '')               AS last_key_type,
       COALESCE(lk.ts, s.started_at)       AS last_key_ts,
       COALESCE(lk.received_at, s.started_at) AS last_key_received_at,
       COALESCE(lk.data, '{}'::jsonb)      AS last_key_data
FROM sessions s
CROSS JOIN LATERAL (
  SELECT count(*)::int AS key_event_count
  FROM events e
  WHERE e.session_id = s.session_id AND e.type = ANY(string_to_array(@key_types::text, ','))
) k
LEFT JOIN LATERAL (
  SELECT e.id, e.seq, e.type, e.ts, e.received_at, e.data
  FROM events e
  WHERE e.session_id = s.session_id AND e.type = ANY(string_to_array(@key_types::text, ','))
  ORDER BY e.ts DESC, e.seq DESC
  LIMIT 1
) lk ON true
ORDER BY (s.status = 'live') DESC, s.started_at DESC, s.session_id
LIMIT @row_limit;

-- name: ListIdleLiveSessions :many
-- Live sessions whose most recent event (or started_at when there are none)
-- is older than idle_before.
SELECT s.* FROM sessions s
WHERE s.status = 'live'
  AND COALESCE(
        (SELECT max(e.received_at) FROM events e WHERE e.session_id = s.session_id),
        s.started_at
      ) < @idle_before
ORDER BY s.started_at, s.session_id;

-- Events --------------------------------------------------------------------

-- name: InsertEvent :one
-- Returns no row when (session_id, seq) was already stored.
INSERT INTO events (session_id, seq, type, ts, data)
VALUES (@session_id, @seq, @type, @ts, @data)
ON CONFLICT (session_id, seq) DO NOTHING
RETURNING id;

-- name: ListEventsBySession :many
SELECT * FROM events
WHERE session_id = @session_id
ORDER BY ts, seq;
