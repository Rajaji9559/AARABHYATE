-- =============================================================================
-- AARABHYATE RESEARCH GROUP — PostgreSQL Schema
-- Run: psql -U <user> -d <database> -f scripts/schema.sql
-- =============================================================================

-- Enable pgcrypto for UUID generation
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- =============================================================================
-- ENUM TYPES
-- =============================================================================

DO $$ BEGIN
    CREATE TYPE user_role AS ENUM ('customer', 'admin');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    CREATE TYPE order_status AS ENUM ('pending', 'processing', 'shipped', 'delivered', 'cancelled');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- =============================================================================
-- USERS
-- =============================================================================

CREATE TABLE IF NOT EXISTS users (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    name          VARCHAR(255) NOT NULL,
    email         VARCHAR(320) NOT NULL UNIQUE,
    password_hash TEXT         NOT NULL,
    role          user_role    NOT NULL DEFAULT 'customer',
    created_at    TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_users_email ON users (email);
CREATE INDEX IF NOT EXISTS idx_users_role  ON users (role);

-- =============================================================================
-- PRODUCTS
-- =============================================================================

CREATE TABLE IF NOT EXISTS products (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(255) NOT NULL,
    description     TEXT,
    technical_specs JSONB        NOT NULL DEFAULT '{}',
    price           NUMERIC(12, 2) NOT NULL CHECK (price >= 0),
    stock           INTEGER        NOT NULL DEFAULT 0 CHECK (stock >= 0),
    image_url       TEXT,
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- GIN index for fast JSONB querying (e.g. filter by spec key/value)
CREATE INDEX IF NOT EXISTS idx_products_technical_specs ON products USING GIN (technical_specs);
CREATE INDEX IF NOT EXISTS idx_products_name           ON products (name);

-- =============================================================================
-- ORDERS
-- =============================================================================

CREATE TABLE IF NOT EXISTS orders (
    id               UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id          UUID           NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    total_amount     NUMERIC(12, 2) NOT NULL CHECK (total_amount >= 0),
    status           order_status   NOT NULL DEFAULT 'pending',
    shipping_address TEXT           NOT NULL,
    phone            VARCHAR(20)    NOT NULL,
    created_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ    NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_user_id ON orders (user_id);
CREATE INDEX IF NOT EXISTS idx_orders_status  ON orders (status);

-- =============================================================================
-- ORDER ITEMS
-- =============================================================================

CREATE TABLE IF NOT EXISTS order_items (
    id         UUID           PRIMARY KEY DEFAULT gen_random_uuid(),
    order_id   UUID           NOT NULL REFERENCES orders   (id) ON DELETE CASCADE,
    product_id UUID           NOT NULL REFERENCES products (id) ON DELETE RESTRICT,
    quantity   INTEGER        NOT NULL CHECK (quantity > 0),
    price      NUMERIC(12, 2) NOT NULL CHECK (price >= 0)  -- unit price at time of purchase
);

CREATE INDEX IF NOT EXISTS idx_order_items_order_id   ON order_items (order_id);
CREATE INDEX IF NOT EXISTS idx_order_items_product_id ON order_items (product_id);

-- =============================================================================
-- AUTO-UPDATE updated_at via trigger
-- =============================================================================

CREATE OR REPLACE FUNCTION trigger_set_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS set_updated_at ON products;
CREATE TRIGGER set_updated_at
    BEFORE UPDATE ON products
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();

DROP TRIGGER IF EXISTS set_updated_at ON orders;
CREATE TRIGGER set_updated_at
    BEFORE UPDATE ON orders
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
