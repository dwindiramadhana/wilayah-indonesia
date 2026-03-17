package postgres

import (
	"context"
	"fmt"
	"log/slog"

	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
	ingestionusecase "github.com/ilmimris/wilayah-indonesia/internal/usecase/ingestion"
)

// IngestionUseCase coordinates dataset refresh operations for PostgreSQL.
type IngestionUseCase struct {
	loader     SQLLoader
	normalizer SQLNormalizer
	adminRepo  *AdminRepository
	logger     *slog.Logger
}

// SQLLoader loads SQL scripts from a source (e.g. filesystem).
type SQLLoader interface {
	Load(path string) (string, error)
}

// SQLNormalizer adjusts SQL scripts to match PostgreSQL syntax expectations.
type SQLNormalizer interface {
	Normalize(sql string) string
}

// NewIngestionUseCase constructs a UseCase instance.
func NewIngestionUseCase(loader SQLLoader, normalizer SQLNormalizer, adminRepo *AdminRepository, logger *slog.Logger) *IngestionUseCase {
	if logger == nil {
		logger = slog.Default()
	}
	return &IngestionUseCase{
		loader:     loader,
		normalizer: normalizer,
		adminRepo:  adminRepo,
		logger:     logger,
	}
}

// Refresh executes the PostgreSQL ingestion workflow.
func (uc *IngestionUseCase) Refresh(ctx context.Context, opts ingestionusecase.RefreshOptions) error {
	uc.logger.Info("Starting PostgreSQL refresh", "wilayah_sql", opts.WilayahSQLPath)

	if err := uc.executeSQLFile(ctx, opts.WilayahSQLPath); err != nil {
		return err
	}
	if err := uc.executeSQLFile(ctx, opts.PostalSQLPath); err != nil {
		return err
	}
	if err := uc.executeSQLFile(ctx, opts.BPSMappingSQLPath); err != nil {
		return err
	}

	// Execute PostgreSQL-specific transformation and indexing
	for _, stmt := range []struct {
		description string
		query       string
	}{
		{"clear regions table", "TRUNCATE regions;"},
		{"denormalize wilayah dataset", denormalizeQuery},
		{"update full_text column", "UPDATE regions SET full_text = full_text WHERE id IS NOT NULL;"},
		{"create FTS index", createFTSIndexQuery},
		{"drop raw wilayah table", "DROP TABLE IF EXISTS wilayah;"},
		{"drop postal table", "DROP TABLE IF EXISTS wilayah_kodepos;"},
	} {
		if err := uc.exec(ctx, stmt.description, stmt.query); err != nil {
			return err
		}
	}

	uc.logger.Info("PostgreSQL refresh completed successfully")
	return nil
}

func (uc *IngestionUseCase) executeSQLFile(ctx context.Context, path string) error {
	if path == "" {
		return sharederrors.New(sharederrors.CodeInvalidInput, "SQL path is required")
	}
	contents, err := uc.loader.Load(path)
	if err != nil {
		return sharederrors.Wrap(sharederrors.CodeInvalidInput, fmt.Sprintf("failed to read SQL file %s", path), err)
	}
	normalized := uc.normalizer.Normalize(contents)
	uc.logger.Info("Executing SQL file", "path", path)
	return uc.exec(ctx, fmt.Sprintf("execute SQL file %s", path), normalized)
}

func (uc *IngestionUseCase) exec(ctx context.Context, description, query string) error {
	if err := uc.adminRepo.Exec(ctx, query); err != nil {
		uc.logger.Error("SQL execution failed", "step", description, "error", err)
		return err
	}
	uc.logger.Info("Step completed", "step", description)
	return nil
}

const denormalizeQuery = `
INSERT INTO regions (
    id, subdistrict, district, city, province, postal_code,
    subdistrict_bps, subdistrict_bps_code,
    district_bps, district_bps_code,
    city_bps, city_bps_code,
    province_bps, province_bps_code,
    full_text
)
SELECT
    sub.kode AS id,
    sub.nama AS subdistrict,
    dist.nama AS district,
    city.nama AS city,
    prov.nama AS province,
    kodepos.kodepos AS postal_code,
    bps_sub.nama_bps AS subdistrict_bps,
    bps_sub.kode_bps AS subdistrict_bps_code,
    bps_dist.nama_bps AS district_bps,
    bps_dist.kode_bps AS district_bps_code,
    bps_city.nama_bps AS city_bps,
    bps_city.kode_bps AS city_bps_code,
    bps_prov.nama_bps AS province_bps,
    bps_prov.kode_bps AS province_bps_code,
    LOWER(TRIM(CONCAT(
        COALESCE(kodepos.kodepos::TEXT, ''), ' ',
        prov.nama, ' ',
        city.nama, ' ',
        dist.nama, ' ',
        sub.nama
    ))) AS full_text
FROM
    wilayah AS sub
JOIN wilayah AS dist ON dist.kode = SUBSTRING(sub.kode FROM 1 FOR 8)
JOIN wilayah AS city ON city.kode = SUBSTRING(sub.kode FROM 1 FOR 5)
JOIN wilayah AS prov ON prov.kode = SUBSTRING(sub.kode FROM 1 FOR 2)
LEFT JOIN wilayah_kodepos AS kodepos ON kodepos.kode = sub.kode
LEFT JOIN (
    SELECT DISTINCT ON (kode_dagri) kode_dagri, nama_bps, kode_bps
    FROM bps_wilayah
    WHERE kode_dagri != '0'
    ORDER BY kode_dagri, kode_bps
) AS bps_sub ON bps_sub.kode_dagri = sub.kode
LEFT JOIN (
    SELECT DISTINCT ON (kode_dagri) kode_dagri, nama_bps, kode_bps
    FROM bps_wilayah
    WHERE kode_dagri != '0'
    ORDER BY kode_dagri, kode_bps
) AS bps_dist ON bps_dist.kode_dagri = dist.kode
LEFT JOIN (
    SELECT DISTINCT ON (kode_dagri) kode_dagri, nama_bps, kode_bps
    FROM bps_wilayah
    WHERE kode_dagri != '0'
    ORDER BY kode_dagri, kode_bps
) AS bps_city ON bps_city.kode_dagri = city.kode
LEFT JOIN (
    SELECT DISTINCT ON (kode_dagri) kode_dagri, nama_bps, kode_bps
    FROM bps_wilayah
    WHERE kode_dagri != '0'
    ORDER BY kode_dagri, kode_bps
) AS bps_prov ON bps_prov.kode_dagri = prov.kode
WHERE
    LENGTH(sub.kode) = 13
`

const createFTSIndexQuery = `
-- Create GIN index for full-text search if it doesn't exist
CREATE INDEX IF NOT EXISTS idx_regions_fts_vector ON regions USING GIN (full_text_vector);

-- Create hierarchical lookup indexes
CREATE INDEX IF NOT EXISTS idx_regions_province ON regions (province);
CREATE INDEX IF NOT EXISTS idx_regions_city ON regions (city);
CREATE INDEX IF NOT EXISTS idx_regions_district ON regions (district);
CREATE INDEX IF NOT EXISTS idx_regions_subdistrict ON regions (subdistrict);
CREATE INDEX IF NOT EXISTS idx_regions_postal_code ON regions (postal_code);

-- Create BPS code indexes
CREATE INDEX IF NOT EXISTS idx_regions_subdistrict_bps_code ON regions (subdistrict_bps_code);
CREATE INDEX IF NOT EXISTS idx_regions_district_bps_code ON regions (district_bps_code);
CREATE INDEX IF NOT EXISTS idx_regions_city_bps_code ON regions (city_bps_code);
CREATE INDEX IF NOT EXISTS idx_regions_province_bps_code ON regions (province_bps_code);
`
