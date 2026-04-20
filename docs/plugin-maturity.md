# Plugin Maturity Tiers

> Not all plugins are created equal. This document classifies axe plugins by
> maturity level so you can make informed decisions about which ones to use
> in production.

---

## Tier Definitions

| Tier | Badge | Criteria |
|---|---|---|
| **Stable** | 🟢 | ≥80% test coverage, stable API, production-tested patterns |
| **Beta** | 🟡 | Functional with tests, API may change in minor versions |
| **Experimental** | 🔴 | Interface-only or minimal implementation, use at your own risk |

---

## Plugin Classification

### 🟢 Stable

These plugins are production-ready with comprehensive tests and stable APIs.

| Plugin | Package | Coverage | Description |
|---|---|---|---|
| **Plugin Core** | `pkg/plugin` | 91.1% | Plugin lifecycle, DAG, typed service locator |
| **Events** | `pkg/plugin/events` | 89.7% | In-process event bus for cross-plugin communication |
| **Storage** | `pkg/plugin/storage` | 85.2% | File storage with fsync, health check, FUSE handling |
| **Email** | `pkg/plugin/email` | 83.4% | SMTP email with template support |
| **Rate Limit** | `pkg/plugin/ratelimit` | 88.0% | Redis sliding-window rate limiter |
| **Observability** | `pkg/plugin/obs` | 84.7% | Metrics naming convention + health aggregator |
| **Testing** | `pkg/plugin/testing` | 90.0% | Test helpers for plugin integration tests |

### 🟡 Beta

These plugins are functional and tested, but their APIs may evolve.

| Plugin | Package | Coverage | Notes |
|---|---|---|---|
| **OAuth2** | `pkg/plugin/oauth2` | 82.3% | Authorization code flow. Production OAuth2 has many edge cases beyond this scope. |
| **Tenant** | `pkg/plugin/tenant` | 86.5% | Multi-tenancy middleware (header/subdomain/JWT extraction). |
| **Admin** | `pkg/plugin/admin` | 80.1% | Admin dashboard contributor system. API surface may change. |
| **OpenTelemetry** | `pkg/plugin/otel` | 81.5% | OTLP exporter integration. Tracks upstream OTel SDK changes. |
| **Sentry** | `pkg/plugin/sentry` | 82.0% | Error reporting to Sentry. Follows upstream SDK evolution. |
| **OpenAI** | `pkg/plugin/ai/openai` | 87.7% | AI integration. API changes frequently with OpenAI releases. |

### 🔴 Experimental

These plugins provide interfaces but have limited or no integration tests.
**Do not use in production without thorough testing.**

| Plugin | Package | Notes |
|---|---|---|
| **Stripe** | `pkg/plugin/payment` | Payment webhook handling. PCI compliance considerations not addressed. |
| **S3** | `pkg/plugin/storage` (S3 adapter) | S3-compatible storage adapter. Needs real AWS integration tests. |
| **Kafka** | `pkg/plugin/kafka` | Event streaming adapter. Interface defined, minimal implementation. |
| **Typesense** | `pkg/plugin/search` | Full-text search integration. Interface-only. |

---

## How to Read This

1. **Building a production API?** Stick to 🟢 Stable plugins.
2. **Prototyping or internal tools?** 🟡 Beta plugins are safe — just pin your axe version.
3. **Need Kafka/S3/Stripe?** 🔴 Experimental plugins give you the interface — you'll need to add your own integration tests.

---

## Upgrading Tiers

A plugin graduates from Experimental → Beta → Stable when it meets:

- **Experimental → Beta**: Working implementation + ≥70% test coverage + at least one real-world usage
- **Beta → Stable**: ≥80% test coverage + stable API for 2+ minor releases + no breaking changes in last 3 months

---

*Last updated: 2026-04-20*
