-- User queries for sqlc
-- Used by internal/repository/user_query.go (read-heavy operations)
-- Write operations use Ent (see internal/repository/user_repo.go)

-- name: GetUserByID :one
SELECT id, email, name, role, active, created_at, updated_at
FROM users
WHERE id = $1 AND active = TRUE
LIMIT 1;

-- name: GetUserByEmail :one
SELECT id, email, name, password_hash, role, active, created_at, updated_at
FROM users
WHERE email = $1 AND active = TRUE
LIMIT 1;

-- name: ListUsers :many
SELECT id, email, name, role, active, created_at, updated_at
FROM users
WHERE active = TRUE
ORDER BY created_at DESC
LIMIT $1 OFFSET $2;

-- name: CountUsers :one
SELECT COUNT(*) FROM users WHERE active = TRUE;

-- name: SearchUsersByName :many
SELECT id, email, name, role, active, created_at, updated_at
FROM users
WHERE active = TRUE
  AND name ILIKE '%' || $1 || '%'
ORDER BY created_at DESC
LIMIT $2 OFFSET $3;
