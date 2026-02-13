package schema

import "fmt"

// topoSort sorts tables so that referenced tables come before referencing tables.
// Uses Kahn's algorithm. Self-referencing FKs are allowed (ignored in sort).
func topoSort(tables []*Table) ([]*Table, error) {
	byName := make(map[string]*Table, len(tables))
	for _, t := range tables {
		byName[t.Name] = t
	}

	// Build adjacency and in-degree
	inDegree := make(map[string]int, len(tables))
	dependents := make(map[string][]string) // table -> tables that depend on it

	for _, t := range tables {
		if _, ok := inDegree[t.Name]; !ok {
			inDegree[t.Name] = 0
		}
		for _, fk := range t.ForeignKeys {
			// Skip self-references
			if fk.ReferencedTable == t.Name {
				continue
			}
			// Skip references to tables not in our set (e.g. cross-schema)
			if _, ok := byName[fk.ReferencedTable]; !ok {
				continue
			}
			inDegree[t.Name]++
			dependents[fk.ReferencedTable] = append(dependents[fk.ReferencedTable], t.Name)
		}
	}

	// Kahn's algorithm
	var queue []string
	for _, t := range tables {
		if inDegree[t.Name] == 0 {
			queue = append(queue, t.Name)
		}
	}

	var sorted []*Table
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		sorted = append(sorted, byName[name])

		for _, dep := range dependents[name] {
			inDegree[dep]--
			if inDegree[dep] == 0 {
				queue = append(queue, dep)
			}
		}
	}

	if len(sorted) != len(tables) {
		return nil, fmt.Errorf("circular foreign key dependencies detected among tables")
	}

	return sorted, nil
}
