-- Order queries for sqlc — Generated: 2026-04-19

-- name: GetOrderByID :one
SELECT id, total, status, created_at, updated_at
FROM orders
WHERE id = $1
LIMIT 1;

-- name: ListOrders :many
SELECT id, total, status, created_at, updated_at
FROM orders
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountOrders :one
SELECT COUNT(*) FROM orders;
