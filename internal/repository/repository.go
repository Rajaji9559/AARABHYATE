// Package repository defines the data access layer interfaces and implementations.
// Each repository encapsulates all SQL for one aggregate root.
package repository

import "github.com/jmoiron/sqlx"

// Repositories is a dependency-injection container holding all repository instances.
type Repositories struct {
	Users     UserRepository
	Products  ProductRepository
	Orders    OrderRepository
	OTPs      OTPRepository
	Inquiries InquiryRepository
}

// NewRepositories wires all concrete repository implementations to the shared DB pool.
func NewRepositories(db *sqlx.DB) *Repositories {
	return &Repositories{
		Users:     NewUserRepository(db),
		Products:  NewProductRepository(db),
		Orders:    NewOrderRepository(db),
		OTPs:      NewOTPRepository(db),
		Inquiries: NewInquiryRepository(db),
	}
}
