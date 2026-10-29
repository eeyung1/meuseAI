-- Version 2: Social sharing link submissions for bonus XP
CREATE TABLE IF NOT EXISTS social_shares (
    id BIGSERIAL PRIMARY KEY,
    student_id VARCHAR(64) NOT NULL REFERENCES students(id) ON DELETE CASCADE,
    day_number INTEGER NOT NULL CHECK (day_number BETWEEN 1 AND 4),
    share_url TEXT NOT NULL,
    platform VARCHAR(32) NOT NULL DEFAULT 'facebook',
    status VARCHAR(32) NOT NULL DEFAULT 'pending',
    xp_awarded INTEGER NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    reviewed_at TIMESTAMPTZ,
    UNIQUE (student_id, day_number)
);
CREATE INDEX IF NOT EXISTS idx_social_shares_status ON social_shares (status, created_at);
CREATE INDEX IF NOT EXISTS idx_social_shares_student ON social_shares (student_id);
