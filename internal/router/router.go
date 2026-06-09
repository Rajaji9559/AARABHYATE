// Package router assembles the chi HTTP router with all API routes.
// Stage 2 additions:
//   - POST /auth/signup          (public, rate-limited by default chi throttle)
//   - POST /auth/verify-otp      (public, STRICT IP rate-limit: 3 req / 10 min)
//   - POST /auth/login           (public)
//   - Admin routes now use the self-contained AdminMiddleware (JWT + role check)
// Stage 3 additions:
//   - Static file server         (GET /  → web/)
//   - POST /api/v1/inquiries     (public — portal form submission)
//   - GET  /api/v1/admin/inquiries etc. (admin inquiry management)
package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"golang.org/x/time/rate"

	"github.com/aarabhyate/backend/internal/config"
	"github.com/aarabhyate/backend/internal/handlers"
	"github.com/aarabhyate/backend/internal/middleware"
)

// New builds and returns the fully configured chi.Router.
func New(h *handlers.Handlers, cfg *config.Config) http.Handler {
	r := chi.NewRouter()

	// ── Global middleware ─────────────────────────────────────────────────────
	r.Use(chiMiddleware.RequestID)
	r.Use(chiMiddleware.RealIP)
	r.Use(chiMiddleware.Logger)
	r.Use(chiMiddleware.Recoverer)
	r.Use(chiMiddleware.CleanPath)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"https://*", "http://*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Request-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// ── Static file server ────────────────────────────────────────────────────
	// Serves index.html, portal.html, style.css from the web/ directory.
	// The FileServer strips the prefix so requests to / map to web/index.html.
	webRoot := http.Dir("./web")
	r.Handle("/", http.FileServer(webRoot))
	r.Handle("/portal", http.RedirectHandler("/portal.html", http.StatusMovedPermanently))
	r.Handle("/admin", http.RedirectHandler("/admin.html", http.StatusMovedPermanently))

	// ── Per-IP rate limiters ───────────────────────────────────────────────────
	//
	// OTP rate limiter: strictly 3 requests per 10 minutes per IP.
	//   - rps   = 3 / 600 = 0.005 tokens/second refill rate
	//   - burst = 3       = maximum token bucket capacity
	//
	// This means an IP can fire at most 3 OTP requests in any 10-minute window.
	// The 4th request (while tokens are depleted) gets 429 + Retry-After: 600.
	otpLimiter := middleware.NewRateLimiter(rate.Limit(3.0/600.0), 3)

	// Inquiry limiter: 10 submissions per hour per IP — prevents portal spam.
	inquiryLimiter := middleware.NewRateLimiter(rate.Limit(10.0/3600.0), 10)

	// ── Health check ──────────────────────────────────────────────────────────
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","service":"aarabhyate-api"}`)) //nolint:errcheck
	})

	// ── API v1 ────────────────────────────────────────────────────────────────
	r.Route("/api/v1", func(r chi.Router) {

		// ── Auth routes (public) ──────────────────────────────────────────────
		r.Route("/auth", func(r chi.Router) {
			// Step 1: Register → creates pending user + dispatches OTP email
			r.Post("/signup", h.Auth.SignUp)

			// Step 2: Verify OTP → activates account + returns JWT
			// Rate-limited to 3 requests per IP per 10 minutes
			r.With(otpLimiter.Limit()).Post("/verify-otp", h.Auth.VerifyOTP)

			// Login → returns JWT for active users
			r.Post("/login", h.Auth.Login)
		})

		// ── Product catalogue (public) ────────────────────────────────────────────
		// GET /products         → storefront listing (is_active=TRUE only, filters, pagination)
		// GET /products/{id}   → single product detail
		r.Get("/products", h.Product.GetProducts)
		r.Get("/products/{id}", h.Product.GetProduct)

		// ── Inquiry (Project Portal) — public submission ──────────────────────
		// Rate-limited to 10 submissions per hour per IP to prevent portal spam.
		r.With(inquiryLimiter.Limit()).Post("/inquiries", h.Inquiry.Submit)

		// ── Authenticated routes (valid JWT required) ──────────────────────────
		r.Group(func(r chi.Router) {
			r.Use(middleware.Auth(cfg.JWTSecret))

			// Own profile
			r.Get("/me", h.User.GetMe)
			r.Put("/me", h.User.UpdateMe)

			// Own orders
			r.Get("/orders", h.Order.GetMyOrders)
			r.Post("/orders", h.Order.CreateOrder)
			r.Get("/orders/{id}", h.Order.GetOrder)
		})

		// ── Admin routes (JWT required AND role must be "admin") ───────────────
		//
		// AdminMiddleware is self-contained: it extracts the Bearer token, verifies
		// the JWT signature/expiry, and enforces role=="admin" — all in one step.
		// This makes the guard explicit and impossible to bypass by omitting Auth.
		r.Group(func(r chi.Router) {
			r.Use(middleware.AdminMiddleware(cfg.JWTSecret))

			// ── User management ───────────────────────────────────────────────
			r.Get("/admin/users", h.User.ListUsers)
			r.Get("/admin/users/{id}", h.User.GetUser)
			r.Delete("/admin/users/{id}", h.User.DeleteUser)

			// ── Product management ────────────────────────────────────────────
			// ListProducts returns ALL products (including is_active=FALSE) for admin management.
			r.Get("/admin/products", h.Product.ListProducts)
			r.Post("/admin/products", h.Product.CreateProduct)
			r.Put("/admin/products/{id}", h.Product.UpdateProduct)
			r.Delete("/admin/products/{id}", h.Product.DeleteProduct)

			// ── Order management ──────────────────────────────────────────────
			r.Get("/admin/orders", h.Order.AdminGetOrders)
			r.Patch("/admin/orders/{id}/status", h.Order.AdminUpdateOrderStatus)

			// ── Inquiry management ────────────────────────────────────────────
			// GET  /admin/inquiries?status=new&project_type=ai_ml&limit=20
			// GET  /admin/inquiries/{id}
			// PATCH /admin/inquiries/{id}/status
			// PUT  /admin/inquiries/{id}/notes
			r.Get("/admin/inquiries", h.Inquiry.ListInquiries)
			r.Get("/admin/inquiries/{id}", h.Inquiry.GetInquiry)
			r.Patch("/admin/inquiries/{id}/status", h.Inquiry.UpdateInquiryStatus)
			r.Put("/admin/inquiries/{id}/notes", h.Inquiry.AddAdminNotes)
		})
	})

	// Fallback to serving static files for any unmatched route
	r.Handle("/*", http.FileServer(webRoot))
	return r
}
