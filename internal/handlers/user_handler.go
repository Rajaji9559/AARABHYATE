package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/aarabhyate/backend/internal/repository"
)

// UserHandler handles user profile CRUD.
type UserHandler struct{ users repository.UserRepository }

// NewUserHandler constructs a UserHandler.
func NewUserHandler(users repository.UserRepository) *UserHandler {
	return &UserHandler{users: users}
}

// GetMe returns the profile of the currently authenticated user.
func (h *UserHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromCtx(r.Context())
	user, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}
	respondJSON(w, http.StatusOK, user)
}

// GetUser returns any user by ID (admin).
func (h *UserHandler) GetUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	user, err := h.users.GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}
	respondJSON(w, http.StatusOK, user)
}

// ListUsers returns a paginated list of users (admin).
func (h *UserHandler) ListUsers(w http.ResponseWriter, r *http.Request) {
	limit, offset := paginationParams(r)
	users, err := h.users.List(r.Context(), limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "could not fetch users")
		return
	}
	respondJSON(w, http.StatusOK, users)
}

// UpdateMe updates the authenticated user's own profile.
func (h *UserHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromCtx(r.Context())
	var req models.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	user, err := h.users.GetByID(r.Context(), userID)
	if err != nil {
		respondError(w, http.StatusNotFound, "user not found")
		return
	}
	if req.Name != "" {
		user.Name = req.Name
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	if err := h.users.Update(r.Context(), user); err != nil {
		respondError(w, http.StatusInternalServerError, "could not update user")
		return
	}
	respondJSON(w, http.StatusOK, user)
}

// DeleteUser deletes a user by ID (admin).
func (h *UserHandler) DeleteUser(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.users.Delete(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "could not delete user")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
