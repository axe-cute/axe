# Epic 9 — Long-term Plugin Ecosystem (v2.0+)

**Goal**: Mở rộng axe plugin ecosystem với các integrations nâng cao — AI, Cloud, Payments, Advanced Observability. Tất cả đều follow cùng `Plugin` interface từ Epic 8.

**Business Value**: Tạo axe thành nền tảng "batteries-included" cho Go backend, cạnh tranh với Spring Boot ecosystem. Developer không cần research và integrate từng library riêng lẻ.

**Status**: 🟡 Planned (Sprint 25+, v2.0)

**Priority**: P3 — Defer đến sau v1.0 release. Prioritize dựa trên community demand.

**Prerequisites**:
- Epic 8 hoàn thành (Plugin interface stable, `axe plugin add` working)
- v1.0 released và có real user feedback

> ⚠️ Thứ tự implement trong Epic 9 sẽ thay đổi dựa trên community demand thực tế.

---

## Stories

### Story 9.1 — `axe-plugin-payment` (Stripe)
**Sprint**: 25 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Payment processing plugin với Stripe backend.

**Acceptance Criteria**:
- [ ] `pkg/plugin/payment/stripe/` — StripePlugin
- [ ] `payment.Charge(ctx, amount, currency, source)` → error
- [ ] Webhook handler auto-registered: `POST /webhooks/stripe`
- [ ] Webhook signature verification
- [ ] Events: `payment.succeeded`, `payment.failed` → typed handlers
- [ ] Dev mode: Stripe test mode + test clock support
- [ ] Prometheus: `axe_payment_transactions_total{status="success|failed"}`

**Interface**:
```go
type PaymentPlugin interface {
    Plugin
    Charge(ctx context.Context, req ChargeRequest) (*ChargeResult, error)
    CreateCustomer(ctx context.Context, email string) (*Customer, error)
    Subscribe(ctx context.Context, customerID, planID string) (*Subscription, error)
}
```

### Story 9.2 — `axe-plugin-payment:payos` (Vietnam)
**Sprint**: 25 | **Priority**: P3 | **Status**: 🟡 Planned

**Goal**: PayOS payment plugin — Vietnam-specific payment gateway.

**Acceptance Criteria**:
- [ ] `pkg/plugin/payment/payos/` — PayOSPlugin (same interface as Stripe plugin)
- [ ] QR code payment support
- [ ] Webhook handler: `POST /webhooks/payos`
- [ ] VND currency handling

### Story 9.3 — `axe-plugin-search:typesense`
**Sprint**: 26 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Full-text search plugin với Typesense backend (self-host friendly).

**Acceptance Criteria**:
- [ ] `pkg/plugin/search/typesense/` — TypesensePlugin
- [ ] `search.Index(ctx, collection, doc)` → upsert document
- [ ] `search.Search(ctx, collection, query, opts)` → results
- [ ] Auto-sync: hook vào axe repository layer (optional)
- [ ] `axe generate resource Post --searchable` → add search indexing code
- [ ] Dev mode: embedded Typesense (testcontainers)

### Story 9.4 — `axe-plugin-search:elastic`
**Sprint**: 26 | **Priority**: P3 | **Status**: 🟡 Planned

**Goal**: Elasticsearch adapter — same interface as Typesense plugin.

### Story 9.5 — `axe-plugin-storage:s3`
**Sprint**: 27 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: S3-compatible storage plugin (AWS S3, Cloudflare R2, MinIO).

**Acceptance Criteria**:
- [ ] `pkg/plugin/storage/s3/` — S3Store (implements StorageBackend interface from Story 8.2)
- [ ] Zero code change khi switch từ FSStore → S3Store
- [ ] Presigned URL support (GET/PUT)
- [ ] Multipart upload cho files > 100MB
- [ ] Config: `S3_BUCKET`, `S3_REGION`, `S3_ENDPOINT` (custom endpoint for R2/MinIO)
- [ ] Prometheus: reuse `axe_storage_*` metrics với `backend="s3"` label

### Story 9.6 — `axe-plugin-kafka`
**Sprint**: 27 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Kafka producer/consumer plugin cho event-driven architecture.

**Acceptance Criteria**:
- [ ] `pkg/plugin/kafka/` — KafkaPlugin
- [ ] `kafka.Publish(ctx, topic, key, value)` → error
- [ ] `kafka.Subscribe(topic, handler)` → consumer goroutine
- [ ] Graceful shutdown: drain in-flight messages
- [ ] Dead letter queue support
- [ ] Prometheus: `axe_kafka_messages_published_total`, `axe_kafka_consumer_lag`

### Story 9.7 — `axe-plugin-otel` (OpenTelemetry)
**Sprint**: 28 | **Priority**: P2 | **Status**: ✅ Done

**Goal**: Distributed tracing với OpenTelemetry (OTLP format — works với Jaeger, Grafana Tempo, Datadog).

**Acceptance Criteria**:
- [ ] `pkg/plugin/otel/` — OtelPlugin
- [ ] Auto-instrument HTTP handlers: span per request
- [ ] Auto-instrument DB queries: span per query
- [ ] Propagate trace context qua HTTP headers (W3C TraceContext)
- [ ] Config: `OTEL_EXPORTER_OTLP_ENDPOINT`
- [ ] Dev mode: stdout exporter

### Story 9.8 — `axe-plugin-sentry`
**Sprint**: 28 | **Priority**: P2 | **Status**: 🟡 Planned

**Goal**: Error tracking và performance monitoring với Sentry.

**Acceptance Criteria**:
- [ ] `pkg/plugin/sentry/` — SentryPlugin
- [ ] Auto-capture panics + 5xx errors
- [ ] User context enrichment (user ID từ JWT claims)
- [ ] Breadcrumbs: log entries → Sentry breadcrumbs
- [ ] Config: `SENTRY_DSN`, `SENTRY_ENVIRONMENT`

### Story 9.9 — `axe-plugin-ai:openai`
**Sprint**: 29 | **Priority**: P2 | **Status**: 🟡 Planned

**Goal**: OpenAI integration plugin (ChatGPT, Embeddings, DALL-E).

> **Inspired by**: Spring AI's OpenAI starter — same clean abstraction idea.

**Acceptance Criteria**:
- [ ] `pkg/plugin/ai/openai/` — OpenAIPlugin
- [ ] `ai.Chat(ctx, messages)` → stream/response
- [ ] `ai.Embed(ctx, text)` → `[]float64` (vector)
- [ ] `ai.Image(ctx, prompt)` → URL
- [ ] Rate limit + retry with exponential backoff
- [ ] Streaming response support via SSE
- [ ] Config: `OPENAI_API_KEY`, `OPENAI_MODEL`

**Interface** (shared across all AI plugins):
```go
type AIPlugin interface {
    Plugin
    Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
    ChatStream(ctx context.Context, req ChatRequest) (<-chan ChatChunk, error)
    Embed(ctx context.Context, text string) ([]float64, error)
}
```

### Story 9.10 — `axe-plugin-ai:gemini`
**Sprint**: 29 | **Priority**: P2 | **Status**: 🟡 Planned

**Goal**: Google Gemini integration — same `AIPlugin` interface as OpenAI.

**Acceptance Criteria**:
- [ ] `pkg/plugin/ai/gemini/` — GeminiPlugin (implements AIPlugin)
- [ ] Multimodal support: text + image input
- [ ] Config: `GEMINI_API_KEY`, `GEMINI_MODEL`

### Story 9.11 — `axe-plugin-ai:ollama`
**Sprint**: 29 | **Priority**: P3 | **Status**: 🟡 Planned

**Goal**: Ollama integration — run local LLMs (Llama, Mistral, etc.) với same interface.

**Acceptance Criteria**:
- [ ] `pkg/plugin/ai/ollama/` — OllamaPlugin (implements AIPlugin)
- [ ] Config: `OLLAMA_BASE_URL`, `OLLAMA_MODEL`
- [ ] Zero latency dev mode — no API key needed

### Story 9.12 — `axe-plugin-sms:twilio`
**Sprint**: 30 | **Priority**: P3 | **Status**: 🟡 Planned

**Goal**: SMS sending plugin với Twilio backend.

**Acceptance Criteria**:
- [ ] `pkg/plugin/sms/twilio/` — TwilioPlugin
- [ ] `sms.Send(ctx, to, body)` → error
- [ ] OTP/verification code helper
- [ ] Config: `TWILIO_ACCOUNT_SID`, `TWILIO_AUTH_TOKEN`, `TWILIO_FROM`

### Story 9.13 — Web Project Configurator (`start.axe.io`)
**Sprint**: 30 | **Priority**: P3 | **Status**: 🟡 Planned

**Goal**: Web UI như start.spring.io — generate `main.go` + `axe.yaml` scaffold. Giúp onboarding devs mới không cần đọc docs.

> **Design**: Web UI chỉ là **code generator** — output là Go code tĩnh. Không có runtime magic. Consistent với axe philosophy.

**Acceptance Criteria**:
- [ ] UI tại `start.axe.io` (hosted separately)
- [ ] Cho phép chọn: DB adapter, plugins, project name, module path
- [ ] Generate + download project zip (like start.spring.io)
- [ ] Shareable config URL: `start.axe.io?plugins=auth,storage,email&db=postgres`
- [ ] Preview generated `main.go` trước khi download
- [ ] Tương đương với `axe new` CLI — same output

---

## Priority Matrix

| Story | Plugin | Priority | Sprint Est. | Trigger |
|---|---|---|---|---|
| 9.1 | Stripe payment | P2 | 25 | eCommerce demand |
| 9.3 | Typesense search | P2 | 26 | Content-heavy app demand |
| 9.5 | S3 storage | P2 | 27 | Scale-out storage demand |
| 9.6 | Kafka | P2 | 27 | Event-driven demand |
| 9.7 | OpenTelemetry | P2 | 28 | Enterprise/compliance demand |
| 9.8 | Sentry | P2 | 28 | Error visibility demand |
| 9.9 | OpenAI | P2 | 29 | AI feature demand |
| 9.10 | Gemini | P2 | 29 | AI feature demand |
| 9.2 | PayOS (VN) | P3 | 25 | Vietnam market |
| 9.4 | Elasticsearch | P3 | 26 | Enterprise search demand |
| 9.11 | Ollama (local) | P3 | 29 | Privacy/offline demand |
| 9.12 | SMS/Twilio | P3 | 30 | OTP/auth demand |
| 9.13 | start.axe.io | P3 | 30 | DX / onboarding demand |

---

## Technical Design

### Shared Interface Pattern

Tất cả plugins trong cùng category implement shared interface:

```go
// Storage: FSStore, S3Store, GCSStore tất cả implement
type StorageBackend interface {
    Put(ctx, key, reader) error
    Get(ctx, key) (io.ReadCloser, error)
    Delete(ctx, key) error
}

// AI: OpenAI, Gemini, Ollama tất cả implement
type AIPlugin interface {
    Plugin
    Chat(ctx, req ChatRequest) (*ChatResponse, error)
    Embed(ctx, text string) ([]float64, error)
}

// Payment: Stripe, PayOS tất cả implement
type PaymentPlugin interface {
    Plugin
    Charge(ctx, req ChargeRequest) (*ChargeResult, error)
}
```

### Zero-lock design

```go
// Switch provider mà không đổi business logic:
// Development:
app.Use(storage.New(storage.FSConfig{Path: "./uploads"}))

// Production:
app.Use(storage.New(storage.S3Config{Bucket: "my-bucket", Region: "ap-southeast-1"}))

// Switch AI provider:
app.Use(ai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}))
// → hoặc →
app.Use(ai.New(ollama.Config{BaseURL: "http://localhost:11434"}))
```

---

## Risks

- **Dependency bloat**: Mỗi AI/payment SDK thêm vào `go.mod` → monitor, tách submodule nếu cần
- **API churn**: OpenAI/Gemini API thay đổi thường xuyên → pin SDK versions, test matrix
- **Community plugins**: Third-party plugins có thể không follow quality standards → cần plugin quality guidelines
- **start.axe.io hosting cost**: Cần dedicated infrastructure → consider Vercel/Cloudflare Pages
