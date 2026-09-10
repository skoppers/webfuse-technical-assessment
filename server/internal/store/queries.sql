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
-- Live sessions first, newest first.
SELECT * FROM sessions
ORDER BY (status = 'live') DESC, started_at DESC, session_id
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

-- name: CountEventsBySession :one
SELECT count(*) FROM events
WHERE session_id = @session_id;
