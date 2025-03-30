-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, email, hashed_password, is_chirpy_red)
VALUES (
    gen_random_uuid(),
    NOW(),
    NOW(),
    $1,
    $2,
    $3
)
RETURNING *;

-- name: GetUserByEmail :one
SELECT *
FROM users
WHERE email = $1;

-- name: GetUserByID :one
SELECT id, created_at, updated_at, email, hashed_password, is_chirpy_red
FROM users
WHERE id = $1;

-- name: UpdateUserByID :exec
UPDATE users
SET email = $1, 
    hashed_password = $2, 
    updated_at = NOW()
WHERE id = $3;

-- name: UpgradeChirpyRedByID :exec
UPDATE users
SET is_chirpy_red = true
WHERE id = $1;
