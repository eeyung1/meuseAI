-- Version 1: student journey foundation. Applied transactionally by db.Migrate.
CREATE TABLE students (
    id VARCHAR(64) NOT NULL PRIMARY KEY,
    full_name VARCHAR(255) NOT NULL,
    email VARCHAR(255) NOT NULL UNIQUE,
    email_verified BOOLEAN NOT NULL DEFAULT FALSE,
    verification_token VARCHAR(128) UNIQUE,
    consent_timestamp TIMESTAMPTZ NOT NULL,
    unsubscribe_token VARCHAR(64) NOT NULL UNIQUE,
    unsubscribed_at TIMESTAMPTZ,
    current_stage VARCHAR(32) NOT NULL DEFAULT 'day_1',
    engagement_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    funnel_tier VARCHAR(32) NOT NULL DEFAULT 'free_week',
    payment_status VARCHAR(32) NOT NULL DEFAULT 'pending',
    tier_2_ready BOOLEAN NOT NULL DEFAULT FALSE,
    xp INTEGER NOT NULL DEFAULT 0 CHECK (xp >= 0),
    streak_days INTEGER NOT NULL DEFAULT 1 CHECK (streak_days >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX idx_students_email_lower ON students (LOWER(email));
CREATE INDEX idx_students_stage_status ON students (current_stage, engagement_status);
CREATE TABLE student_progress (
    id BIGSERIAL PRIMARY KEY,
    student_id VARCHAR(64) NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    day_number INTEGER NOT NULL CHECK (day_number BETWEEN 1 AND 4),
    completed BOOLEAN NOT NULL DEFAULT FALSE,
    submission_payload TEXT,
    completed_at TIMESTAMPTZ,
    last_nudge_at TIMESTAMPTZ,
    UNIQUE (student_id, day_number)
);
CREATE TABLE payments (
    id BIGSERIAL PRIMARY KEY,
    reference VARCHAR(128) NOT NULL UNIQUE,
    student_id VARCHAR(64) NOT NULL REFERENCES students(id),
    amount_kobo BIGINT NOT NULL CHECK (amount_kobo > 0),
    currency VARCHAR(8) NOT NULL DEFAULT 'NGN',
    tier_unlocked VARCHAR(32) NOT NULL,
    status VARCHAR(32) NOT NULL,
    paystack_signature VARCHAR(255),
    raw_payload JSONB,
    paid_at TIMESTAMPTZ
);
CREATE TABLE activity_logs (
    id BIGSERIAL PRIMARY KEY,
    student_id VARCHAR(64) REFERENCES students(id) ON DELETE SET NULL,
    event_type VARCHAR(64) NOT NULL,
    details JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_payments_student ON payments (student_id);
CREATE INDEX idx_activity_student_created ON activity_logs (student_id, created_at);
