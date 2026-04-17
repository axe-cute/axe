# ADR-010: FSStore POSIX Adapter over S3 SDK for File Storage

**Status**: Accepted  
**Date**: 2026-04-16  
**Deciders**: Henry Nguyen  
**Supersedes**: —  
**Superseded by**: —

---

## Context

The axe framework needs a file storage plugin for handling user uploads. Two categories of approach were considered:

1. **Object Storage SDK** — integrate with an S3-compatible API (AWS S3, MinIO, Cloudflare R2) via `aws-sdk-go-v2` or a compatible client.
2. **POSIX Filesystem** — use standard Go `os` package operations on any directory, local or mounted.

In the VN/SEA production context, the common storage choice is **JuiceFS** — a distributed filesystem that exposes a POSIX-compliant FUSE mount point. JuiceFS can sit on top of object storage backends (OSS, COS, S3) but presents itself locally as a directory.

---

## Decision

**Use a POSIX filesystem adapter (`FSStore`) backed by the Go `os` standard library.**

The same `FSStore` implementation works for:
- **Local dev**: `./uploads` on the developer's machine
- **JuiceFS production**: `/mnt/jfs/axe-uploads` — a FUSE mount backed by distributed object storage

No S3 SDK dependency is added.

---

## Consequences

### Positive

- **Zero external dependencies** — `os`, `io`, `path/filepath` only; no SDK pinned.
- **Works with any POSIX mount** — local, NFS, JuiceFS, EFS, or any FUSE filesystem.  
- **No storage vendor lock-in** — switching from JuiceFS to another POSIX-compatible FS requires only changing `STORAGE_MOUNT_PATH`.
- **Simpler ops model** — standard Linux filesystem tooling (`ls`, `du`, `rsync`) works on the mount; no special S3 tooling needed.
- **Consistent DX** — same code path in dev and prod, reducing "works on my machine" issues.

### Negative / Trade-offs

- **JuiceFS must be installed and mounted** on every production node (FUSE daemon dependency).
- **No presigned URLs** — can't generate short-lived S3-style URLs; all file serving goes through the axe HTTP handler.
- **No multi-region replication** natively — JuiceFS handles this at its layer but isn't transparent to axe.
- **FUSE failure modes require explicit handling** — `ENOTCONN`, `EIO`, `EROFS` must be wrapped (done in `wrapFSError()` as of Sprint 21).

### Neutral

- The `Store` interface (`Upload`, `Delete`, `Open`, `Exists`, `URL`) is backend-agnostic. A future `S3Store` implementing the same interface could be added without changing any handler or plugin code.

---

## Alternatives Considered

### A: aws-sdk-go-v2 + S3 directly

```
+ Presigned URLs, multipart upload, CDN integration
+ No FUSE daemon required
- Adds ~10MB of dependency surface
- Requires S3-compatible endpoint even in dev
- Different error types (aws/smithy vs os/syscall)
- Breaks "zero external deps" design goal for axe plugins
```

**Rejected**: Overhead too high for the target use case; JuiceFS is already in the production stack.

### B: Use MinIO Go client

```
+ S3-compatible, self-hostable
- Still requires MinIO or S3-compatible server
- Adds external dependency
```

**Rejected**: Same concerns as (A).

### C: Indirect via JuiceFS Go SDK

```
+ Direct JuiceFS API access
- Tightly couples code to JuiceFS
- SDK not stable enough for framework use
```

**Rejected**: Would break local dev transparency and add vendor lock-in.

---

## Implementation Notes

### Config

```go
// STORAGE_BACKEND selects logging/metrics label: "local" or "juicefs"
// Both use the same FSStore — backend distinction is cosmetic.
STORAGE_BACKEND=local           # or "juicefs" in production
STORAGE_MOUNT_PATH=./uploads    # or "/mnt/jfs/axe-uploads"
STORAGE_MAX_FILE_SIZE=10485760  # 10MB default
STORAGE_URL_PREFIX=/upload
```

### JuiceFS production setup

```bash
# On production host
juicefs mount redis://localhost/1 /mnt/jfs --background
# axe reads STORAGE_MOUNT_PATH=/mnt/jfs/axe-uploads
```

### Operational hardening (Sprint 21)

The original `os.Stat` health check was insufficient for stale FUSE mounts. As of Sprint 21:

1. **`FSStore.HealthCheck()`** — performs write→read→delete cycle on the mount (avoids false-positive on read-only/stale JuiceFS).
2. **`f.Sync()` in `Upload()`** — flushes FUSE buffers before returning, avoiding silent data loss on crash.
3. **`wrapFSError()`** — translates `ENOTCONN`, `EIO`, `EROFS`, `ENOSPC`, `EACCES` into human-readable errors; prevents raw syscall details from leaking into HTTP responses.

### Future: S3Store adapter

If presigned URL support or direct S3 access becomes necessary:

```go
// Implement Store interface with S3 backend
type S3Store struct { client *s3.Client; bucket string }
func (s *S3Store) Upload(...)  (*Result, error) { ... }
func (s *S3Store) Delete(...)  error             { ... }
// ... etc
```

No handler or plugin changes would be required — just register `S3Store` instead of `FSStore`.

---

## Related

- [ADR-001](./001-chi-over-gin-echo.md) — HTTP framework choice
- [ADR-003](./003-ent-writes-sqlc-reads.md) — Database access patterns
- [JuiceFS docs](https://juicefs.com/docs/community/getting-started/standalone/)
- `pkg/plugin/storage/` — implementation
- `cmd/api/main.go:readyHandler` — health check wiring
