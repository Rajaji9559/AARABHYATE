// Package repository — ProductRepository
//
// Stage 4 additions:
//   - ListActive: filtered storefront query with full-text search, category,
//     price range, in-stock flag, and a total-count for pagination metadata.
//   - GetByIDForUpdate: SELECT … FOR UPDATE for use inside order transactions.
//   - Update now handles is_active, category, sku.
package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/jmoiron/sqlx"
)

// ProductRepository defines all data-access operations for the products table.
type ProductRepository interface {
	// ── Admin / Internal ─────────────────────────────────────────────────────
	Create(ctx context.Context, p *models.Product) error
	GetByID(ctx context.Context, id string) (*models.Product, error)
	Update(ctx context.Context, p *models.Product) error
	Delete(ctx context.Context, id string) error

	// GetByIDForUpdate fetches a product row with a pessimistic write-lock (SELECT FOR UPDATE).
	// Must be called inside an active sqlx.Tx — used by the order transaction to prevent overselling.
	GetByIDForUpdate(ctx context.Context, tx *sqlx.Tx, id string) (*models.Product, error)

	// ── Storefront ────────────────────────────────────────────────────────────
	// List returns ALL products (admin catalogue, no active filter).
	List(ctx context.Context, limit, offset int) ([]*models.Product, error)

	// ListActive returns only is_active = TRUE products with optional filters.
	// Returns the matching page AND the total count for pagination metadata.
	ListActive(ctx context.Context, f models.ProductFilter) ([]*models.Product, int, error)

	// UpdateStock atomically adjusts stock by delta (negative = decrement).
	// Enforces the CHECK (stock >= 0) constraint at DB level.
	UpdateStock(ctx context.Context, id string, delta int) error
}

type productRepository struct{ db *sqlx.DB }

// NewProductRepository returns a concrete ProductRepository backed by Postgres.
func NewProductRepository(db *sqlx.DB) ProductRepository { return &productRepository{db: db} }

// ── Create ─────────────────────────────────────────────────────────────────────

func (r *productRepository) Create(ctx context.Context, p *models.Product) error {
	const q = `
		INSERT INTO products
			(id, name, description, technical_specs, price, stock, image_url,
			 is_active, category, sku, created_at, updated_at)
		VALUES
			(gen_random_uuid(), :name, :description, :technical_specs, :price, :stock, :image_url,
			 :is_active, :category, :sku, NOW(), NOW())
		RETURNING id, created_at, updated_at`

	rows, err := r.db.NamedQueryContext(ctx, q, p)
	if err != nil {
		return fmt.Errorf("productRepository.Create: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return rows.StructScan(p)
	}
	return fmt.Errorf("productRepository.Create: no row returned")
}

// ── GetByID ────────────────────────────────────────────────────────────────────

func (r *productRepository) GetByID(ctx context.Context, id string) (*models.Product, error) {
	p := &models.Product{}
	if err := r.db.GetContext(ctx, p,
		`SELECT * FROM products WHERE id = $1`, id); err != nil {
		return nil, fmt.Errorf("productRepository.GetByID: %w", err)
	}
	return p, nil
}

// GetByIDForUpdate acquires a row-level write lock on the product inside an open TX.
// This prevents concurrent checkouts from reading a stale stock value before we decrement.
//
//	-- Equivalent SQL:
//	SELECT * FROM products WHERE id = $1 FOR UPDATE;
func (r *productRepository) GetByIDForUpdate(ctx context.Context, tx *sqlx.Tx, id string) (*models.Product, error) {
	p := &models.Product{}
	if err := tx.GetContext(ctx, p,
		`SELECT * FROM products WHERE id = $1 FOR UPDATE`, id); err != nil {
		return nil, fmt.Errorf("productRepository.GetByIDForUpdate: %w", err)
	}
	return p, nil
}

// ── Update ─────────────────────────────────────────────────────────────────────

func (r *productRepository) Update(ctx context.Context, p *models.Product) error {
	const q = `
		UPDATE products
		SET name            = :name,
		    description     = :description,
		    technical_specs = :technical_specs,
		    price           = :price,
		    stock           = :stock,
		    image_url       = :image_url,
		    is_active       = :is_active,
		    category        = :category,
		    sku             = :sku,
		    updated_at      = NOW()
		WHERE id = :id`
	if _, err := r.db.NamedExecContext(ctx, q, p); err != nil {
		return fmt.Errorf("productRepository.Update: %w", err)
	}
	return nil
}

// ── Delete ─────────────────────────────────────────────────────────────────────

func (r *productRepository) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM products WHERE id = $1`, id); err != nil {
		return fmt.Errorf("productRepository.Delete: %w", err)
	}
	return nil
}

// ── List (admin, no active filter) ────────────────────────────────────────────

func (r *productRepository) List(ctx context.Context, limit, offset int) ([]*models.Product, error) {
	var products []*models.Product
	if err := r.db.SelectContext(ctx, &products,
		`SELECT * FROM products ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	); err != nil {
		return nil, fmt.Errorf("productRepository.List: %w", err)
	}
	return products, nil
}

// ── ListActive (storefront, filtered, paginated) ───────────────────────────────

// ListActive builds a dynamic WHERE clause based on the ProductFilter fields that are set,
// then executes two queries: one for the page data and one for the total count.
// It returns (products, totalCount, error).
//
// SQL contract:
//   - Always filters is_active = TRUE
//   - Full-text search uses the GIN index created in 004_product_active.sql
//   - Price range, category, in-stock are applied if non-zero/non-empty
func (r *productRepository) ListActive(
	ctx context.Context,
	f models.ProductFilter,
) ([]*models.Product, int, error) {
	// ── 1. Build parameterised WHERE clause ───────────────────────────────────
	conds := []string{"is_active = TRUE"}
	args  := []any{}
	i     := 1 // $1, $2, … argument index

	if f.Search != "" {
		// Full-text search using tsvector — matches the GIN index on name+description
		conds = append(conds,
			fmt.Sprintf("to_tsvector('english', coalesce(name,'') || ' ' || coalesce(description,'')) @@ plainto_tsquery('english', $%d)", i))
		args = append(args, f.Search)
		i++
	}
	if f.Category != "" {
		conds = append(conds, fmt.Sprintf("category = $%d", i))
		args = append(args, f.Category)
		i++
	}
	if f.MinPrice > 0 {
		conds = append(conds, fmt.Sprintf("price >= $%d", i))
		args = append(args, f.MinPrice)
		i++
	}
	if f.MaxPrice > 0 {
		conds = append(conds, fmt.Sprintf("price <= $%d", i))
		args = append(args, f.MaxPrice)
		i++
	}
	if f.InStock {
		conds = append(conds, "stock > 0")
	}

	where := "WHERE " + strings.Join(conds, " AND ")

	// ── 2. Count total matching rows (for pagination metadata) ────────────────
	var total int
	countQ := fmt.Sprintf("SELECT COUNT(*) FROM products %s", where)
	if err := r.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("productRepository.ListActive: count: %w", err)
	}

	// ── 3. Fetch the requested page ───────────────────────────────────────────
	// Append LIMIT and OFFSET at the end of the arg list
	args = append(args, f.Limit, f.Offset)
	dataQ := fmt.Sprintf(`
		SELECT *
		FROM products
		%s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`,
		where, i, i+1)

	var products []*models.Product
	if err := r.db.SelectContext(ctx, &products, dataQ, args...); err != nil {
		return nil, 0, fmt.Errorf("productRepository.ListActive: select: %w", err)
	}

	return products, total, nil
}

// ── UpdateStock ────────────────────────────────────────────────────────────────

// UpdateStock atomically adjusts stock by delta (positive = add, negative = reduce).
// The DB CHECK (stock >= 0) constraint prevents the balance from going negative.
// NOTE: for order transactions, stock is decremented directly inside the TX using
// the FOR UPDATE pattern — this method is for standalone adjustments (e.g., restocking).
func (r *productRepository) UpdateStock(ctx context.Context, id string, delta int) error {
	const q = `UPDATE products SET stock = stock + $1, updated_at = NOW() WHERE id = $2`
	if _, err := r.db.ExecContext(ctx, q, delta, id); err != nil {
		return fmt.Errorf("productRepository.UpdateStock: %w", err)
	}
	return nil
}
