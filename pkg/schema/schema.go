package schema

// SchemaGraph represents the full database schema with dependency ordering.
type SchemaGraph struct {
	Tables     []*Table // topologically sorted by FK dependencies
	tableIndex map[string]*Table
}

type Table struct {
	Schema      string
	Name        string
	Columns     []*Column
	PrimaryKey  []string
	ForeignKeys []*ForeignKey
	Checks      []*CheckConstraint
	Indexes     []*Index
}

type Column struct {
	Name         string
	DataType     string // e.g. "integer", "varchar(255)", "text"
	IsNullable   bool
	DefaultValue string // raw SQL default expression
	IsGenerated  bool   // generated column (GENERATED ALWAYS AS)
	Comment      string
}

type ForeignKey struct {
	Name            string
	Columns         []string
	ReferencedTable string
	ReferencedCols  []string
}

type CheckConstraint struct {
	Name       string
	Expression string
}

type Index struct {
	Name     string
	Columns  []string
	IsUnique bool
}

// TableByName returns a table by name, or nil if not found.
func (sg *SchemaGraph) TableByName(name string) *Table {
	if sg.tableIndex == nil {
		return nil
	}
	return sg.tableIndex[name]
}

// TableNames returns all table names in topological order.
func (sg *SchemaGraph) TableNames() []string {
	names := make([]string, len(sg.Tables))
	for i, t := range sg.Tables {
		names[i] = t.Name
	}
	return names
}

// ColumnByName returns a column by name, or nil if not found.
func (t *Table) ColumnByName(name string) *Column {
	for _, c := range t.Columns {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// UniqueColumns returns column names that have unique indexes.
func (t *Table) UniqueColumns() [][]string {
	var result [][]string
	for _, idx := range t.Indexes {
		if idx.IsUnique {
			result = append(result, idx.Columns)
		}
	}
	return result
}
