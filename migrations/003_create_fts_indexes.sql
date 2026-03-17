-- Migration 003: Create full-text search indexes and triggers
-- Sets up native PostgreSQL FTS with automatic tsvector updates

-- Create GIN index for full-text search (fast text searching)
CREATE INDEX IF NOT EXISTS idx_regions_fts_vector ON regions USING GIN (full_text_vector);

-- Create function to automatically update tsvector column
-- This ensures full_text_vector stays in sync with full_text
CREATE OR REPLACE FUNCTION update_regions_fts_vector() RETURNS TRIGGER AS $$
BEGIN
    -- Use 'simple' text search configuration (no stemming, language-agnostic)
    -- This works well for Indonesian proper nouns (region names)
    NEW.full_text_vector := to_tsvector('simple', COALESCE(NEW.full_text, ''));
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to auto-update tsvector on INSERT or UPDATE
DROP TRIGGER IF EXISTS trigger_regions_fts_update ON regions;
CREATE TRIGGER trigger_regions_fts_update
    BEFORE INSERT OR UPDATE ON regions
    FOR EACH ROW
    EXECUTE FUNCTION update_regions_fts_vector();

-- Populate full_text for existing rows (if any)
UPDATE regions
SET full_text = COALESCE(subdistrict, '') || ' ' ||
                COALESCE(district, '') || ' ' ||
                COALESCE(city, '') || ' ' ||
                COALESCE(province, '') || ' ' ||
                COALESCE(postal_code, '')
WHERE full_text IS NULL;

-- Verify the trigger exists
SELECT
    trigger_name,
    event_manipulation,
    event_object_table
FROM information_schema.triggers
WHERE trigger_name = 'trigger_regions_fts_update';
