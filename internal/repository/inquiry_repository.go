package repository

import (
	"context"
	"fmt"

	"github.com/aarabhyate/backend/internal/models"
	"github.com/jmoiron/sqlx"
)

// InquiryRepository defines all data-access operations for the project_inquiries table.
type InquiryRepository interface {
	// Create persists a new project inquiry and back-fills the generated UUID and timestamp.
	Create(ctx context.Context, inq *models.ProjectInquiry) error

	// GetByID retrieves a single inquiry by its UUID.
	GetByID(ctx context.Context, id string) (*models.ProjectInquiry, error)

	// List returns a paginated list of inquiries ordered by submitted_at DESC.
	// Optionally filter by status or project type (empty string = no filter).
	List(ctx context.Context, status, projectType string, limit, offset int) ([]*models.ProjectInquiry, error)

	// UpdateStatus changes the lifecycle status of an inquiry (admin action).
	UpdateStatus(ctx context.Context, id string, status models.InquiryStatus) error

	// AddAdminNotes sets internal notes on an inquiry (admin action).
	AddAdminNotes(ctx context.Context, id, notes string) error
}

type inquiryRepository struct{ db *sqlx.DB }

// NewInquiryRepository returns a concrete InquiryRepository backed by Postgres.
func NewInquiryRepository(db *sqlx.DB) InquiryRepository { return &inquiryRepository{db: db} }

// Create inserts an inquiry row and back-fills generated fields (id, submitted_at).
func (r *inquiryRepository) Create(ctx context.Context, inq *models.ProjectInquiry) error {
	const q = `
		INSERT INTO project_inquiries
			(id, full_name, email, project_type, budget_estimate, timeline,
			 technical_brief, status, ip_address, submitted_at, updated_at)
		VALUES
			(gen_random_uuid(), :full_name, :email, :project_type, :budget_estimate, :timeline,
			 :technical_brief, 'new', :ip_address, NOW(), NOW())
		RETURNING id, status, submitted_at, updated_at`

	rows, err := r.db.NamedQueryContext(ctx, q, inq)
	if err != nil {
		return fmt.Errorf("inquiryRepository.Create: %w", err)
	}
	defer rows.Close()
	if rows.Next() {
		return rows.StructScan(inq)
	}
	return fmt.Errorf("inquiryRepository.Create: no row returned")
}

// GetByID fetches a single inquiry by UUID.
func (r *inquiryRepository) GetByID(ctx context.Context, id string) (*models.ProjectInquiry, error) {
	inq := &models.ProjectInquiry{}
	if err := r.db.GetContext(ctx, inq,
		`SELECT * FROM project_inquiries WHERE id = $1`, id); err != nil {
		return nil, fmt.Errorf("inquiryRepository.GetByID: %w", err)
	}
	return inq, nil
}

// List returns inquiries with optional status / type filters and pagination.
func (r *inquiryRepository) List(
	ctx context.Context,
	status, projectType string,
	limit, offset int,
) ([]*models.ProjectInquiry, error) {
	// Build the WHERE clause dynamically to avoid blind string interpolation
	args := []any{}
	where := "WHERE 1=1"
	i := 1

	if status != "" {
		where += fmt.Sprintf(" AND status = $%d", i)
		args = append(args, status)
		i++
	}
	if projectType != "" {
		where += fmt.Sprintf(" AND project_type = $%d", i)
		args = append(args, projectType)
		i++
	}

	// Append pagination params
	args = append(args, limit, offset)
	q := fmt.Sprintf(`
		SELECT * FROM project_inquiries
		%s
		ORDER BY submitted_at DESC
		LIMIT $%d OFFSET $%d`, where, i, i+1)

	var results []*models.ProjectInquiry
	if err := r.db.SelectContext(ctx, &results, q, args...); err != nil {
		return nil, fmt.Errorf("inquiryRepository.List: %w", err)
	}
	return results, nil
}

// UpdateStatus transitions an inquiry to a new lifecycle status.
func (r *inquiryRepository) UpdateStatus(ctx context.Context, id string, status models.InquiryStatus) error {
	const q = `UPDATE project_inquiries SET status = $1 WHERE id = $2`
	if _, err := r.db.ExecContext(ctx, q, status, id); err != nil {
		return fmt.Errorf("inquiryRepository.UpdateStatus: %w", err)
	}
	return nil
}

// AddAdminNotes upserts internal admin notes for an inquiry.
func (r *inquiryRepository) AddAdminNotes(ctx context.Context, id, notes string) error {
	const q = `UPDATE project_inquiries SET admin_notes = $1 WHERE id = $2`
	if _, err := r.db.ExecContext(ctx, q, notes, id); err != nil {
		return fmt.Errorf("inquiryRepository.AddAdminNotes: %w", err)
	}
	return nil
}
