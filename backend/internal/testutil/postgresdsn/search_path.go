// Package postgresdsn configures isolated PostgreSQL test connections.
package postgresdsn

import (
	"errors"
	"net/url"
	"strings"

	"github.com/lib/pq"
)

// WithSearchPath selects one schema without changing the DSN's other options.
// The schema is quoted as a single PostgreSQL identifier in both URL and
// keyword/value DSNs, even when its name contains search-path separators.
func WithSearchPath(dsn, schema string) (string, error) {
	if strings.TrimSpace(dsn) == "" || strings.ContainsRune(dsn, '\x00') {
		return "", errors.New("a PostgreSQL test DSN is required")
	}
	if schema == "" || strings.ContainsRune(schema, '\x00') {
		return "", errors.New("a nonempty PostgreSQL schema without NUL bytes is required")
	}
	searchPath := pq.QuoteIdentifier(schema)
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		parsed, err := url.Parse(dsn)
		if err != nil {
			return "", errors.New("invalid PostgreSQL URL DSN")
		}
		query, err := url.ParseQuery(parsed.RawQuery)
		if err != nil {
			return "", errors.New("invalid PostgreSQL URL DSN query")
		}
		query.Set("search_path", searchPath)
		parsed.RawQuery = query.Encode()
		return parsed.String(), nil
	}
	// lib/pq keyword values escape both backslashes and single quotes with a
	// backslash. Preserve the original DSN, including escaped trailing spaces.
	quoted := strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(searchPath)
	return dsn + " search_path='" + quoted + "'", nil
}
