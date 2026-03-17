// compare-backends - Compare search results between DuckDB and PostgreSQL backends
// Usage: go run scripts/compare-backends/main.go [query]
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// RegionResponse represents a single region result
type RegionResponse struct {
	ID         string      `json:"id"`
	Subdistrict string     `json:"subdistrict"`
	District   string      `json:"district"`
	City       string      `json:"city"`
	Province   string      `json:"province"`
	PostalCode string      `json:"postal_code"`
	FullText   string      `json:"full_text"`
	BPS        *RegionBPS  `json:"bps,omitempty"`
	Score      *RegionScore `json:"scores,omitempty"`
}

type RegionBPS struct {
	Subdistrict *BPSDetail `json:"subdistrict,omitempty"`
	District    *BPSDetail `json:"district,omitempty"`
	City        *BPSDetail `json:"city,omitempty"`
	Province    *BPSDetail `json:"province,omitempty"`
}

type BPSDetail struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

type RegionScore struct {
	FTS         *float64 `json:"fts,omitempty"`
	Subdistrict *float64 `json:"subdistrict,omitempty"`
	District    *float64 `json:"district,omitempty"`
	City        *float64 `json:"city,omitempty"`
	Province    *float64 `json:"province,omitempty"`
}

// Backend represents an API endpoint
type Backend struct {
	Name string
	URL  string
}

func main() {
	// Parse command-line flags
	query := flag.String("q", "bandung", "Search query")
	limit := flag.Int("limit", 5, "Number of results")
	includeBPS := flag.Bool("include_bps", false, "Include BPS data")
	includeScores := flag.Bool("include_scores", false, "Include scores")
	flag.Parse()

	// Override query if positional argument provided
	if flag.NArg() > 0 {
		*query = flag.Arg(0)
	}

	ctx := context.Background()

	// Define backends
	backends := []Backend{
		{Name: "DuckDB", URL: "http://localhost:8081"},
		{Name: "PostgreSQL", URL: "http://localhost:8001"},
	}

	fmt.Println("=== Indonesian Regions API - Backend Comparison ===")
	fmt.Printf("Query: %q\n", *query)
	fmt.Printf("Limit: %d\n", *limit)
	fmt.Printf("Include BPS: %v\n", *includeBPS)
	fmt.Printf("Include Scores: %v\n", *includeScores)
	fmt.Println()

	// Fetch results from each backend
	results := make(map[string][]RegionResponse)
	errors := make(map[string]error)
	timings := make(map[string]time.Duration)

	for _, backend := range backends {
		fmt.Printf("Fetching from %s...\n", backend.Name)
		start := time.Now()
		resp, err := search(ctx, backend.URL, *query, *limit, *includeBPS, *includeScores)
		elapsed := time.Since(start)
		timings[backend.Name] = elapsed

		if err != nil {
			fmt.Printf("  ERROR: %v\n", err)
			errors[backend.Name] = err
		} else {
			fmt.Printf("  Got %d results in %v\n", len(resp), elapsed.Round(time.Millisecond))
			results[backend.Name] = resp
		}
	}

	fmt.Println()

	// Compare results
	if len(errors) > 0 {
		fmt.Println("=== ERRORS ===")
		for name, err := range errors {
			fmt.Printf("%s: %v\n", name, err)
		}
		fmt.Println()
	}

	if len(results) < 2 {
		fmt.Println("Cannot compare: need both backends to return successfully")
		os.Exit(1)
	}

	duckdb := results["DuckDB"]
	postgres := results["PostgreSQL"]

	// Compare result counts
	fmt.Println("=== RESULT COUNT ===")
	fmt.Printf("DuckDB:     %d results\n", len(duckdb))
	fmt.Printf("PostgreSQL: %d results\n", len(postgres))
	fmt.Printf("Difference: %d\n", len(duckdb)-len(postgres))
	fmt.Println()

	// Compare by ID
	duckdbIDs := make(map[string]int)
	postgresIDs := make(map[string]int)

	for i, r := range duckdb {
		duckdbIDs[r.ID] = i
	}
	for i, r := range postgres {
		postgresIDs[r.ID] = i
	}

	// Find matches and differences
	var matches, onlyDuckDB, onlyPostgres []string

	for id := range duckdbIDs {
		if _, ok := postgresIDs[id]; ok {
			matches = append(matches, id)
		} else {
			onlyDuckDB = append(onlyDuckDB, id)
		}
	}
	for id := range postgresIDs {
		if _, ok := duckdbIDs[id]; !ok {
			onlyPostgres = append(onlyPostgres, id)
		}
	}

	fmt.Println("=== ID COMPARISON ===")
	fmt.Printf("Matching IDs:        %d\n", len(matches))
	fmt.Printf("Only in DuckDB:      %d\n", len(onlyDuckDB))
	fmt.Printf("Only in PostgreSQL:  %d\n", len(onlyPostgres))
	fmt.Println()

	if len(onlyDuckDB) > 0 {
		fmt.Println("IDs only in DuckDB:")
		for _, id := range onlyDuckDB {
			idx := duckdbIDs[id]
			fmt.Printf("  %s - %s, %s, %s\n", id, duckdb[idx].Subdistrict, duckdb[idx].District, duckdb[idx].City)
		}
		fmt.Println()
	}

	if len(onlyPostgres) > 0 {
		fmt.Println("IDs only in PostgreSQL:")
		for _, id := range onlyPostgres {
			idx := postgresIDs[id]
			fmt.Printf("  %s - %s, %s, %s\n", id, postgres[idx].Subdistrict, postgres[idx].District, postgres[idx].City)
		}
		fmt.Println()
	}

	// Compare scores for matching IDs
	if *includeScores && len(matches) > 0 {
		fmt.Println("=== SCORE COMPARISON (Matching IDs) ===")
		fmt.Printf("%-15s | %-10s | %-10s | %-10s | %-10s | %-10s\n", "ID", "FTS (DD)", "FTS (PG)", "City (DD)", "City (PG)", "Diff")
		fmt.Println(strings.Repeat("-", 80))

		for _, id := range matches[:min(5, len(matches))] {
			ddIdx := duckdbIDs[id]
			pgIdx := postgresIDs[id]

			ddFts := scoreStr(duckdb[ddIdx].Score.FTS)
			pgFts := scoreStr(postgres[pgIdx].Score.FTS)
			ddCity := scoreStr(duckdb[ddIdx].Score.City)
			pgCity := scoreStr(postgres[pgIdx].Score.City)

			diff := "-"
			if duckdb[ddIdx].Score.FTS != nil && postgres[pgIdx].Score.FTS != nil {
				diff = fmt.Sprintf("%.4f", abs(*duckdb[ddIdx].Score.FTS-*postgres[pgIdx].Score.FTS))
			}

			fmt.Printf("%-15s | %-10s | %-10s | %-10s | %-10s | %-10s\n",
				truncate(id, 15), ddFts, pgFts, ddCity, pgCity, diff)
		}
		fmt.Println()
	}

	// Performance comparison
	fmt.Println("=== PERFORMANCE ===")
	for _, backend := range backends {
		if _, ok := errors[backend.Name]; !ok {
			fmt.Printf("%s: %v\n", backend.Name, timings[backend.Name].Round(time.Millisecond))
		}
	}
	fmt.Println()

	// Sample results
	fmt.Println("=== SAMPLE RESULTS (Top 3) ===")
	fmt.Println()

	for _, backend := range backends {
		if _, ok := errors[backend.Name]; ok {
			continue
		}

		fmt.Printf("--- %s ---\n", backend.Name)
		resps := results[backend.Name]
		for i := 0; i < min(3, len(resps)); i++ {
			r := resps[i]
			fmt.Printf("%d. [%s] %s, %s, %s, %s\n",
				i+1, r.ID, r.Subdistrict, r.District, r.City, r.Province)
			if r.Score != nil && r.Score.FTS != nil {
				fmt.Printf("   FTS Score: %.4f\n", *r.Score.FTS)
			}
		}
		fmt.Println()
	}

	// Final verdict
	fmt.Println("=== SUMMARY ===")
	if len(matches) == len(duckdb) && len(matches) == len(postgres) {
		fmt.Println("✓ Both backends return identical results")
	} else {
		fmt.Println("○ Backends return different results (may be expected due to different scoring algorithms)")
	}

	if timings["PostgreSQL"] < timings["DuckDB"] {
		fmt.Println("✓ PostgreSQL is faster")
	} else {
		fmt.Println("✓ DuckDB is faster")
	}
}

func search(ctx context.Context, baseURL, query string, limit int, includeBPS, includeScores bool) ([]RegionResponse, error) {
	url := fmt.Sprintf("%s/v1/search?q=%s&limit=%d&include_bps=%v&include_scores=%v",
		baseURL, query, limit, includeBPS, includeScores)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("API returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var results []RegionResponse
	if err := json.Unmarshal(body, &results); err != nil {
		return nil, err
	}

	return results, nil
}

func scoreStr(s *float64) string {
	if s == nil {
		return "-"
	}
	return fmt.Sprintf("%.4f", *s)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-3] + "..."
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}
