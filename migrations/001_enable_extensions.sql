-- Migration 001: Enable required extensions
-- Creates pgvector for vector embeddings, fuzzystrmatch for string functions, and pg_trgm for trigram similarity

-- Enable pgvector extension for vector embeddings and vector similarity search
CREATE EXTENSION IF NOT EXISTS vector;

-- Enable fuzzystrmatch extension for string similarity functions (levenshtein, soundex, etc.)
CREATE EXTENSION IF NOT EXISTS fuzzystrmatch;

-- Enable pg_trgm extension for trigram-based similarity search
CREATE EXTENSION IF NOT EXISTS pg_trgm;

-- Verify extensions are enabled
SELECT
    extname,
    extversion
FROM pg_extension
WHERE extname IN ('vector', 'fuzzystrmatch', 'pg_trgm');
