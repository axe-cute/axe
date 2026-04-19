-- Episode queries for sqlc — Generated: 2026-04-19

-- name: GetEpisodeByID :one
SELECT id, title, episode_number, thumbnail_url, published, created_at, updated_at
FROM episodes
WHERE id = $1
LIMIT 1;

-- name: ListEpisodes :many
SELECT id, title, episode_number, thumbnail_url, published, created_at, updated_at
FROM episodes
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountEpisodes :one
SELECT COUNT(*) FROM episodes;
