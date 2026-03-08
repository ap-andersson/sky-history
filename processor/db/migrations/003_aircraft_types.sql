-- Aircraft types lookup table

CREATE TABLE IF NOT EXISTS aircraft_types (
    id SERIAL PRIMARY KEY,
    type_code TEXT UNIQUE NOT NULL,
    description TEXT NOT NULL DEFAULT ''
);

ALTER TABLE aircraft ADD COLUMN IF NOT EXISTS aircraft_type_id INTEGER REFERENCES aircraft_types(id);
CREATE INDEX IF NOT EXISTS idx_aircraft_type_id ON aircraft(aircraft_type_id);

-- Backfill: populate aircraft_types from existing aircraft data
INSERT INTO aircraft_types (type_code, description)
SELECT DISTINCT type_code, MAX(description)
FROM aircraft
WHERE type_code IS NOT NULL AND type_code != ''
GROUP BY type_code
ON CONFLICT DO NOTHING;

-- Link existing aircraft to their types
UPDATE aircraft SET aircraft_type_id = at.id
FROM aircraft_types at
WHERE aircraft.type_code = at.type_code
  AND aircraft.type_code IS NOT NULL
  AND aircraft.type_code != '';
