package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/aarabhyate/backend/internal/repository"
)

// InquiryHandler handles project inquiry form submissions and admin management.
type InquiryHandler struct {
	inquiries repository.InquiryRepository
}

// NewInquiryHandler constructs an InquiryHandler.
func NewInquiryHandler(inquiries repository.InquiryRepository) *InquiryHandler {
	return &InquiryHandler{inquiries: inquiries}
}

// =============================================================================
// Submit — public endpoint consumed by the portal.html form
// =============================================================================

// Submit godoc
//
//	@Summary      Submit a custom project inquiry
//	@Description  Accepts the portal form payload, validates it, and persists a new inquiry.
//	@Tags         inquiries
//	@Accept       json
//	@Produce      json
//	@Param        body  body  models.CreateInquiryRequest  true  "Project inquiry payload"
//	@Success      201   {object}  models.InquiryResponse
//	@Failure      400   {object}  map[string]string
//	@Failure      422   {object}  map[string]string
//	@Router       /inquiries [post]
func (h *InquiryHandler) Submit(w http.ResponseWriter, r *http.Request) {
	// ── 1. Decode ─────────────────────────────────────────────────────────────
	var req models.CreateInquiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// ── 2. Sanitise & validate ────────────────────────────────────────────────
	req.FullName       = strings.TrimSpace(req.FullName)
	req.Email         = strings.ToLower(strings.TrimSpace(req.Email))
	req.TechnicalBrief = strings.TrimSpace(req.TechnicalBrief)

	if errs := validateInquiryRequest(&req); len(errs) > 0 {
		respondJSON(w, http.StatusUnprocessableEntity, map[string]any{
			"error":  "validation failed",
			"fields": errs,
		})
		return
	}

	// ── 3. Capture client IP for abuse tracking ───────────────────────────────
	clientIP := extractIP(r)

	// ── 4. Build and persist the inquiry ─────────────────────────────────────
	inq := &models.ProjectInquiry{
		FullName:       req.FullName,
		Email:         req.Email,
		ProjectType:   models.ProjectType(req.ProjectType),
		BudgetEstimate: req.BudgetEstimate,
		Timeline:      req.Timeline,
		TechnicalBrief: req.TechnicalBrief,
		IPAddress:     clientIP,
	}

	if err := h.inquiries.Create(r.Context(), inq); err != nil {
		slog.Error("failed to save inquiry", "email", inq.Email, "err", err)
		respondError(w, http.StatusInternalServerError, "could not save your inquiry — please try again")
		return
	}

	slog.Info("new project inquiry received",
		"id", inq.ID,
		"email", inq.Email,
		"type", inq.ProjectType,
	)

	// ── 5. Return created inquiry (omit admin-only fields) ────────────────────
	respondJSON(w, http.StatusCreated, models.InquiryResponse{
		Message: "Your project brief has been received. We will respond within 48 hours.",
		Inquiry: inq,
	})
}

// =============================================================================
// Admin endpoints
// =============================================================================

// ListInquiries returns a paginated list of all project inquiries (admin only).
//
//	@Summary      List all project inquiries (admin)
//	@Tags         admin, inquiries
//	@Produce      json
//	@Param        status        query  string  false  "Filter by status"
//	@Param        project_type  query  string  false  "Filter by project type"
//	@Param        limit         query  int     false  "Limit (default 20)"
//	@Param        offset        query  int     false  "Offset (default 0)"
//	@Success      200  {array}  models.ProjectInquiry
//	@Router       /admin/inquiries [get]
func (h *InquiryHandler) ListInquiries(w http.ResponseWriter, r *http.Request) {
	status      := r.URL.Query().Get("status")
	projectType := r.URL.Query().Get("project_type")
	limit, offset := paginationParams(r)

	inquiries, err := h.inquiries.List(r.Context(), status, projectType, limit, offset)
	if err != nil {
		slog.Error("failed to list inquiries", "err", err)
		respondError(w, http.StatusInternalServerError, "could not fetch inquiries")
		return
	}
	respondJSON(w, http.StatusOK, inquiries)
}

// GetInquiry returns a single inquiry by ID (admin only).
func (h *InquiryHandler) GetInquiry(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inq, err := h.inquiries.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "inquiry not found")
		return
	}
	respondJSON(w, http.StatusOK, inq)
}

// UpdateInquiryStatus changes the lifecycle status of an inquiry (admin only).
func (h *InquiryHandler) UpdateInquiryStatus(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req models.UpdateInquiryStatusRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	validStatuses := map[models.InquiryStatus]bool{
		models.InquiryStatusNew:        true,
		models.InquiryStatusReviewed:   true,
		models.InquiryStatusInProgress: true,
		models.InquiryStatusClosed:     true,
	}
	if !validStatuses[req.Status] {
		respondError(w, http.StatusBadRequest, "invalid status value")
		return
	}

	if err := h.inquiries.UpdateStatus(r.Context(), id, req.Status); err != nil {
		respondError(w, http.StatusInternalServerError, "could not update inquiry status")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"status": string(req.Status)})
}

// AddAdminNotes stores internal notes against an inquiry (admin only).
func (h *InquiryHandler) AddAdminNotes(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req models.AdminNotesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.Notes) == "" {
		respondError(w, http.StatusBadRequest, "notes cannot be empty")
		return
	}

	if err := h.inquiries.AddAdminNotes(r.Context(), id, req.Notes); err != nil {
		respondError(w, http.StatusInternalServerError, "could not save notes")
		return
	}
	respondJSON(w, http.StatusOK, map[string]string{"message": "notes saved"})
}

// =============================================================================
// Validation helpers
// =============================================================================

func validateInquiryRequest(req *models.CreateInquiryRequest) map[string]string {
	errs := make(map[string]string)

	if len(req.FullName) < 2 || len(req.FullName) > 255 {
		errs["full_name"] = "full name must be between 2 and 255 characters"
	}

	if !isValidEmail(req.Email) {
		errs["email"] = "a valid email address is required"
	}

	validTypes := map[string]bool{
		"embedded_systems": true,
		"ai_ml":            true,
		"custom_automation": true,
		"robotics_hardware": true,
		"other":            true,
	}
	if !validTypes[req.ProjectType] {
		errs["project_type"] = "invalid project type"
	}

	if len(req.TechnicalBrief) < 50 {
		errs["technical_brief"] = "technical brief must be at least 50 characters"
	}
	if len(req.TechnicalBrief) > 5000 {
		errs["technical_brief"] = "technical brief must not exceed 5000 characters"
	}

	if len(req.BudgetEstimate) > 100 {
		errs["budget_estimate"] = "budget estimate must not exceed 100 characters"
	}
	if len(req.Timeline) > 100 {
		errs["timeline"] = "timeline must not exceed 100 characters"
	}

	return errs
}

// extractIP pulls the real client IP from the request, respecting proxy headers.
func extractIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		if ip := strings.TrimSpace(parts[0]); ip != "" {
			return ip
		}
	}
	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}
	// RemoteAddr is "IP:port"
	if idx := strings.LastIndex(r.RemoteAddr, ":"); idx != -1 {
		return r.RemoteAddr[:idx]
	}
	return r.RemoteAddr
}
