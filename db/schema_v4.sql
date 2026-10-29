-- Version 4: curated admin images, capped to twelve compressed JPEGs.
CREATE TABLE showcase_images (
 id VARCHAR(16) PRIMARY KEY,
 content BYTEA NOT NULL,
 sha256 VARCHAR(64) NOT NULL,
 source VARCHAR(32) NOT NULL,
 approved BOOLEAN NOT NULL DEFAULT FALSE,
 review_note TEXT NOT NULL DEFAULT '',
 uploaded_at TIMESTAMPTZ NOT NULL,
 reviewed_at TIMESTAMPTZ
);
CREATE TABLE showcase_settings (
 id INTEGER PRIMARY KEY CHECK (id=1),
 published BOOLEAN NOT NULL DEFAULT FALSE,
 rights_note TEXT NOT NULL DEFAULT '',
 published_at TIMESTAMPTZ
);
INSERT INTO showcase_settings(id,published) VALUES(1,FALSE);
ALTER TABLE showcase_images ENABLE ROW LEVEL SECURITY;
ALTER TABLE showcase_settings ENABLE ROW LEVEL SECURITY;
REVOKE ALL ON showcase_images,showcase_settings FROM PUBLIC;
