-- Track releases that failed processing

CREATE TABLE IF NOT EXISTS failed_releases (
    tag TEXT PRIMARY KEY,
    date DATE NOT NULL,
    attempt_count INTEGER NOT NULL DEFAULT 1,
    last_error TEXT NOT NULL,
    first_failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_failed_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    permanent BOOLEAN NOT NULL DEFAULT FALSE
);

CREATE INDEX IF NOT EXISTS idx_failed_releases_date ON failed_releases(date);
CREATE INDEX IF NOT EXISTS idx_failed_releases_permanent ON failed_releases(permanent);
