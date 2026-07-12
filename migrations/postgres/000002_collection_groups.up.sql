CREATE TABLE IF NOT EXISTS collection_groups (name VARCHAR(64) PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(), updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW());
INSERT INTO collection_groups(name) SELECT DISTINCT group_name FROM collection_points WHERE deleted=FALSE ON CONFLICT(name) DO NOTHING;
INSERT INTO collection_groups(name) VALUES('default') ON CONFLICT(name) DO NOTHING;
ALTER TABLE collection_points DROP COLUMN IF EXISTS valid_min;
ALTER TABLE collection_points DROP COLUMN IF EXISTS valid_max;
