package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	_ "github.com/marcboeker/go-duckdb"
)

func main() {
	// Connect to a new or existing DuckDB file: data/regions.duckdb
	dbPath := filepath.Join("data", "regions.duckdb")
	db, err := sql.Open("duckdb", dbPath)
	if err != nil {
		log.Fatal("Failed to open database:", err)
	}
	defer db.Close()

	// Load the Kemendagri wilayah SQL dump
	execSQLFromFile(db, filepath.Join("data", "wilayah.sql"))

	// Load the postal-code supplement
	execSQLFromFile(db, filepath.Join("data", "wilayah_kodepos.sql"))

	// Load the BPS wilayah mapping dump (generated via make fetch-bps)
	bpsSQLPath := filepath.Join("data", "bps_wilayah.sql")
	if _, err := os.Stat(bpsSQLPath); err != nil {
		log.Fatalf("Failed to locate %s: %v. Run 'make fetch-bps' first.", bpsSQLPath, err)
	}
	execSQLFromFile(db, bpsSQLPath)

	// Execute the transformation query to denormalize the data and create the final regions table
	// Using LEFT JOIN to maintain backward compatibility - postal code will be NULL if not available

	transformationQuery := `
CREATE OR REPLACE TABLE regions AS
SELECT
	   sub.kode AS id,
	   sub.nama AS subdistrict,
	   dist.nama AS district,
	   city.nama AS city,
	   prov.nama AS province,
	   bps_sub.nama_bps AS subdistrict_bps,
	   bps_dist.nama_bps AS district_bps,
	   bps_city.nama_bps AS city_bps,
	   bps_prov.nama_bps AS province_bps,
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

	_, err = db.Exec(transformationQuery)
	if err != nil {
		log.Fatal("Failed to execute transformation query:", err)
	}

	mappingQuery := `
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

	_, err = db.Exec(mappingQuery)
	if err != nil {
		log.Fatal("Failed to create BPS mapping table:", err)
	}

	// Clean up by dropping the raw wilayah table
	_, err = db.Exec("DROP TABLE IF EXISTS wilayah;")
	if err != nil {
		log.Fatal("Failed to drop wilayah table:", err)
	}

	// Clean up by dropping the wilayah_kodepos table
	_, err = db.Exec("DROP TABLE IF EXISTS wilayah_kodepos;")
	if err != nil {
		log.Fatal("Failed to drop wilayah_kodepos table:", err)
	}

	// Install and load the FTS extension
	_, err = db.Exec("INSTALL fts;")
	if err != nil {
		log.Fatal("Failed to install FTS extension:", err)
	}
	_, err = db.Exec("LOAD fts;")
	if err != nil {
		log.Fatal("Failed to load FTS extension:", err)
	}

	// Create the FTS index on the 'full_text' column of the 'regions' table
	_, err = db.Exec("PRAGMA create_fts_index('regions', 'id', 'full_text', overwrite=1);")
	if err != nil {
		log.Fatal("Failed to create FTS index:", err)
	}

	fmt.Println("Data ingestion and preparation completed successfully with postal codes and BPS mappings!")
}

// removeMySQLSyntax removes MySQL-specific syntax to make the SQL compatible with DuckDB
func removeMySQLSyntax(sql string) string {
	// Remove ENGINE specification
	re := regexp.MustCompile(`\) ENGINE=[^;]+;`)
	sql = re.ReplaceAllString(sql, ");")

	// Remove CREATE INDEX statements (DuckDB handles indexing differently)
	re = regexp.MustCompile(`CREATE INDEX [^;]+;`)
	sql = re.ReplaceAllString(sql, "")

	// Remove lines that only contain whitespace after processing
	lines := strings.Split(sql, "\n")
	var result []string
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

// execSQLFromFile reads a SQL dump, normalizes it for DuckDB, and executes it.
func execSQLFromFile(db *sql.DB, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("Failed to read SQL file %s: %v", path, err)
	}

	sql := removeMySQLSyntax(string(data))
	if _, err := db.Exec(sql); err != nil {
		log.Fatalf("Failed to execute SQL from %s: %v", path, err)
	}
}
