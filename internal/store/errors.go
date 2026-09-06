// Handwritten helpers next to the sqlc-generated Queries; sqlc never
// touches this file.
package store

import (
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// IsUniqueViolation reports whether err is a SQLite unique-constraint
// violation.
func IsUniqueViolation(err error) bool {
	var sqErr *sqlite.Error
	return errors.As(err, &sqErr) && sqErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}
