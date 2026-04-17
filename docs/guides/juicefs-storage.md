# JuiceFS Storage Integration Guide

> **Target audience**: Backend engineers deploying an axe application with JuiceFS as the distributed filesystem.
>
> **ADR**: [ADR-010 — FSStore POSIX over S3 SDK](../adr/010-fsstore-posix-over-s3.md)
>
> 🇻🇳 [Phiên bản tiếng Việt](#vietnamese-summary)

---

## Overview

`axe-plugin-storage` uses the standard Go `os` package on any POSIX directory. Because JuiceFS exposes a POSIX-compliant FUSE mount, **no special SDK or JuiceFS-specific code is required** — the same `FSStore` adapter works for local dev and JuiceFS production without any code changes.

```
Developer machine   →   ./uploads/          (local dir)
Production server   →   /mnt/jfs/uploads/   (JuiceFS FUSE mount)
```

Both are configured via a single env var: `STORAGE_MOUNT_PATH`.

---

## Prerequisites

| Requirement | Version | Notes |
|---|---|---|
| JuiceFS client | ≥ 1.1 | `juicefs --version` |
| Metadata backend | Redis 6+ / TiKV / etcd | JuiceFS uses it for metadata |
| Object storage | OSS / COS / S3 / MinIO | Actual data blocks |
| Go | 1.22+ | Already required by axe |
| Linux | kernel 4.18+ (FUSE 3) | macOS supported for dev |

---

## Part 1 — JuiceFS Setup

### 1.1 Install JuiceFS client

```bash
# Linux (x86_64)
curl -sSL https://d.juicefs.com/install | sh -
# or via package manager:
# brew install juicefs   (macOS)

juicefs --version   # Verify: juicefs version 1.x.x
```

### 1.2 Format a new filesystem

Run **once** when provisioning the storage volume.

```bash
# Using Redis as metadata engine + Alibaba Cloud OSS as object storage
juicefs format \
  --storage oss \
  --bucket  https://<your-bucket>.oss-<region>.aliyuncs.com \
  --access-key  $OSS_ACCESS_KEY \
  --secret-key  $OSS_SECRET_KEY \
  redis://:<redis-password>@<redis-host>:6379/1 \
  axe-data

# Using MinIO (self-hosted, development/staging)
juicefs format \
  --storage minio \
  --bucket  http://localhost:9000/axe-bucket \
  --access-key minioadmin \
  --secret-key minioadmin \
  redis://localhost:6379/1 \
  axe-data

# Using S3 (AWS)
juicefs format \
  --storage s3 \
  --bucket https://axe-bucket.s3.ap-southeast-1.amazonaws.com \
  --access-key $AWS_ACCESS_KEY_ID \
  --secret-key $AWS_SECRET_ACCESS_KEY \
  redis://localhost:6379/1 \
  axe-data
```

> **Note**: The last argument (`axe-data`) is the filesystem name — pick something meaningful for your project.

### 1.3 Mount the filesystem

```bash
# Foreground (for debugging)
juicefs mount redis://localhost:6379/1 /mnt/jfs --cache-size=1024

# Background (production)
juicefs mount redis://localhost:6379/1 /mnt/jfs \
  --background \
  --cache-size=1024 \
  --cache-dir=/var/jfs-cache \
  --log=/var/log/juicefs.log

# Verify mount is working
ls /mnt/jfs                   # Should show empty dir or existing files
touch /mnt/jfs/.test && rm /mnt/jfs/.test && echo "Writeable ✅"
```

### 1.4 Create the uploads directory

```bash
mkdir -p /mnt/jfs/axe-uploads
chmod 755 /mnt/jfs/axe-uploads
```

---

## Part 2 — axe Configuration

### 2.1 Environment variables

Add these to your `.env` (and `.env.example`):

```bash
# Storage plugin
STORAGE_BACKEND=juicefs                  # Label for metrics (local | juicefs)
STORAGE_MOUNT_PATH=/mnt/jfs/axe-uploads  # Absolute path to JuiceFS uploads dir
STORAGE_MAX_FILE_SIZE=52428800           # 50MB (bytes); default: 10MB
STORAGE_URL_PREFIX=/upload               # HTTP prefix for serving files
STORAGE_REQUIRE_AUTH=false               # true → require JWT on GET as well
```

> **Local dev**: set `STORAGE_BACKEND=local` and `STORAGE_MOUNT_PATH=./uploads`.
> **Production**: set `STORAGE_BACKEND=juicefs` and `STORAGE_MOUNT_PATH=/mnt/jfs/axe-uploads`.
> The only difference is the metric label — the code path is identical.

### 2.2 Wire the plugin in `cmd/api/main.go`

```go
import "github.com/axe-cute/axe/pkg/plugin/storage"

// In main():
storageCfg := storage.Config{
    Backend:     cfg.StorageBackend,    // "local" | "juicefs"
    MountPath:   cfg.StorageMountPath,  // "/mnt/jfs/axe-uploads"
    MaxFileSize: cfg.StorageMaxFileSize,
    URLPrefix:   cfg.StorageURLPrefix,
    RequireAuth: cfg.StorageRequireAuth,
}
pluginApp.Use(storage.New(storageCfg))
```

> If your project was scaffolded with `axe new --with-storage`, this is already wired.
> For an existing project: `axe plugin add storage`.

### 2.3 Supported config struct fields (config/config.go)

```go
StorageBackend     string `env:"STORAGE_BACKEND"      env-default:"local"`
StorageMountPath   string `env:"STORAGE_MOUNT_PATH"   env-default:"./uploads"`
StorageMaxFileSize int64  `env:"STORAGE_MAX_FILE_SIZE" env-default:"10485760"`
StorageURLPrefix   string `env:"STORAGE_URL_PREFIX"   env-default:"/upload"`
StorageRequireAuth bool   `env:"STORAGE_REQUIRE_AUTH" env-default:"false"`
```

---

## Part 3 — HTTP API

Once the plugin is registered, axe exposes these routes automatically:

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/upload` | ✅ JWT required | Upload a file (multipart/form-data) |
| `GET` | `/upload/{key...}` | Public (or JWT if RequireAuth=true) | Serve a file |
| `DELETE` | `/upload/{key...}` | ✅ JWT required | Delete a file |

### Upload example

```bash
# Obtain a JWT first
TOKEN=$(curl -s -X POST http://localhost:8080/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@example.com","password":"secret"}' | jq -r .access_token)

# Upload a file
curl -X POST http://localhost:8080/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@photo.jpg;type=image/jpeg"

# Response
{
  "key":          "2026/04/17/photo.jpg",
  "url":          "/upload/2026/04/17/photo.jpg",
  "size":         148302,
  "content_type": "image/jpeg"
}
```

### Serve example

```bash
# Public (no JWT needed by default)
curl http://localhost:8080/upload/2026/04/17/photo.jpg \
  -o downloaded-photo.jpg
```

### Hierarchical keys

Use `storage.KeyForFile(name)` (provided by the package) to generate date-namespaced keys:

```go
key := storage.KeyForFile("avatar.png")   // → "2026/04/17/avatar.png"
store := plugin.MustResolve[storage.Store](app, storage.ServiceKey)
result, err := store.Upload(ctx, key, reader, size, "image/png")
```

---

## Part 4 — Health Check

The `/ready` endpoint calls `FSStore.HealthCheck()` which performs a **write → read → delete** cycle on the mount — not just `os.Stat`. This is critical because a stale or read-only JuiceFS mount passes `Stat` but fails on writes.

```bash
# Should return 200 OK when JuiceFS is healthy
curl http://localhost:8080/ready

# Expected response
{"status":"ready","storage":"ok"}

# When JuiceFS mount is stale/unavailable
{"status":"degraded","storage":"storage: health-check write: mount unavailable (check JuiceFS connection)"}
→ HTTP 503
```

### Automate health checks

```yaml
# docker-compose.yml (app service)
healthcheck:
  test: ["CMD", "curl", "-f", "http://localhost:8080/ready"]
  interval: 30s
  timeout: 5s
  retries: 3
  start_period: 10s
```

```yaml
# Kubernetes liveness + readiness probes
livenessProbe:
  httpGet:
    path: /health
    port: 8080
  initialDelaySeconds: 10
  periodSeconds: 30

readinessProbe:
  httpGet:
    path: /ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
  failureThreshold: 3
```

---

## Part 5 — FUSE Failure Modes

The storage plugin translates low-level FUSE/OS errors into human-readable messages via `wrapFSError()`. No raw `syscall` error will leak into HTTP responses.

| Syscall error | Meaning | Wrapped message |
|---|---|---|
| `ENOTCONN` | JuiceFS FUSE daemon disconnected | `mount unavailable (check JuiceFS connection)` |
| `EIO` | Transport-level I/O error | `mount unavailable (check JuiceFS connection)` |
| `EROFS` | Mount is read-only (quota exceeded or explicit RO) | `mount is read-only` |
| `ENOSPC` | No space left (disk full or JuiceFS quota) | `no space left on mount` |
| `EACCES` / `EPERM` | Permission denied | `permission denied` |

### Recovery checklist

```bash
# 1. Check if JuiceFS is still mounted
mountpoint -q /mnt/jfs && echo "Mounted ✅" || echo "NOT mounted ❌"
df -h /mnt/jfs

# 2. Check JuiceFS daemon
ps aux | grep juicefs
journalctl -u juicefs --since "10 min ago"

# 3. Check metadata backend (Redis) connectivity
redis-cli -h <redis-host> ping

# 4. Re-mount if needed (zero-downtime on Linux: lazy unmount first)
umount -l /mnt/jfs
juicefs mount redis://<redis-host>:6379/1 /mnt/jfs --background

# 5. Verify health
curl http://localhost:8080/ready

# 6. Check quota
juicefs quota get redis://<redis-host>:6379/1 --path /
```

---

## Part 6 — Production Deployment

### 6.1 systemd service for JuiceFS mount

```ini
# /etc/systemd/system/juicefs.service
[Unit]
Description=JuiceFS mount — axe storage
After=network-online.target
Wants=network-online.target

[Service]
Type=forking
ExecStart=/usr/bin/juicefs mount \
  redis://:<redis-password>@<redis-host>:6379/1 /mnt/jfs \
  --background \
  --cache-size=2048 \
  --cache-dir=/var/jfs-cache \
  --log=/var/log/juicefs.log
ExecStop=/bin/umount /mnt/jfs
Restart=on-failure
RestartSec=10s

[Install]
WantedBy=multi-user.target
```

```bash
systemctl daemon-reload
systemctl enable juicefs
systemctl start juicefs
```

### 6.2 Docker deployment

When running axe in Docker with JuiceFS, the mount must be **on the host** and bind-mounted into the container. FUSE inside containers requires `--privileged` or a special capability setup.

**Recommended approach: mount on host, bind into container**

```yaml
# docker-compose.yml
services:
  app:
    image: axe-app:latest
    environment:
      STORAGE_BACKEND: juicefs
      STORAGE_MOUNT_PATH: /mnt/axe-uploads   # Inside container
    volumes:
      - /mnt/jfs/axe-uploads:/mnt/axe-uploads:rw   # Host JuiceFS → container
    depends_on:
      - redis
      - postgres
```

```bash
# On host: mount JuiceFS before starting Docker
juicefs mount redis://localhost:6379/1 /mnt/jfs --background
mkdir -p /mnt/jfs/axe-uploads

# Then start containers
docker compose up -d
```

**Alternative: JuiceFS sidecar container**

```yaml
services:
  juicefs:
    image: juicedata/juicefs:latest
    privileged: true
    cap_add:
      - SYS_ADMIN
    devices:
      - /dev/fuse
    volumes:
      - /mnt/jfs:/mnt/jfs:rshared
    command: >
      mount redis://<redis-host>:6379/1 /mnt/jfs
      --cache-size 1024
      --no-update-config

  app:
    image: axe-app:latest
    depends_on:
      - juicefs
    volumes:
      - /mnt/jfs:/mnt/jfs:rshared
    environment:
      STORAGE_MOUNT_PATH: /mnt/jfs/axe-uploads
```

> ⚠️ The sidecar approach requires `privileged: true` — use only in trusted environments.

### 6.3 Kubernetes (with CSI driver)

JuiceFS provides an official CSI driver for Kubernetes:

```yaml
# PersistentVolumeClaim
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: axe-uploads-pvc
spec:
  storageClassName: juicefs-sc
  accessModes: [ReadWriteMany]  # JuiceFS supports RWX — multiple pods
  resources:
    requests:
      storage: 100Gi

---
# Deployment
apiVersion: apps/v1
kind: Deployment
metadata:
  name: axe-app
spec:
  template:
    spec:
      containers:
        - name: axe
          image: axe-app:latest
          env:
            - name: STORAGE_BACKEND
              value: juicefs
            - name: STORAGE_MOUNT_PATH
              value: /mnt/axe-uploads
          volumeMounts:
            - name: uploads
              mountPath: /mnt/axe-uploads
      volumes:
        - name: uploads
          persistentVolumeClaim:
            claimName: axe-uploads-pvc
```

---

## Part 7 — Prometheus Monitoring

The storage plugin exports these metrics automatically:

| Metric | Labels | Description |
|---|---|---|
| `axe_storage_upload_bytes_total` | `backend` | Total bytes uploaded |
| `axe_storage_operations_total` | `backend`, `op`, `status` | All operations (upload/delete/open) |
| `axe_storage_upload_errors_total` | `backend` | Failed uploads |

### Recommended Grafana dashboard queries

```promql
# Upload throughput (bytes/sec)
rate(axe_storage_upload_bytes_total{backend="juicefs"}[5m])

# Upload success rate %
100 * (
  rate(axe_storage_operations_total{op="upload", status="ok"}[5m])
  / rate(axe_storage_operations_total{op="upload"}[5m])
)

# Error rate spikes (alert threshold: > 5/min)
rate(axe_storage_upload_errors_total[1m]) * 60 > 5

# p99 upload sizing (bucket histogram if instrumented)
histogram_quantile(0.99, rate(axe_storage_upload_size_bytes_bucket[5m]))
```

### Alerting rules (example)

```yaml
# prometheus/alerts.yml
groups:
  - name: axe_storage
    rules:
      - alert: StorageMountUnavailable
        expr: axe_storage_upload_errors_total{backend="juicefs"} > 0
        for: 2m
        labels:
          severity: critical
        annotations:
          summary: "JuiceFS storage errors detected"
          description: "Check /ready endpoint and JuiceFS mount status."

      - alert: StorageHealthCheckFailing
        expr: up{job="axe"} == 1 and axe_http_requests_total{path="/ready", status="503"} > 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "/ready returning 503 — JuiceFS mount may be stale"
```

---

## Part 8 — Security Checklist

| Item | Default | Production recommendation |
|---|---|---|
| JWT on upload (POST) | ✅ Always enforced | — |
| JWT on delete (DELETE) | ✅ Always enforced | — |
| JWT on serve (GET) | ❌ Public | Set `STORAGE_REQUIRE_AUTH=true` for private files |
| Path traversal protection | ✅ `safePath()` | — |
| CORS | Configured via CORS config | Restrict to your frontend domain |
| Max file size | 10MB default | Set `STORAGE_MAX_FILE_SIZE` for your use case |
| Allowed MIME types | All (empty list) | Set `AllowedTypes: ["image/png", "image/jpeg", "application/pdf"]` |
| Mount directory permissions | 755 by default | Use `chmod 750` + dedicated app user |

---

## Part 9 — Ops Runbook

### Expand quota

```bash
juicefs quota set redis://<redis-host>:6379/1 \
  --path / \
  --capacity 1T \
  --inodes 10000000
```

### Check storage usage

```bash
juicefs summary redis://<redis-host>:6379/1 --path /axe-uploads
```

### Evict local cache

```bash
juicefs rmr /mnt/jfs/.juicefs/cache/  # Only if needed
```

### Graceful restart (zero downtime)

```bash
# 1. New instance starts & mounts JuiceFS
# 2. Kubernetes / ECS rolling update
# 3. Old instance's readiness probe fails → drained from LB
# 4. Old instance shuts down (SIGTERM → graceful shutdown in Shutdown())
# 5. No data loss because JuiceFS is shared across instances
```

---

## Quick Reference

```bash
# Local development
STORAGE_BACKEND=local STORAGE_MOUNT_PATH=./uploads make run

# Production smoke test after deploy
curl https://api.yourapp.com/ready
# Expected: {"status":"ready","storage":"ok"}

# Force health check via CLI
TOKEN=$(curl -s -X POST https://api.yourapp.com/api/v1/auth/login \
  -d '{"email":"admin@example.com","password":"secret"}' | jq -r .access_token)
curl -X POST https://api.yourapp.com/upload \
  -H "Authorization: Bearer $TOKEN" \
  -F "file=@test.txt;type=text/plain"
```

---

## Vietnamese Summary

<a id="vietnamese-summary"></a>

> **Tóm tắt**: `axe-plugin-storage` sử dụng Go `os` package chuẩn trên bất kỳ thư mục POSIX nào.
> JuiceFS mount FUSE như một directory bình thường → **không cần thay đổi code**.
>
> **Các bước chính**:
> 1. Cài `juicefs`, format filesystem, mount vào `/mnt/jfs`
> 2. Tạo thư mục: `mkdir -p /mnt/jfs/axe-uploads`
> 3. Set env: `STORAGE_BACKEND=juicefs`, `STORAGE_MOUNT_PATH=/mnt/jfs/axe-uploads`
> 4. Wire plugin: `pluginApp.Use(storage.New(storageCfg))` (đã auto-wired nếu dùng `axe new --with-storage`)
> 5. Health check: `GET /ready` → gọi `FSStore.HealthCheck()` (write→read→delete sentinel, không chỉ `os.Stat`)
>
> **FUSE failure modes** đã được handle trong `wrapFSError()`: `ENOTCONN`, `EIO`, `EROFS`, `ENOSPC`, `EACCES` → human-readable error, không leak syscall details ra HTTP response.
>
> **Docker**: mount JuiceFS trên host trước, sau đó bind-mount vào container. Không cần `privileged` cho app container.

---

## Related

- [ADR-010 — FSStore POSIX over S3 SDK](../adr/010-fsstore-posix-over-s3.md)
- [`pkg/plugin/storage/`](../../pkg/plugin/storage/) — implementation
- [`pkg/plugin/storage/fs_store.go`](../../pkg/plugin/storage/fs_store.go) — `FSStore`, `HealthCheck()`, `wrapFSError()`
- [JuiceFS official docs](https://juicefs.com/docs/community/getting-started/standalone/)
- [JuiceFS CSI Driver](https://juicefs.com/docs/csi/introduction/)
- [JuiceFS Prometheus integration](https://juicefs.com/docs/community/administration/monitoring/)
