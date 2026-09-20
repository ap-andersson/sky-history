-- Live gap-filling from crowdsourced feeders.
--
-- The archive only becomes searchable once adsb.lol has published the day and
-- the processor has ingested it, which leaves the most recent 24-48 hours
-- invisible. A feeder is an ADS-B receiver serving readsb's aircraft.json;
-- the collector polls the enabled ones and writes flight rows for that gap.
--
-- Anyone may submit a feeder, but a submission is inert: the row lands with
-- enabled = FALSE and stays that way until an administrator flips it by hand.

CREATE TABLE IF NOT EXISTS feeders (
    id         SERIAL PRIMARY KEY,
    -- Normalised (lowercase scheme and host, default port dropped) so the
    -- same receiver cannot be submitted twice under cosmetic variations.
    url        TEXT UNIQUE NOT NULL,
    name       TEXT NOT NULL,

    -- The approval switch. Nothing in the application ever sets this to TRUE.
    enabled    BOOLEAN NOT NULL DEFAULT FALSE,

    -- What the submitter told us, and the little we record about them.
    -- submitter_contact is free text they chose to type; submitter_ip_hash is
    -- an HMAC of the submitting address under a secret salt, kept only to rate
    -- limit submissions. Neither is ever exposed by the API.
    submitted_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    submitter_contact TEXT,
    submitter_ip_hash TEXT,

    -- Result of the probe run at submission time.
    validated_at     TIMESTAMPTZ,
    validation_ok    BOOLEAN,
    validation_error TEXT,
    probe_aircraft   INTEGER,

    -- Maintained by the collector while polling.
    last_poll_at TIMESTAMPTZ,
    last_ok_at   TIMESTAMPTZ,
    last_error   TEXT,
    fail_streak  INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_feeders_enabled ON feeders(enabled) WHERE enabled;
CREATE INDEX IF NOT EXISTS idx_feeders_ip_hash ON feeders(submitter_ip_hash, submitted_at);

-- Provenance on the flight row.
--
-- 'archive' is the adsb.lol release data that was the only source until now,
-- so the default backfills every existing row correctly. 'live' rows come from
-- a feeder and are provisional: when the release covering their date is
-- ingested, the processor deletes them and the archive replaces them wholesale.
-- The two disagree about segment boundaries -- the archive splits on the
-- trace's new-leg flag, the collector on a gap timeout -- so merging them would
-- leave near-duplicates that no unique constraint would catch.
ALTER TABLE flights ADD COLUMN IF NOT EXISTS source TEXT NOT NULL DEFAULT 'archive';

-- NOT VALID: the constraint binds every new and updated row from here on, but
-- Postgres skips the full-table scan that verifying the existing millions
-- would need. They all carry the default, so there is nothing to find.
DO $$
BEGIN
    ALTER TABLE flights ADD CONSTRAINT flights_source_check
        CHECK (source IN ('archive', 'live')) NOT VALID;
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- Which feeder opened the segment. Null for archive rows, and for live rows
-- whose feeder has since been deleted. Several feeders often see the same
-- flight and the collector merges them into one row, so this names the first
-- one to report it rather than all of them -- enough to purge a feeder that
-- turns out to be serving junk.
ALTER TABLE flights ADD COLUMN IF NOT EXISTS feeder_id INTEGER
    REFERENCES feeders(id) ON DELETE SET NULL;

-- Both indexes are partial: live rows are a tiny minority of the table and
-- these only ever serve live queries, so the archive pays nothing for them.
CREATE INDEX IF NOT EXISTS idx_flights_live_date ON flights(date) WHERE source = 'live';
CREATE INDEX IF NOT EXISTS idx_flights_feeder ON flights(feeder_id) WHERE feeder_id IS NOT NULL;

-- The collector checks this table on every flight it writes, to avoid writing
-- into a day the archive already owns. Looked up by date, which until now only
-- ever had the primary key on tag.
CREATE INDEX IF NOT EXISTS idx_processed_releases_date ON processed_releases(date);

-- Whether this aircraft has ever appeared in archive data.
--
-- The collector upserts aircraft rows too, because a live flight needs its
-- registration and type. That would otherwise let an airframe seen only in the
-- gap window count towards the aircraft totals, which are meant to describe
-- the archive. The processor sets this; the collector never does.
--
-- Existing rows all predate the collector, so the backfill below is exact.
ALTER TABLE aircraft ADD COLUMN IF NOT EXISTS archive_seen BOOLEAN NOT NULL DEFAULT FALSE;
UPDATE aircraft SET archive_seen = TRUE WHERE NOT archive_seen;

-- Partial: the statistics count only the true rows, and live-only aircraft are
-- a small minority that the index does not need to carry.
CREATE INDEX IF NOT EXISTS idx_aircraft_archive_seen ON aircraft(icao) WHERE archive_seen;
