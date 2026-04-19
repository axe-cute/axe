-- Bookmark queries for sqlc — Generated: 2026-04-19

-- name: GetBookmarkByID :one
SELECT id, series_id, created_at, updated_at
FROM bookmarks
WHERE id = $1
LIMIT 1;

-- name: ListBookmarks :many
SELECT id, series_id, created_at, updated_at
FROM bookmarks
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountBookmarks :one
SELECT COUNT(*) FROM bookmarks;
