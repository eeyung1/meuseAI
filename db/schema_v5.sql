-- Version 5: separately purchased Tier 1 learning tracks.
ALTER TABLE students ADD COLUMN tier1_landing BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE students ADD COLUMN tier1_video BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE students SET tier1_landing=TRUE WHERE funnel_tier IN ('tier_1_active','tier_2_active');
ALTER TABLE payments ADD COLUMN tracks VARCHAR(64) NOT NULL DEFAULT 'landing_pages';
