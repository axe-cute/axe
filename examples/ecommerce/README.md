# 🛒 E-Commerce API — axe Example

A **production-grade e-commerce API** built with `axe new` + `axe generate resource`, then customized with real business logic.

## Quick Start

```bash
docker-compose up -d          # PostgreSQL + Redis
cp .env.example .env
make migrate-up
make run                      # API at :8080
```

## Domain Model

```
Product (name, description, price, stock, image_url)
  └── Review (body, rating 1-5) [belongs-to Product]

Order (user_id, total, status, items[])
  Status machine: pending → confirmed → shipped → delivered
                   pending → cancelled
                   confirmed → cancelled
```

## How This Was Built

```bash
# Step 1: Scaffold (2 minutes)
axe new ecommerce --module github.com/axe-cute/examples-ecommerce --db postgres --yes

# Step 2: Generate resources (auto-wires main.go)
axe generate resource Product --fields="name:string,description:text,price:float,stock:int,image_url:string" --with-auth
axe generate resource Order   --fields="total:float,status:string" --with-auth
axe generate resource Review  --fields="body:text,rating:int" --belongs-to=Product --with-auth

# Step 3: Add business logic (manual — see below)
```

## Business Logic (Beyond CRUD)

This project demonstrates **real production patterns** that can't be auto-generated:

### 1. PlaceOrder — Transactional Flow
`POST /api/v1/orders/place` validates stock, calculates total from product prices, creates the order, and deducts inventory.

```bash
curl -X POST http://localhost:8080/api/v1/orders/place \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "items": [
      {"product_id": "PRODUCT_UUID_1", "quantity": 2},
      {"product_id": "PRODUCT_UUID_2", "quantity": 1}
    ]
  }'
```

**Response:**
```json
{
  "id": "order-uuid",
  "user_id": "user-from-jwt",
  "total": 89.97,
  "status": "pending",
  "items": [
    {"product_id": "...", "product_name": "Wireless Mouse", "quantity": 2, "unit_price": 29.99},
    {"product_id": "...", "product_name": "USB Cable", "quantity": 1, "unit_price": 29.99}
  ]
}
```

### 2. Order Status Machine
`PUT /api/v1/orders/{id}/status` validates state transitions before applying.

```bash
# Confirm a pending order
curl -X PUT http://localhost:8080/api/v1/orders/{id}/status \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"status": "confirmed"}'

# Ship a confirmed order
curl -X PUT http://localhost:8080/api/v1/orders/{id}/status \
  -d '{"status": "shipped"}'

# Invalid transition → 400 error
curl -X PUT http://localhost:8080/api/v1/orders/{id}/status \
  -d '{"status": "pending"}'
# → "cannot transition from \"shipped\" to \"pending\""
```

### 3. Product Validation
- Name required, price > 0, stock ≥ 0
- Invalid input returns structured error response

### 4. Review Validation
- Rating must be 1-5
- Body is required
- ProductID verified before creation

## API Endpoints

### Products
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| GET | `/api/v1/products` | No | List all products |
| GET | `/api/v1/products/{id}` | No | Get product details |
| POST | `/api/v1/products` | Yes | Create product (validates name, price, stock) |
| PUT | `/api/v1/products/{id}` | Yes | Update product |
| DELETE | `/api/v1/products/{id}` | Yes | Delete product |

### Orders
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/orders/place` | Yes | **PlaceOrder** — validates stock, creates order + items, deducts inventory |
| PUT | `/api/v1/orders/{id}/status` | Yes | **UpdateStatus** — validates state transitions |
| GET | `/api/v1/orders` | Yes | List orders |
| GET | `/api/v1/orders/{id}` | Yes | Get order details |
| POST | `/api/v1/orders` | Yes | Create order (basic) |
| DELETE | `/api/v1/orders/{id}` | Yes | Cancel order |

### Reviews
| Method | Path | Auth | Description |
|--------|------|------|-------------|
| POST | `/api/v1/reviews` | Yes | Create review (rating 1-5 validated) |
| GET | `/api/v1/reviews` | No | List reviews |
| GET | `/api/v1/reviews/{id}` | No | Get review |
| DELETE | `/api/v1/reviews/{id}` | Yes | Delete review |

### Infrastructure
| Method | Path | Description |
|--------|------|-------------|
| GET | `/health` | Liveness probe |
| GET | `/ready` | Readiness probe (checks DB) |
| GET | `/metrics` | Prometheus metrics |

## Architecture

```
cmd/api/main.go              ← Composition root (auto-wired by axe)
internal/
├── domain/                  ← Pure domain models + business rules
│   ├── product.go           ← Product entity + validation
│   ├── order.go             ← Order + OrderItem + status machine + PlaceOrderInput
│   └── review.go            ← Review entity
├── handler/                 ← HTTP handlers
│   ├── product_handler.go   ← Standard CRUD
│   ├── order_handler.go     ← CRUD + PlaceOrder + UpdateStatus
│   └── review_handler.go    ← CRUD with rating validation
├── service/                 ← Business logic layer
│   ├── product_service.go   ← Input validation
│   ├── order_service.go     ← PlaceOrder flow + status machine
│   └── review_service.go    ← Rating 1-5 validation
└── repository/              ← Database access (Ent ORM)
```

## axe Features Demonstrated

| Feature | Implementation |
|---|---|
| `axe new` | Full project scaffold with Docker, migrations, CI |
| `axe generate resource` | 3 resources × 9 files = 27 files auto-generated |
| `--with-auth` | JWT auth middleware on all routes |
| `--belongs-to` | Review → Product foreign key |
| Auto-wiring | main.go DI code automatically injected |
| **Custom business logic** | PlaceOrder, status machine, validation |
| Clean Architecture | domain → handler → service → repository |

## License

MIT
