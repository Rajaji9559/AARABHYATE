// Package models defines the core domain types for the Aarabhyate backend.
// All structs use both `db` tags (for sqlx) and `json` tags (for API responses).
package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
)

// =============================================================================
// COMMON TYPES
// =============================================================================

// UserRole represents the role of a user in the system.
type UserRole string

const (
	RoleCustomer UserRole = "customer"
	RoleAdmin    UserRole = "admin"
)

// UserStatus represents the lifecycle state of a user account.
type UserStatus string

const (
	StatusPending   UserStatus = "pending"   // registered but OTP not yet verified
	StatusActive    UserStatus = "active"    // email verified, fully operational
	StatusSuspended UserStatus = "suspended" // admin-disabled account
)

// OrderStatus represents the lifecycle state of an order.
type OrderStatus string

const (
	OrderPending    OrderStatus = "pending"
	OrderProcessing OrderStatus = "processing"
	OrderShipped    OrderStatus = "shipped"
	OrderDelivered  OrderStatus = "delivered"
	OrderCancelled  OrderStatus = "cancelled"
)

// =============================================================================
// ADMIN ORDER VIEW
// =============================================================================

// AdminOrderView is the rich projection returned by AdminGetOrders.
// It joins orders → users and aggregates order_items so the admin dashboard
// can display all fulfilment information without additional round-trips.
type AdminOrderView struct {
	// ── Order fields ─────────────────────────────────────────────────────────
	ID              string      `db:"id"               json:"id"`
	TotalAmount     float64     `db:"total_amount"     json:"total_amount"`
	Status          OrderStatus `db:"status"           json:"status"`
	ShippingAddress string      `db:"shipping_address" json:"shipping_address"`
	Phone           string      `db:"phone"            json:"phone"`
	CreatedAt       time.Time   `db:"created_at"       json:"created_at"`
	UpdatedAt       time.Time   `db:"updated_at"       json:"updated_at"`

	// ── Customer fields (from JOIN with users) ────────────────────────────────
	UserID    string `db:"user_id"    json:"user_id"`
	UserName  string `db:"user_name"  json:"user_name"`
	UserEmail string `db:"user_email" json:"user_email"`

	// ── Aggregate ─────────────────────────────────────────────────────────────
	ItemCount int `db:"item_count" json:"item_count"` // COUNT(order_items)
}

// AdminOrderListResponse is the paginated envelope returned by AdminGetOrders.
type AdminOrderListResponse struct {
	Orders []*AdminOrderView `json:"orders"`
	Total  int               `json:"total"`
	Limit  int               `json:"limit"`
	Offset int               `json:"offset"`
	Stats  AdminOrderStats   `json:"stats"`
}

// AdminOrderStats carries the aggregate metrics displayed in the dashboard summary cards.
type AdminOrderStats struct {
	TotalOrders     int     `json:"total_orders"`
	TotalRevenue    float64 `json:"total_revenue"`
	PendingCount    int     `json:"pending_count"`
	ProcessingCount int     `json:"processing_count"`
	ShippedCount    int     `json:"shipped_count"`
	DeliveredCount  int     `json:"delivered_count"`
}

// JSONB is a generic map type that serialises to/from PostgreSQL JSONB columns.
type JSONB map[string]any

// Value implements the driver.Valuer interface for database writes.
func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	b, err := json.Marshal(j)
	if err != nil {
		return nil, fmt.Errorf("JSONB.Value: %w", err)
	}
	return string(b), nil
}

// Scan implements the sql.Scanner interface for database reads.
func (j *JSONB) Scan(src any) error {
	if src == nil {
		*j = JSONB{}
		return nil
	}
	var b []byte
	switch v := src.(type) {
	case string:
		b = []byte(v)
	case []byte:
		b = v
	default:
		return fmt.Errorf("JSONB.Scan: unsupported type %T", src)
	}
	if err := json.Unmarshal(b, j); err != nil {
		return fmt.Errorf("JSONB.Scan: %w", err)
	}
	return nil
}

// =============================================================================
// USER
// =============================================================================

// User represents a registered user of the platform.
type User struct {
	ID           string     `db:"id"            json:"id"`
	Name         string     `db:"name"          json:"name"`
	Email        string     `db:"email"         json:"email"`
	PasswordHash string     `db:"password_hash" json:"-"` // never expose in JSON
	Role         UserRole   `db:"role"          json:"role"`
	Status       UserStatus `db:"status"        json:"status"`
	CreatedAt    time.Time  `db:"created_at"    json:"created_at"`
}

// IsActive returns true if the user has completed email verification.
func (u *User) IsActive() bool { return u.Status == StatusActive }

// =============================================================================
// AUTH DTOs
// =============================================================================

// SignUpRequest is the DTO for the two-step registration flow.
// After a successful signup the server sends an OTP — no token is returned yet.
type SignUpRequest struct {
	Name     string `json:"name"     validate:"required,min=2,max=255"`
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// SignUpResponse is returned after successful signup (before OTP verification).
type SignUpResponse struct {
	Message string `json:"message"`
	UserID  string `json:"user_id"`
}

// VerifyOTPRequest is the DTO submitted to the OTP verification endpoint.
type VerifyOTPRequest struct {
	Email string `json:"email" validate:"required,email"`
	Code  string `json:"code"  validate:"required,len=6"`
}

// UnmarshalJSON customizes JSON decoding to support both "code" and "otp_code" keys.
func (v *VerifyOTPRequest) UnmarshalJSON(data []byte) error {
	type Alias VerifyOTPRequest
	aux := &struct {
		OTPCode string `json:"otp_code"`
		*Alias
	}{
		Alias: (*Alias)(v),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	if aux.OTPCode != "" {
		v.Code = aux.OTPCode
	}
	return nil
}

// CreateUserRequest is kept for backward-compatible internal use.
type CreateUserRequest struct {
	Name     string `json:"name"     validate:"required,min=2,max=255"`
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required,min=8"`
}

// UpdateUserRequest is the DTO for updating user profile fields.
type UpdateUserRequest struct {
	Name  string `json:"name"  validate:"omitempty,min=2,max=255"`
	Email string `json:"email" validate:"omitempty,email"`
}

// LoginRequest is the DTO for authentication.
type LoginRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

// LoginResponse wraps the JWT token returned on successful authentication.
type LoginResponse struct {
	Token string `json:"token"`
	User  *User  `json:"user"`
}

// =============================================================================
// OTP
// =============================================================================

// OTP represents a one-time password stored in the database.
type OTP struct {
	ID        string    `db:"id"         json:"id"`
	UserID    string    `db:"user_id"    json:"user_id"`
	Code      string    `db:"code"       json:"-"` // never expose raw OTP in API
	Purpose   string    `db:"purpose"    json:"purpose"`
	Used      bool      `db:"used"       json:"used"`
	ExpiresAt time.Time `db:"expires_at" json:"expires_at"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
}

// =============================================================================
// PRODUCT
// =============================================================================

// Product represents a robotics product offered by Aarabhyate.
type Product struct {
	ID             string    `db:"id"              json:"id"`
	Name           string    `db:"name"            json:"name"`
	Description    string    `db:"description"     json:"description"`
	TechnicalSpecs JSONB     `db:"technical_specs" json:"technical_specs"`
	Price          float64   `db:"price"           json:"price"`
	Stock          int       `db:"stock"           json:"stock"`
	ImageURL       string    `db:"image_url"       json:"image_url"`
	// Stage 4 additions
	IsActive  bool   `db:"is_active"  json:"is_active"`
	Category  string `db:"category"   json:"category,omitempty"`
	SKU       string `db:"sku"        json:"sku,omitempty"`
	CreatedAt time.Time `db:"created_at" json:"created_at"`
	UpdatedAt time.Time `db:"updated_at" json:"updated_at"`
}

// InStock returns true if the product is available for purchase.
func (p *Product) InStock() bool { return p.IsActive && p.Stock > 0 }

// ProductFilter holds the optional query-string filters for the storefront listing.
type ProductFilter struct {
	Search   string  // full-text search across name + description
	Category string  // exact category match
	MinPrice float64 // inclusive lower bound
	MaxPrice float64 // inclusive upper bound (0 = no cap)
	InStock  bool    // if true, only return products with stock > 0
	Limit    int
	Offset   int
}

// ProductListResponse is the paginated response envelope for the storefront listing.
type ProductListResponse struct {
	Products []*Product `json:"products"`
	Total    int        `json:"total"`   // total rows matching the filter (for pagination UI)
	Limit    int        `json:"limit"`
	Offset   int        `json:"offset"`
}

// CreateProductRequest is the DTO for adding a new product (admin only).
type CreateProductRequest struct {
	Name           string  `json:"name"            validate:"required,min=2,max=255"`
	Description    string  `json:"description"     validate:"required"`
	TechnicalSpecs JSONB   `json:"technical_specs"`
	Price          float64 `json:"price"           validate:"required,gt=0"`
	Stock          int     `json:"stock"           validate:"min=0"`
	ImageURL       string  `json:"image_url"       validate:"omitempty,url"`
	Category       string  `json:"category"        validate:"omitempty,max=100"`
	SKU            string  `json:"sku"             validate:"omitempty,max=100"`
}

// UpdateProductRequest is the DTO for editing a product (admin only).
type UpdateProductRequest struct {
	Name           string  `json:"name"            validate:"omitempty,min=2,max=255"`
	Description    string  `json:"description"     validate:"omitempty"`
	TechnicalSpecs JSONB   `json:"technical_specs"`
	Price          float64 `json:"price"           validate:"omitempty,gt=0"`
	Stock          int     `json:"stock"           validate:"omitempty,min=0"`
	ImageURL       string  `json:"image_url"       validate:"omitempty,url"`
	Category       string  `json:"category"        validate:"omitempty,max=100"`
	SKU            string  `json:"sku"             validate:"omitempty,max=100"`
	IsActive       *bool   `json:"is_active"` // pointer so false can be distinguished from omitted
}

// =============================================================================
// ORDER
// =============================================================================

// Order represents a customer purchase transaction.
type Order struct {
	ID              string      `db:"id"               json:"id"`
	UserID          string      `db:"user_id"          json:"user_id"`
	TotalAmount     float64     `db:"total_amount"     json:"total_amount"`
	Status          OrderStatus `db:"status"           json:"status"`
	ShippingAddress string      `db:"shipping_address" json:"shipping_address"`
	Phone           string      `db:"phone"            json:"phone"`
	CreatedAt       time.Time   `db:"created_at"       json:"created_at"`
	UpdatedAt       time.Time   `db:"updated_at"       json:"updated_at"`

	// Eagerly-loaded related data (not a DB column)
	Items []*OrderItem `db:"-" json:"items,omitempty"`
}

// CreateOrderRequest is the DTO used by the customer to place an order.
// IMPORTANT: prices are NEVER accepted from the client.
// The server fetches authoritative prices from the database inside the transaction.
type CreateOrderRequest struct {
	ShippingAddress string             `json:"shipping_address" validate:"required"`
	Phone           string             `json:"phone"            validate:"required"`
	Items           []OrderItemRequest `json:"items"            validate:"required,min=1,dive"`
}

// OrderItemRequest represents a single line-item in an order creation request.
// Only product_id and quantity come from the client — price is resolved server-side.
type OrderItemRequest struct {
	ProductID string `json:"product_id" validate:"required"`
	Quantity  int    `json:"quantity"   validate:"required,min=1"`
}

// CheckoutItem is an internal type that carries the server-fetched product price
// alongside the customer-supplied quantity. It is assembled in the handler and
// passed into the order repository — never serialised to JSON.
type CheckoutItem struct {
	ProductID string
	Name      string  // for the confirmation receipt
	Price     float64 // authoritative price fetched from DB
	Quantity  int
	Subtotal  float64 // Price * Quantity, computed before the TX
}

// OrderConfirmation is the rich response returned to the client after a successful order.
type OrderConfirmation struct {
	Order       *Order              `json:"order"`
	Items       []*CheckoutItemView `json:"items"`
	TotalAmount float64             `json:"total_amount"`
	Message     string              `json:"message"`
}

// CheckoutItemView is the JSON-safe projection of CheckoutItem for the confirmation receipt.
type CheckoutItemView struct {
	ProductID string  `json:"product_id"`
	Name      string  `json:"name"`
	Quantity  int     `json:"quantity"`
	UnitPrice float64 `json:"unit_price"`
	Subtotal  float64 `json:"subtotal"`
}

// UpdateOrderStatusRequest allows an admin to update the order status.
type UpdateOrderStatusRequest struct {
	Status OrderStatus `json:"status" validate:"required,oneof=pending processing shipped delivered cancelled"`
}

// =============================================================================
// ORDER ITEM
// =============================================================================

// OrderItem represents a single product line within an order.
type OrderItem struct {
	ID        string  `db:"id"         json:"id"`
	OrderID   string  `db:"order_id"   json:"order_id"`
	ProductID string  `db:"product_id" json:"product_id"`
	Quantity  int     `db:"quantity"   json:"quantity"`
	Price     float64 `db:"price"      json:"price"` // unit price at time of purchase

	// Optionally populated join data
	Product *Product `db:"-" json:"product,omitempty"`
}

// =============================================================================
// PROJECT INQUIRY
// =============================================================================

// ProjectType represents the domain of a custom project request.
type ProjectType string

const (
	ProjectEmbeddedSystems  ProjectType = "embedded_systems"
	ProjectAIML             ProjectType = "ai_ml"
	ProjectCustomAutomation ProjectType = "custom_automation"
	ProjectRoboticsHardware ProjectType = "robotics_hardware"
	ProjectOther            ProjectType = "other"
)

// InquiryStatus represents the lifecycle state of a project inquiry.
type InquiryStatus string

const (
	InquiryStatusNew        InquiryStatus = "new"
	InquiryStatusReviewed   InquiryStatus = "reviewed"
	InquiryStatusInProgress InquiryStatus = "in_progress"
	InquiryStatusClosed     InquiryStatus = "closed"
)

// ProjectInquiry represents a custom project brief submitted via the portal.
type ProjectInquiry struct {
	ID             string        `db:"id"              json:"id"`
	FullName       string        `db:"full_name"       json:"full_name"`
	Email          string        `db:"email"           json:"email"`
	ProjectType    ProjectType   `db:"project_type"    json:"project_type"`
	BudgetEstimate string        `db:"budget_estimate" json:"budget_estimate,omitempty"`
	Timeline       string        `db:"timeline"        json:"timeline,omitempty"`
	TechnicalBrief string        `db:"technical_brief" json:"technical_brief"`
	Status         InquiryStatus `db:"status"          json:"status"`
	AdminNotes     string        `db:"admin_notes"     json:"-"` // never expose to clients
	IPAddress      string        `db:"ip_address"      json:"-"` // internal use only
	SubmittedAt    time.Time     `db:"submitted_at"    json:"submitted_at"`
	UpdatedAt      time.Time     `db:"updated_at"      json:"updated_at"`
}

// CreateInquiryRequest is the public DTO for submitting a project brief via the portal.
type CreateInquiryRequest struct {
	FullName       string `json:"full_name"       validate:"required,min=2,max=255"`
	Email          string `json:"email"           validate:"required,email"`
	ProjectType    string `json:"project_type"    validate:"required,oneof=embedded_systems ai_ml custom_automation robotics_hardware other"`
	BudgetEstimate string `json:"budget_estimate" validate:"omitempty,max=100"`
	Timeline       string `json:"timeline"        validate:"omitempty,max=100"`
	TechnicalBrief string `json:"technical_brief" validate:"required,min=50,max=5000"`
}

// InquiryResponse wraps the created inquiry returned after a successful submission.
type InquiryResponse struct {
	Message string          `json:"message"`
	Inquiry *ProjectInquiry `json:"inquiry"`
}

// UpdateInquiryStatusRequest is the admin DTO for changing inquiry lifecycle status.
type UpdateInquiryStatusRequest struct {
	Status InquiryStatus `json:"status" validate:"required,oneof=new reviewed in_progress closed"`
}

// AdminNotesRequest is the admin DTO for adding internal notes to an inquiry.
type AdminNotesRequest struct {
	Notes string `json:"notes" validate:"required"`
}

