// Package handlers — Product catalogue handlers
//
// Stage 4 additions:
//   - GetProducts: public storefront endpoint with search, category, price-range,
//     in-stock filters and a paginated response envelope with total count.
//   - CreateProduct / UpdateProduct: now handle is_active, category, sku.
package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/aarabhyate/backend/internal/repository"
)

// ProductHandler handles product CRUD operations.
type ProductHandler struct{ products repository.ProductRepository }

// NewProductHandler constructs a ProductHandler.
func NewProductHandler(products repository.ProductRepository) *ProductHandler {
	return &ProductHandler{products: products}
}

// =============================================================================
// GetProducts — public storefront listing
// =============================================================================

// GetProducts godoc
//
//	@Summary      List active products (storefront)
//	@Description  Returns only is_active=TRUE products. Supports full-text search,
//	              category filter, price range, in-stock filter, and pagination.
//	@Tags         products
//	@Produce      json
//	@Param        search    query  string   false  "Full-text search across name and description"
//	@Param        category  query  string   false  "Exact category match"
//	@Param        min_price query  number   false  "Minimum price (inclusive)"
//	@Param        max_price query  number   false  "Maximum price (inclusive)"
//	@Param        in_stock  query  bool     false  "If true, only return in-stock products"
//	@Param        limit     query  integer  false  "Page size (default 20, max 100)"
//	@Param        offset    query  integer  false  "Page offset (default 0)"
//	@Success      200  {object}  models.ProductListResponse
//	@Failure      500  {object}  map[string]string
//	@Router       /products [get]
func (h *ProductHandler) GetProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	// Parse filters from query-string — all optional
	filter := models.ProductFilter{
		Search:   q.Get("search"),
		Category: q.Get("category"),
	}

	if v := q.Get("min_price"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			filter.MinPrice = f
		}
	}
	if v := q.Get("max_price"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			filter.MaxPrice = f
		}
	}
	if q.Get("in_stock") == "true" {
		filter.InStock = true
	}

	// Pagination: default limit=20, max=100
	filter.Limit, filter.Offset = paginationParams(r)
	if filter.Limit > 100 {
		filter.Limit = 100
	}

	prods, total, err := h.products.ListActive(r.Context(), filter)
	if err != nil {
		slog.Error("GetProducts failed", "filter", filter, "err", err)
		respondError(w, http.StatusInternalServerError, "could not fetch products")
		return
	}

	// Return empty slice instead of null when no results
	if prods == nil {
		prods = []*models.Product{}
	}

	respondJSON(w, http.StatusOK, models.ProductListResponse{
		Products: prods,
		Total:    total,
		Limit:    filter.Limit,
		Offset:   filter.Offset,
	})
}

// =============================================================================
// ListProducts — admin catalogue (all products, no active filter)
// =============================================================================

// ListProducts returns a paginated list of ALL products (admin only, bypasses is_active filter).
func (h *ProductHandler) ListProducts(w http.ResponseWriter, r *http.Request) {
	limit, offset := paginationParams(r)
	prods, err := h.products.List(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not fetch products")
		return
	}
	respondJSON(w, http.StatusOK, prods)
}

// =============================================================================
// GetProduct — single product by ID
// =============================================================================

// GetProduct returns a single product by UUID.
func (h *ProductHandler) GetProduct(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	prod, err := h.products.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "product not found")
		return
	}
	respondJSON(w, http.StatusOK, prod)
}

// =============================================================================
// CreateProduct — admin
// =============================================================================

// CreateProduct adds a new product (admin only).
func (h *ProductHandler) CreateProduct(w http.ResponseWriter, r *http.Request) {
	var req models.CreateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	prod := &models.Product{
		Name:           req.Name,
		Description:    req.Description,
		TechnicalSpecs: req.TechnicalSpecs,
		Price:          req.Price,
		Stock:          req.Stock,
		ImageURL:       req.ImageURL,
		Category:       req.Category,
		SKU:            req.SKU,
		IsActive:       true, // new products are active by default
	}
	if err := h.products.Create(r.Context(), prod); err != nil {
		slog.Error("CreateProduct failed", "err", err)
		respondError(w, http.StatusInternalServerError, "could not create product")
		return
	}
	respondJSON(w, http.StatusCreated, prod)
}

// =============================================================================
// UpdateProduct — admin
// =============================================================================

// UpdateProduct performs a partial update on an existing product (admin only).
func (h *ProductHandler) UpdateProduct(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	prod, err := h.products.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "product not found")
		return
	}

	var req models.UpdateProductRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Only apply fields that were explicitly set in the request
	if req.Name != ""         { prod.Name = req.Name }
	if req.Description != ""  { prod.Description = req.Description }
	if req.TechnicalSpecs != nil { prod.TechnicalSpecs = req.TechnicalSpecs }
	if req.Price > 0          { prod.Price = req.Price }
	if req.Stock >= 0         { prod.Stock = req.Stock }
	if req.ImageURL != ""     { prod.ImageURL = req.ImageURL }
	if req.Category != ""     { prod.Category = req.Category }
	if req.SKU != ""          { prod.SKU = req.SKU }
	if req.IsActive != nil    { prod.IsActive = *req.IsActive } // *bool allows explicit false

	if err := h.products.Update(r.Context(), prod); err != nil {
		slog.Error("UpdateProduct failed", "id", id, "err", err)
		respondError(w, http.StatusInternalServerError, "could not update product")
		return
	}
	respondJSON(w, http.StatusOK, prod)
}

// =============================================================================
// DeleteProduct — admin
// =============================================================================

// DeleteProduct removes a product permanently (admin only).
// Prefer setting is_active=false to soft-delete products that have existing order references.
func (h *ProductHandler) DeleteProduct(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.products.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "could not delete product")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
