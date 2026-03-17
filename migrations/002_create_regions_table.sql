-- Migration 002: Create regions table
-- Main table for Indonesian administrative regions (Wilayah Indonesia)

-- Drop existing table if exists (for clean migration)
DROP TABLE IF EXISTS regions CASCADE;

-- Create main regions table
CREATE TABLE regions (
    -- Primary key: unique region code (e.g., "11.01.01.2001")
    id TEXT PRIMARY KEY,

    -- Administrative hierarchy
    subdistrict TEXT NOT NULL,  -- Kelurahan/Desi (lowest level)
    district   TEXT NOT NULL,   -- Kecamatan
    city       TEXT NOT NULL,   -- Kabupaten/Kota
    province   TEXT NOT NULL,   -- Provinsi

    -- Postal code
    postal_code TEXT,

    -- Full text for FTS (concatenation of searchable fields)
    full_text TEXT,

    -- TSVECTOR column for native PostgreSQL full-text search
    -- Automatically updated via trigger
    full_text_vector TSVECTOR,

    -- BPS (Badan Pusat Statistik) mapping columns
    -- These may differ from standard wilayah data
    subdistrict_bps      TEXT,
    subdistrict_bps_code TEXT,
    district_bps         TEXT,
    district_bps_code    TEXT,
    city_bps             TEXT,
    city_bps_code        TEXT,
    province_bps         TEXT,
    province_bps_code    TEXT,

    -- Metadata
    created_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP
);

-- Create indexes for hierarchical lookups
CREATE INDEX idx_regions_province ON regions (province);
CREATE INDEX idx_regions_city ON regions (city);
CREATE INDEX idx_regions_district ON regions (district);
CREATE INDEX idx_regions_subdistrict ON regions (subdistrict);
CREATE INDEX idx_regions_postal_code ON regions (postal_code);

-- Create index for BPS code lookups
CREATE INDEX idx_regions_subdistrict_bps_code ON regions (subdistrict_bps_code);
CREATE INDEX idx_regions_district_bps_code ON regions (district_bps_code);
CREATE INDEX idx_regions_city_bps_code ON regions (city_bps_code);
CREATE INDEX idx_regions_province_bps_code ON regions (province_bps_code);

-- Add comment to table
COMMENT ON TABLE regions IS 'Indonesian administrative regions (Wilayah Indonesia) with BPS mapping support';
