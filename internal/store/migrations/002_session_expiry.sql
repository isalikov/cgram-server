ALTER TABLE sessions ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

CREATE INDEX idx_sessions_expires ON sessions(expires_at) WHERE expires_at IS NOT NULL;
