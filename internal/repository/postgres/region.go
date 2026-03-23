package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/ilmimris/wilayah-indonesia/internal/entity"
	repository "github.com/ilmimris/wilayah-indonesia/internal/repository"
	sharederrors "github.com/ilmimris/wilayah-indonesia/internal/shared/errors"
	"github.com/jackc/pgx/v5/pgconn"
)

// RegionRepository implements repository.RegionRepository for PostgreSQL backends.
type RegionRepository struct {
	pool        *Pool
	columns     map[string]bool
	hasFTSIndex bool
	hasBPSIndex bool
}

// NewRegionRepository constructs a PostgreSQL-backed RegionRepository.
func NewRegionRepository(pool *Pool) *RegionRepository {
	repo := &RegionRepository{
		pool:    pool,
		columns: make(map[string]bool),
	}
	repo.loadSchemaColumns()
	repo.detectFTSIndexes()
	return repo
}

var _ repository.RegionRepository = (*RegionRepository)(nil)

// Search executes a fuzzy lookup across the regions table applying optional BPS logic.
func (r *RegionRepository) Search(ctx context.Context, params repository.RegionSearchParams) ([]entity.RegionWithScore, error) {
	query, args := r.buildSearchQuery(params)
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		slog.Error("search query failed", "query", query, "args", fmt.Sprintf("%v", args), "error", err)
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, fmt.Sprintf("search query failed: query=%q args=%v error=%v", query, args, err), err)
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
	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "postal code query failed", err)
	}
	defer rows.Close()

	return r.scanRegions(rows, opts)
}

// Capabilities reports optional dataset features available to callers.
func (r *RegionRepository) Capabilities(ctx context.Context) (repository.RegionRepositoryCapabilities, error) {
	return repository.RegionRepositoryCapabilities{
		HasFTSIndex:   r.hasFTSIndex,
		HasBPSColumns: r.hasColumn("subdistrict_bps") && r.hasColumn("province_bps"),
		HasBPSIndex:   r.hasBPSIndex,
	}, nil
}

func (r *RegionRepository) hasColumn(name string) bool {
	return r.columns[strings.ToLower(name)]
}

func (r *RegionRepository) loadSchemaColumns() {
	ctx := context.Background()
	query := `
		SELECT column_name
		FROM information_schema.columns
		WHERE table_name = 'regions'`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		slog.Warn("failed to load schema columns", "error", err)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var columnName string
		if err := rows.Scan(&columnName); err != nil {
			return
		}
		r.columns[strings.ToLower(columnName)] = true
	}
}

func (r *RegionRepository) detectFTSIndexes() {
	ctx := context.Background()

	// Check if GIN index exists on full_text_vector
	query := `
		SELECT COUNT(*)
		FROM pg_indexes
		WHERE tablename = 'regions'
		  AND indexname LIKE '%fts%'
		  AND indexdef LIKE '%gin%'`

	row := r.pool.QueryRow(ctx, query)
	var count int
	if err := row.Scan(&count); err != nil {
		slog.Warn("failed to detect FTS index", "error", err)
		return
	}

	if count > 0 {
		r.hasFTSIndex = true
		slog.Info("PostgreSQL FTS index detected")
	} else {
		slog.Debug("PostgreSQL FTS index missing")
	}

	// Check if BPS columns exist and have indexes
	if r.hasColumn("full_text_bps") {
		query = `
			SELECT COUNT(*)
			FROM pg_indexes
			WHERE tablename = 'regions_bps'
			  AND indexname LIKE '%fts%'`
		row = r.pool.QueryRow(ctx, query)
		if err := row.Scan(&count); err == nil && count > 0 {
			r.hasBPSIndex = true
		}
	}
}

func (r *RegionRepository) buildSearchQuery(params repository.RegionSearchParams) (string, []interface{}) {
	queryTrimmed := strings.TrimSpace(params.Query)
	queryEmpty := queryTrimmed == ""

	useBPS := params.Options.SearchBPS
	var useFTS bool
	if queryEmpty {
		useFTS = false
	} else {
		useFTS = r.hasFTSIndex
	}

	args := make([]interface{}, 0, 32)
	whereClauses := make([]string, 0, 5)
	whereArgs := make([]interface{}, 0, 5)

	computedColumns := make([]string, 0, 5)
	if useFTS {
		// Use native PostgreSQL FTS with ts_rank
		computedColumns = append(computedColumns, `
			CASE WHEN ? <> '' THEN ts_rank(full_text_vector, plainto_tsquery('simple', ?)) ELSE NULL::double precision END AS fts_score`)
		args = append(args, queryTrimmed, queryTrimmed)
		whereClauses = append(whereClauses, "(? = '' OR full_text_vector @@ plainto_tsquery('simple', ?))")
		whereArgs = append(whereArgs, queryTrimmed, queryTrimmed)
	} else {
		computedColumns = append(computedColumns, "NULL::double precision AS fts_score")
	}

	// Add similarity matching for main query parameter (fuzzy search)
	if !queryEmpty {
		computedColumns = append(computedColumns, `
			CASE WHEN ? <> '' THEN similarity(full_text, ?)::double precision ELSE NULL::double precision END AS query_similarity_score`)
		args = append(args, queryTrimmed, queryTrimmed)
		whereClauses = append(whereClauses, "(? = '' OR query_similarity_score >= 0.1)")
		whereArgs = append(whereArgs, queryTrimmed)
	} else {
		computedColumns = append(computedColumns, "NULL::double precision AS query_similarity_score")
	}

	subdistrictExpr := "subdistrict"
	districtExpr := "district"
	provinceExpr := "province"
	cityExpr := "city"
	cityScoreExpr := fmt.Sprintf("CASE WHEN ? <> '' THEN similarity(%s, ?)::double precision ELSE NULL::double precision END AS city_score", cityExpr)
	cityArgsCount := 2

	if useBPS {
		subdistrictExpr = "COALESCE(subdistrict_bps, subdistrict)"
		districtExpr = "COALESCE(district_bps, district)"
		provinceExpr = "COALESCE(province_bps, province)"
		cityExpr = "COALESCE(city_bps, city)"
		cityScoreExpr = fmt.Sprintf("CASE WHEN $1 <> '' THEN similarity(%s, $1)::double precision ELSE NULL::double precision END AS city_score", cityExpr)
		cityArgsCount = 2
	}

	computedColumns = append(computedColumns,
		fmt.Sprintf("CASE WHEN ? <> '' THEN similarity(%s, ?)::double precision ELSE NULL::double precision END AS subdistrict_score", subdistrictExpr),
		fmt.Sprintf("CASE WHEN ? <> '' THEN similarity(%s, ?)::double precision ELSE NULL::double precision END AS district_score", districtExpr),
		cityScoreExpr,
		fmt.Sprintf("CASE WHEN ? <> '' THEN similarity(%s, ?)::double precision ELSE NULL::double precision END AS province_score", provinceExpr),
	)

	args = append(args, params.Subdistrict, params.Subdistrict)
	whereClauses = append(whereClauses, "(? = '' OR subdistrict_score >= 0.2)")
	whereArgs = append(whereArgs, params.Subdistrict)

	args = append(args, params.District, params.District)
	whereClauses = append(whereClauses, "(? = '' OR district_score >= 0.2)")
	whereArgs = append(whereArgs, params.District)

	for i := 0; i < cityArgsCount; i++ {
		args = append(args, params.City)
	}
	whereClauses = append(whereClauses, "(? = '' OR city_score >= 0.2)")
	whereArgs = append(whereArgs, params.City)

	args = append(args, params.Province, params.Province)
	whereClauses = append(whereClauses, "(? = '' OR province_score >= 0.2)")
	whereArgs = append(whereArgs, params.Province)

	baseColumns := []string{
		"id",
		"subdistrict",
		"district",
		"city",
		"province",
		"postal_code",
		"full_text",
	}
	// Include full_text_vector column when FTS is enabled (needed for ts_rank and @@ operator)
	if useFTS && r.hasColumn("full_text_vector") {
		baseColumns = append(baseColumns, "full_text_vector")
	}
	appendColumn := func(col string) {
		if r.hasColumn(col) {
			baseColumns = append(baseColumns, col)
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
	outerColumns = append(outerColumns, "fts_score", "query_similarity_score", "subdistrict_score", "district_score", "city_score", "province_score")

	builder.WriteString(strings.Join(outerColumns, ",\n\t"))
	builder.WriteString("\nFROM scored")

	// Combine all args: first the SELECT clause args, then WHERE clause args
	allArgs := make([]interface{}, 0, len(args)+len(whereArgs)+1)
	allArgs = append(allArgs, args...)
	allArgs = append(allArgs, whereArgs...)

	if len(whereClauses) > 0 {
		builder.WriteString("\nWHERE\n\t")
		// Join WHERE clauses - all ? placeholders will be converted below
		builder.WriteString(strings.Join(whereClauses, "\n\tOR "))
	}
	builder.WriteString("\nORDER BY\n\t(COALESCE(fts_score, 0) + COALESCE(query_similarity_score, 0) + COALESCE(subdistrict_score, 0) + COALESCE(district_score, 0) + COALESCE(city_score, 0) + COALESCE(province_score, 0)) DESC\n")

	// Add limit argument and convert all ? placeholders
	allArgs = append(allArgs, params.Options.Limit)
	query := r.convertPlaceholders(builder.String(), 0, len(allArgs))
	// Append LIMIT clause with bigint cast
	query = fmt.Sprintf("%s\nLIMIT $%d::bigint", query, len(allArgs))

	return query, allArgs
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

	query := fmt.Sprintf("SELECT %s FROM regions WHERE postal_code = $1 LIMIT $2", strings.Join(selectCols, ", "))
	return query, []interface{}{postalCode, opts.Limit}
}

func (r *RegionRepository) scanRegions(rows *Rows, opts repository.RegionSearchOptions) ([]entity.RegionWithScore, error) {
	columns := rows.FieldDescriptions()

	var results []entity.RegionWithScore
	for rows.Next() {
		values := make([]interface{}, len(columns))
		valuePtrs := make([]interface{}, len(columns))
		for i := range values {
			valuePtrs[i] = &values[i]
		}

		if err := rows.Scan(valuePtrs...); err != nil {
			return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "failed to scan row", err)
		}

		mapped := mapRow(columns, values, opts)
		results = append(results, mapped)
	}

	if err := rows.Err(); err != nil {
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, "error iterating rows", err)
	}

	return results, nil
}

func mapRow(columns []pgconn.FieldDescription, values []interface{}, opts repository.RegionSearchOptions) entity.RegionWithScore {
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
		switch col.Name {
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
	if val == nil {
		return ""
	}
	switch v := val.(type) {
	case string:
		return v
	case []byte:
		return string(v)
	default:
		return fmt.Sprintf("%v", val)
	}
}

func asFloatPointer(val interface{}) *float64 {
	if val == nil {
		return nil
	}
	switch v := val.(type) {
	case float64:
		return &v
	case float32:
		f := float64(v)
		return &f
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

// convertPlaceholders converts ? placeholders to PostgreSQL $N placeholders
func (r *RegionRepository) convertPlaceholders(query string, offset, count int) string {
	result := query
	for i := 0; i < count; i++ {
		placeholder := fmt.Sprintf("$%d", offset+i+1)
		result = strings.Replace(result, "?", placeholder, 1)
	}
	return result
}
