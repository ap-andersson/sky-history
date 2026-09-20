-- Read-only public access to search and stats, for anyone who emails to ask
-- for it (see the README's Public API section). A key is minted with two
-- SQL statements by hand -- no admin UI, matching how feeders are approved.
--
-- Only the hash of a key is ever stored, the same reasoning as a password:
-- whoever runs this database should not be able to read out a live key and
-- use it. pgcrypto's digest() computes that hash server-side, and
-- gen_random_bytes() generates the key itself, so minting one never needs the
-- raw value to pass through anywhere but the operator's own terminal.
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS api_keys (
    id         SERIAL PRIMARY KEY,
    key_hash   TEXT UNIQUE NOT NULL,

    -- Whose integration this is, and why -- both free text the operator fills
    -- in from the access-request email, kept for their own future reference
    -- rather than read by anything in the application.
    name       TEXT NOT NULL,
    contact    TEXT,
    purpose    TEXT,

    -- The approval switch, same pattern as feeders.enabled: nothing in the
    -- application ever sets this, an administrator flips it by hand.
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,

    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    last_used_at  TIMESTAMPTZ,
    request_count BIGINT NOT NULL DEFAULT 0
);
