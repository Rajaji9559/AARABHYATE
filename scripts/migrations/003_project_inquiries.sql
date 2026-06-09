-- =============================================================================
-- AARABHYATE — Stage 3 Migration: Project Inquiries Table
-- Run: psql -U <user> -d aarabhyate_db -f scripts/migrations/003_project_inquiries.sql
-- =============================================================================

-- ENUM for project types matching the frontend dropdown exactly
DO $$ BEGIN
    CREATE TYPE project_type AS ENUM (
        'embedded_systems',
        'ai_ml',
        'custom_automation',
        'robotics_hardware',
        'other'
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- ENUM for inquiry lifecycle status
DO $$ BEGIN
    CREATE TYPE inquiry_status AS ENUM (
        'new',          -- just submitted
        'reviewed',     -- team has read it
        'in_progress',  -- actively being scoped / quoted
        'closed'        -- completed, declined, or archived
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- =============================================================================
-- PROJECT INQUIRIES
-- =============================================================================

CREATE TABLE IF NOT EXISTS project_inquiries (
    id              UUID            PRIMARY KEY DEFAULT gen_random_uuid(),
    full_name       VARCHAR(255)    NOT NULL,
    email           VARCHAR(320)    NOT NULL,
    project_type    project_type    NOT NULL,
    budget_estimate VARCHAR(100),                        -- free-form range, e.g. "₹50K – ₹1L"
    timeline        VARCHAR(100),                        -- e.g. "3 months", "Q3 2026"
    technical_brief TEXT            NOT NULL,
    status          inquiry_status  NOT NULL DEFAULT 'new',
    admin_notes     TEXT,                                -- internal notes, never shown to client
    ip_address      INET,                                -- stored for abuse detection
    submitted_at    TIMESTAMPTZ     NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ     NOT NULL DEFAULT NOW()
);

-- Fast admin dashboard queries: latest first, filter by status/type
CREATE INDEX IF NOT EXISTS idx_inquiries_status       ON project_inquiries (status);
CREATE INDEX IF NOT EXISTS idx_inquiries_project_type ON project_inquiries (project_type);
CREATE INDEX IF NOT EXISTS idx_inquiries_email        ON project_inquiries (email);
CREATE INDEX IF NOT EXISTS idx_inquiries_submitted_at ON project_inquiries (submitted_at DESC);

-- Auto-update trigger (reuses the trigger function from Stage 1)
DROP TRIGGER IF EXISTS set_updated_at ON project_inquiries;
CREATE TRIGGER set_updated_at
    BEFORE UPDATE ON project_inquiries
    FOR EACH ROW EXECUTE FUNCTION trigger_set_updated_at();
