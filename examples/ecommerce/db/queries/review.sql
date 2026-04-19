-- Review queries for sqlc — Generated: 2026-04-19

-- name: GetReviewByID :one
SELECT id, body, rating, created_at, updated_at
FROM reviews
WHERE id = $1
LIMIT 1;

-- name: ListReviews :many
SELECT id, body, rating, created_at, updated_at
FROM reviews
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountReviews :one
SELECT COUNT(*) FROM reviews;
