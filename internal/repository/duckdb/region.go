package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/ilmimris/wilayah-indonesia/internal/entity"
	repository "github.com/ilmimris/wilayah-indonesia/internal/repository"
	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
)

// RegionRepository implements repository.RegionRepository for DuckDB backends.
type RegionRepository struct {
	db          *sql.DB
	columns     map[string]bool
	hasBPSIndex bool
}

// NewRegionRepository constructs a DuckDB-backed RegionRepository.
func NewRegionRepository(db *sql.DB) *RegionRepository {
	if _, err := db.Exec("SET scalar_subquery_error_on_multiple_rows=false"); err != nil {
		slog.Warn("duckdb scalar subquery compatibility flag failed", "error", err)
	}
	repo := &RegionRepository{db: db, columns: make(map[string]bool)}
	repo.loadSchemaColumns()
	repo.detectFTSIndexes()
	return repo
}

var _ repository.RegionRepository = (*RegionRepository)(nil)

// Search executes a fuzzy lookup across the regions table applying optional BPS logic.
func (r *RegionRepository) Search(ctx context.Context, params repository.RegionSearchParams) ([]entity.RegionWithScore, error) {
	query, args := r.buildSearchQuery(params)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "database query failed", err)
	}
	defer rows.Close()

	return r.scanRegions(rows, params.Options)
}

// SearchByDistrict delegates to the general search flow with district filters applied.
func (r *RegionRepository) SearchByDistrict(ctx context.Context, params repository.RegionSearchParams) ([]entity.RegionWithScore, error) {
	params.Subdistrict = ""
	return r.Search(ctx, params)
}

// SearchBySubdistrict delegates to the general search flow with subdistrict filters applied.
func (r *RegionRepository) SearchBySubdistrict(ctx context.Context, params repository.RegionSearchParams) ([]entity.RegionWithScore, error) {
	return r.Search(ctx, params)
}

// SearchByCity delegates to the general search flow with city filters applied.
func (r *RegionRepository) SearchByCity(ctx context.Context, params repository.RegionSearchParams) ([]entity.RegionWithScore, error) {
	params.Subdistrict = ""
	params.District = ""
	params.Province = ""
	return r.Search(ctx, params)
}

// SearchByProvince delegates to the general search flow with province filters applied.
func (r *RegionRepository) SearchByProvince(ctx context.Context, params repository.RegionSearchParams) ([]entity.RegionWithScore, error) {
	params.Subdistrict = ""
	params.District = ""
	params.City = ""
	return r.Search(ctx, params)
}

// SearchByPostalCode performs an exact postal-code lookup with optional enrichment.
func (r *RegionRepository) SearchByPostalCode(ctx context.Context, postalCode string, opts repository.RegionSearchOptions) ([]entity.RegionWithScore, error) {
	query, args := r.buildPostalCodeQuery(postalCode, opts)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "postal code query failed", err)
	}
	defer rows.Close()

	return r.scanRegions(rows, opts)
}

// Capabilities reports optional dataset features available to callers.
func (r *RegionRepository) Capabilities(ctx context.Context) (repository.RegionRepositoryCapabilities, error) {
	return repository.RegionRepositoryCapabilities{
		HasBPSColumns: r.hasColumn("subdistrict_bps") && r.hasColumn("province_bps"),
		HasBPSIndex:   r.hasBPSIndex,
	}, nil
}

func (r *RegionRepository) hasColumn(name string) bool {
	return r.columns[strings.ToLower(name)]
}

func (r *RegionRepository) loadSchemaColumns() {
	rows, err := r.db.Query("PRAGMA table_info('regions')")
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid        int
			name       string
			colType    string
			notNull    bool
			defaultVal sql.NullString
			pk         bool
		)
		if err := rows.Scan(&cid, &name, &colType, &notNull, &defaultVal, &pk); err != nil {
			return
		}
		r.columns[strings.ToLower(name)] = true
	}
}

func (r *RegionRepository) detectFTSIndexes() {
	if !r.hasColumn("full_text_bps") {
		return
	}
	rows, err := r.db.Query("SELECT fts_main_regions_bps.match_bm25(id, '') FROM regions LIMIT 1")
	if err != nil {
		return
	}
	rows.Close()
	r.hasBPSIndex = true
}

func (r *RegionRepository) buildSearchQuery(params repository.RegionSearchParams) (string, []interface{}) {
	ftsIndex := "fts_main_regions"
	if params.Options.SearchBPS {
		ftsIndex = "fts_main_regions_bps"
	}

	args := make([]interface{}, 0, 32)
	queryTrimmed := strings.TrimSpace(params.Query)

	ftsScoreExpr := fmt.Sprintf("CASE WHEN ? <> '' THEN %s.match_bm25(r.id, ?) ELSE NULL END AS fts_score", ftsIndex)
	args = append(args, queryTrimmed, queryTrimmed)

	subdistrictExpr := "r.subdistrict"
	districtExpr := "r.district"
	provinceExpr := "r.province"
	cityScoreExpr := "CASE WHEN ? <> '' THEN GREATEST(jaro_winkler_similarity(r.city, 'Kota ' || ?), jaro_winkler_similarity(r.city, 'Kabupaten ' || ?)) ELSE NULL END AS city_score"
	cityArgsCount := 3

	if params.Options.SearchBPS {
		subdistrictExpr = "COALESCE(r.subdistrict_bps, r.subdistrict)"
		districtExpr = "COALESCE(r.district_bps, r.district)"
		provinceExpr = "COALESCE(r.province_bps, r.province)"
		cityExpr := "COALESCE(r.city_bps, r.city)"
		cityScoreExpr = fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS city_score", cityExpr)
		cityArgsCount = 2
	}

	computedColumns := []string{ftsScoreExpr,
		fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS subdistrict_score", subdistrictExpr),
		fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS district_score", districtExpr),
		cityScoreExpr,
		fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS province_score", provinceExpr),
	}

	args = append(args, params.Subdistrict, params.Subdistrict)
	args = append(args, params.District, params.District)
	for i := 0; i < cityArgsCount; i++ {
		args = append(args, params.City)
	}
	args = append(args, params.Province, params.Province)

	baseColumns := []string{
		"r.id",
		"r.subdistrict",
		"r.district",
		"r.city",
		"r.province",
		"r.postal_code",
		"r.full_text",
	}
	appendColumn := func(col string) {
		if r.hasColumn(col) {
			baseColumns = append(baseColumns, fmt.Sprintf("r.%s", col))
		}
	}
	appendColumn("subdistrict_bps")
	appendColumn("subdistrict_bps_code")
	appendColumn("district_bps")
	appendColumn("district_bps_code")
	appendColumn("city_bps")
	appendColumn("city_bps_code")
	appendColumn("province_bps")
	appendColumn("province_bps_code")

	innerSelect := fmt.Sprintf("%s,\n\t%s", strings.Join(baseColumns, ",\n\t"), strings.Join(computedColumns, ",\n\t"))

	var builder strings.Builder
	builder.WriteString("WITH scored AS (\nSELECT\n\t")
	builder.WriteString(innerSelect)
	builder.WriteString("\nFROM regions AS r\n)\n")
	builder.WriteString("SELECT\n\t")

	outerColumns := []string{
		"id",
		"subdistrict",
		"district",
		"city",
		"province",
		"postal_code",
		"full_text",
	}
	addOuter := func(name string) {
		if r.hasColumn(name) {
			outerColumns = append(outerColumns, name)
		}
	}
	addOuter("subdistrict_bps")
	addOuter("subdistrict_bps_code")
	addOuter("district_bps")
	addOuter("district_bps_code")
	addOuter("city_bps")
	addOuter("city_bps_code")
	addOuter("province_bps")
	addOuter("province_bps_code")
	outerColumns = append(outerColumns, "fts_score", "subdistrict_score", "district_score", "city_score", "province_score")

	builder.WriteString(strings.Join(outerColumns, ",\n\t"))
	builder.WriteString("\nFROM scored\nWHERE\n\t")
	whereClauses := []string{
		"(? = '' OR fts_score IS NOT NULL)",
		"(? = '' OR subdistrict_score >= 0.8)",
		"(? = '' OR district_score >= 0.8)",
		"(? = '' OR city_score >= 0.8)",
		"(? = '' OR province_score >= 0.8)",
	}
	builder.WriteString(strings.Join(whereClauses, "\n\tAND "))
	builder.WriteString("\nORDER BY\n\t(COALESCE(fts_score, 0) + COALESCE(subdistrict_score, 0) + COALESCE(district_score, 0) + COALESCE(city_score, 0) + COALESCE(province_score, 0)) DESC\n")
	builder.WriteString("LIMIT ?")

	args = append(args, queryTrimmed, params.Subdistrict, params.District, params.City, params.Province, params.Options.Limit)

	return builder.String(), args
}

func (r *RegionRepository) buildPostalCodeQuery(postalCode string, opts repository.RegionSearchOptions) (string, []interface{}) {
	selectCols := []string{
		"id",
		"subdistrict",
		"district",
		"city",
		"province",
		"postal_code",
		"full_text",
	}
	appendIfPresent := func(col string) {
		if r.hasColumn(col) {
			selectCols = append(selectCols, col)
		}
	}
	appendIfPresent("subdistrict_bps")
	appendIfPresent("subdistrict_bps_code")
	appendIfPresent("district_bps")
	appendIfPresent("district_bps_code")
	appendIfPresent("city_bps")
	appendIfPresent("city_bps_code")
	appendIfPresent("province_bps")
	appendIfPresent("province_bps_code")

	query := fmt.Sprintf("SELECT %s FROM regions WHERE postal_code = ? LIMIT ?", strings.Join(selectCols, ", "))
	return query, []interface{}{postalCode, opts.Limit}
}

func (r *RegionRepository) scanRegions(rows *sql.Rows, opts repository.RegionSearchOptions) ([]entity.RegionWithScore, error) {
	columns, err := rows.Columns()
	if err != nil {
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "failed to get columns", err)
	}

	var results []entity.RegionWithScore
	for rows.Next() {
		rowValues := make([]interface{}, len(columns))
		dests := make([]interface{}, len(columns))
		for i := range columns {
			dests[i] = &rowValues[i]
		}
		if err := rows.Scan(dests...); err != nil {
			return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "failed to scan row", err)
		}
		mapped := mapRow(columns, rowValues, opts)
		results = append(results, mapped)
	}

	if err := rows.Err(); err != nil {
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "error iterating rows", err)
	}

	return results, nil
}

func mapRow(columns []string, values []interface{}, opts repository.RegionSearchOptions) entity.RegionWithScore {
	var (
		region entity.Region
	)

	var (
		subdistrictBPS     string
		subdistrictBPSCode string
		districtBPS        string
		districtBPSCode    string
		cityBPS            string
		cityBPSCode        string
		provinceBPS        string
		provinceBPSCode    string
		ftsScore           *float64
		subdistrictScore   *float64
		districtScore      *float64
		cityScore          *float64
		provinceScore      *float64
	)

	for i, col := range columns {
		val := values[i]
		switch col {
		case "id":
			region.ID = asString(val)
		case "subdistrict":
			region.Subdistrict = asString(val)
		case "district":
			region.District = asString(val)
		case "city":
			region.City = asString(val)
		case "province":
			region.Province = asString(val)
		case "postal_code":
			region.PostalCode = asString(val)
		case "full_text":
			region.FullText = asString(val)
		case "subdistrict_bps":
			subdistrictBPS = asString(val)
		case "subdistrict_bps_code":
			subdistrictBPSCode = asString(val)
		case "district_bps":
			districtBPS = asString(val)
		case "district_bps_code":
			districtBPSCode = asString(val)
		case "city_bps":
			cityBPS = asString(val)
		case "city_bps_code":
			cityBPSCode = asString(val)
		case "province_bps":
			provinceBPS = asString(val)
		case "province_bps_code":
			provinceBPSCode = asString(val)
		case "fts_score":
			ftsScore = asFloatPointer(val)
		case "subdistrict_score":
			subdistrictScore = asFloatPointer(val)
		case "district_score":
			districtScore = asFloatPointer(val)
		case "city_score":
			cityScore = asFloatPointer(val)
		case "province_score":
			provinceScore = asFloatPointer(val)
		}
	}

	result := entity.RegionWithScore{Region: region}

	if opts.IncludeBPS {
		bps := entity.RegionBPS{
			Subdistrict: buildBPSDetail(subdistrictBPS, subdistrictBPSCode),
			District:    buildBPSDetail(districtBPS, districtBPSCode),
			City:        buildBPSDetail(cityBPS, cityBPSCode),
			Province:    buildBPSDetail(provinceBPS, provinceBPSCode),
		}
		if bps.Subdistrict != nil || bps.District != nil || bps.City != nil || bps.Province != nil {
			result.Region.BPS = &bps
		}
	}

	if opts.IncludeScores {
		scores := entity.RegionScore{
			FTS:         ftsScore,
			Subdistrict: subdistrictScore,
			District:    districtScore,
			City:        cityScore,
			Province:    provinceScore,
		}
		if scores.FTS != nil || scores.Subdistrict != nil || scores.District != nil || scores.City != nil || scores.Province != nil {
			result.Score = &scores
		}
	}

	return result
}

func asString(val interface{}) string {
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	case nil:
		return ""
	case fmt.Stringer:
		return v.String()
	case time.Time:
		return v.Format(time.RFC3339)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func asFloatPointer(val interface{}) *float64 {
	switch v := val.(type) {
	case float64:
		return &v
	case nil:
		return nil
	case []byte:
		if len(v) == 0 {
			return nil
		}
		f, err := strconv.ParseFloat(string(v), 64)
		if err != nil {
			return nil
		}
		return &f
	default:
		return nil
	}
}

func buildBPSDetail(name, code string) *entity.BPSDetail {
	if name == "" && code == "" {
		return nil
	}
	detail := &entity.BPSDetail{}
	if name != "" {
		detail.Name = name
	}
	if code != "" {
		detail.Code = code
	}
	return detail
}
