-- =============================================================================
-- AARABHYATE — Stage 2 Migration: User Status + OTP Table
-- Run AFTER scripts/schema.sql (Stage 1 base schema)
-- psql -U <user> -d <database> -f scripts/migrations/002_auth_otp.sql
-- =============================================================================

-- ── New enum: user lifecycle status ──────────────────────────────────────────
DO $$ BEGIN
    CREATE TYPE user_status AS ENUM ('pending', 'active', 'suspended');
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

-- ── Add status column to users (defaults to pending until OTP verified) ──────
ALTER TABLE users
    ADD COLUMN IF NOT EXISTS status user_status NOT NULL DEFAULT 'pending';

CREATE INDEX IF NOT EXISTS idx_users_status ON users (status);

-- =============================================================================
-- OTP TABLE
-- Stores one-time passwords for email verification.
-- Each row is invalidated on first use or expiry.
-- =============================================================================

CREATE TABLE IF NOT EXISTS otps (
    id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    code       CHAR(6)     NOT NULL,
    purpose    VARCHAR(32) NOT NULL DEFAULT 'email_verification',
    used       BOOLEAN     NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Partial index: fast lookup of unused, unexpired OTPs for a given user
CREATE INDEX IF NOT EXISTS idx_otps_user_unused
    ON otps (user_id, code)
    WHERE used = FALSE;
