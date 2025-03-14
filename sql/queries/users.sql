-- name: CreateUser :one
INSERT INTO users (id, created_at, updated_at, name)
VALUES (
    $1,
    $2,
    $3,
    $4
)
RETURNING *;

-- name: GetUser :one
SELECT * FROM users
WHERE name = $1;

-- name: ResetUsers :exec
DELETE FROM users;

-- name: GetUsers :many
SELECT id, created_at, updated_at, name FROM users;

-- name: CreateFeed :one
INSERT INTO feeds (name, url, user_id)
VALUES (
    $1, 
    $2, 
    $3
)
RETURNING *;

-- name: GetFeedsWithUsers :many
SELECT 
    feeds.id, 
    feeds.name, 
    feeds.url, 
    users.name AS user_name
FROM feeds
JOIN users ON feeds.user_id = users.id;

-- name: CreateFeedFollow :one
WITH inserted_feed_follow AS (
    INSERT INTO feed_follows (user_id, feed_id, created_at, updated_at)
    VALUES ($1, $2, now(), now())
    RETURNING *
)
SELECT
    inserted_feed_follow.*,
    feeds.name AS feed_name,
    users.name AS user_name
FROM inserted_feed_follow
JOIN feeds ON feeds.id = inserted_feed_follow.feed_id
JOIN users ON users.id = inserted_feed_follow.user_id;


-- name: GetFeedByUrl :one
SELECT id, name, url, user_id FROM feeds WHERE url = $1;

-- name: GetFeedFollowsForUser :many
SELECT 
    ff.id AS follow_id,
    ff.created_at,
    ff.updated_at,
    u.name AS user_name,
    f.name AS feed_name,
    f.url AS feed_url
FROM feed_follows ff
JOIN users u ON ff.user_id = u.id
JOIN feeds f ON ff.feed_id = f.id
WHERE ff.user_id = $1;

-- name: DeleteFeedFollow :exec
DELETE FROM feed_follows
WHERE user_id = $1 AND feed_id = $2;

-- name: MarkFeedFetched :exec
UPDATE feeds
SET last_fetched_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $1;

-- name: GetNextFeedToFetch :one
SELECT id, name, url, user_id, last_fetched_at, created_at, updated_at
FROM feeds
ORDER BY last_fetched_at NULLS FIRST, updated_at ASC
LIMIT 1;

-- name: CreatePost :exec
INSERT INTO posts (title, url, description, published_at, feed_id)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (url) DO NOTHING;

-- name: GetPostsForUser :many
SELECT p.id, p.title, p.url, p.description, p.published_at, p.feed_id, p.created_at, p.updated_at
FROM posts p
JOIN feeds f ON p.feed_id = f.id
WHERE f.user_id = $1
ORDER BY p.published_at DESC
LIMIT $2;

-- name: GetPosts :many
SELECT id, title, url, description, published_at, feed_id, created_at, updated_at
FROM posts
ORDER BY published_at DESC
LIMIT $1;
