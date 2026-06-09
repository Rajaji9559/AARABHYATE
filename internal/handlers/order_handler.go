// Package handlers — Order handlers
//
// Stage 4 complete rewrite of CreateOrder:
//
//  Security model:
//  • Prices are NEVER read from the request body.
//  • All prices are fetched from the database before the transaction begins.
//  • total_amount is computed server-side from those authoritative prices.
//  • The order repository locks each product row with SELECT FOR UPDATE
//    inside the transaction to prevent overselling under concurrent load.
//
//  Flow:
//  ① Decode + validate request (product_id, quantity, shipping)
//  ② Pre-flight: deduplicate product IDs, fetch every product from DB
//  ③ Validate: active, quantity > 0, stock >= quantity (pre-TX fast path)
//  ④ Build []CheckoutItem with server-side prices
//  ⑤ Compute total_amount = Σ(price × quantity)
//  ⑥ Pass to OrderRepository.Create which runs the ACID TX with FOR UPDATE
//  ⑦ Return OrderConfirmation receipt
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/aarabhyate/backend/internal/repository"
)

// OrderHandler handles order placement and management.
type OrderHandler struct {
	orders   repository.OrderRepository
	products repository.ProductRepository // needed to resolve prices server-side
}

// NewOrderHandler constructs an OrderHandler with both required repositories.
func NewOrderHandler(
	orders repository.OrderRepository,
	products repository.ProductRepository,
) *OrderHandler {
	return &OrderHandler{orders: orders, products: products}
}

// =============================================================================
// CreateOrder — authenticated, transactional checkout
// =============================================================================

// CreateOrder godoc
//
//	@Summary      Place a new order (authenticated)
//	@Description  Accepts a list of {product_id, quantity} items and shipping details.
//	              Prices are fetched server-side; total is computed before the DB transaction.
//	              Stock is decremented atomically inside a single ACID transaction.
//	@Tags         orders
//	@Accept       json
//	@Produce      json
//	@Param        body  body  models.CreateOrderRequest  true  "Order payload"
//	@Success      201   {object}  models.OrderConfirmation
//	@Failure      400   {object}  map[string]string
//	@Failure      409   {object}  map[string]string  "Insufficient stock"
//	@Failure      422   {object}  map[string]string
//	@Router       /orders [post]
func (h *OrderHandler) CreateOrder(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromCtx(r.Context())

	// ── ① Decode & validate request ───────────────────────────────────────────
	var req models.CreateOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if errs := validateOrderRequest(&req); len(errs) > 0 {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error": "validation failed", "fields": errs,
		})
		return
	}

	// ── ② Pre-flight: deduplicate and fetch all products in one batch ──────────
	// Deduplication: merge quantities for repeated product IDs so we only
	// hold one lock per product in the transaction (avoids deadlocks).
	qtyByProduct := make(map[string]int, len(req.Items))
	for _, item := range req.Items {
		qtyByProduct[item.ProductID] += item.Quantity
	}

	// Fetch each product from DB to resolve authoritative price
	checkoutItems := make([]*models.CheckoutItem, 0, len(qtyByProduct))
	for productID, qty := range qtyByProduct {
		prod, err := h.products.GetByID(r.Context(), productID)
		if err != nil {
			respondError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("product not found: %s", productID))
			return
		}

		// ── ③ Pre-TX stock validation (fast path — avoids entering TX on bad data)
		if !prod.IsActive {
			respondError(w, http.StatusUnprocessableEntity,
				fmt.Sprintf("product %q is not available", prod.Name))
			return
		}
		if prod.Stock < qty {
			respondJSON(w, http.StatusConflict, map[string]any{
				"error":     "insufficient stock",
				"product":   prod.Name,
				"requested": qty,
				"available": prod.Stock,
			})
			return
		}

		// ── ④ Build CheckoutItem with server-fetched price ─────────────────────
		checkoutItems = append(checkoutItems, &models.CheckoutItem{
			ProductID: prod.ID,
			Name:      prod.Name,
			Price:     prod.Price, // authoritative — never from client
			Quantity:  qty,
			Subtotal:  prod.Price * float64(qty),
		})
	}

	// ── ⑤ Compute total_amount server-side ────────────────────────────────────
	var totalAmount float64
	for _, ci := range checkoutItems {
		totalAmount += ci.Subtotal
	}

	// ── ⑥ Build the Order header and run the ACID transaction ─────────────────
	order := &models.Order{
		UserID:          userID,
		TotalAmount:     totalAmount,
		Status:          models.OrderPending,
		ShippingAddress: strings.TrimSpace(req.ShippingAddress),
		Phone:           strings.TrimSpace(req.Phone),
	}

	if err := h.orders.Create(r.Context(), order, checkoutItems); err != nil {
		// Distinguish "business rule" failures (stock, inactive) from
		// unexpected infrastructure errors to avoid leaking internals.
		if isBusinessError(err) {
			respondError(w, http.StatusConflict, err.Error())
		} else {
			slog.Error("CreateOrder TX failed",
				"user_id", userID,
				"err", err,
			)
			respondError(w, http.StatusInternalServerError,
				"checkout failed — please try again")
		}
		return
	}

	// ── ⑦ Build and return the confirmation receipt ───────────────────────────
	slog.Info("order placed",
		"order_id", order.ID,
		"user_id", userID,
		"total", totalAmount,
		"items", len(checkoutItems),
	)

	itemViews := make([]*models.CheckoutItemView, len(checkoutItems))
	for i, ci := range checkoutItems {
		itemViews[i] = &models.CheckoutItemView{
			ProductID: ci.ProductID,
			Name:      ci.Name,
			Quantity:  ci.Quantity,
			UnitPrice: ci.Price,
			Subtotal:  ci.Subtotal,
		}
	}

	respondJSON(w, http.StatusCreated, models.OrderConfirmation{
		Order:       order,
		Items:       itemViews,
		TotalAmount: totalAmount,
		Message: fmt.Sprintf(
			"Order placed successfully. Your order #%s is being processed.",
			order.ID,
		),
	})
}

// =============================================================================
// GetOrder — single order detail
// =============================================================================

// GetOrder returns full details of a single order (owner or admin).
func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	order, err := h.orders.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "order not found")
		return
	}
	respondJSON(w, http.StatusOK, order)
}

// =============================================================================
// GetMyOrders — paginated orders for the current user
// =============================================================================

// GetMyOrders returns the authenticated user's order history.
func (h *OrderHandler) GetMyOrders(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromCtx(r.Context())
	limit, offset := paginationParams(r)
	orders, err := h.orders.GetByUserID(r.Context(), userID, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not fetch orders")
		return
	}
	if orders == nil {
		orders = []*models.Order{} // return [] not null
	}
	respondJSON(w, http.StatusOK, orders)
}

// =============================================================================
// AdminGetOrders — admin dashboard list
// =============================================================================

// AdminGetOrders returns a paginated list of all orders enriched with user details,
// plus aggregate stats for the admin dashboard.
func (h *OrderHandler) AdminGetOrders(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")
	limit, offset := paginationParams(r)

	orders, total, stats, err := h.orders.ListWithUserDetails(r.Context(), status, search, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not fetch orders")
		return
	}

	if orders == nil {
		orders = []*models.AdminOrderView{} // return [] instead of null
	}

	respondJSON(w, http.StatusOK, models.AdminOrderListResponse{
		Orders: orders,
		Total:  total,
		Limit:  limit,
		Offset: offset,
		Stats:  stats,
	})
}

// =============================================================================
// AdminUpdateOrderStatus — admin
// =============================================================================

// AdminUpdateOrderStatus allows an admin to advance the shipping lifecycle.
func (h *OrderHandler) AdminUpdateOrderStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req models.UpdateOrderStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	validStatuses := map[models.OrderStatus]bool{
		models.OrderPending:    true,
		models.OrderProcessing: true,
		models.OrderShipped:    true,
	}
	if !validStatuses[req.Status] {
		respondError(w, http.StatusBadRequest, "invalid order status")
		return
	}

	if err := h.orders.UpdateStatus(r.Context(), id, req.Status); err != nil {
		respondError(w, http.StatusInternalServerError, "could not update order status")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": string(req.Status)})
}

// =============================================================================
// Helpers
// =============================================================================

// validateOrderRequest checks the CreateOrderRequest and returns a field-error map.
func validateOrderRequest(req *models.CreateOrderRequest) map[string]string {
	errs := make(map[string]string)

	if strings.TrimSpace(req.ShippingAddress) == "" {
		errs["shipping_address"] = "shipping address is required"
	}
	if strings.TrimSpace(req.Phone) == "" {
		errs["phone"] = "phone number is required"
	}
	if len(req.Items) == 0 {
		errs["items"] = "order must contain at least one item"
	}
	for i, item := range req.Items {
		if strings.TrimSpace(item.ProductID) == "" {
			errs[fmt.Sprintf("items[%d].product_id", i)] = "product_id is required"
		}
		if item.Quantity <= 0 {
			errs[fmt.Sprintf("items[%d].quantity", i)] = "quantity must be at least 1"
		}
	}
	return errs
}

// isBusinessError returns true for errors that represent a domain rule violation
// (insufficient stock, inactive product) rather than an infrastructure failure.
// These are safe to propagate to the client.
func isBusinessError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "insufficient stock") ||
		strings.Contains(msg, "no longer available") ||
		strings.Contains(msg, "is not available") ||
		errors.Is(err, errBusinessRule)
}

// errBusinessRule is a sentinel for domain-rule errors when we need to wrap them.
var errBusinessRule = errors.New("business rule violation")
