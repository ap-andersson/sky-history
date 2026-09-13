-- Denormalise the aircraft type onto flights.
--
-- Searching "all flights of type X, most recent first" was resolved by joining
-- flights to aircraft and ordering by date. The planner cannot estimate how
-- many flights a type has -- the correlation lives across two tables -- so for
-- a rare type it walked the whole date index looking for matches: 5.3s for a
-- type with 133 flights, against 76ms for one with 3.1M.
--
-- With the type on the row, a single index answers the query directly in sort
-- order, so LIMIT stops after the rows it needs regardless of how rare the
-- type is.
--
-- The column is populated by the processor for new flights. Existing rows are
-- filled in by db/maintenance/003_backfill_flight_type.sql, and the supporting
-- index is created there too: CREATE INDEX CONCURRENTLY cannot run inside the
-- transaction this migration runner uses.

ALTER TABLE flights ADD COLUMN IF NOT EXISTS aircraft_type_id INTEGER
    REFERENCES aircraft_types(id);
