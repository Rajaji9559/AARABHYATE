// Package handlers — Authentication: SignUp, VerifyOTP, Login
//
// Flow:
//  1. POST /auth/signup      → validate input, hash password, save user (status=pending),
//                              generate 6-digit OTP, persist OTP, send email → 201
//  2. POST /auth/verify-otp  → validate OTP against DB, mark used, activate user → 200 + JWT
//  3. POST /auth/login       → verify credentials for active users → 200 + JWT
package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/aarabhyate/backend/internal/config"
	"github.com/aarabhyate/backend/internal/models"
	"github.com/aarabhyate/backend/internal/repository"
	pkgemail "github.com/aarabhyate/backend/pkg/email"
	pkgotp "github.com/aarabhyate/backend/pkg/otp"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

// otpPurpose is the fixed purpose string stored in the otps table.
const otpPurpose = "email_verification"

// otpTTL is how long an OTP remains valid.
const otpTTL = 10 * time.Minute

// AuthHandler handles user registration, OTP verification, and login.
type AuthHandler struct {
	users  repository.UserRepository
	otps   repository.OTPRepository
	mailer *pkgemail.Mailer
	cfg    *config.Config
}

// NewAuthHandler constructs an AuthHandler with all required dependencies.
func NewAuthHandler(
	users repository.UserRepository,
	otps repository.OTPRepository,
	mailer *pkgemail.Mailer,
	cfg *config.Config,
) *AuthHandler {
	return &AuthHandler{
		users:  users,
		otps:   otps,
		mailer: mailer,
		cfg:    cfg,
	}
}

// =============================================================================
// SignUp — Step 1 of the two-step registration flow
// =============================================================================

// SignUp godoc
//
//	@Summary      Register a new user (step 1 of 2)
//	@Description  Validates input, hashes password, creates a pending user, and sends an OTP to the supplied email.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body  models.SignUpRequest  true  "Registration payload"
//	@Success      201   {object}  models.SignUpResponse
//	@Failure      400   {object}  map[string]string
//	@Failure      409   {object}  map[string]string
//	@Router       /auth/signup [post]
func (h *AuthHandler) SignUp(w http.ResponseWriter, r *http.Request) {
	// ── 1. Decode & validate request body ────────────────────────────────────
	var req models.SignUpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if errs := validateSignUpRequest(&req); len(errs) > 0 {
		respondJSON(w, http.StatusBadRequest, map[string]any{
			"error":  "validation failed",
			"fields": errs,
		})
		return
	}

	// ── 2. Hash password (bcrypt cost=12 for stronger security) ──────────────
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), 12)
	if err != nil {
		slog.Error("bcrypt failed", "err", err)
		respondError(w, http.StatusInternalServerError, "could not process password")
		return
	}

	// ── 3. Persist the user with status='pending' (or update if already pending) ─
	emailToUse := strings.ToLower(strings.TrimSpace(req.Email))
	existingUser, err := h.users.GetByEmail(r.Context(), emailToUse)
	var user *models.User

	if err == nil {
		if existingUser.Status == models.StatusActive {
			respondError(w, http.StatusConflict, "an account with this email already exists")
			return
		}
		// They are still pending. Allow re-registration (overwrite).
		existingUser.Name = strings.TrimSpace(req.Name)
		existingUser.PasswordHash = string(hash)
		if updateErr := h.users.Update(r.Context(), existingUser); updateErr != nil {
			slog.Error("failed to update pending user", "err", updateErr)
			respondError(w, http.StatusInternalServerError, "could not process registration")
			return
		}
		user = existingUser
	} else {
		user = &models.User{
			Name:         strings.TrimSpace(req.Name),
			Email:        emailToUse,
			PasswordHash: string(hash),
			Role:         models.RoleCustomer,
			Status:       models.StatusPending,
		}
		if createErr := h.users.Create(r.Context(), user); createErr != nil {
			respondError(w, http.StatusConflict, "an account with this email already exists")
			return
		}
	}

	// ── 4. Invalidate any previous pending OTPs for this user ─────────────────
	if err := h.otps.InvalidateAll(r.Context(), user.ID, otpPurpose); err != nil {
		slog.Warn("could not invalidate old OTPs", "user_id", user.ID, "err", err)
		// Non-fatal — proceed with issuing the new OTP
	}

	// ── 5. Generate a cryptographically-secure 6-digit OTP ───────────────────
	code, err := pkgotp.Generate(6)
	if err != nil {
		slog.Error("otp generation failed", "err", err)
		respondError(w, http.StatusInternalServerError, "could not generate verification code")
		return
	}

	// ── 6. Persist the OTP ────────────────────────────────────────────────────
	otpRecord := &models.OTP{
		UserID:    user.ID,
		Code:      code,
		Purpose:   otpPurpose,
		ExpiresAt: time.Now().Add(otpTTL),
	}
	if err := h.otps.Create(r.Context(), otpRecord); err != nil {
		slog.Error("failed to persist OTP", "user_id", user.ID, "err", err)
		respondError(w, http.StatusInternalServerError, "could not create verification code")
		return
	}

	// ── 7. Dispatch OTP email asynchronously (non-blocking) ──────────────────
	go func() {
		if err := h.mailer.SendOTP(user.Email, user.Name, code); err != nil {
			// Log the failure — the user can request a resend via /auth/resend-otp
			slog.Error("failed to send OTP email",
				"user_id", user.ID,
				"email", user.Email,
				"err", err,
			)
		} else {
			slog.Info("OTP email dispatched", "user_id", user.ID, "email", user.Email)
		}
	}()

	// ── 8. Return a 201 without leaking the OTP or any tokens ─────────────────
	respondJSON(w, http.StatusCreated, models.SignUpResponse{
		Message: fmt.Sprintf("Account created. A 6-digit verification code has been sent to %s. It expires in 10 minutes.", user.Email),
		UserID:  user.ID,
	})
}

// =============================================================================
// VerifyOTP — Step 2 of the two-step registration flow
// =============================================================================

// VerifyOTP godoc
//
//	@Summary      Verify email OTP and activate account (step 2 of 2)
//	@Description  Checks the 6-digit OTP against the database. On success, activates the user and returns a JWT.
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body  models.VerifyOTPRequest  true  "OTP verification payload"
//	@Success      200   {object}  models.LoginResponse
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Router       /auth/verify-otp [post]
func (h *AuthHandler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	// ── 1. Decode request ─────────────────────────────────────────────────────
	var req models.VerifyOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))
	req.Code = strings.TrimSpace(req.Code)

	if req.Email == "" || len(req.Code) != 6 {
		respondError(w, http.StatusBadRequest, "email and a 6-digit code are required")
		return
	}

	// ── 2. Look up the user by email ──────────────────────────────────────────
	user, err := h.users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		// Deliberate vagueness — don't reveal whether the email exists
		respondError(w, http.StatusUnauthorized, "invalid email or code")
		return
	}

	// ── 3. Guard: already-active users should use /auth/login ────────────────
	if user.IsActive() {
		respondError(w, http.StatusBadRequest, "account is already verified")
		return
	}

	// ── 4. Look up a valid OTP ────────────────────────────────────────────────
	otpRecord, err := h.otps.FindValid(r.Context(), user.ID, req.Code, otpPurpose)
	if err != nil {
		// Could be sql.ErrNoRows (not found), expired, or already used
		respondError(w, http.StatusUnauthorized, "invalid or expired verification code")
		return
	}

	// ── 5. Mark the OTP as consumed (prevent replay) ──────────────────────────
	if err := h.otps.MarkUsed(r.Context(), otpRecord.ID); err != nil {
		slog.Error("failed to mark OTP used", "otp_id", otpRecord.ID, "err", err)
		respondError(w, http.StatusInternalServerError, "could not complete verification")
		return
	}

	// ── 6. Activate the user account ─────────────────────────────────────────
	if err := h.users.ActivateUser(r.Context(), user.ID); err != nil {
		slog.Error("failed to activate user", "user_id", user.ID, "err", err)
		respondError(w, http.StatusInternalServerError, "could not activate account")
		return
	}
	user.Status = models.StatusActive

	// ── 7. Issue JWT ──────────────────────────────────────────────────────────
	token, err := h.issueJWT(user)
	if err != nil {
		slog.Error("JWT signing failed", "user_id", user.ID, "err", err)
		respondError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	slog.Info("user verified and activated", "user_id", user.ID, "email", user.Email)
	respondJSON(w, http.StatusOK, models.LoginResponse{Token: token, User: user})
}

// =============================================================================
// Login — credential verification for active users
// =============================================================================

// Login godoc
//
//	@Summary      Authenticate an active user and return a JWT
//	@Tags         auth
//	@Accept       json
//	@Produce      json
//	@Param        body  body  models.LoginRequest  true  "Login credentials"
//	@Success      200   {object}  models.LoginResponse
//	@Failure      400   {object}  map[string]string
//	@Failure      401   {object}  map[string]string
//	@Failure      403   {object}  map[string]string
//	@Router       /auth/login [post]
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	// ── 1. Decode request ─────────────────────────────────────────────────────
	var req models.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	req.Email = strings.ToLower(strings.TrimSpace(req.Email))

	if req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	// ── 2. Look up the user ───────────────────────────────────────────────────
	user, err := h.users.GetByEmail(r.Context(), req.Email)
	if err != nil {
		// Constant-time failure — don't distinguish "user not found" from "wrong password"
		respondError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// ── 3. Verify password (bcrypt — timing-safe comparison) ─────────────────
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		if !errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
			slog.Error("bcrypt comparison error", "err", err)
		}
		respondError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	// ── 4. Enforce account status ─────────────────────────────────────────────
	switch user.Status {
	case models.StatusPending:
		respondError(w, http.StatusForbidden,
			"account not yet verified: please check your email for the verification code")
		return
	case models.StatusSuspended:
		respondError(w, http.StatusForbidden,
			"account has been suspended: please contact support")
		return
	}

	// ── 5. Issue JWT ──────────────────────────────────────────────────────────
	token, err := h.issueJWT(user)
	if err != nil {
		slog.Error("JWT signing failed", "user_id", user.ID, "err", err)
		respondError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	slog.Info("user logged in", "user_id", user.ID, "role", user.Role)
	respondJSON(w, http.StatusOK, models.LoginResponse{Token: token, User: user})
}

// =============================================================================
// Internal helpers
// =============================================================================

// issueJWT creates and signs a HS256 JWT containing the user's ID, email, and role.
// The token expiration is read from config (default: 24h).
func (h *AuthHandler) issueJWT(user *models.User) (string, error) {
	expiry, err := time.ParseDuration(h.cfg.JWTExpiration)
	if err != nil {
		expiry = 24 * time.Hour // safe fallback
	}

	now := time.Now()
	claims := jwt.MapClaims{
		"sub":   user.ID,             // subject — user UUID
		"email": user.Email,          // included for convenience in frontend/admin tools
		"role":  string(user.Role),   // "customer" | "admin"
		"iat":   now.Unix(),          // issued at
		"exp":   now.Add(expiry).Unix(), // expiry
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString([]byte(h.cfg.JWTSecret))
	if err != nil {
		return "", fmt.Errorf("issueJWT: %w", err)
	}
	return signed, nil
}

// validateSignUpRequest performs lightweight field validation without an external library.
// Returns a map of field → error message for invalid fields.
func validateSignUpRequest(req *models.SignUpRequest) map[string]string {
	errs := make(map[string]string)

	name := strings.TrimSpace(req.Name)
	if len(name) < 2 || len(name) > 255 {
		errs["name"] = "name must be between 2 and 255 characters"
	}

	email := strings.TrimSpace(req.Email)
	if !isValidEmail(email) {
		errs["email"] = "a valid email address is required"
	}

	if len(req.Password) < 8 {
		errs["password"] = "password must be at least 8 characters"
	}

	return errs
}

// isValidEmail performs a simple RFC-5322-ish email check.
var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}$`)

func isValidEmail(email string) bool {
	return emailRegex.MatchString(email)
}
