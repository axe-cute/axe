-- Post queries for sqlc — Generated: 2026-04-15

-- name: GetPostByID :one
SELECT id, title, body, published, views, created_at, updated_at
FROM posts
WHERE id = $1
LIMIT 1;

-- name: ListPosts :many
SELECT id, title, body, published, views, created_at, updated_at
FROM posts
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountPosts :one
SELECT COUNT(*) FROM posts;
