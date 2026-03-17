//go:build integration

package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/ilmimris/wilayah-indonesia/internal/entity"
	repository "github.com/ilmimris/wilayah-indonesia/internal/repository"
)

// TestMain sets up the test database and runs all integration tests.
// Requires a running PostgreSQL instance with migrations applied.
// Set POSTGRES_TEST_URL environment variable or uses default test connection.
func TestMain(m *testing.M) {
	// Run tests
	code := m.Run()
	os.Exit(code)
}

func getTestPool(t *testing.T) *Pool {
	t.Helper()

	dsn := os.Getenv("POSTGRES_TEST_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/wilayah_indonesia?sslmode=disable"
	}

	pool, err := NewPool(context.Background(), dsn)
	if err != nil {
		t.Fatalf("failed to create test pool: %v", err)
	}

	// Verify connection
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatalf("failed to ping database: %v", err)
	}

	return pool
}

func TestRegionRepository_Search_FullText(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	tests := []struct {
		name          string
		query         string
		expectedCount int
		expectedCity  string
	}{
		{
			name:          "search bandung returns West Java results",
			query:         "bandung",
			expectedCount: 1,
			expectedCity:  "Kota Bandung",
		},
		{
			name:          "search jakarta returns DKI Jakarta results",
			query:         "jakarta",
			expectedCount: 1,
			expectedCity:  "DKI Jakarta",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			params := repository.RegionSearchParams{
				Query: tt.query,
				Options: repository.RegionSearchOptions{
					Limit: 10,
				},
			}

			results, err := repo.Search(ctx, params)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}

			if len(results) == 0 {
				t.Fatalf("expected results for query '%s', got 0", tt.query)
			}

			// Verify FTS score is populated when FTS index exists
			caps, err := repo.Capabilities(ctx)
			if err != nil {
				t.Fatalf("Capabilities failed: %v", err)
			}

			if caps.HasFTSIndex {
				for i, r := range results {
					if r.Score == nil || r.Score.FTS == nil {
						t.Errorf("result %d: expected FTS score to be populated, got nil", i)
					}
				}
			}
		})
	}
}

func TestRegionRepository_Search_ByField(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	ctx := context.Background()

	t.Run("search by province", func(t *testing.T) {
		params := repository.RegionSearchParams{
			Province: "Jawa Barat",
			Options: repository.RegionSearchOptions{
				Limit: 5,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("expected results for province 'Jawa Barat', got 0")
		}

		// Verify all results match the province
		for i, r := range results {
			if r.Region.Province != "Jawa Barat" {
				t.Errorf("result %d: expected province 'Jawa Barat', got '%s'", i, r.Region.Province)
			}
		}
	})

	t.Run("search by city", func(t *testing.T) {
		params := repository.RegionSearchParams{
			City: "Bandung",
			Options: repository.RegionSearchOptions{
				Limit: 5,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("expected results for city 'Bandung', got 0")
		}

		// Verify city scores are populated
		for i, r := range results {
			if r.Score == nil || r.Score.City == nil {
				t.Errorf("result %d: expected city score, got nil", i)
			}
		}
	})

	t.Run("search by district", func(t *testing.T) {
		params := repository.RegionSearchParams{
			District: "Sukasari",
			Options: repository.RegionSearchOptions{
				Limit: 5,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("expected results for district 'Sukasari', got 0")
		}
	})

	t.Run("search by subdistrict", func(t *testing.T) {
		params := repository.RegionSearchParams{
			Subdistrict: "Sukasari",
			Options: repository.RegionSearchOptions{
				Limit: 5,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("expected results for subdistrict 'Sukasari', got 0")
		}
	})
}

func TestRegionRepository_Search_CombinedFilters(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	ctx := context.Background()

	t.Run("combined full-text and province filter", func(t *testing.T) {
		params := repository.RegionSearchParams{
			Query:    "bandung",
			Province: "Jawa Barat",
			Options: repository.RegionSearchOptions{
				Limit: 10,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("expected results for combined query, got 0")
		}

		// All results should match both filters
		for i, r := range results {
			if r.Region.Province != "Jawa Barat" {
				t.Errorf("result %d: expected province 'Jawa Barat', got '%s'", i, r.Region.Province)
			}
		}
	})
}

func TestRegionRepository_SearchByPostalCode(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	ctx := context.Background()

	t.Run("search by valid postal code", func(t *testing.T) {
		postalCode := "40151"

		results, err := repo.SearchByPostalCode(ctx, postalCode, repository.RegionSearchOptions{
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("SearchByPostalCode failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatalf("expected results for postal code '%s', got 0", postalCode)
		}

		// Verify all results have the correct postal code
		for i, r := range results {
			if r.Region.PostalCode != postalCode {
				t.Errorf("result %d: expected postal code '%s', got '%s'", i, postalCode, r.Region.PostalCode)
			}
		}
	})

	t.Run("search by non-existent postal code", func(t *testing.T) {
		postalCode := "00000"

		results, err := repo.SearchByPostalCode(ctx, postalCode, repository.RegionSearchOptions{
			Limit: 10,
		})
		if err != nil {
			t.Fatalf("SearchByPostalCode failed: %v", err)
		}

		// Should return empty results, not error
		if len(results) != 0 {
			t.Errorf("expected no results for non-existent postal code, got %d", len(results))
		}
	})
}

func TestRegionRepository_Search_WithBPS(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	ctx := context.Background()

	// Check if BPS columns exist
	caps, err := repo.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities failed: %v", err)
	}

	if !caps.HasBPSColumns {
		t.Skip("BPS columns not available, skipping BPS tests")
	}

	t.Run("search with BPS data included", func(t *testing.T) {
		params := repository.RegionSearchParams{
			Query: "bandung",
			Options: repository.RegionSearchOptions{
				Limit:      5,
				IncludeBPS: true,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("expected results, got 0")
		}

		// Verify BPS data is populated
		hasBPS := false
		for _, r := range results {
			if r.Region.BPS != nil {
				hasBPS = true
				break
			}
		}

		if !hasBPS {
			t.Error("expected at least one result with BPS data")
		}
	})

	t.Run("search with BPS names for fuzzy matching", func(t *testing.T) {
		params := repository.RegionSearchParams{
			Query: "bandung",
			Options: repository.RegionSearchOptions{
				Limit:     5,
				SearchBPS: true,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("expected results, got 0")
		}
	})
}

func TestRegionRepository_Search_WithScores(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	ctx := context.Background()

	t.Run("search with scores included", func(t *testing.T) {
		params := repository.RegionSearchParams{
			Query: "bandung",
			Options: repository.RegionSearchOptions{
				Limit:         5,
				IncludeScores: true,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		if len(results) == 0 {
			t.Fatal("expected results, got 0")
		}

		// Verify scores are populated
		for i, r := range results {
			if r.Score == nil {
				t.Errorf("result %d: expected scores to be populated, got nil", i)
				continue
			}

			// At least one score should be non-nil
			hasScore := r.Score.FTS != nil ||
				r.Score.Subdistrict != nil ||
				r.Score.District != nil ||
				r.Score.City != nil ||
				r.Score.Province != nil

			if !hasScore {
				t.Errorf("result %d: expected at least one score to be populated", i)
			}
		}
	})
}

func TestRegionRepository_Capabilities(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	ctx := context.Background()

	caps, err := repo.Capabilities(ctx)
	if err != nil {
		t.Fatalf("Capabilities failed: %v", err)
	}

	t.Logf("Repository capabilities: HasFTS=%v, HasBPS=%v, HasBPSIndex=%v",
		caps.HasFTSIndex, caps.HasBPSColumns, caps.HasBPSIndex)

	// FTS index should be present after migrations
	if !caps.HasFTSIndex {
		t.Error("expected FTS index to be present after migrations")
	}
}

func TestRegionRepository_Search_Limit(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	ctx := context.Background()

	tests := []struct {
		name      string
		limit     int
		maxResults int
	}{
		{"limit 1", 1, 1},
		{"limit 3", 3, 3},
		{"limit 10", 10, 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := repository.RegionSearchParams{
				Query: "jakarta",
				Options: repository.RegionSearchOptions{
					Limit: tt.limit,
				},
			}

			results, err := repo.Search(ctx, params)
			if err != nil {
				t.Fatalf("Search failed: %v", err)
			}

			if len(results) > tt.maxResults {
				t.Errorf("expected at most %d results, got %d", tt.maxResults, len(results))
			}
		})
	}
}

func TestRegionRepository_Search_JaroWinkler(t *testing.T) {
	pool := getTestPool(t)
	repo := NewRegionRepository(pool)

	ctx := context.Background()

	t.Run("fuzzy match with typo", func(t *testing.T) {
		// Test Jaro-Winkler fuzzy matching with intentional typo
		params := repository.RegionSearchParams{
			Province: "Jawa Brat", // Typo: should be "Barat"
			Options: repository.RegionSearchOptions{
				Limit: 5,
			},
		}

		results, err := repo.Search(ctx, params)
		if err != nil {
			t.Fatalf("Search failed: %v", err)
		}

		// Should still find results due to fuzzy matching
		if len(results) == 0 {
			t.Error("expected fuzzy matching to find results for 'Jawa Brat'")
		}

		// Verify results are actually Jawa Barat
		for i, r := range results {
			if r.Region.Province != "Jawa Barat" {
				t.Errorf("result %d: expected 'Jawa Barat', got '%s'", i, r.Region.Province)
			}
		}
	})
}

func BenchmarkRegionRepository_Search(b *testing.B) {
	// Skip in short mode
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	pool := getTestPool(b)
	repo := NewRegionRepository(pool)

	ctx := context.Background()
	params := repository.RegionSearchParams{
		Query: "bandung",
		Options: repository.RegionSearchOptions{
			Limit: 10,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := repo.Search(ctx, params)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
	}
}

func BenchmarkRegionRepository_Search_Fuzzy(b *testing.B) {
	if testing.Short() {
		b.Skip("skipping benchmark in short mode")
	}

	pool := getTestPool(b)
	repo := NewRegionRepository(pool)

	ctx := context.Background()
	params := repository.RegionSearchParams{
		Subdistrict: "Sukasari",
		District:    "Sukasari",
		City:        "Bandung",
		Province:    "Jawa Barat",
		Options: repository.RegionSearchOptions{
			Limit: 10,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := repo.Search(ctx, params)
		if err != nil {
			b.Fatalf("Search failed: %v", err)
		}
	}
}
