package service

import (
	"database/sql"
	"testing"

	_ "github.com/marcboeker/go-duckdb"
)

func setupTestService(t *testing.T) *Service {
	t.Helper()

	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	statements := []string{
		"INSTALL fts;",
		"LOAD fts;",
		`CREATE TABLE regions (
            id VARCHAR,
            subdistrict VARCHAR,
            district VARCHAR,
            city VARCHAR,
            province VARCHAR,
            postal_code VARCHAR,
            full_text VARCHAR,
            subdistrict_bps VARCHAR,
            subdistrict_bps_code VARCHAR,
            district_bps VARCHAR,
            district_bps_code VARCHAR,
            city_bps VARCHAR,
            city_bps_code VARCHAR,
            province_bps VARCHAR,
            province_bps_code VARCHAR,
            full_text_bps VARCHAR
        );`,
		`INSERT INTO regions VALUES (
            '3171010001001',
            'cempaka putih barat',
            'cempaka putih',
            'kota jakarta pusat',
            'dki jakarta',
            '10510',
            '10510 dki jakarta kota jakarta pusat cempaka putih cempaka putih barat',
            'cempaka putih barat',
            '3171010001001',
            'cempaka putih',
            '3171010001',
            'jakarta pusat',
            '3171',
            'dki jakarta',
            '31',
            'dki jakarta jakarta pusat cempaka putih cempaka putih barat'
        );`,
		`INSERT INTO regions VALUES (
            '3171020002002',
            'kelapa gading timur',
            'kelapa gading',
            'kota jakarta utara',
            'dki jakarta',
            '14240',
            '14240 dki jakarta kota jakarta utara kelapa gading kelapa gading timur',
            NULL,
            NULL,
            NULL,
            NULL,
            NULL,
            NULL,
            NULL,
            NULL,
            'dki jakarta jakarta utara kelapa gading kelapa gading timur'
        );`,
		`CREATE TABLE regions_bps AS
            SELECT id, full_text_bps
            FROM regions;`,
		"PRAGMA create_fts_index('regions', 'id', 'full_text', overwrite=1);",
		"PRAGMA create_fts_index('regions_bps', 'id', 'full_text_bps', overwrite=1);",
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("execute statement %q: %v", stmt, err)
		}
	}

	return New(db)
}

func setupLegacyService(t *testing.T) *Service {
	t.Helper()

	db, err := sql.Open("duckdb", ":memory:")
	if err != nil {
		t.Fatalf("open duckdb: %v", err)
	}

	t.Cleanup(func() {
		_ = db.Close()
	})

	statements := []string{
		"INSTALL fts;",
		"LOAD fts;",
		`CREATE TABLE regions (
            id VARCHAR,
            subdistrict VARCHAR,
            district VARCHAR,
            city VARCHAR,
            province VARCHAR,
            postal_code VARCHAR,
            full_text VARCHAR
        );`,
		`INSERT INTO regions VALUES (
            '3171010001001',
            'cempaka putih barat',
            'cempaka putih',
            'kota jakarta pusat',
            'dki jakarta',
            '10510',
            '10510 dki jakarta kota jakarta pusat cempaka putih cempaka putih barat'
        );`,
		"PRAGMA create_fts_index('regions', 'id', 'full_text', overwrite=1);",
	}

	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("execute statement %q: %v", stmt, err)
		}
	}

	return New(db)
}

func TestSearchIncludesBPSAndScores(t *testing.T) {
	svc := setupTestService(t)

	results, err := svc.Search(SearchQuery{
		Query: "cempaka",
		Options: SearchOptions{
			SearchBPS:     true,
			IncludeBPS:    true,
			IncludeScores: true,
			Limit:         1,
		},
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	region := results[0]
	if region.BPS == nil || region.BPS.City == nil || region.BPS.City.Code == "" {
		t.Fatalf("expected BPS city metadata to be present, got %#v", region.BPS)
	}
	if region.Scores == nil || region.Scores.FTS == nil {
		t.Fatalf("expected FTS score, got %#v", region.Scores)
	}
}

func TestSearchLimitApplied(t *testing.T) {
	svc := setupTestService(t)

	results, err := svc.Search(SearchQuery{
		Query: "jakarta",
		Options: SearchOptions{
			Limit: 1,
		},
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected limit to restrict results to 1, got %d", len(results))
	}
}

func TestSearchByCityScores(t *testing.T) {
	svc := setupTestService(t)

	results, err := svc.SearchByCity("jakarta pusat", SearchOptions{IncludeScores: true})
	if err != nil {
		t.Fatalf("SearchByCity failed: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for city search")
	}
	if results[0].Scores == nil || results[0].Scores.City == nil {
		t.Fatalf("expected city similarity score, got %#v", results[0].Scores)
	}
}

func TestSearchByPostalCodeIncludesBPS(t *testing.T) {
	svc := setupTestService(t)

	results, err := svc.SearchByPostalCode("10510", SearchOptions{IncludeBPS: true, Limit: 5})
	if err != nil {
		t.Fatalf("SearchByPostalCode failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected exactly one postal code match, got %d", len(results))
	}
	if results[0].BPS == nil || results[0].BPS.Subdistrict == nil {
		t.Fatalf("expected BPS subdistrict metadata, got %#v", results[0].BPS)
	}
}

func TestSearchRejectsNegativeLimit(t *testing.T) {
	svc := setupTestService(t)

	_, err := svc.Search(SearchQuery{
		Query:   "jakarta",
		Options: SearchOptions{Limit: -1},
	})
	if err == nil {
		t.Fatalf("expected error for negative limit")
	}
	if !IsError(err, ErrCodeInvalidInput) {
		t.Fatalf("expected invalid input error, got %v", err)
	}
}

func TestSearchRejectsIncludeBPSWhenSchemaMissing(t *testing.T) {
	svc := setupLegacyService(t)

	_, err := svc.Search(SearchQuery{
		Query: "jakarta",
		Options: SearchOptions{
			IncludeBPS: true,
		},
	})
	if err == nil {
		t.Fatalf("expected error when requesting BPS metadata without columns")
	}
	if !IsError(err, ErrCodeInvalidInput) {
		t.Fatalf("expected invalid input error, got %v", err)
	}

	results, err := svc.Search(SearchQuery{Query: "jakarta"})
	if err != nil {
		t.Fatalf("search failed without BPS columns: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results for legacy dataset")
	}
}
