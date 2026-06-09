// Package repository — OrderRepository
//
// Stage 4 complete rewrite of the Create transaction:
//
//  1. Takes []*models.CheckoutItem (server-side prices, never client-supplied)
//  2. Locks each product row with SELECT … FOR UPDATE before reading stock —
//     eliminates the Time-of-Check / Time-of-Use (TOCTOU) race that allows overselling
//     under concurrent checkout traffic
//  3. Validates availability inside the TX, after the lock is held
//  4. Decrements stock atomically inside the same TX
//  5. On any failure, defers tx.Rollback() restores all locks and stock changes
//
// Concurrency model:
//
//	Goroutine A and B check out the last unit of product P simultaneously.
//	Without FOR UPDATE:  both read stock=1, both proceed, stock goes to -1.
//	With FOR UPDATE:     A acquires the lock, B blocks. A commits (stock=0).
//	                     B unblocks, reads stock=0, returns "insufficient stock".
package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/jmoiron/sqlx"
)

// OrderRepository defines all data-access operations for the orders / order_items tables.
type OrderRepository interface {
	// Create persists a new order with its items inside a single ACID transaction.
	// It accepts []*models.CheckoutItem whose prices have been fetched server-side.
	Create(ctx context.Context, o *models.Order, items []*models.CheckoutItem) error

	GetByID(ctx context.Context, id string) (*models.Order, error)
	GetByUserID(ctx context.Context, userID string, limit, offset int) ([]*models.Order, error)
	UpdateStatus(ctx context.Context, id string, status models.OrderStatus) error
	List(ctx context.Context, limit, offset int) ([]*models.Order, error)

	// ListWithUserDetails returns a JOIN projection of orders + users + item count
	// for the admin dashboard, with optional status filter and pagination.
	// Also returns the total count (for pagination) and aggregate stats (for the summary cards).
	ListWithUserDetails(
		ctx context.Context,
		status string,
		search string,
		limit, offset int,
	) ([]*models.AdminOrderView, int, models.AdminOrderStats, error)
}

type orderRepository struct{ db *sqlx.DB }

// NewOrderRepository returns a concrete OrderRepository backed by Postgres.
func NewOrderRepository(db *sqlx.DB) OrderRepository { return &orderRepository{db: db} }

// =============================================================================
// Create — ACID transactional order placement
// =============================================================================

// Create persists an order and all its items in a single database transaction
// with pessimistic locking to prevent overselling.
//
// Transaction steps (all-or-nothing):
//
//	① BEGIN
//	② For each item: SELECT … FOR UPDATE  (acquires a write lock on the product row)
//	③ Validate: product must be is_active=TRUE and stock >= quantity
//	④ INSERT into orders
//	⑤ For each item:
//	     INSERT into order_items  (price stored at time of purchase, immutable)
//	     UPDATE products SET stock = stock - qty  (within same TX, under the lock)
//	⑥ COMMIT  (releases all locks atomically)
//	   If any step fails → deferred tx.Rollback()
func (r *orderRepository) Create(
	ctx context.Context,
	o *models.Order,
	items []*models.CheckoutItem,
) error {
	// ── ① Begin transaction ───────────────────────────────────────────────────
	tx, err := r.db.BeginTxx(ctx, nil)
	if err != nil {
		return fmt.Errorf("orderRepository.Create: begin tx: %w", err)
	}
	// Guard: on any non-nil err path, roll back the transaction.
	// If Commit() succeeded, err will be nil and Rollback() is a no-op.
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	// ── ② + ③ Lock product rows and validate availability ────────────────────
	// We do this BEFORE inserting the order so that we fail fast if any product
	// is unavailable, without creating a dangling order row.
	for _, item := range items {
		var p models.Product
		if err = tx.GetContext(ctx, &p,
			`SELECT id, name, price, stock, is_active FROM products WHERE id = $1 FOR UPDATE`,
			item.ProductID,
		); err != nil {
			return fmt.Errorf(
				"orderRepository.Create: lock product %s: %w", item.ProductID, err,
			)
		}

		if !p.IsActive {
			err = fmt.Errorf("product %q is no longer available", p.Name)
			return err
		}
		if p.Stock < item.Quantity {
			err = fmt.Errorf(
				"insufficient stock for %q: requested %d, available %d",
				p.Name, item.Quantity, p.Stock,
			)
			return err
		}
	}

	// ── ④ Insert the order header ─────────────────────────────────────────────
	// total_amount is pre-computed by the handler (sum of price * quantity),
	// using the same server-side prices that were passed in CheckoutItem.
	const qOrder = `
		INSERT INTO orders
			(id, user_id, total_amount, status, shipping_address, phone, created_at, updated_at)
		VALUES
			(gen_random_uuid(), $1, $2, $3, $4, $5, NOW(), NOW())
		RETURNING id, created_at, updated_at`

	if err = tx.QueryRowxContext(ctx, qOrder,
		o.UserID, o.TotalAmount, o.Status, o.ShippingAddress, o.Phone,
	).StructScan(o); err != nil {
		return fmt.Errorf("orderRepository.Create: insert order: %w", err)
	}

	// ── ⑤ Insert order items and decrement stock (inside the same TX) ─────────
	const qItem = `
		INSERT INTO order_items (id, order_id, product_id, quantity, price)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		RETURNING id`

	const qDecrStock = `
		UPDATE products
		SET stock     = stock - $1,
		    updated_at = NOW()
		WHERE id = $2
		  AND stock >= $1`   // redundant safety guard; the FOR UPDATE check above is the real guard

	for _, item := range items {
		// Persist the line item (price is the authoritative DB price at order time)
		var itemID string
		if err = tx.QueryRowContext(ctx, qItem,
			o.ID, item.ProductID, item.Quantity, item.Price,
		).Scan(&itemID); err != nil {
			return fmt.Errorf(
				"orderRepository.Create: insert order_item (product %s): %w",
				item.ProductID, err,
			)
		}

		// Decrement stock — the FOR UPDATE lock above guarantees no concurrent
		// writer has changed the value between steps ② and ⑤
		res, execErr := tx.ExecContext(ctx, qDecrStock, item.Quantity, item.ProductID)
		if execErr != nil {
			err = fmt.Errorf(
				"orderRepository.Create: decrement stock (product %s): %w",
				item.ProductID, execErr,
			)
			return err
		}
		// Sanity check: rows affected should always be 1 after the lock validation above.
		// A 0-row result here would indicate a logic bug.
		if n, _ := res.RowsAffected(); n == 0 {
			err = fmt.Errorf(
				"orderRepository.Create: stock decrement returned 0 rows for product %s (logic bug)",
				item.ProductID,
			)
			return err
		}
	}

	// ── ⑥ Commit ──────────────────────────────────────────────────────────────
	if err = tx.Commit(); err != nil {
		return fmt.Errorf("orderRepository.Create: commit: %w", err)
	}
	return nil
}

// =============================================================================
// Read operations
// =============================================================================

// GetByID fetches an order with its items eagerly loaded.
func (r *orderRepository) GetByID(ctx context.Context, id string) (*models.Order, error) {
	o := &models.Order{}
	if err := r.db.GetContext(ctx, o,
		`SELECT * FROM orders WHERE id = $1`, id); err != nil {
		return nil, fmt.Errorf("orderRepository.GetByID: %w", err)
	}
	if err := r.db.SelectContext(ctx, &o.Items,
		`SELECT * FROM order_items WHERE order_id = $1 ORDER BY id`, id); err != nil {
		return nil, fmt.Errorf("orderRepository.GetByID: load items: %w", err)
	}
	return o, nil
}

// GetByUserID returns a user's orders, newest first, with pagination and eagerly loaded items.
func (r *orderRepository) GetByUserID(
	ctx context.Context, userID string, limit, offset int,
) ([]*models.Order, error) {
	// 1. Get paginated order IDs for this user
	var ids []string
	err := r.db.SelectContext(ctx, &ids, 
		`SELECT id FROM orders WHERE user_id = $1 ORDER BY created_at DESC LIMIT $2 OFFSET $3`, 
		userID, limit, offset)
	if err != nil {
		return nil, fmt.Errorf("orderRepository.GetByUserID: fetch ids: %w", err)
	}
	
	if len(ids) == 0 {
		return []*models.Order{}, nil
	}

	// 2. Fetch joined rows for these order IDs using the explicit inner join
	query, args, err := sqlx.In(`
		SELECT 
			o.id, o.total_amount, o.status, o.created_at, o.shipping_address, o.phone,
			oi.quantity, oi.price, 
			p.name AS product_name
		FROM orders o
		JOIN order_items oi ON o.id = oi.order_id
		JOIN products p ON oi.product_id = p.id
		WHERE o.id IN (?)
		ORDER BY o.created_at DESC`, ids)
	if err != nil {
		return nil, fmt.Errorf("orderRepository.GetByUserID: sqlx.In: %w", err)
	}
	query = r.db.Rebind(query)

	type joinRow struct {
		ID              string             `db:"id"`
		TotalAmount     float64            `db:"total_amount"`
		Status          models.OrderStatus `db:"status"`
		CreatedAt       time.Time          `db:"created_at"`
		ShippingAddress string             `db:"shipping_address"`
		Phone           string             `db:"phone"`
		Quantity        int                `db:"quantity"`
		Price           float64            `db:"price"`
		ProductName     string             `db:"product_name"`
	}

	var rows []joinRow
	if err := r.db.SelectContext(ctx, &rows, query, args...); err != nil {
		return nil, fmt.Errorf("orderRepository.GetByUserID: query rows: %w", err)
	}

	// 3. Group flat rows into hierarchical models.Order slice preserving order of ids
	orderMap := make(map[string]*models.Order)
	var orders []*models.Order

	for _, id := range ids {
		orderMap[id] = nil
	}

	for _, row := range rows {
		o := orderMap[row.ID]
		if o == nil {
			o = &models.Order{
				ID:              row.ID,
				TotalAmount:     row.TotalAmount,
				Status:          row.Status,
				ShippingAddress: row.ShippingAddress,
				Phone:           row.Phone,
				CreatedAt:       row.CreatedAt,
				Items:           []*models.OrderItem{},
			}
			orderMap[row.ID] = o
			orders = append(orders, o)
		}

		o.Items = append(o.Items, &models.OrderItem{
			Quantity:  row.Quantity,
			Price:     row.Price,
			Product: &models.Product{
				Name: row.ProductName,
			},
		})
	}

	return orders, nil
}

// UpdateStatus transitions an order to a new status (admin action).
func (r *orderRepository) UpdateStatus(
	ctx context.Context, id string, status models.OrderStatus,
) error {
	const q = `UPDATE orders SET status = $1, updated_at = NOW() WHERE id = $2`
	if _, err := r.db.ExecContext(ctx, q, status, id); err != nil {
		return fmt.Errorf("orderRepository.UpdateStatus: %w", err)
	}
	return nil
}

// List returns all orders, newest first, with pagination (admin view).
func (r *orderRepository) List(ctx context.Context, limit, offset int) ([]*models.Order, error) {
	var orders []*models.Order
	if err := r.db.SelectContext(ctx, &orders,
		`SELECT * FROM orders ORDER BY created_at DESC LIMIT $1 OFFSET $2`,
		limit, offset,
	); err != nil {
		return nil, fmt.Errorf("orderRepository.List: %w", err)
	}
	return orders, nil
}

// =============================================================================
// ListWithUserDetails — admin dashboard JOIN query
// =============================================================================

// ListWithUserDetails executes two queries in parallel logical steps:
//
//  1. A paginated JOIN of orders → users → order_items (aggregated item count)
//     with optional status and customer name/email search filtering.
//  2. A stats aggregate query across ALL orders (regardless of page filters)
//     to power the summary dashboard cards.
//
// SQL:
//
//	SELECT
//	    o.id, o.total_amount, o.status, o.shipping_address, o.phone,
//	    o.created_at, o.updated_at,
//	    u.id   AS user_id,
//	    u.name AS user_name,
//	    u.email AS user_email,
//	    COUNT(oi.id) AS item_count
//	FROM orders o
//	JOIN  users u        ON u.id = o.user_id
//	LEFT JOIN order_items oi ON oi.order_id = o.id
//	[WHERE ...]
//	GROUP BY o.id, u.id
//	ORDER BY o.created_at DESC
//	LIMIT $N OFFSET $M
func (r *orderRepository) ListWithUserDetails(
	ctx context.Context,
	status string,
	search string,
	limit, offset int,
) ([]*models.AdminOrderView, int, models.AdminOrderStats, error) {
	var zero models.AdminOrderStats

	// ── 1. Build dynamic WHERE clause ─────────────────────────────────────────
	conds := []string{"1=1"}
	args  := []any{}
	i     := 1

	if status != "" {
		conds = append(conds, fmt.Sprintf("o.status = $%d", i))
		args = append(args, status)
		i++
	}
	if search != "" {
		// Search by customer name OR email (case-insensitive ILIKE)
		conds = append(conds, fmt.Sprintf(
			"(u.name ILIKE $%d OR u.email ILIKE $%d)", i, i))
		args = append(args, "%"+search+"%")
		i++
	}

	whereClause := "WHERE " + joinConds(conds)

	// ── 2. Count total matching rows ──────────────────────────────────────────
	countQ := fmt.Sprintf(`
		SELECT COUNT(DISTINCT o.id)
		FROM orders o
		JOIN users u ON u.id = o.user_id
		%s`, whereClause)

	var total int
	if err := r.db.QueryRowContext(ctx, countQ, args...).Scan(&total); err != nil {
		return nil, 0, zero, fmt.Errorf("orderRepository.ListWithUserDetails: count: %w", err)
	}

	// ── 3. Fetch the requested page ───────────────────────────────────────────
	pageArgs := append(args, limit, offset)
	dataQ := fmt.Sprintf(`
		SELECT
		    o.id,
		    o.total_amount,
		    o.status,
		    o.shipping_address,
		    o.phone,
		    o.created_at,
		    o.updated_at,
		    u.id        AS user_id,
		    u.name      AS user_name,
		    u.email     AS user_email,
		    COUNT(oi.id)::int AS item_count
		FROM orders o
		JOIN       users       u  ON u.id = o.user_id
		LEFT JOIN  order_items oi ON oi.order_id = o.id
		%s
		GROUP BY o.id, u.id
		ORDER BY o.created_at DESC
		LIMIT $%d OFFSET $%d`, whereClause, i, i+1)

	var orders []*models.AdminOrderView
	if err := r.db.SelectContext(ctx, &orders, dataQ, pageArgs...); err != nil {
		return nil, 0, zero, fmt.Errorf("orderRepository.ListWithUserDetails: select: %w", err)
	}

	// ── 4. Aggregate stats (across ALL orders, ignoring page filter) ──────────
	// These power the summary cards: total revenue, counts by status.
	const statsQ = `
		SELECT
		    COUNT(*)                                           AS total_orders,
		    COALESCE(SUM(total_amount), 0)                    AS total_revenue,
		    COUNT(*) FILTER (WHERE status = 'pending')        AS pending_count,
		    COUNT(*) FILTER (WHERE status = 'processing')     AS processing_count,
		    COUNT(*) FILTER (WHERE status = 'shipped')        AS shipped_count,
		    COUNT(*) FILTER (WHERE status = 'delivered')      AS delivered_count
		FROM orders`

	var stats models.AdminOrderStats
	if err := r.db.QueryRowContext(ctx, statsQ).Scan(
		&stats.TotalOrders,
		&stats.TotalRevenue,
		&stats.PendingCount,
		&stats.ProcessingCount,
		&stats.ShippedCount,
		&stats.DeliveredCount,
	); err != nil {
		return nil, 0, zero, fmt.Errorf("orderRepository.ListWithUserDetails: stats: %w", err)
	}

	return orders, total, stats, nil
}

// joinConds joins a slice of WHERE conditions with AND.
func joinConds(conds []string) string {
	if len(conds) == 0 {
		return "1=1"
	}
	result := conds[0]
	for _, c := range conds[1:] {
		result += " AND " + c
	}
	return result
}

