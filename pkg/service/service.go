// Package service provides business logic for the wilayah-indonesia API.
// It encapsulates the core functionality for searching Indonesian regions
// by various criteria such as name, postal code, etc.
package service

import (
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Region represents a region in Indonesia with all its administrative divisions.
type Region struct {
	ID          string        `json:"id"`
	Subdistrict string        `json:"subdistrict"`
	District    string        `json:"district"`
	City        string        `json:"city"`
	Province    string        `json:"province"`
	PostalCode  string        `json:"postal_code"`
	FullText    string        `json:"full_text"`
	BPS         *RegionBPS    `json:"bps,omitempty"`
	Scores      *SearchScores `json:"scores,omitempty"`
}

// RegionBPS groups optional BPS metadata for each administrative level.
type RegionBPS struct {
	Subdistrict *BPSDetail `json:"subdistrict,omitempty"`
	District    *BPSDetail `json:"district,omitempty"`
	City        *BPSDetail `json:"city,omitempty"`
	Province    *BPSDetail `json:"province,omitempty"`
}

// BPSDetail carries the code and name emitted from the BPS dataset.
type BPSDetail struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// SearchScores exposes the overall FTS score and the per-field similarity scores.
type SearchScores struct {
	FTS         *float64 `json:"fts,omitempty"`
	Subdistrict *float64 `json:"subdistrict,omitempty"`
	District    *float64 `json:"district,omitempty"`
	City        *float64 `json:"city,omitempty"`
	Province    *float64 `json:"province,omitempty"`
}

// Service encapsulates the business logic for region searches.
type Service struct {
	db          *sql.DB
	columns     map[string]bool
	hasBPSIndex bool
}

// New creates a new Service instance with the provided database connection.
func New(db *sql.DB) *Service {
	svc := &Service{
		db:      db,
		columns: make(map[string]bool),
	}
	if _, err := db.Exec("SET scalar_subquery_error_on_multiple_rows=false"); err != nil {
		slog.Warn("Failed to relax scalar subquery constraint", "error", err)
	}
	svc.loadSchemaColumns()
	svc.detectFTSIndexes()
	return svc
}

func (s *Service) detectFTSIndexes() {
	if !s.hasColumn("full_text_bps") {
		return
	}
	rows, err := s.db.Query("SELECT fts_main_regions_bps.match_bm25(id, '') FROM regions LIMIT 1")
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Debug("BPS FTS index not detected", "error", err)
		}
		return
	}
	rows.Close()
	s.hasBPSIndex = true
}

func (s *Service) loadSchemaColumns() {
	rows, err := s.db.Query("PRAGMA table_info('regions')")
	if err != nil {
		slog.Warn("Failed to introspect regions schema", "error", err)
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
			slog.Warn("Failed to scan regions schema row", "error", err)
			return
		}
		s.columns[strings.ToLower(name)] = true
	}

	if err := rows.Err(); err != nil {
		slog.Warn("Error iterating regions schema", "error", err)
	}
}

func (s *Service) hasColumn(name string) bool {
	return s.columns[strings.ToLower(name)]
}

// SearchQuery represents the parameters for a search query.
type SearchQuery struct {
	Query       string
	Subdistrict string
	District    string
	City        string
	Province    string
	Options     SearchOptions
}

// SearchOptions configures response enrichment and scoring behaviour.
type SearchOptions struct {
	Limit         int
	SearchBPS     bool
	IncludeBPS    bool
	IncludeScores bool
}

// Search performs a general search across all regions based on the provided query.
const (
	defaultSearchLimit = 10
	maxSearchLimit     = 100
)

func normalizeOptions(opts *SearchOptions) error {
	if opts.Limit == 0 {
		opts.Limit = defaultSearchLimit
	}
	if opts.Limit < 0 {
		return NewError(ErrCodeInvalidInput, "limit must be a positive integer")
	}
	if opts.Limit > maxSearchLimit {
		opts.Limit = maxSearchLimit
	}
	return nil
}

func (s *Service) Search(searchQuery SearchQuery) ([]Region, error) {
	if searchQuery.Query == "" && searchQuery.Subdistrict == "" && searchQuery.District == "" && searchQuery.City == "" && searchQuery.Province == "" {
		return nil, NewError(ErrCodeInvalidInput, "at least one search parameter is required")
	}

	if err := normalizeOptions(&searchQuery.Options); err != nil {
		return nil, err
	}

	if searchQuery.Options.SearchBPS && !s.hasColumn("full_text_bps") {
		return nil, NewError(ErrCodeInvalidInput, "BPS search requested but dataset is missing BPS columns; run 'make prepare-db'")
	}
	if searchQuery.Options.IncludeBPS && !s.hasColumn("subdistrict_bps") {
		return nil, NewError(ErrCodeInvalidInput, "BPS metadata requested but dataset is missing BPS columns; run 'make prepare-db'")
	}
	if searchQuery.Options.SearchBPS && !s.hasBPSIndex {
		return nil, NewError(ErrCodeInvalidInput, "BPS search requested but dataset is missing BPS FTS index; run 'make prepare-db'")
	}

	args := make([]interface{}, 0, 32)

	ftsIndex := "fts_main_regions"
	if searchQuery.Options.SearchBPS {
		ftsIndex = "fts_main_regions_bps"
	}
	slog.Info("Processing search request", "query", searchQuery, "fts_index", ftsIndex)
	ftsScoreExpr := fmt.Sprintf("CASE WHEN ? <> '' THEN %s.match_bm25(r.id, ?) ELSE NULL END AS fts_score", ftsIndex)

	subdistrictExpr := "r.subdistrict"
	districtExpr := "r.district"
	cityScoreExpr := "CASE WHEN ? <> '' THEN GREATEST(jaro_winkler_similarity(r.city, 'Kota ' || ?), jaro_winkler_similarity(r.city, 'Kabupaten ' || ?)) ELSE NULL END AS city_score"
	provinceExpr := "r.province"
	cityArgsCount := 3

	if searchQuery.Options.SearchBPS {
		subdistrictExpr = "COALESCE(r.subdistrict_bps, r.subdistrict)"
		districtExpr = "COALESCE(r.district_bps, r.district)"
		provinceExpr = "COALESCE(r.province_bps, r.province)"
		cityExpr := "COALESCE(r.city_bps, r.city)"
		cityScoreExpr = fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS city_score", cityExpr)
		cityArgsCount = 2
	}

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
		if s.hasColumn(col) {
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

	computedColumns := []string{}

	computedColumns = append(computedColumns, ftsScoreExpr)
	args = append(args, searchQuery.Query, searchQuery.Query)

	computedColumns = append(computedColumns, fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS subdistrict_score", subdistrictExpr))
	args = append(args, searchQuery.Subdistrict, searchQuery.Subdistrict)

	computedColumns = append(computedColumns, fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS district_score", districtExpr))
	args = append(args, searchQuery.District, searchQuery.District)

	computedColumns = append(computedColumns, cityScoreExpr)
	for i := 0; i < cityArgsCount; i++ {
		args = append(args, searchQuery.City)
	}

	computedColumns = append(computedColumns, fmt.Sprintf("CASE WHEN ? <> '' THEN jaro_winkler_similarity(%s, ?) ELSE NULL END AS province_score", provinceExpr))
	args = append(args, searchQuery.Province, searchQuery.Province)

	innerSelect := fmt.Sprintf("%s,\n\t%s", strings.Join(baseColumns, ",\n\t"), strings.Join(computedColumns, ",\n\t"))

	var queryBuilder strings.Builder
	queryBuilder.WriteString("WITH scored AS (\n")
	queryBuilder.WriteString("SELECT\n\t")
	queryBuilder.WriteString(innerSelect)
	queryBuilder.WriteString("\nFROM regions AS r\n)\n")

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
		if s.hasColumn(name) {
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

	outerColumns = append(outerColumns,
		"fts_score",
		"subdistrict_score",
		"district_score",
		"city_score",
		"province_score",
	)

	queryBuilder.WriteString("SELECT\n\t")
	queryBuilder.WriteString(strings.Join(outerColumns, ",\n\t"))
	queryBuilder.WriteString("\nFROM scored\n")

	whereClauses := []string{
		"(? = '' OR fts_score IS NOT NULL)",
		"(? = '' OR subdistrict_score >= 0.8)",
		"(? = '' OR district_score >= 0.8)",
		"(? = '' OR city_score >= 0.8)",
		"(? = '' OR province_score >= 0.8)",
	}
	queryBuilder.WriteString("WHERE\n\t")
	queryBuilder.WriteString(strings.Join(whereClauses, "\n\tAND "))
	queryBuilder.WriteString("\n")
	args = append(args, searchQuery.Query, searchQuery.Subdistrict, searchQuery.District, searchQuery.City, searchQuery.Province)

	queryBuilder.WriteString("ORDER BY\n\t(COALESCE(fts_score, 0) + COALESCE(subdistrict_score, 0) + COALESCE(district_score, 0) + COALESCE(city_score, 0) + COALESCE(province_score, 0)) DESC\n")
	queryBuilder.WriteString("LIMIT ?")
	args = append(args, searchQuery.Options.Limit)

	rows, err := s.db.Query(queryBuilder.String(), args...)
	if err != nil {
		slog.Error("Database query failed", "error", err, "query", searchQuery)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
	}
	defer rows.Close()

	results, err := s.scanRegions(rows, searchQuery.Options)
	if err != nil {
		return nil, err
	}

	slog.Info("Search completed", "query", searchQuery, "results", len(results))
	return results, nil
}

// SearchByDistrict searches for regions by district name, optionally narrowed by city and province.
func (s *Service) SearchByDistrict(district string, city string, province string, opts SearchOptions) ([]Region, error) {
	if district == "" {
		return nil, NewError(ErrCodeInvalidInput, "query parameter is required")
	}

	return s.Search(SearchQuery{
		District: district,
		City:     city,
		Province: province,
		Options:  opts,
	})
}

// SearchBySubdistrict searches for regions by subdistrict name, optionally narrowed by district, city, and province.
func (s *Service) SearchBySubdistrict(subdistrict string, district string, city string, province string, opts SearchOptions) ([]Region, error) {
	if subdistrict == "" {
		return nil, NewError(ErrCodeInvalidInput, "query parameter is required")
	}

	return s.Search(SearchQuery{
		Subdistrict: subdistrict,
		District:    district,
		City:        city,
		Province:    province,
		Options:     opts,
	})
}

// SearchByCity searches for regions by city name.
func (s *Service) SearchByCity(query string, opts SearchOptions) ([]Region, error) {
	if query == "" {
		return nil, NewError(ErrCodeInvalidInput, "query parameter is required")
	}

	return s.Search(SearchQuery{
		City:    query,
		Options: opts,
	})
}

// SearchByProvince searches for regions by province name.
func (s *Service) SearchByProvince(query string, opts SearchOptions) ([]Region, error) {
	if query == "" {
		return nil, NewError(ErrCodeInvalidInput, "query parameter is required")
	}

	return s.Search(SearchQuery{
		Province: query,
		Options:  opts,
	})
}

// SearchByPostalCode searches for regions by postal code.
func (s *Service) SearchByPostalCode(postalCode string, opts SearchOptions) ([]Region, error) {
	if postalCode == "" {
		return nil, NewError(ErrCodeInvalidInput, "postal code parameter is required")
	}

	if err := normalizeOptions(&opts); err != nil {
		return nil, err
	}
	if opts.IncludeBPS && !s.hasColumn("subdistrict_bps") {
		return nil, NewError(ErrCodeInvalidInput, "BPS metadata requested but dataset is missing BPS columns; run 'make prepare-db'")
	}

	slog.Info("Processing postal code search request", "postalCode", postalCode)

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
		if s.hasColumn(col) {
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

	selectCols = append(selectCols,
		"NULL AS fts_score",
		"NULL AS subdistrict_score",
		"NULL AS district_score",
		"NULL AS city_score",
		"NULL AS province_score",
	)

	sqlQuery := fmt.Sprintf(`
		SELECT
			%s
		FROM regions
		WHERE postal_code = ?
		ORDER BY full_text
		LIMIT ?
	`, strings.Join(selectCols, ",\n\t\t"))

	rows, err := s.db.Query(sqlQuery, postalCode, opts.Limit)
	if err != nil {
		slog.Error("Database query failed", "error", err, "postalCode", postalCode)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
	}
	defer rows.Close()

	// Iterate through the results
	results, err := s.scanRegions(rows, opts)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		slog.Info("No results found for postal code", "postalCode", postalCode)
		return nil, NewError(ErrCodeNotFound, "no regions found for the provided postal code")
	}

	slog.Info("Postal code search completed", "postalCode", postalCode, "results", len(results))
	return results, nil
}

// scanRegions iterates through the SQL rows and converts them to Region structs.
func (s *Service) scanRegions(rows *sql.Rows, opts SearchOptions) ([]Region, error) {
	columns, err := rows.Columns()
	if err != nil {
		slog.Error("Failed to get columns", "error", err)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "failed to get columns: %v", err)
	}

	var results []Region
	for rows.Next() {
		var (
			id                 sql.NullString
			subdistrict        sql.NullString
			district           sql.NullString
			city               sql.NullString
			province           sql.NullString
			postalCode         sql.NullString
			fullText           sql.NullString
			subdistrictBPS     sql.NullString
			subdistrictBPSCode sql.NullString
			districtBPS        sql.NullString
			districtBPSCode    sql.NullString
			cityBPS            sql.NullString
			cityBPSCode        sql.NullString
			provinceBPS        sql.NullString
			provinceBPSCode    sql.NullString
			ftsScore           sql.NullFloat64
			subdistrictScore   sql.NullFloat64
			districtScore      sql.NullFloat64
			cityScore          sql.NullFloat64
			provinceScore      sql.NullFloat64
		)

		dest := make([]interface{}, len(columns))
		for i, col := range columns {
			switch col {
			case "id":
				dest[i] = &id
			case "subdistrict":
				dest[i] = &subdistrict
			case "district":
				dest[i] = &district
			case "city":
				dest[i] = &city
			case "province":
				dest[i] = &province
			case "postal_code":
				dest[i] = &postalCode
			case "full_text":
				dest[i] = &fullText
			case "subdistrict_bps":
				dest[i] = &subdistrictBPS
			case "subdistrict_bps_code":
				dest[i] = &subdistrictBPSCode
			case "district_bps":
				dest[i] = &districtBPS
			case "district_bps_code":
				dest[i] = &districtBPSCode
			case "city_bps":
				dest[i] = &cityBPS
			case "city_bps_code":
				dest[i] = &cityBPSCode
			case "province_bps":
				dest[i] = &provinceBPS
			case "province_bps_code":
				dest[i] = &provinceBPSCode
			case "fts_score":
				dest[i] = &ftsScore
			case "subdistrict_score":
				dest[i] = &subdistrictScore
			case "district_score":
				dest[i] = &districtScore
			case "city_score":
				dest[i] = &cityScore
			case "province_score":
				dest[i] = &provinceScore
			default:
				var discard interface{}
				dest[i] = &discard
			}
		}

		if err := rows.Scan(dest...); err != nil {
			slog.Error("Failed to scan row", "error", err)
			return nil, NewErrorf(ErrCodeDatabaseFailure, "failed to scan row: %v", err)
		}

		region := Region{
			ID:          nullString(id),
			Subdistrict: nullString(subdistrict),
			District:    nullString(district),
			City:        nullString(city),
			Province:    nullString(province),
			PostalCode:  nullString(postalCode),
			FullText:    nullString(fullText),
		}

		if opts.IncludeBPS {
			bps := &RegionBPS{
				Subdistrict: buildBPSDetail(subdistrictBPS, subdistrictBPSCode),
				District:    buildBPSDetail(districtBPS, districtBPSCode),
				City:        buildBPSDetail(cityBPS, cityBPSCode),
				Province:    buildBPSDetail(provinceBPS, provinceBPSCode),
			}
			if bps.Subdistrict != nil || bps.District != nil || bps.City != nil || bps.Province != nil {
				region.BPS = bps
			}
		}

		if opts.IncludeScores {
			scores := &SearchScores{
				FTS:         nullFloatPtr(ftsScore),
				Subdistrict: nullFloatPtr(subdistrictScore),
				District:    nullFloatPtr(districtScore),
				City:        nullFloatPtr(cityScore),
				Province:    nullFloatPtr(provinceScore),
			}
			if scores.FTS != nil || scores.Subdistrict != nil || scores.District != nil || scores.City != nil || scores.Province != nil {
				region.Scores = scores
			}
		}

		results = append(results, region)
	}

	if err := rows.Err(); err != nil {
		slog.Error("Error iterating rows", "error", err)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "error iterating rows: %v", err)
	}

	return results, nil
}

func nullString(ns sql.NullString) string {
	if ns.Valid {
		return ns.String
	}
	return ""
}

func nullFloatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	val := n.Float64
	return &val
}

func buildBPSDetail(name sql.NullString, code sql.NullString) *BPSDetail {
	if !name.Valid && !code.Valid {
		return nil
	}
	detail := &BPSDetail{}
	if name.Valid {
		detail.Name = name.String
	}
	if code.Valid {
		detail.Code = code.String
	}
	if detail.Name == "" && detail.Code == "" {
		return nil
	}
	return detail
}
