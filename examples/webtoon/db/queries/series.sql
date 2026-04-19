-- Series queries for sqlc — Generated: 2026-04-19

-- name: GetSeriesByID :one
SELECT id, title, description, genre, author, cover_url, status, created_at, updated_at
FROM serieses
WHERE id = $1
LIMIT 1;

-- name: ListSeriess :many
SELECT id, title, description, genre, author, cover_url, status, created_at, updated_at
FROM serieses
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountSeriess :one
SELECT COUNT(*) FROM serieses;
