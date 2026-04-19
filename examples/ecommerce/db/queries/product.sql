-- Product queries for sqlc — Generated: 2026-04-19

-- name: GetProductByID :one
SELECT id, name, description, price, stock, image_url, created_at, updated_at
FROM products
WHERE id = $1
LIMIT 1;

-- name: ListProducts :many
SELECT id, name, description, price, stock, image_url, created_at, updated_at
FROM products
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountProducts :one
SELECT COUNT(*) FROM products;
