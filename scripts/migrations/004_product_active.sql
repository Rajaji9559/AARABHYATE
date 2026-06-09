-- =============================================================================
-- AARABHYATE — Stage 4 Migration: Product Catalogue Enhancements
-- Run: psql -U <user> -d aarabhyate_db -f scripts/migrations/004_product_active.sql
-- =============================================================================

-- ── is_active flag ────────────────────────────────────────────────────────────
-- Allows soft-disabling products without deleting them.
-- Only is_active = TRUE products are shown in the public storefront.
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS is_active BOOLEAN NOT NULL DEFAULT TRUE;

-- ── category ─────────────────────────────────────────────────────────────────
-- Optional free-text category tag for storefront filtering (e.g. 'sensors', 'actuators').
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS category VARCHAR(100);

-- ── sku ───────────────────────────────────────────────────────────────────────
-- Optional unique stock-keeping unit for inventory management.
ALTER TABLE products
    ADD COLUMN IF NOT EXISTS sku VARCHAR(100);

ALTER TABLE products
    ADD CONSTRAINT uq_products_sku UNIQUE (sku);

-- ── Indexes ───────────────────────────────────────────────────────────────────
-- Storefront query: active products, ordered by name / created_at
CREATE INDEX IF NOT EXISTS idx_products_is_active  ON products (is_active);
CREATE INDEX IF NOT EXISTS idx_products_category   ON products (category) WHERE category IS NOT NULL;

-- Full-text search index for name + description (GIN tsvector)
CREATE INDEX IF NOT EXISTS idx_products_fts ON products
    USING GIN (to_tsvector('english', coalesce(name,'') || ' ' || coalesce(description,'')));

-- ── Backfill: set all existing products to active ─────────────────────────────
UPDATE products SET is_active = TRUE WHERE is_active IS NULL;
