package usecase

import "strings"

// defaultProject normalizes the project selector for writes: blank -> "default".
// (Read/export functions intentionally keep "" to mean "all projects".)
func defaultProject(p string) string {
	if p = strings.TrimSpace(p); p == "" {
		return "default"
	}
	return p
}

// normalizeEntityType lowercases + trims; empty becomes "concept".
// The DB FK (memory_entity_types) rejects anything not registered.
func normalizeEntityType(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		s = "concept"
	}
	return s
}

// normalizeRelationType forces UPPER_SNAKE_CASE: trim, uppercase, and turn any
// run of non-alphanumeric characters into a single "_".
func normalizeRelationType(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	var b strings.Builder
	prevUnder := false
	for _, r := range s {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevUnder = false
			continue
		}
		if !prevUnder && b.Len() > 0 {
			b.WriteByte('_')
			prevUnder = true
		}
	}
	return strings.Trim(b.String(), "_")
}
