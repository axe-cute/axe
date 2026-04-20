# Early Adopter Kit

> **Goal**: Get 3+ external users to try axe and provide feedback before v1.0.0.
>
> Framework without users = library without purpose.

---

## What is Axe?

Axe is a Go framework for building production-ready REST APIs. It generates a
complete project structure with layered architecture, transactional outbox,
WebSocket support, and a plugin system — all from a single CLI command.

**15-minute pitch**: `axe new` → generate resources → add plugins → deploy.
Zero boilerplate, zero magic, full control.

---

## Quick Start (for evaluators)

```bash
# Install
go install github.com/axe-cute/axe/cmd/axe@v0.5.0

# Create a project
axe new my-api --db=postgres --module=github.com/myorg/my-api --yes

# Generate a resource
cd my-api
axe generate resource Post --fields="title:string,body:text,published:bool"

# Run
docker compose up -d
go run cmd/api/main.go
```

Full guide: [Getting Started](docs/guides/getting-started.md)

---

## What We Need Feedback On

### Priority Questions

1. **First impression** — Did `axe new` + `axe generate resource` work on your machine? Any errors?
2. **Architecture fit** — Does the `domain/ → handler/ → service/ → repository/` layering feel natural or over-engineered for your use case?
3. **Plugin system** — Did you try any plugins? Was the `app.Use()` pattern intuitive?
4. **Documentation** — Could you follow the Getting Started guide without help?
5. **Missing features** — What blocked you from using axe for a real project?

### Bonus Questions

6. **Database choice** — Did you use PostgreSQL, MySQL, or SQLite? Any issues?
7. **Ent vs sqlc** — Was the "choose one per project" model clear?
8. **Error handling** — Did the `apperror` taxonomy make sense?

---

## Feedback Form

Please share your experience via any of these channels:

- **GitHub Issue**: [github.com/axe-cute/axe/issues/new](https://github.com/axe-cute/axe/issues/new) (label: `early-adopter`)
- **Email**: (add your contact)
- **Go Discord**: #axe channel (to be created)

### Template

```
## Environment
- OS: macOS / Linux / Windows
- Go version: 
- Database: PostgreSQL / MySQL / SQLite

## What I tried
(describe your project/experiment)

## What worked well
(list positives)

## What was confusing or broken
(list issues)

## Would I use this for a real project?
(yes/no and why)
```

---

## Outreach Channels

| Channel | Action | When |
|---|---|---|
| **r/golang** | Post "Show /r/golang: axe — Go REST API framework with layered architecture" | After v0.5.0 tag |
| **Go Discord** | Share in #projects channel | After v0.5.0 tag |
| **Colleagues** | Direct message 3-5 Go developers you know | Immediately |
| **Hacker News** | "Show HN" post (only if r/golang reception is positive) | 2 weeks after v0.5.0 |
| **Dev.to / Hashnode** | Write "Building a REST API in Go with axe" tutorial | 1 week after v0.5.0 |

---

## v1.0.0 Gate

> [!CAUTION]
> **Hard requirement**: v1.0.0 MUST NOT be tagged until:
> 1. ≥3 external users have completed the feedback form
> 2. All "confusing or broken" items are addressed
> 3. At least 1 user answers "yes" to "Would I use this for a real project?"

Current status: **0 / 3 external users** ❌

---

## What Evaluators Get

- Direct support from the maintainer during evaluation
- Their feedback shapes v1.0.0 API decisions
- Attribution in CHANGELOG and README (if desired)
- Priority on feature requests

---

*Last updated: 2026-04-20*
