# AARABHYATE Research Group — Backend API

A high-performance REST API for the AARABHYATE robotics platform, built with Go and PostgreSQL.

## Tech Stack

| Layer | Technology |
|---|---|
| Language | Go 1.22+ |
| Router | chi v5 |
| Database | PostgreSQL 15+ |
| DB Driver | sqlx + lib/pq |
| Auth | JWT (HS256) |
| Passwords | bcrypt |

## Project Structure

```
aarabhyate/
├── cmd/
│   └── api/
│       └── main.go            # Entry point — server bootstrap & graceful shutdown
├── internal/
│   ├── config/
│   │   └── config.go          # Env-based config with validation
│   ├── database/
│   │   └── db.go              # sqlx connection pool initialisation
│   ├── models/
│   │   └── models.go          # Domain structs with db/json tags & DTOs
│   ├── repository/
│   │   ├── repository.go      # DI container for all repositories
│   │   ├── user_repository.go
│   │   ├── product_repository.go
│   │   └── order_repository.go
│   ├── handlers/
│   │   ├── handlers.go        # Handler DI container
│   │   ├── helpers.go         # respondJSON, respondError, pagination
│   │   ├── auth_handler.go    # POST /auth/register, POST /auth/login
│   │   ├── user_handler.go    # GET/PUT /me, admin user management
│   │   ├── product_handler.go # Product CRUD
│   │   └── order_handler.go   # Order placement & management
│   ├── middleware/
│   │   └── middleware.go      # JWT auth, AdminOnly guard, Logger
│   └── router/
│       └── router.go          # chi router with all route groups
├── scripts/
│   └── schema.sql             # PostgreSQL DDL — run this first
├── .env.example               # Environment variable template
├── .gitignore
├── go.mod
└── README.md
```

## Quick Start

### 1. Prerequisites

- Go 1.22+
- PostgreSQL 15+

### 2. Clone & Configure

```bash
# Copy and edit your environment
cp .env.example .env
# Fill in DATABASE_URL and JWT_SECRET
```

### 3. Create Database & Run Schema

```bash
createdb aarabhyate_db
psql -U <your_user> -d aarabhyate_db -f scripts/schema.sql
```

### 4. Download Dependencies

```bash
go mod tidy
```

### 5. Run the Server

```bash
go run ./cmd/api
```

The API will be available at `http://localhost:8080`.

## API Endpoints

### Public

| Method | Path | Description |
|---|---|---|
| POST | `/api/v1/auth/register` | Register a new user |
| POST | `/api/v1/auth/login` | Login and receive JWT |
| GET | `/api/v1/products` | List products (paginated) |
| GET | `/api/v1/products/{id}` | Get product detail |
| GET | `/health` | Health check |

### Authenticated (Bearer JWT required)

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/me` | Get own profile |
| PUT | `/api/v1/me` | Update own profile |
| POST | `/api/v1/orders` | Place a new order |
| GET | `/api/v1/orders` | List own orders |
| GET | `/api/v1/orders/{id}` | Get order detail |

### Admin only

| Method | Path | Description |
|---|---|---|
| GET | `/api/v1/admin/users` | List all users |
| GET | `/api/v1/admin/users/{id}` | Get user by ID |
| DELETE | `/api/v1/admin/users/{id}` | Delete user |
| POST | `/api/v1/admin/products` | Create product |
| PUT | `/api/v1/admin/products/{id}` | Update product |
| DELETE | `/api/v1/admin/products/{id}` | Delete product |
| GET | `/api/v1/admin/orders` | List all orders |
| PATCH | `/api/v1/admin/orders/{id}/status` | Update order status |

## Pagination

All list endpoints support `?limit=20&offset=0` query parameters. Maximum limit is 100.

## Environment Variables

| Variable | Default | Required | Description |
|---|---|---|---|
| `PORT` | `8080` | No | HTTP server port |
| `APP_ENV` | `development` | No | Environment name |
| `DATABASE_URL` | — | **Yes** | PostgreSQL DSN |
| `DB_MAX_OPEN` | `25` | No | Max open DB connections |
| `DB_MAX_IDLE` | `5` | No | Max idle DB connections |
| `DB_CONN_TTL` | `5m` | No | Connection max lifetime |
| `JWT_SECRET` | — | **Yes** | HS256 signing key |
| `JWT_EXPIRATION` | `24h` | No | Token TTL |
