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
	hasFTSIndex bool
	hasBPSIndex bool
	catalog     string
	ftsSchemas  []string
}

// NewRegionRepository constructs a DuckDB-backed RegionRepository.
func NewRegionRepository(db *sql.DB) *RegionRepository {
	if _, err := db.Exec("SET scalar_subquery_error_on_multiple_rows=false"); err != nil {
		slog.Warn("duckdb scalar subquery compatibility flag failed", "error", err)
	}
	repo := &RegionRepository{db: db, columns: make(map[string]bool)}
	repo.detectCatalog()
	if repo.catalog != "" {
		slog.Info("duckdb catalog detected", "catalog", repo.catalog)
	}
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
		return nil, sharederrors.Wrap(sharederrors.CodeDatabaseFailure, err.Error(), err)
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
		HasFTSIndex:   r.hasFTSIndex,
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
	if _, err := r.db.Exec("INSTALL fts;"); err != nil {
		slog.Warn("duckdb fts extension install failed", "error", err)
		return
	}

	if _, err := r.db.Exec("LOAD fts;"); err != nil {
		slog.Warn("duckdb fts extension unavailable", "error", err)
		return
	}

	if r.ensureFTSIndex("regions", "id", "full_text") {
		r.hasFTSIndex = true
	} else {
		slog.Debug("duckdb regions FTS index missing")
	}

	if !r.hasColumn("full_text_bps") {
		return
	}

	if r.ensureFTSIndex("regions_bps", "id", "full_text_bps") {
		r.hasBPSIndex = true
	} else {
		slog.Debug("duckdb BPS FTS index missing")
	}
}

func (r *RegionRepository) ensureFTSIndex(table, idColumn, textColumn string) bool {
	schema := r.ftsSchema(table)
	if r.ftsSchemaExists(schema) {
		r.addFTSSchema(schema)
		return true
	}

	createStmt := fmt.Sprintf("PRAGMA create_fts_index('%s', '%s', '%s', overwrite=1);", table, idColumn, textColumn)
	if _, createErr := r.db.Exec(createStmt); createErr != nil {
		slog.Warn("duckdb FTS index rebuild failed", "table", table, "error", createErr)
		return false
	}

	if r.ftsSchemaExists(schema) {
		r.addFTSSchema(schema)
		slog.Info("duckdb FTS index ready", "table", table)
		return true
	}

	slog.Warn("duckdb FTS index unavailable after rebuild", "table", table)
	return false
}

func (r *RegionRepository) detectCatalog() {
	row := r.db.QueryRow("SELECT current_database()")
	var name string
	if err := row.Scan(&name); err != nil {
		return
	}
	if name != "" && name != "main" && name != "memory" {
		r.catalog = name
	}
}

func (r *RegionRepository) catalogPrefix() string {
	if r.catalog == "" {
		return ""
	}
	return r.catalog + "."
}

func (r *RegionRepository) ftsSchema(table string) string {
	return fmt.Sprintf("fts_main_%s", table)
}

func (r *RegionRepository) ftsSchemaExists(schema string) bool {
	var count int
	query := `
SELECT COUNT(*)
FROM information_schema.tables
WHERE table_schema = ?`
	var err error
	if r.catalog != "" {
		query += " AND table_catalog = ?"
		err = r.db.QueryRow(query, schema, r.catalog).Scan(&count)
	} else {
		err = r.db.QueryRow(query, schema).Scan(&count)
	}
	if err != nil {
		slog.Debug("duckdb FTS schema lookup failed", "schema", schema, "error", err)
		return false
	}
	return count > 0
}

func sqlQuoteLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func (r *RegionRepository) addFTSSchema(schema string) {
	for _, existing := range r.ftsSchemas {
		if existing == schema {
			return
		}
	}
	r.ftsSchemas = append(r.ftsSchemas, schema)
	r.configureSearchPath()
}

func (r *RegionRepository) configureSearchPath() {
	baseSchemas := make([]string, 0, len(r.ftsSchemas)+2)
	if r.catalog != "" {
		baseSchemas = append(baseSchemas, fmt.Sprintf("%s.main", r.catalog))
	} else {
		baseSchemas = append(baseSchemas, "main")
	}
	for _, schema := range r.ftsSchemas {
		if r.catalog != "" {
			baseSchemas = append(baseSchemas, fmt.Sprintf("%s.%s", r.catalog, schema))
		} else {
			baseSchemas = append(baseSchemas, schema)
		}
	}
	items := make([]string, 0, len(baseSchemas))
	seen := make(map[string]struct{}, len(baseSchemas))
	for _, schema := range baseSchemas {
		if schema == "" {
			continue
		}
		if _, ok := seen[schema]; ok {
			continue
		}
		seen[schema] = struct{}{}
		items = append(items, schema)
	}
	if len(items) == 0 {
		return
	}
	stmt := fmt.Sprintf("SET search_path=%s;", sqlQuoteLiteral(strings.Join(items, ",")))
	if _, err := r.db.Exec(stmt); err != nil {
		slog.Debug("duckdb search_path update failed", "statement", stmt, "error", err)
	} else {
		slog.Info("duckdb search_path configured", "value", strings.Join(items, ","))
	}
}

func (r *RegionRepository) ftsFunction(table string) string {
	return fmt.Sprintf("%s.match_bm25", r.ftsSchema(table))
}

func (r *RegionRepository) buildSearchQuery(params repository.RegionSearchParams) (string, []interface{}) {
	queryTrimmed := strings.TrimSpace(params.Query)
	queryEmpty := queryTrimmed == ""
	ftsLiteral := sqlQuoteLiteral(queryTrimmed)

	useBPS := params.Options.SearchBPS
	var useFTS bool
	ftsFunc := r.ftsFunction("regions")
	if useBPS {
		ftsFunc = r.ftsFunction("regions_bps")
		useFTS = r.hasBPSIndex
	} else {
		useFTS = r.hasFTSIndex
	}
	if queryEmpty {
		useFTS = false
	}

	args := make([]interface{}, 0, 32)
	whereClauses := make([]string, 0, 5)
	whereArgs := make([]interface{}, 0, 5)

	computedColumns := make([]string, 0, 5)
	if useFTS {
		computedColumns = append(computedColumns, fmt.Sprintf("CASE WHEN ? <> '' THEN %s(r.id, %s) ELSE NULL END AS fts_score", ftsFunc, ftsLiteral))
		args = append(args, queryTrimmed)
		whereClauses = append(whereClauses, "(? = '' OR fts_score IS NOT NULL)")
		whereArgs = append(whereArgs, queryTrimmed)
	} else {
		computedColumns = append(computedColumns, "NULL AS fts_score")
	}

	subdistrictExpr := "r.subdistrict"
	districtExpr := "r.district"
	provinceExpr := "r.province"
	cityScoreExpr := "CASE WHEN ? <> '' THEN GREATEST(jaro_winkler_similarity(r.city, 'Kota ' || ?), jaro_winkler_similarity(r.city, 'Kabupaten ' || ?)) ELSE NULL END AS city_score"
	cityArgsCount := 3

	if useBPS {
		subdistrictExpr = "COALESCE(r.subdistrict_bps, r.subdistrict)"
		districtExpr = "COALESCE(r.district_bps, r.district)"
		provinceExpr = "COALESCE(r.province_bps, r.province)"
		cityExpr := "COALESCE(r.city_bps, r.city)"
		cityScoreExpr = fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS city_score", cityExpr)
		cityArgsCount = 2
	}

	computedColumns = append(computedColumns,
		fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS subdistrict_score", subdistrictExpr),
		fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS district_score", districtExpr),
		cityScoreExpr,
		fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS province_score", provinceExpr),
	)

	args = append(args, params.Subdistrict, params.Subdistrict)
	whereClauses = append(whereClauses, "(? = '' OR subdistrict_score >= 0.8)")
	whereArgs = append(whereArgs, params.Subdistrict)

	args = append(args, params.District, params.District)
	whereClauses = append(whereClauses, "(? = '' OR district_score >= 0.8)")
	whereArgs = append(whereArgs, params.District)

	for i := 0; i < cityArgsCount; i++ {
		args = append(args, params.City)
	}
	whereClauses = append(whereClauses, "(? = '' OR city_score >= 0.8)")
	whereArgs = append(whereArgs, params.City)

	args = append(args, params.Province, params.Province)
	whereClauses = append(whereClauses, "(? = '' OR province_score >= 0.8)")
	whereArgs = append(whereArgs, params.Province)

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
	builder.WriteString("\nFROM scored")
	if len(whereClauses) > 0 {
		builder.WriteString("\nWHERE\n\t")
		builder.WriteString(strings.Join(whereClauses, "\n\tAND "))
	}
	builder.WriteString("\nORDER BY\n\t(COALESCE(fts_score, 0) + COALESCE(subdistrict_score, 0) + COALESCE(district_score, 0) + COALESCE(city_score, 0) + COALESCE(province_score, 0)) DESC\n")
	builder.WriteString("LIMIT ?")

	args = append(args, whereArgs...)
	args = append(args, params.Options.Limit)

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
