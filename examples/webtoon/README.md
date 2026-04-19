# 📖 Webtoon API — axe Example

A **production-grade webtoon/manhwa platform API** built entirely with `axe new` + `axe generate resource`.

Demonstrates: multi-resource CRUD, parent-child relationships (Series → Episode), user bookmarks, and JWT authentication.

## Quick Start

```bash
# 1. Start infrastructure
docker-compose up -d     # PostgreSQL + Redis

# 2. Setup
cp .env.example .env
make migrate-up

# 3. Run
make run                 # API at :8080
```

## Domain Model

```
Series (title, description, genre, author, cover_url, status)
  └── Episode (title, episode_number, thumbnail_url, published) [belongs-to Series]

Bookmark (series_id) [auth required — user's reading list]
```

## How This Was Built

```bash
# Step 1: Scaffold project
axe new webtoon --module github.com/axe-cute/examples-webtoon --db postgres --yes

# Step 2: Generate resources
axe generate resource Series   --fields="title:string,description:text,genre:string,author:string,cover_url:string,status:string" --with-auth
axe generate resource Episode  --fields="title:string,episode_number:int,thumbnail_url:string,published:bool" --belongs-to=Series --with-auth
axe generate resource Bookmark --fields="series_id:uuid" --with-auth
```

**Total time: ~3 minutes.** Everything compiled and auto-wired.

## API Endpoints

### Series (Public Read, Auth Write)
```bash
# List all series (public — browsing catalog)
curl http://localhost:8080/serieses

# Get series detail
curl http://localhost:8080/serieses/{id}

# Create series (creator/admin)
curl -X POST http://localhost:8080/serieses \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Solo Leveling",
    "description": "10 years since a portal appeared...",
    "genre": "action",
    "author": "Chu-Gong",
    "cover_url": "/covers/solo-leveling.jpg",
    "status": "ongoing"
  }'

# Update series status
curl -X PUT http://localhost:8080/serieses/{id} \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"status": "completed"}'
```

### Episodes (belongs-to Series)
```bash
# List episodes for a series
curl http://localhost:8080/episodes

# Create episode
curl -X POST http://localhost:8080/episodes \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "title": "Chapter 1: Weakest Hunter",
    "episode_number": 1,
    "thumbnail_url": "/thumbnails/sl-ch1.jpg",
    "published": true,
    "series_id": "..."
  }'
```

### Bookmarks (Auth Required — User's Reading List)
```bash
# Add to reading list
curl -X POST http://localhost:8080/bookmarks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"series_id": "..."}'

# Get my bookmarks
curl http://localhost:8080/bookmarks \
  -H "Authorization: Bearer $TOKEN"

# Remove from reading list
curl -X DELETE http://localhost:8080/bookmarks/{id} \
  -H "Authorization: Bearer $TOKEN"
```

### Health & Monitoring
```bash
curl http://localhost:8080/health   # liveness probe
curl http://localhost:8080/ready    # readiness probe
curl http://localhost:8080/metrics  # Prometheus
```

## Architecture

```
cmd/api/main.go              ← Composition root (auto-wired by axe)
internal/
├── domain/                  ← Pure structs + interfaces
│   ├── series.go
│   ├── episode.go
│   └── bookmark.go
├── handler/                 ← HTTP handlers (chi router)
│   ├── series_handler.go
│   ├── episode_handler.go
│   └── bookmark_handler.go
├── service/                 ← Business logic
│   ├── series_service.go
│   ├── episode_service.go
│   └── bookmark_service.go
└── repository/              ← Database queries (Ent ORM)
    ├── series_repo.go
    ├── episode_repo.go
    └── bookmark_repo.go
ent/schema/                  ← Ent ORM schemas
db/migrations/               ← SQL migrations
```

## axe Features Demonstrated

| Feature | How |
|---|---|
| `axe new` | Project scaffolded with full infrastructure |
| `axe generate resource` | 3 resources × 9 files = 27 files generated |
| `--with-auth` | JWT authentication on write operations |
| `--belongs-to` | Episode → Series foreign key relationship |
| Auto-wiring | `main.go` automatically updated with DI code |
| Clean Architecture | domain → handler → service → repository |
| UUID primary keys | All entities use UUID (Ent + Google UUID) |
| Background jobs | Asynq worker ready for notifications |
| Caching | Redis cache ready for popular series |
| WebSocket | Hub ready for real-time episode notifications |

## Extending This Example

### Add real-time notifications
```bash
axe generate resource Notification --fields="message:text,read:bool" --with-auth --with-ws
```

### Add payment for premium episodes
```bash
axe plugin add stripe
```

### Add file storage for episode pages
```bash
axe plugin add storage
```

## License

MIT
