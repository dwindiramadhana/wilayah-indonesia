// Package service provides business logic for the wilayah-indonesia API.
// It encapsulates the core functionality for searching Indonesian regions
// by various criteria such as name, postal code, etc.
package service

import (
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
)

// Region represents a region in Indonesia with all its administrative divisions.
type Region struct {
	ID          string `json:"id"`
	Subdistrict string `json:"subdistrict"`
	District    string `json:"district"`
	City        string `json:"city"`
	Province    string `json:"province"`
	PostalCode  string `json:"postal_code"`
	FullText    string `json:"full_text"`
}

// Service encapsulates the business logic for region searches.
type Service struct {
	db *sql.DB
}

// New creates a new Service instance with the provided database connection.
func New(db *sql.DB) *Service {
	return &Service{
		db: db,
	}
}

// SearchQuery represents the parameters for a search query.
type SearchQuery struct {
	Query       string
	Subdistrict string
	District    string
	City        string
	Province    string
}

// Search performs a general search across all regions based on the provided query.
func (s *Service) Search(searchQuery SearchQuery) ([]Region, error) {
	// Check if any search criteria are provided
	if searchQuery.Query == "" && searchQuery.Subdistrict == "" && searchQuery.District == "" && searchQuery.City == "" && searchQuery.Province == "" {
		return nil, NewError(ErrCodeInvalidInput, "at least one search parameter is required")
	}

	slog.Info("Processing search request", "query", searchQuery)

	// If only the general query is provided, use the existing FTS
	if searchQuery.Query != "" && searchQuery.Subdistrict == "" && searchQuery.District == "" && searchQuery.City == "" && searchQuery.Province == "" {
		slog.Info("Performing full-text search", "query", searchQuery.Query)
		sqlQuery := `
			SELECT id, subdistrict, district, city, province, postal_code, full_text, score
			FROM (
				SELECT *, fts_main_regions.match_bm25(id, ?) AS score
				FROM regions
			)
			WHERE score IS NOT NULL
			ORDER BY score DESC
			LIMIT 10;
		`
		rows, err := s.db.Query(sqlQuery, searchQuery.Query)
		if err != nil {
			slog.Error("Database query failed", "error", err, "query", searchQuery.Query)
			return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
		}
		defer rows.Close()

		results, err := s.scanRegions(rows)
		if err != nil {
			return nil, err
		}

		slog.Info("Search completed", "query", searchQuery.Query, "results", len(results))
		return results, nil
	}

	// Build a dynamic query with Jaro-Winkler similarity for specific fields
	var conditions []string
	var scoreComponents []string
	var args []interface{}

	if searchQuery.Subdistrict != "" {
		conditions = append(conditions, "jaro_winkler_similarity(subdistrict, ?) >= 0.8")
		scoreComponents = append(scoreComponents, "jaro_winkler_similarity(subdistrict, ?)")
		args = append(args, searchQuery.Subdistrict)
	}
	if searchQuery.District != "" {
		conditions = append(conditions, "jaro_winkler_similarity(district, ?) >= 0.8")
		scoreComponents = append(scoreComponents, "jaro_winkler_similarity(district, ?)")
		args = append(args, searchQuery.District)
	}
	if searchQuery.City != "" {
		conditions = append(conditions, "(jaro_winkler_similarity(city, 'Kota ' || ?) >= 0.8 OR jaro_winkler_similarity(city, 'Kabupaten ' || ?) >= 0.8)")
		scoreComponents = append(scoreComponents, "GREATEST(jaro_winkler_similarity(city, 'Kota ' || ?), jaro_winkler_similarity(city, 'Kabupaten ' || ?))")
		args = append(args, searchQuery.City, searchQuery.City)
	}
	if searchQuery.Province != "" {
		conditions = append(conditions, "jaro_winkler_similarity(province, ?) >= 0.8")
		scoreComponents = append(scoreComponents, "jaro_winkler_similarity(province, ?)")
		args = append(args, searchQuery.Province)
	}
	if searchQuery.Query != "" {
		conditions = append(conditions, "fts_main_regions.match_bm25(id, ?) IS NOT NULL")
		scoreComponents = append(scoreComponents, "fts_main_regions.match_bm25(id, ?)")
		args = append(args, searchQuery.Query)
	}

	// Prepare arguments for ORDER BY clause
	var orderByArgs []interface{}
	if searchQuery.Subdistrict != "" {
		orderByArgs = append(orderByArgs, searchQuery.Subdistrict)
	}
	if searchQuery.District != "" {
		orderByArgs = append(orderByArgs, searchQuery.District)
	}
	if searchQuery.City != "" {
		orderByArgs = append(orderByArgs, searchQuery.City, searchQuery.City)
	}
	if searchQuery.Province != "" {
		orderByArgs = append(orderByArgs, searchQuery.Province)
	}
	if searchQuery.Query != "" {
		orderByArgs = append(orderByArgs, searchQuery.Query)
	}

	finalArgs := append(args, orderByArgs...)

	// Construct the final query
	sqlQuery := fmt.Sprintf(`
		SELECT id, subdistrict, district, city, province, postal_code, full_text
		FROM regions
		WHERE %s
		ORDER BY (%s) DESC
		LIMIT 10;
	`, strings.Join(conditions, " AND "), strings.Join(scoreComponents, " + "))

	rows, err := s.db.Query(sqlQuery, finalArgs...)
	if err != nil {
		slog.Error("Database query failed", "error", err, "query", searchQuery)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
	}
	defer rows.Close()

	results, err := s.scanRegions(rows)
	if err != nil {
		return nil, err
	}

	slog.Info("Search completed", "query", searchQuery, "results", len(results))
	return results, nil
}

// SearchByDistrict searches for regions by district name, optionally narrowed by city and province.
func (s *Service) SearchByDistrict(district string, city string, province string) ([]Region, error) {
    if district == "" {
        return nil, NewError(ErrCodeInvalidInput, "query parameter is required")
    }

    slog.Info("Processing district search request", "district", district, "city", city, "province", province)

    // Build dynamic conditions and scoring based on provided filters
    var conditions []string
    var scoreComponents []string
    var args []interface{}
    var orderByArgs []interface{}

    // District is required
    conditions = append(conditions, "jaro_winkler_similarity(district, ?) >= 0.8")
    scoreComponents = append(scoreComponents, "jaro_winkler_similarity(district, ?)")
    args = append(args, district)
    orderByArgs = append(orderByArgs, district)

    // Optional city filter (handles both Kota and Kabupaten prefixes)
    if city != "" {
        conditions = append(conditions, "(jaro_winkler_similarity(city, 'Kota ' || ?) >= 0.8 OR jaro_winkler_similarity(city, 'Kabupaten ' || ?) >= 0.8)")
        scoreComponents = append(scoreComponents, "GREATEST(jaro_winkler_similarity(city, 'Kota ' || ?), jaro_winkler_similarity(city, 'Kabupaten ' || ?))")
        args = append(args, city, city)
        orderByArgs = append(orderByArgs, city, city)
    }

    // Optional province filter
    if province != "" {
        conditions = append(conditions, "jaro_winkler_similarity(province, ?) >= 0.8")
        scoreComponents = append(scoreComponents, "jaro_winkler_similarity(province, ?)")
        args = append(args, province)
        orderByArgs = append(orderByArgs, province)
    }

    finalArgs := append(args, orderByArgs...)

    sqlQuery := fmt.Sprintf(`
        SELECT id, subdistrict, district, city, province, postal_code, full_text
        FROM regions
        WHERE %s
        ORDER BY (%s) DESC
        LIMIT 10
    `, strings.Join(conditions, " AND "), strings.Join(scoreComponents, " + "))

    rows, err := s.db.Query(sqlQuery, finalArgs...)
    if err != nil {
        slog.Error("Database query failed", "error", err, "district", district, "city", city, "province", province)
        return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
    }
    defer rows.Close()

    // Iterate through the results
    results, err := s.scanRegions(rows)
    if err != nil {
        return nil, err
    }

    slog.Info("District search completed", "district", district, "city", city, "province", province, "results", len(results))
    return results, nil
}

// SearchBySubdistrict searches for regions by subdistrict name, optionally narrowed by district, city, and province.
func (s *Service) SearchBySubdistrict(subdistrict string, district string, city string, province string) ([]Region, error) {
    if subdistrict == "" {
        return nil, NewError(ErrCodeInvalidInput, "query parameter is required")
    }

    slog.Info("Processing subdistrict search request", "subdistrict", subdistrict, "district", district, "city", city, "province", province)

    var conditions []string
    var scoreComponents []string
    var args []interface{}
    var orderByArgs []interface{}

    // Required subdistrict condition
    conditions = append(conditions, "jaro_winkler_similarity(subdistrict, ?) >= 0.8")
    scoreComponents = append(scoreComponents, "jaro_winkler_similarity(subdistrict, ?)")
    args = append(args, subdistrict)
    orderByArgs = append(orderByArgs, subdistrict)

    // Optional district filter
    if district != "" {
        conditions = append(conditions, "jaro_winkler_similarity(district, ?) >= 0.8")
        scoreComponents = append(scoreComponents, "jaro_winkler_similarity(district, ?)")
        args = append(args, district)
        orderByArgs = append(orderByArgs, district)
    }

    // Optional city filter (supports Kota/Kabupaten)
    if city != "" {
        conditions = append(conditions, "(jaro_winkler_similarity(city, 'Kota ' || ?) >= 0.8 OR jaro_winkler_similarity(city, 'Kabupaten ' || ?) >= 0.8)")
        scoreComponents = append(scoreComponents, "GREATEST(jaro_winkler_similarity(city, 'Kota ' || ?), jaro_winkler_similarity(city, 'Kabupaten ' || ?))")
        args = append(args, city, city)
        orderByArgs = append(orderByArgs, city, city)
    }

    // Optional province filter
    if province != "" {
        conditions = append(conditions, "jaro_winkler_similarity(province, ?) >= 0.8")
        scoreComponents = append(scoreComponents, "jaro_winkler_similarity(province, ?)")
        args = append(args, province)
        orderByArgs = append(orderByArgs, province)
    }

    finalArgs := append(args, orderByArgs...)

    sqlQuery := fmt.Sprintf(`
        SELECT id, subdistrict, district, city, province, postal_code, full_text
        FROM regions
        WHERE %s
        ORDER BY (%s) DESC
        LIMIT 10
    `, strings.Join(conditions, " AND "), strings.Join(scoreComponents, " + "))

    rows, err := s.db.Query(sqlQuery, finalArgs...)
    if err != nil {
        slog.Error("Database query failed", "error", err, "subdistrict", subdistrict, "district", district, "city", city, "province", province)
        return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
    }
    defer rows.Close()

    // Iterate through the results
    results, err := s.scanRegions(rows)
    if err != nil {
        return nil, err
    }

    slog.Info("Subdistrict search completed", "subdistrict", subdistrict, "district", district, "city", city, "province", province, "results", len(results))
    return results, nil
}

// SearchByCity searches for regions by city name.
func (s *Service) SearchByCity(query string) ([]Region, error) {
	if query == "" {
		return nil, NewError(ErrCodeInvalidInput, "query parameter is required")
	}

	slog.Info("Processing city search request", "query", query)

	// Prepare and execute the SQL query
	sqlQuery := `
		SELECT id, subdistrict, district, city, province, postal_code, full_text
		FROM regions
		WHERE
		    jaro_winkler_similarity (city, 'Kota ' || ?) >= 0.8
			OR jaro_winkler_similarity (city, 'Kabupaten ' || ?) >= 0.8
		ORDER BY jaro_winkler_similarity (city, 'Kota ' || ?) DESC, jaro_winkler_similarity (city, 'Kabupaten ' || ?) DESC
		LIMIT 10
	`

	rows, err := s.db.Query(sqlQuery, query, query, query, query)
	if err != nil {
		slog.Error("Database query failed", "error", err, "query", query)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
	}
	defer rows.Close()

	// Iterate through the results
	results, err := s.scanRegions(rows)
	if err != nil {
		return nil, err
	}

	slog.Info("City search completed", "query", query, "results", len(results))
	return results, nil
}

// SearchByProvince searches for regions by province name.
func (s *Service) SearchByProvince(query string) ([]Region, error) {
	if query == "" {
		return nil, NewError(ErrCodeInvalidInput, "query parameter is required")
	}

	slog.Info("Processing province search request", "query", query)

	// Prepare and execute the SQL query
	sqlQuery := `
		SELECT id, subdistrict, district, city, province, postal_code, full_text
		FROM regions
		WHERE jaro_winkler_similarity (province, ?) >= 0.8
		ORDER BY jaro_winkler_similarity (province, ?) DESC
		LIMIT 10
	`

	rows, err := s.db.Query(sqlQuery, query, query)
	if err != nil {
		slog.Error("Database query failed", "error", err, "query", query)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
	}
	defer rows.Close()

	// Iterate through the results
	results, err := s.scanRegions(rows)
	if err != nil {
		return nil, err
	}

	slog.Info("Province search completed", "query", query, "results", len(results))
	return results, nil
}

// SearchByPostalCode searches for regions by postal code.
func (s *Service) SearchByPostalCode(postalCode string) ([]Region, error) {
	if postalCode == "" {
		return nil, NewError(ErrCodeInvalidInput, "postal code parameter is required")
	}

	

	slog.Info("Processing postal code search request", "postalCode", postalCode)

	// Prepare and execute the SQL query
	sqlQuery := `
		SELECT id, subdistrict, district, city, province, postal_code, full_text
		FROM regions
		WHERE postal_code = ?
		ORDER BY full_text
		LIMIT 10
	`

	rows, err := s.db.Query(sqlQuery, postalCode)
	if err != nil {
		slog.Error("Database query failed", "error", err, "postalCode", postalCode)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "database query failed: %v", err)
	}
	defer rows.Close()

	// Iterate through the results
	results, err := s.scanRegions(rows)
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
func (s *Service) scanRegions(rows *sql.Rows) ([]Region, error) {
	var results []Region
	for rows.Next() {
		var region Region
		var score sql.NullFloat64 // Use sql.NullFloat64 for the score

		// Check the column names to determine which columns to scan
		cols, err := rows.Columns()
		if err != nil {
			return nil, NewErrorf(ErrCodeDatabaseFailure, "failed to get columns: %v", err)
		}

		// Prepare the scan arguments based on the available columns
		scanArgs := []interface{}{
			&region.ID,
			&region.Subdistrict,
			&region.District,
			&region.City,
			&region.Province,
			&region.PostalCode,
			&region.FullText,
		}

		// If the score column is present, add it to the scan arguments
		if len(cols) > 7 {
			scanArgs = append(scanArgs, &score)
		}

		err = rows.Scan(scanArgs...)
		if err != nil {
			slog.Error("Failed to scan row", "error", err)
			return nil, NewErrorf(ErrCodeDatabaseFailure, "failed to scan row: %v", err)
		}
		results = append(results, region)
	}

	// Check for errors during iteration
	if err := rows.Err(); err != nil {
		slog.Error("Error iterating rows", "error", err)
		return nil, NewErrorf(ErrCodeDatabaseFailure, "error iterating rows: %v", err)
	}

	return results, nil
}
