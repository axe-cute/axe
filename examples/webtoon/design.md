# Design — Webtoon

Architectural decisions and rationale. Focus on the *why* — for the *how*,
read the code; for the *gotchas*, read `skills.md`.

---

## 1. Stack at a glance

| Layer    | Tech                                       |
|----------|--------------------------------------------|
| API      | Go + chi + Ent + raw SQL (pgx)             |
| Auth     | JWT (HS256) — access 15m / refresh 7d      |
| DB       | PostgreSQL 16                              |
| Cache/MQ | Redis 7 + Asynq                            |
| Storage  | S3-compatible (MinIO local; B2/R2 prod)    |
| Frontend | Next.js 15 App Router (standalone build)   |
| Style    | TailwindCSS, lucide-react icons            |

All services run in Docker via `docker-compose.yml`. The API and web binaries
each have their own multi-stage Dockerfile.

---

## 2. API layering

```
handler/   HTTP. Translates request ↔ JSON. Calls service.
service/   Business rules + cross-repo orchestration.
repository/Data access. Two flavors per aggregate:
              (a) Ent generated client for typed CRUD
              (b) Raw SQL for performance / complex joins (e.g. comments)
domain/    Plain Go types. NO framework imports.
```

**Why mix Ent + raw SQL?** Ent gives type-safe schema migration and CRUD for
the 80% case. Raw SQL escape hatch is used when:
- A query needs JOINs Ent doesn't express well (e.g. comment threading).
- Reads are hot enough that we want exact control over the query plan.

The `EpisodeRepo` is the canonical example — Ent for episode CRUD, raw SQL
for `ListComments`, `GetCommentLikeInfo`, `ToggleCommentLike`.

### 2.1 Errors
`pkg/apperror` defines a small, typed error set (`ErrNotFound`,
`ErrUnauthorized`, `ErrInvalidInput`, `ErrInternal`). Handlers don't write
status codes directly — `middleware.WriteError` maps the error to a code and
JSON body. **Never** return `fmt.Errorf` from a handler; wrap or convert to
`apperror`.

### 2.2 Pagination
`domain.Pagination{Limit, Offset}` is the canonical shape; default 20 / 0.
Repository methods that paginate also return `total int` for the **top-level
collection only**, never including descendants (see §4 on comments).

---

## 3. Storage abstraction

`pkg/storage` wraps the S3 SDK with a tiny interface:

```go
type Storage interface {
    Upload(ctx, key, body, contentType, sizeHint) (publicURL string, err error)
    Delete(ctx, key) error
}
```

Switching from MinIO to Backblaze B2 / Cloudflare R2 is purely an `.env`
change (`STORAGE_*`). The code path is identical.

`STORAGE_PUBLIC_URL` is what we **return to clients** in URL fields. It must
be reachable from the user's browser (not the Docker network). For dev that's
`http://localhost:9000`; for prod it's the CDN origin.

---

## 4. Comment threading

### Goals
- Reading: visual nesting is **at most 1 level** (avoid runaway indents).
- Writing: a user can reply to any comment, including replies. The intent
  ("I'm replying to Bob mid-thread") must be preserved.
- Pagination is on top-level threads only — never split a thread across
  pages.

### Data model
```sql
episode_comments (
  id                 UUID PK,
  episode_id         UUID FK,
  user_id            TEXT,
  content            TEXT,
  parent_comment_id  UUID FK,   -- direct reply target (any depth)
  root_comment_id    UUID FK,   -- top-level ancestor (NULL for tops)
  created_at         TIMESTAMPTZ,
  updated_at         TIMESTAMPTZ
)
```

Why two columns?
- `parent_comment_id` alone would force the UI to walk an arbitrarily deep
  tree to render — and we'd hit indent hell.
- `root_comment_id` alone loses the @-mention context for reply-to-reply.

Keeping both is cheap (UUID FK) and lets us:
1. Group by `root_comment_id` for rendering — guaranteed flat tree.
2. Show `@parent_user` chip when `parent.user_id ≠ root.user_id`.

### Write path (`EpisodeRepo.CreateComment`)
- No `parentID` → top-level: both columns NULL.
- With `parentID`:
  - Validate parent belongs to the same episode (cheap join, prevents
    cross-episode threading).
  - `root = parent.root_comment_id ?? parent.id`.
  - Store `parent_comment_id = parentID`, `root_comment_id = root`.

### Read path (`EpisodeRepo.ListComments`)
Single query (no recursive CTE needed — depth is bounded by 1 root layer +
flat replies):

```sql
WITH top AS (
  SELECT id, created_at FROM episode_comments
  WHERE episode_id = $1 AND parent_comment_id IS NULL
  ORDER BY created_at DESC LIMIT $2 OFFSET $3
)
SELECT c.*, COALESCE(p.user_id, '') AS parent_user_id
FROM episode_comments c
LEFT JOIN episode_comments p ON p.id = c.parent_comment_id
WHERE c.episode_id = $1
  AND ((c.parent_comment_id IS NULL AND c.id IN (SELECT id FROM top))
       OR c.root_comment_id IN (SELECT id FROM top))
ORDER BY <thread_created_at DESC>, <root>, <depth ASC>, c.created_at ASC;
```

`total` (top-level count) is queried separately to keep pagination stable as
replies arrive.

### Frontend rendering (`web/app/series/[id]/episode/[num]/page.tsx`)
- `useMemo` splits the flat array into `{ topLevelComments, repliesByRoot }`
  using `root_comment_id ?? parent_comment_id` as the group key.
- Top-level: full-size avatar (36px) + indent line for the reply group.
- Replies: smaller avatar (28px), `pl-4 ml-12 border-l-2 border-neutral-100`.
- When > 2 replies, collapsed by default with a "View N replies" toggle
  (`expandedReplies` map keyed by root id).
- Reply submit always passes the **direct parent** id. The backend computes
  the root.
- `@User` chip is shown on a reply iff `parent_user_id !== root.user_id` (so
  it appears for mid-thread replies but not for direct replies to the root).

---

## 5. Auth flow

Session is **token in HttpOnly cookie + access token in localStorage** for
the SPA. On login, `auth.login()` returns `{ access, refresh }` and the
client stores them via `setSession`. All `api.*` helpers attach
`Authorization: Bearer <access>`. On 401, the client calls `auth.refresh()`
once before bubbling the error.

**`requireAuth(fn)`** in the page wraps any action that needs login —
prompts the user to sign in (preserving the current URL as `next=`) before
running the action. This is how comment posting, liking, bookmarking work.

---

## 6. Frontend conventions

### 6.1 Routing
- Server components by default. Client components are explicit (`"use client"`).
- Pages that read URL state (`useSearchParams`, etc.) wrap their body in
  `<Suspense>` (see `skills.md` §3.1).
- Shared UI within an `app/foo/` group goes in `app/foo/_components/`. The
  leading `_` keeps Next from treating it as a route.

### 6.2 API client (`web/lib/api.ts`)
- One module, grouped by resource (`episodesApi.*`, `seriesApi.*`, `auth.*`).
- All requests go through `req<T>()` which:
  - Prepends `/api` (proxied through Next rewrites to the API container —
    same-origin, no CORS).
  - Injects the bearer token.
  - Parses JSON, throws on non-2xx with the server's `message`.

### 6.3 Same-origin proxy
The browser hits `/api/v1/...` (relative). Next.js rewrites that path to the
API service. Inside Docker the destination is `http://api:8080`, baked at
build time (see `skills.md` §2.2). This avoids CORS entirely and lets cookies
flow naturally.

---

## 7. Background jobs

`internal/jobs` defines Asynq tasks (e.g. trending refresh). The worker runs
inside the same `webtoon_api` container as the HTTP server — single
deployment unit for simplicity. Asynqmon is exposed at `:8081` for
inspection.

For prod scale, split the worker into its own service (same image, different
entrypoint flag).

---

## 8. Configuration

`config/config.go` uses `cleanenv` with **per-field tags**:
- `env` — variable name
- `env-default` — fallback (for non-secret defaults)
- `env-required` — must be set (for secrets like `JWT_SECRET`)

Anything that varies per-environment lives here. Never read `os.Getenv`
directly elsewhere — always go through `config.Config`.

`.env.example` is the source of truth for which keys exist; `.env` is local
override and gitignored.

---

## 9. Things deliberately NOT done

- **No GraphQL.** REST + simple JSON is fine at this scale.
- **No ORM-only stance.** Raw SQL where it pays off (see §2).
- **No client-side global store** (Redux/Zustand). Component state +
  React Query-shaped manual fetches are plenty for a reader app.
- **No SSR for the reader.** It's a logged-in interactive surface, so we
  pay the small SPA penalty in exchange for simpler client code.
- **No multi-level visual reply nesting.** UX call — see §4.

If any of these change, this section needs an update first.
