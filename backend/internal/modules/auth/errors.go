package moduleauth

import "strings"

// isUniqueViolation detects SQLite UNIQUE constraint violations on insert.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint failed") ||
		strings.Contains(msg, "duplicate key value")
}
