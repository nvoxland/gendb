package generator

import (
	"fmt"
	"strings"

	"github.com/nvoxland/gendb/pkg/schema"
)

// uniqueTracker enforces UNIQUE constraints during generation.
type uniqueTracker struct {
	constraints [][]string          // each is a list of column names forming a unique constraint
	seen        map[string]struct{} // composite key string -> exists
}

func newUniqueTracker(table *schema.Table) *uniqueTracker {
	ut := &uniqueTracker{
		seen: make(map[string]struct{}),
	}
	for _, idx := range table.Indexes {
		if idx.IsUnique {
			ut.constraints = append(ut.constraints, idx.Columns)
		}
	}
	return ut
}

func (ut *uniqueTracker) isEmpty() bool {
	return len(ut.constraints) == 0
}

func (ut *uniqueTracker) isUnique(row map[string]any) bool {
	for _, cols := range ut.constraints {
		key := ut.makeKey(cols, row)
		if _, exists := ut.seen[key]; exists {
			return false
		}
	}
	return true
}

func (ut *uniqueTracker) add(row map[string]any) {
	for _, cols := range ut.constraints {
		key := ut.makeKey(cols, row)
		ut.seen[key] = struct{}{}
	}
}

func (ut *uniqueTracker) makeKey(cols []string, row map[string]any) string {
	parts := make([]string, len(cols))
	for i, col := range cols {
		parts[i] = fmt.Sprintf("%v", row[col])
	}
	return strings.Join(parts, "\x00")
}
