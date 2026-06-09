package repository

import (
	"context"
	"fmt"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/jmoiron/sqlx"
)

// UserRepository defines all data-access operations for the users table.
type UserRepository interface {
	Create(ctx context.Context, u *models.User) error
	GetByID(ctx context.Context, id string) (*models.User, error)
	GetByEmail(ctx context.Context, email string) (*models.User, error)
	Update(ctx context.Context, u *models.User) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, limit, offset int) ([]*models.User, error)
	// ActivateUser flips the user's status from 'pending' to 'active'
	// after successful OTP verification.
	ActivateUser(ctx context.Context, id string) error
}

type userRepository struct{ db *sqlx.DB }

// NewUserRepository returns a concrete UserRepository backed by Postgres.
func NewUserRepository(db *sqlx.DB) UserRepository { return &userRepository{db: db} }

func (r *userRepository) Create(ctx context.Context, u *models.User) error {
	const q = `
		INSERT INTO users (id, name, email, password_hash, role, created_at)
		VALUES (gen_random_uuid(), :name, :email, :password_hash, :role, NOW())
		RETURNING id, created_at`

	rows, err := r.db.NamedQueryContext(ctx, q, u)
	if err != nil {
		return fmt.Errorf("userRepository.Create: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return rows.StructScan(u)
	}
	return fmt.Errorf("userRepository.Create: no row returned")
}

func (r *userRepository) GetByID(ctx context.Context, id string) (*models.User, error) {
	u := &models.User{}
	if err := r.db.GetContext(ctx, u, `SELECT * FROM users WHERE id = $1`, id); err != nil {
		return nil, fmt.Errorf("userRepository.GetByID: %w", err)
	}
	return u, nil
}

func (r *userRepository) GetByEmail(ctx context.Context, email string) (*models.User, error) {
	u := &models.User{}
	if err := r.db.GetContext(ctx, u, `SELECT * FROM users WHERE email = $1`, email); err != nil {
		return nil, fmt.Errorf("userRepository.GetByEmail: %w", err)
	}
	return u, nil
}

func (r *userRepository) Update(ctx context.Context, u *models.User) error {
	const q = `
		UPDATE users SET name = :name, email = :email, password_hash = :password_hash
		WHERE id = :id`
	if _, err := r.db.NamedExecContext(ctx, q, u); err != nil {
		return fmt.Errorf("userRepository.Update: %w", err)
	}
	return nil
}

func (r *userRepository) Delete(ctx context.Context, id string) error {
	if _, err := r.db.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
		return fmt.Errorf("userRepository.Delete: %w", err)
	}
	return nil
}

func (r *userRepository) List(ctx context.Context, limit, offset int) ([]*models.User, error) {
	var users []*models.User
	if err := r.db.SelectContext(ctx, &users,
		`SELECT * FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset); err != nil {
		return nil, fmt.Errorf("userRepository.List: %w", err)
	}
	return users, nil
}

// ActivateUser atomically sets status='active' for the given user ID.
func (r *userRepository) ActivateUser(ctx context.Context, id string) error {
	const q = `UPDATE users SET status = 'active' WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("userRepository.ActivateUser: %w", err)
	}
	return nil
}
