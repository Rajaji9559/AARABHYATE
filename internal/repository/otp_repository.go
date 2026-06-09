package repository

import (
	"context"
	"fmt"
	"time"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/jmoiron/sqlx"
)

// OTPRepository defines all data-access operations for the otps table.
type OTPRepository interface {
	// Create persists a new OTP record for the given user.
	Create(ctx context.Context, otp *models.OTP) error

	// FindValid retrieves the most recent unused, unexpired OTP for a user+purpose.
	// Returns an error (wrapping sql.ErrNoRows) if none is found.
	FindValid(ctx context.Context, userID, code, purpose string) (*models.OTP, error)

	// MarkUsed marks an OTP row as consumed so it cannot be replayed.
	MarkUsed(ctx context.Context, id string) error

	// InvalidateAll marks all pending OTPs for a user+purpose as used.
	// Call before issuing a fresh OTP to avoid accumulation of stale codes.
	InvalidateAll(ctx context.Context, userID, purpose string) error
}

type otpRepository struct{ db *sqlx.DB }

// NewOTPRepository returns a concrete OTPRepository backed by Postgres.
func NewOTPRepository(db *sqlx.DB) OTPRepository { return &otpRepository{db: db} }

// Create inserts a new OTP row and back-fills the generated fields.
func (r *otpRepository) Create(ctx context.Context, otp *models.OTP) error {
	const q = `
		INSERT INTO otps (id, user_id, code, purpose, used, expires_at, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, FALSE, $4, NOW())
		RETURNING id, created_at`

	if err := r.db.QueryRowxContext(ctx, q,
		otp.UserID,
		otp.Code,
		otp.Purpose,
		otp.ExpiresAt,
	).StructScan(otp); err != nil {
		return fmt.Errorf("otpRepository.Create: %w", err)
	}
	return nil
}

// FindValid looks up a single, unexpired, unused OTP matching the supplied code.
// The query is index-covered by idx_otps_user_unused.
func (r *otpRepository) FindValid(ctx context.Context, userID, code, purpose string) (*models.OTP, error) {
	const q = `
		SELECT *
		FROM   otps
		WHERE  user_id    = $1
		  AND  code       = $2
		  AND  purpose    = $3
		  AND  used       = FALSE
		  AND  expires_at > NOW()
		ORDER  BY created_at DESC
		LIMIT  1`

	otp := &models.OTP{}
	if err := r.db.GetContext(ctx, otp, q, userID, code, purpose); err != nil {
		return nil, fmt.Errorf("otpRepository.FindValid: %w", err)
	}
	return otp, nil
}

// MarkUsed sets used=true on a specific OTP row to prevent replay attacks.
func (r *otpRepository) MarkUsed(ctx context.Context, id string) error {
	const q = `UPDATE otps SET used = TRUE WHERE id = $1`
	if _, err := r.db.ExecContext(ctx, q, id); err != nil {
		return fmt.Errorf("otpRepository.MarkUsed: %w", err)
	}
	return nil
}

// InvalidateAll bulk-expires all pending OTPs for a user+purpose.
// Called before issuing a fresh code to avoid multiple valid tokens coexisting.
func (r *otpRepository) InvalidateAll(ctx context.Context, userID, purpose string) error {
	const q = `
		UPDATE otps
		SET    used = TRUE, expires_at = $1
		WHERE  user_id = $2 AND purpose = $3 AND used = FALSE`

	if _, err := r.db.ExecContext(ctx, q, time.Now(), userID, purpose); err != nil {
		return fmt.Errorf("otpRepository.InvalidateAll: %w", err)
	}
	return nil
}
