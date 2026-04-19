# 🛒 E-Commerce API — axe Example

A **production-grade e-commerce API** built entirely with `axe new` + `axe generate resource`.

Demonstrates: multi-resource CRUD, JWT auth, parent-child relationships, and auto-wiring.

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
Product (name, description, price, stock, image_url)
  └── Review (body, rating) [belongs-to Product]

Order (total, status) [auth required]
```

## How This Was Built

```bash
# Step 1: Scaffold project
axe new ecommerce --module github.com/axe-cute/examples-ecommerce --db postgres --yes

# Step 2: Generate resources (auto-wires main.go)
axe generate resource Product --fields="name:string,description:text,price:float,stock:int,image_url:string" --with-auth
axe generate resource Order   --fields="total:float,status:string" --with-auth
axe generate resource Review  --fields="body:text,rating:int" --belongs-to=Product --with-auth
```

**Total time: ~3 minutes.** Everything compiled and auto-wired.

## API Endpoints

### Products
```bash
# List products (public)
curl http://localhost:8080/products

# Get product by ID
curl http://localhost:8080/products/{id}

# Create product (auth required)
curl -X POST http://localhost:8080/products \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Wireless Mouse","description":"Ergonomic design","price":29.99,"stock":100}'

# Update product
curl -X PUT http://localhost:8080/products/{id} \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Wireless Mouse Pro","price":39.99}'

# Delete product
curl -X DELETE http://localhost:8080/products/{id} \
  -H "Authorization: Bearer $TOKEN"
```

### Orders (Auth Required)
```bash
# Create order
curl -X POST http://localhost:8080/orders \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"total":129.97,"status":"pending"}'

# List my orders
curl http://localhost:8080/orders \
  -H "Authorization: Bearer $TOKEN"
```

### Reviews (belongs-to Product)
```bash
# Create review
curl -X POST http://localhost:8080/reviews \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"body":"Amazing product!","rating":5,"product_id":"..."}'

# List reviews
curl http://localhost:8080/reviews
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
├── domain/                  ← Pure structs + interfaces (no deps)
│   ├── product.go
│   ├── order.go  
│   └── review.go
├── handler/                 ← HTTP handlers (chi router)
│   ├── product_handler.go
│   ├── order_handler.go
│   └── review_handler.go
├── service/                 ← Business logic
│   ├── product_service.go
│   ├── order_service.go
│   └── review_service.go
└── repository/              ← Database queries (Ent ORM)
    ├── product_repo.go
    ├── order_repo.go
    └── review_repo.go
ent/schema/                  ← Ent ORM schemas
db/migrations/               ← SQL migrations
```

## axe Features Demonstrated

| Feature | How |
|---|---|
| `axe new` | Project scaffolded with full infrastructure |
| `axe generate resource` | 3 resources × 9 files = 27 files generated |
| `--with-auth` | JWT authentication on all routes |
| `--belongs-to` | Review → Product foreign key relationship |
| Auto-wiring | `main.go` automatically updated with DI code |
| Clean Architecture | domain → handler → service → repository |
| Ent ORM | Schema-first code generation |

## License

MIT
