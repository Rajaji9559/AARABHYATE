// Package handlers wires all HTTP handler groups together.
package handlers

import (
	"github.com/aarabhyate/backend/internal/config"
	"github.com/aarabhyate/backend/internal/repository"
	pkgemail "github.com/aarabhyate/backend/pkg/email"
)

// Handlers aggregates all HTTP handler groups.
type Handlers struct {
	Auth     *AuthHandler
	User     *UserHandler
	Product  *ProductHandler
	Order    *OrderHandler
	Inquiry  *InquiryHandler
}

// NewHandlers constructs all handlers with their required dependencies.
// The Mailer is built from config and injected into AuthHandler.
func NewHandlers(repos *repository.Repositories, cfg *config.Config) *Handlers {
	mailer := pkgemail.New(
		cfg.SMTPHost,
		cfg.SMTPPort,
		cfg.SMTPFrom,
		cfg.SMTPUser,
		cfg.SMTPPassword,
	)

	return &Handlers{
		Auth:    NewAuthHandler(repos.Users, repos.OTPs, mailer, cfg),
		User:    NewUserHandler(repos.Users),
		Product: NewProductHandler(repos.Products),
		Order:   NewOrderHandler(repos.Orders, repos.Products), // products needed for server-side price resolution
		Inquiry: NewInquiryHandler(repos.Inquiries),
	}
}
