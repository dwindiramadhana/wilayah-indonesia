package ingestion

import (
	"context"
	"fmt"
	"log/slog"

	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
)

// SQLLoader loads SQL scripts from a source (e.g. filesystem).
type SQLLoader interface {
	Load(path string) (string, error)
}

// SQLNormalizer adjusts SQL scripts to match DuckDB syntax expectations.
type SQLNormalizer interface {
	Normalize(sql string) string
}

// AdminRepository executes administrative SQL statements.
type AdminRepository interface {
	Exec(ctx context.Context, sql string) error
}

// RefreshOptions enumerates SQL artifacts required for a full refresh.
type RefreshOptions struct {
	WilayahSQLPath    string
	PostalSQLPath     string
	BPSMappingSQLPath string
}

// UseCase coordinates dataset refresh operations.
type UseCase interface {
	Refresh(ctx context.Context, opts RefreshOptions) error
}

// Options configures the ingestion use case.
type Options struct {
	Logger *slog.Logger
}

type useCase struct {
	loader     SQLLoader
	normalizer SQLNormalizer
	adminRepo  AdminRepository
	logger     *slog.Logger
}

// New constructs a UseCase instance.
func New(loader SQLLoader, normalizer SQLNormalizer, adminRepo AdminRepository, opts Options) UseCase {
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &useCase{
		loader:     loader,
		normalizer: normalizer,
		adminRepo:  adminRepo,
		logger:     logger,
	}
}

func (uc *useCase) Refresh(ctx context.Context, opts RefreshOptions) error {
	uc.logger.Info("Starting DuckDB refresh", "wilayah_sql", opts.WilayahSQLPath)

	if err := uc.executeSQLFile(ctx, opts.WilayahSQLPath); err != nil {
		return err
	}
	if err := uc.executeSQLFile(ctx, opts.PostalSQLPath); err != nil {
		return err
	}
	if err := uc.executeSQLFile(ctx, opts.BPSMappingSQLPath); err != nil {
		return err
	}

	for _, stmt := range []struct {
		description string
		query       string
	}{
		{"denormalize wilayah dataset", transformationQuery},
		{"create BPS mapping", mappingQuery},
		{"create BPS corpus table", regionsBPSTableQuery},
		{"drop raw wilayah table", "DROP TABLE IF EXISTS wilayah;"},
		{"drop postal table", "DROP TABLE IF EXISTS wilayah_kodepos;"},
		{"install fts extension", "INSTALL fts;"},
		{"load fts extension", "LOAD fts;"},
		{"create regions FTS index", "PRAGMA create_fts_index('regions', 'id', 'full_text', overwrite=1);"},
		{"create BPS FTS index", "PRAGMA create_fts_index('regions_bps', 'id', 'full_text_bps', overwrite=1);"},
	} {
		if err := uc.exec(ctx, stmt.description, stmt.query); err != nil {
			return err
		}
	}

	uc.logger.Info("DuckDB refresh completed successfully")
	return nil
}

func (uc *useCase) executeSQLFile(ctx context.Context, path string) error {
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

func (uc *useCase) exec(ctx context.Context, description, query string) error {
	if err := uc.adminRepo.Exec(ctx, query); err != nil {
		uc.logger.Error("SQL execution failed", "step", description, "error", err)
		return err
	}
	uc.logger.Info("Step completed", "step", description)
	return nil
}

const transformationQuery = `
CREATE OR REPLACE TABLE regions AS
SELECT
	sub.kode AS id,
	sub.nama AS subdistrict,
	dist.nama AS district,
	city.nama AS city,
	prov.nama AS province,
	bps_sub.nama_bps AS subdistrict_bps,
	bps_sub.kode_bps AS subdistrict_bps_code,
	bps_dist.nama_bps AS district_bps,
	bps_dist.kode_bps AS district_bps_code,
	bps_city.nama_bps AS city_bps,
	bps_city.kode_bps AS city_bps_code,
	bps_prov.nama_bps AS province_bps,
	bps_prov.kode_bps AS province_bps_code,
	kodepos.kodepos AS postal_code,
	LOWER(TRIM(CONCAT(
		COALESCE(CAST(kodepos.kodepos AS VARCHAR), ''), ' ',
		prov.nama, ' ',
		city.nama, ' ',
		dist.nama, ' ',
		sub.nama
	))) AS full_text,
	LOWER(TRIM(CONCAT(
		COALESCE(bps_prov.nama_bps, prov.nama, ''), ' ',
		COALESCE(bps_city.nama_bps, city.nama, ''), ' ',
		COALESCE(bps_dist.nama_bps, dist.nama, ''), ' ',
		COALESCE(bps_sub.nama_bps, sub.nama, '')
	))) AS full_text_bps
FROM
	wilayah AS sub
JOIN wilayah AS dist ON dist.kode = SUBSTRING(sub.kode FROM 1 FOR 8)
JOIN wilayah AS city ON city.kode = SUBSTRING(sub.kode FROM 1 FOR 5)
JOIN wilayah AS prov ON prov.kode = SUBSTRING(sub.kode FROM 1 FOR 2)
LEFT JOIN wilayah_kodepos AS kodepos ON kodepos.kode = sub.kode
LEFT JOIN bps_wilayah AS bps_sub ON bps_sub.kode_dagri = sub.kode
LEFT JOIN bps_wilayah AS bps_dist ON bps_dist.kode_dagri = dist.kode
LEFT JOIN bps_wilayah AS bps_city ON bps_city.kode_dagri = city.kode
LEFT JOIN bps_wilayah AS bps_prov ON bps_prov.kode_dagri = prov.kode
WHERE
	LENGTH(sub.kode) = 13;
`

const mappingQuery = `
CREATE OR REPLACE TABLE bps_region_mapping AS
SELECT
	bw.kode_bps,
	bw.nama_bps,
	bw.kode_dagri,
	bw.nama_dagri,
	bw.level,
	bw.parent_kode_bps,
	bw.periode_merge,
	bw.fetched_at,
	r.id AS region_id,
	r.subdistrict,
	r.subdistrict_bps,
	r.district,
	r.district_bps,
	r.city,
	r.city_bps,
	r.province,
	r.province_bps,
	r.postal_code,
	r.full_text,
	r.full_text_bps
FROM
	bps_wilayah AS bw
LEFT JOIN regions AS r ON r.id = bw.kode_dagri;
`

const regionsBPSTableQuery = `
CREATE OR REPLACE TABLE regions_bps AS
SELECT id, full_text_bps
FROM regions;
`
