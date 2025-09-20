package sqlnormalize

import "regexp"

var (
	engineRegex      = regexp.MustCompile(`\) ENGINE=[^;]+;`)
	createIndexRegex = regexp.MustCompile(`CREATE INDEX [^;]+;`)
	lineSplitRegex   = regexp.MustCompile("\r?\n")
	blankLineRegex   = regexp.MustCompile(`^\s*$`)
)

// Normalizer removes engine-specific SQL clauses.
type Normalizer interface {
	Normalize(sql string) string
}

// MySQLStripper transforms MySQL dumps into DuckDB-friendly SQL.
type MySQLStripper struct{}

// Normalize strips MySQL-specific clauses to improve DuckDB compatibility.
func (MySQLStripper) Normalize(sql string) string {
	return RemoveMySQLSyntax(sql)
}

// RemoveMySQLSyntax strips MySQL-specific clauses to improve DuckDB compatibility.
func RemoveMySQLSyntax(sql string) string {
	sql = engineRegex.ReplaceAllString(sql, ");")
	sql = createIndexRegex.ReplaceAllString(sql, "")

	lines := make([]string, 0)
	for _, line := range lineSplitRegex.Split(sql, -1) {
		if blankLineRegex.MatchString(line) {
			continue
		}
		lines = append(lines, line)
	}

	return joinLines(lines)
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	result := lines[0]
	for _, line := range lines[1:] {
		result += "\n" + line
	}
	return result
}
