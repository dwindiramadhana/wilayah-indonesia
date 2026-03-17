-- Migration 004: Create vector embedding columns (optional, for semantic search)
-- Adds pgvector columns for advanced similarity search

-- Add vector embedding column for semantic search (optional feature)
-- Using 384 dimensions as default (common for sentence-transformers)
-- Adjust dimension based on your embedding model

ALTER TABLE regions
ADD COLUMN IF NOT EXISTS embedding vector(384);

-- Create HNSW index for fast approximate nearest neighbor search
-- Note: HNSW requires pgvector >= 0.5.0 and PostgreSQL >= 16
-- If HNSW is not available, this will be skipped or use IVFFlat as fallback

-- Check if HNSW is available and create index
DO $$
BEGIN
    -- Try to create HNSW index (may fail on older pgvector versions)
    BEGIN
        CREATE INDEX IF NOT EXISTS idx_regions_embedding_hnsw
            ON regions USING HNSW (embedding vector_cosine_ops)
            WITH (m = 16, ef_construction = 64);
    EXCEPTION WHEN OTHERS THEN
        -- HNSW not available, skip index creation
        RAISE NOTICE 'HNSW index not available, skipping embedding index';
    END;
END $$;

-- Add comment to column
COMMENT ON COLUMN regions.embedding IS 'Vector embedding for semantic search (384 dimensions, cosine similarity)';
