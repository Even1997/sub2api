ALTER TABLE groups ADD COLUMN IF NOT EXISTS cache_read_reduction_rules JSONB NOT NULL DEFAULT '[]'::jsonb;
