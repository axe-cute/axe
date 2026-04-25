# 📖 Webtoon — axe full-stack example

A **reader-first webtoon platform** — Go + Ent backend scaffolded with `axe`, Next.js 15 frontend with a Linear-inspired dark UI.

## Quick Start — full stack via Docker

```bash
cp .env.example .env
docker compose up -d --build         # postgres + redis + api + web
make migrate-up                       # apply schema
make seed                             # 8 series × 3–5 episodes
open http://localhost:3000
```

## Quick Start — local dev (two terminals)

```bash
# Terminal 1: infra + API
docker compose up -d postgres redis
cp .env.example .env
make migrate-up && make seed
make run                              # API at :8080

# Terminal 2: web
cd web && npm install && npm run dev  # UI at :3000
```

**Demo login** (any email works, ≥4-char password):
`reader@axe.dev` / `demo1234`

## Domain Model

```
Series (title, description, genre, author, cover_url, status)
  │  status: ongoing | completed | hiatus
  │  genre: action, romance, comedy, drama, fantasy, horror, ...
  │
  └── Episode (title, episode_number, thumbnail_url, published, view_count)
        [belongs-to Series]

Bookmark (user_id, series_id)
  └── ToggleBookmark: one-click add/remove from reading list
```

## How This Was Built

```bash
# Step 1: Scaffold (2 minutes)
axe new webtoon --module github.com/axe-cute/examples-webtoon --db postgres --yes

# Step 2: Generate resources
axe generate resource Series   --fields="title:string,description:text,genre:string,author:string,cover_url:string,status:string" --with-auth
axe generate resource Episode  --fields="title:string,episode_number:int,thumbnail_url:string,published:bool" --belongs-to=Series --with-auth
axe generate resource Bookmark --fields="series_id:uuid" --with-auth

# Step 3: Add business logic (manual — see below)
```

## Business Logic (Beyond CRUD)

### 1. Series Validation
- Title and author required
- Genre validated against whitelist: `action, romance, comedy, drama, fantasy, horror, thriller, slice-of-life, sci-fi, sports, historical`
- Status validated: `ongoing`, `completed`, `hiatus` (defaults to `ongoing`)

```bash
# Create a series
curl -X POST http://localhost:8080/api/v1/serieses \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Solo Leveling",
    "description": "10 years since a portal connecting worlds appeared...",
    "genre": "action",
    "author": "Chu-Gong",
    "cover_url": "/covers/solo-leveling.jpg",
    "status": "ongoing"
  }'

# Invalid genre → 400
curl -X POST http://localhost:8080/api/v1/serieses \
  -d '{"title": "Test", "author": "Me", "genre": "invalid"}'
# → "invalid genre \"invalid\" — allowed: action, romance, ..."
```

### 2. Episode Validation + View Tracking
- Title required, episode_number > 0, series_id must exist
- View count tracked on each read (incremented automatically)

```bash
# Create episode
curl -X POST http://localhost:8080/api/v1/episodes \
  -H "Authorization: Bearer $TOKEN" \
  -d '{
    "title": "Chapter 1: Weakest Hunter",
    "episode_number": 1,
    "thumbnail_url": "/thumbnails/sl-ch1.jpg",
    "published": true,
    "series_id": "SERIES_UUID"
  }'

# Get episode (view_count auto-increments)
curl http://localhost:8080/api/v1/episodes/{id}
```

### 3. Bookmark Toggle — The Hero Feature
`POST /api/v1/bookmarks/toggle` — one-click add/remove from reading list.

```bash
# Add to reading list (first call)
curl -X POST http://localhost:8080/api/v1/bookmarks/toggle \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"series_id": "SERIES_UUID"}'
# → {"bookmarked": true, "series_id": "..."}

# Remove from reading list (second call with same series_id)
curl -X POST http://localhost:8080/api/v1/bookmarks/toggle \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"series_id": "SERIES_UUID"}'
# → {"bookmarked": false, "series_id": "..."}
```

## API Endpoints

### Series
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/serieses` | No | Browse catalog |
| GET | `/api/v1/serieses/{id}` | No | Series detail |
| POST | `/api/v1/serieses` | Yes | Create (validates genre + status) |
| PUT | `/api/v1/serieses/{id}` | Yes | Update (validates on change) |
| DELETE | `/api/v1/serieses/{id}` | Yes | Delete series |

### Episodes
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/episodes` | No | List episodes |
| GET | `/api/v1/episodes/{id}` | No | Read episode (view count ++) |
| POST | `/api/v1/episodes` | Yes | Create (validates series exists) |
| PUT | `/api/v1/episodes/{id}` | Yes | Update |
| DELETE | `/api/v1/episodes/{id}` | Yes | Delete |

### Bookmarks
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/bookmarks/toggle` | Yes | **Toggle** bookmark (add/remove) |
| GET | `/api/v1/bookmarks` | Yes | List my bookmarks |
| POST | `/api/v1/bookmarks` | Yes | Create bookmark |
| DELETE | `/api/v1/bookmarks/{id}` | Yes | Remove bookmark |

### Infrastructure
| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness probe |
| GET | `/ready` | Readiness probe |
| GET | `/metrics` | Prometheus metrics |

## Architecture

```
cmd/api/main.go              ← Composition root (auto-wired by axe)
internal/
├── domain/                  ← Pure domain models + business rules
│   ├── series.go            ← Genre whitelist + status validation
│   ├── episode.go           ← Episode with ViewCount tracking
│   └── bookmark.go          ← Bookmark + ToggleResult type
├── handler/                 ← HTTP handlers
│   ├── series_handler.go    ← Standard CRUD
│   ├── episode_handler.go   ← CRUD with view tracking
│   └── bookmark_handler.go  ← CRUD + ToggleBookmark endpoint
├── service/                 ← Business logic layer
│   ├── series_service.go    ← Genre/status validation on create+update
│   ├── episode_service.go   ← Series existence check + view counting
│   └── bookmark_service.go  ← ToggleBookmark (add if missing, remove if exists)
└── repository/              ← Database access (Ent ORM)
```

## axe Features Demonstrated

| Feature | Implementation |
|---|---|
| `axe new` | Full project scaffold |
| `axe generate resource` | 3 resources auto-generated + auto-wired |
| `--with-auth` | JWT auth middleware |
| `--belongs-to` | Episode → Series foreign key |
| **Series validation** | Genre whitelist + status enum |
| **Episode tracking** | View count on read |
| **Bookmark toggle** | One-click add/remove pattern |
| Clean Architecture | domain → handler → service → repository |

## Extending This Example

```bash
# Add real-time new episode notifications
axe generate resource Notification --fields="message:text,read:bool" --with-auth --with-ws

# Add payment for premium episodes
axe plugin add stripe

# Add file storage for episode pages
axe plugin add storage
```

## License

MIT
