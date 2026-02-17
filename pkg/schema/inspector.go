package schema

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Bucket represents a histogram bucket for column statistics.
type Bucket struct {
	Range   string // human-readable range label
	Count   int64
	Percent float64
}

// ColumnStats holds distribution statistics for a single column.
type ColumnStats struct {
	ColumnName  string
	DataType    string
	PercentNull float64

	// String columns — histogram buckets for length
	LengthBuckets []Bucket
	PercentEmpty  *float64

	// Numeric columns — histogram buckets for value range
	ValueBuckets []Bucket
	PercentZero  *float64
}

// TableStats holds statistics for a table's data distribution.
type TableStats struct {
	TableName   string
	RowCount    int64
	ColumnStats []ColumnStats
}

// Inspector introspects a PostgreSQL database schema.
type Inspector struct {
	conn *pgx.Conn
}

// NewInspectorFromConn creates an inspector from an existing connection.
// The caller is responsible for managing the connection's lifecycle.
func NewInspectorFromConn(conn *pgx.Conn) *Inspector {
	return &Inspector{conn: conn}
}

// InspectOptions controls schema introspection behavior.
type InspectOptions struct {
	ExcludeSchemas []string
}

// Inspect reads the full schema and returns a topologically sorted SchemaGraph.
func (i *Inspector) Inspect(ctx context.Context) (*SchemaGraph, error) {
	return i.InspectWithOptions(ctx, InspectOptions{})
}

// InspectTable reads the schema for a single named table (excluding system schemas).
func (i *Inspector) InspectTable(ctx context.Context, tableName string) (*SchemaGraph, error) {
	rows, err := i.conn.Query(ctx, `
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_name = $1
		  AND table_schema NOT IN ('pg_catalog', 'information_schema')
		  AND table_type = 'BASE TABLE'
		ORDER BY table_schema
		LIMIT 1
	`, tableName)
	if err != nil {
		return nil, fmt.Errorf("querying table %s: %w", tableName, err)
	}
	defer rows.Close()

	var table *Table
	for rows.Next() {
		table = &Table{}
		if err := rows.Scan(&table.Schema, &table.Name); err != nil {
			return nil, fmt.Errorf("scanning table %s: %w", tableName, err)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if table == nil {
		return nil, fmt.Errorf("table %s not found", tableName)
	}

	if err := i.getColumns(ctx, table); err != nil {
		return nil, err
	}
	if err := i.getForeignKeys(ctx, table); err != nil {
		return nil, err
	}
	if err := i.getChecks(ctx, table); err != nil {
		return nil, err
	}
	if err := i.getIndexes(ctx, table); err != nil {
		return nil, err
	}

	tableIndex := map[string]*Table{table.Name: table}
	return &SchemaGraph{Tables: []*Table{table}, tableIndex: tableIndex}, nil
}

// InspectWithOptions reads the schema with filtering options.
func (i *Inspector) InspectWithOptions(ctx context.Context, opts InspectOptions) (*SchemaGraph, error) {
	tables, err := i.getTablesWithOptions(ctx, opts)
	if err != nil {
		return nil, err
	}

	tableIndex := make(map[string]*Table, len(tables))
	for _, t := range tables {
		tableIndex[t.Name] = t
	}

	for _, t := range tables {
		if err := i.getColumns(ctx, t); err != nil {
			return nil, err
		}
		if err := i.getForeignKeys(ctx, t); err != nil {
			return nil, err
		}
		if err := i.getChecks(ctx, t); err != nil {
			return nil, err
		}
		if err := i.getIndexes(ctx, t); err != nil {
			return nil, err
		}
	}

	sorted, err := topoSort(tables)
	if err != nil {
		return nil, err
	}

	return &SchemaGraph{Tables: sorted, tableIndex: tableIndex}, nil
}

func (i *Inspector) getTablesWithOptions(ctx context.Context, opts InspectOptions) ([]*Table, error) {
	excludeSchemas := []string{"pg_catalog", "information_schema"}
	excludeSchemas = append(excludeSchemas, opts.ExcludeSchemas...)

	rows, err := i.conn.Query(ctx, `
		SELECT table_schema, table_name
		FROM information_schema.tables
		WHERE table_schema != ALL($1)
		  AND table_type = 'BASE TABLE'
		ORDER BY table_schema, table_name
	`, excludeSchemas)
	if err != nil {
		return nil, fmt.Errorf("querying tables: %w", err)
	}
	defer rows.Close()

	var tables []*Table
	for rows.Next() {
		t := &Table{}
		if err := rows.Scan(&t.Schema, &t.Name); err != nil {
			return nil, fmt.Errorf("scanning table: %w", err)
		}
		tables = append(tables, t)
	}
	return tables, rows.Err()
}

func (i *Inspector) getColumns(ctx context.Context, t *Table) error {
	rows, err := i.conn.Query(ctx, `
		SELECT
			c.column_name,
			c.data_type,
			CASE WHEN c.character_maximum_length IS NOT NULL
				THEN c.data_type || '(' || c.character_maximum_length || ')'
				WHEN c.numeric_precision IS NOT NULL AND c.data_type NOT IN ('integer', 'bigint', 'smallint')
				THEN c.data_type || '(' || c.numeric_precision || ',' || c.numeric_scale || ')'
				ELSE c.udt_name
			END AS full_type,
			c.is_nullable = 'YES' AS is_nullable,
			COALESCE(c.column_default, '') AS column_default,
			COALESCE(c.is_generated, 'NEVER') != 'NEVER' AS is_generated,
			COALESCE(pgd.description, '') AS comment
		FROM information_schema.columns c
		LEFT JOIN pg_catalog.pg_statio_all_tables psat
			ON psat.schemaname = c.table_schema AND psat.relname = c.table_name
		LEFT JOIN pg_catalog.pg_description pgd
			ON pgd.objoid = psat.relid AND pgd.objsubid = c.ordinal_position
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position
	`, t.Schema, t.Name)
	if err != nil {
		return fmt.Errorf("querying columns for %s: %w", t.Name, err)
	}
	defer rows.Close()

	for rows.Next() {
		c := &Column{}
		var rawType string
		if err := rows.Scan(&c.Name, &rawType, &c.DataType, &c.IsNullable, &c.DefaultValue, &c.IsGenerated, &c.Comment); err != nil {
			return fmt.Errorf("scanning column for %s: %w", t.Name, err)
		}
		t.Columns = append(t.Columns, c)
	}
	return rows.Err()
}

func (i *Inspector) getForeignKeys(ctx context.Context, t *Table) error {
	rows, err := i.conn.Query(ctx, `
		SELECT
			tc.constraint_name,
			kcu.column_name,
			ccu.table_name AS referenced_table,
			ccu.column_name AS referenced_column
		FROM information_schema.table_constraints tc
		JOIN information_schema.key_column_usage kcu
			ON tc.constraint_name = kcu.constraint_name
			AND tc.table_schema = kcu.table_schema
		JOIN information_schema.constraint_column_usage ccu
			ON ccu.constraint_name = tc.constraint_name
			AND ccu.table_schema = tc.table_schema
		WHERE tc.constraint_type = 'FOREIGN KEY'
			AND tc.table_schema = $1
			AND tc.table_name = $2
		ORDER BY tc.constraint_name, kcu.ordinal_position
	`, t.Schema, t.Name)
	if err != nil {
		return fmt.Errorf("querying foreign keys for %s: %w", t.Name, err)
	}
	defer rows.Close()

	fkMap := make(map[string]*ForeignKey)
	var fkOrder []string
	for rows.Next() {
		var name, col, refTable, refCol string
		if err := rows.Scan(&name, &col, &refTable, &refCol); err != nil {
			return fmt.Errorf("scanning foreign key for %s: %w", t.Name, err)
		}
		fk, ok := fkMap[name]
		if !ok {
			fk = &ForeignKey{Name: name, ReferencedTable: refTable}
			fkMap[name] = fk
			fkOrder = append(fkOrder, name)
		}
		fk.Columns = append(fk.Columns, col)
		fk.ReferencedCols = append(fk.ReferencedCols, refCol)
	}
	for _, name := range fkOrder {
		t.ForeignKeys = append(t.ForeignKeys, fkMap[name])
	}
	return rows.Err()
}

func (i *Inspector) getChecks(ctx context.Context, t *Table) error {
	rows, err := i.conn.Query(ctx, `
		SELECT tc.constraint_name, cc.check_clause
		FROM information_schema.table_constraints tc
		JOIN information_schema.check_constraints cc
			ON tc.constraint_name = cc.constraint_name
			AND tc.constraint_schema = cc.constraint_schema
		WHERE tc.constraint_type = 'CHECK'
			AND tc.table_schema = $1
			AND tc.table_name = $2
			AND tc.constraint_name NOT LIKE '%_not_null'
		ORDER BY tc.constraint_name
	`, t.Schema, t.Name)
	if err != nil {
		return fmt.Errorf("querying checks for %s: %w", t.Name, err)
	}
	defer rows.Close()

	for rows.Next() {
		c := &CheckConstraint{}
		if err := rows.Scan(&c.Name, &c.Expression); err != nil {
			return fmt.Errorf("scanning check for %s: %w", t.Name, err)
		}
		t.Checks = append(t.Checks, c)
	}
	return rows.Err()
}

func (i *Inspector) getIndexes(ctx context.Context, t *Table) error {
	rows, err := i.conn.Query(ctx, `
		SELECT
			i.relname AS index_name,
			ix.indisunique AS is_unique,
			array_agg(a.attname ORDER BY array_position(ix.indkey, a.attnum)) AS columns
		FROM pg_catalog.pg_index ix
		JOIN pg_catalog.pg_class i ON i.oid = ix.indexrelid
		JOIN pg_catalog.pg_class tbl ON tbl.oid = ix.indrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = tbl.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = ANY(ix.indkey)
		WHERE n.nspname = $1
			AND tbl.relname = $2
			AND NOT ix.indisprimary
		GROUP BY i.relname, ix.indisunique
		ORDER BY i.relname
	`, t.Schema, t.Name)
	if err != nil {
		return fmt.Errorf("querying indexes for %s: %w", t.Name, err)
	}
	defer rows.Close()

	for rows.Next() {
		idx := &Index{}
		if err := rows.Scan(&idx.Name, &idx.IsUnique, &idx.Columns); err != nil {
			return fmt.Errorf("scanning index for %s: %w", t.Name, err)
		}
		t.Indexes = append(t.Indexes, idx)
	}

	// Also get primary key columns
	pkRows, err := i.conn.Query(ctx, `
		SELECT a.attname
		FROM pg_catalog.pg_index ix
		JOIN pg_catalog.pg_class tbl ON tbl.oid = ix.indrelid
		JOIN pg_catalog.pg_namespace n ON n.oid = tbl.relnamespace
		JOIN pg_catalog.pg_attribute a ON a.attrelid = tbl.oid AND a.attnum = ANY(ix.indkey)
		WHERE n.nspname = $1
			AND tbl.relname = $2
			AND ix.indisprimary
		ORDER BY array_position(ix.indkey, a.attnum)
	`, t.Schema, t.Name)
	if err != nil {
		return fmt.Errorf("querying primary key for %s: %w", t.Name, err)
	}
	defer pkRows.Close()

	for pkRows.Next() {
		var col string
		if err := pkRows.Scan(&col); err != nil {
			return fmt.Errorf("scanning primary key for %s: %w", t.Name, err)
		}
		t.PrimaryKey = append(t.PrimaryKey, col)
	}

	return pkRows.Err()
}

// GatherStats collects distribution statistics for a table's columns.
// Returns nil if the table is empty. Errors are non-fatal — callers should
// log and continue if stats gathering fails.
func (i *Inspector) GatherStats(ctx context.Context, table *Table) (*TableStats, error) {
	// Get row count
	var rowCount int64
	err := i.conn.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s", pgx.Identifier{table.Schema, table.Name}.Sanitize()),
	).Scan(&rowCount)
	if err != nil {
		return nil, fmt.Errorf("counting rows in %s: %w", table.Name, err)
	}
	if rowCount == 0 {
		return nil, nil
	}

	stats := &TableStats{
		TableName: table.Name,
		RowCount:  rowCount,
	}

	for _, col := range table.Columns {
		if shouldSkipStatsColumn(col) {
			continue
		}

		cs, err := i.gatherColumnStats(ctx, table, col, rowCount)
		if err != nil {
			// Non-fatal: skip this column
			continue
		}
		stats.ColumnStats = append(stats.ColumnStats, *cs)
	}

	return stats, nil
}

// shouldSkipStatsColumn returns true for columns where stats aren't useful.
func shouldSkipStatsColumn(col *Column) bool {
	if col.IsGenerated {
		return true
	}
	dt := strings.ToLower(col.DataType)
	// Skip json/jsonb — not useful for histograms
	if strings.Contains(dt, "json") {
		return true
	}
	// Skip serial/sequence columns
	if strings.Contains(dt, "serial") {
		return true
	}
	return false
}

func (i *Inspector) gatherColumnStats(ctx context.Context, table *Table, col *Column, rowCount int64) (*ColumnStats, error) {
	quotedCol := pgx.Identifier{col.Name}.Sanitize()
	quotedTable := pgx.Identifier{table.Schema, table.Name}.Sanitize()

	// Get null count
	var nullCount int64
	err := i.conn.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NULL", quotedTable, quotedCol),
	).Scan(&nullCount)
	if err != nil {
		return nil, err
	}

	cs := &ColumnStats{
		ColumnName:  col.Name,
		DataType:    col.DataType,
		PercentNull: float64(nullCount) / float64(rowCount) * 100,
	}

	dt := strings.ToLower(col.DataType)
	switch {
	case isStringType(dt):
		i.gatherStringStats(ctx, quotedTable, quotedCol, rowCount, cs)
	case isNumericType(dt):
		i.gatherNumericStats(ctx, quotedTable, quotedCol, rowCount, cs)
	}

	return cs, nil
}

func isStringType(dt string) bool {
	return strings.Contains(dt, "char") || strings.Contains(dt, "text") || dt == "name"
}

func isNumericType(dt string) bool {
	return strings.Contains(dt, "int") || strings.Contains(dt, "float") ||
		strings.Contains(dt, "double") || strings.Contains(dt, "real") ||
		strings.Contains(dt, "numeric") || strings.Contains(dt, "decimal")
}

func (i *Inspector) gatherStringStats(ctx context.Context, quotedTable, quotedCol string, rowCount int64, cs *ColumnStats) {
	// Empty count
	var emptyCount int64
	err := i.conn.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s IS NOT NULL AND %s = ''", quotedTable, quotedCol, quotedCol),
	).Scan(&emptyCount)
	if err == nil {
		pct := float64(emptyCount) / float64(rowCount) * 100
		cs.PercentEmpty = &pct
	}

	// Length histogram using CASE-based bucketing
	type lenBucket struct {
		label string
		min   int
		max   int // -1 means unbounded
	}
	buckets := []lenBucket{
		{"1-10", 1, 10},
		{"11-25", 11, 25},
		{"26-50", 26, 50},
		{"51-100", 51, 100},
		{"101+", 101, -1},
	}

	var parts []string
	for _, b := range buckets {
		if b.max == -1 {
			parts = append(parts, fmt.Sprintf(
				"COUNT(*) FILTER (WHERE length(%s) >= %d)", quotedCol, b.min))
		} else {
			parts = append(parts, fmt.Sprintf(
				"COUNT(*) FILTER (WHERE length(%s) BETWEEN %d AND %d)", quotedCol, b.min, b.max))
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL AND %s != ''",
		strings.Join(parts, ", "), quotedTable, quotedCol, quotedCol)

	row := i.conn.QueryRow(ctx, query)
	counts := make([]int64, len(buckets))
	ptrs := make([]any, len(buckets))
	for i := range counts {
		ptrs[i] = &counts[i]
	}
	if err := row.Scan(ptrs...); err != nil {
		return
	}

	nonNull := rowCount - int64(cs.PercentNull/100*float64(rowCount))
	if cs.PercentEmpty != nil {
		nonNull -= int64(*cs.PercentEmpty / 100 * float64(rowCount))
	}
	if nonNull <= 0 {
		nonNull = 1
	}

	for idx, b := range buckets {
		if counts[idx] > 0 {
			cs.LengthBuckets = append(cs.LengthBuckets, Bucket{
				Range:   b.label,
				Count:   counts[idx],
				Percent: math.Round(float64(counts[idx])/float64(nonNull)*1000) / 10,
			})
		}
	}
}

func (i *Inspector) gatherNumericStats(ctx context.Context, quotedTable, quotedCol string, rowCount int64, cs *ColumnStats) {
	// Zero count
	var zeroCount int64
	err := i.conn.QueryRow(ctx,
		fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE %s = 0", quotedTable, quotedCol),
	).Scan(&zeroCount)
	if err == nil {
		pct := float64(zeroCount) / float64(rowCount) * 100
		cs.PercentZero = &pct
	}

	// Get min/max for bucketing
	var minVal, maxVal float64
	err = i.conn.QueryRow(ctx,
		fmt.Sprintf("SELECT COALESCE(MIN(%s)::float8, 0), COALESCE(MAX(%s)::float8, 0) FROM %s WHERE %s IS NOT NULL",
			quotedCol, quotedCol, quotedTable, quotedCol),
	).Scan(&minVal, &maxVal)
	if err != nil || minVal == maxVal {
		return
	}

	// Create 5 equal-width buckets
	numBuckets := 5
	width := (maxVal - minVal) / float64(numBuckets)

	var parts []string
	type numBucket struct {
		label string
		lo    float64
		hi    float64
	}
	buckets := make([]numBucket, numBuckets)

	for b := 0; b < numBuckets; b++ {
		lo := minVal + float64(b)*width
		hi := minVal + float64(b+1)*width
		if b == numBuckets-1 {
			hi = maxVal
		}
		buckets[b] = numBucket{
			label: fmt.Sprintf("%s-%s", formatNum(lo), formatNum(hi)),
			lo:    lo,
			hi:    hi,
		}
		if b == numBuckets-1 {
			parts = append(parts, fmt.Sprintf(
				"COUNT(*) FILTER (WHERE %s::float8 >= %f AND %s::float8 <= %f)",
				quotedCol, lo, quotedCol, hi))
		} else {
			parts = append(parts, fmt.Sprintf(
				"COUNT(*) FILTER (WHERE %s::float8 >= %f AND %s::float8 < %f)",
				quotedCol, lo, quotedCol, hi))
		}
	}

	query := fmt.Sprintf("SELECT %s FROM %s WHERE %s IS NOT NULL",
		strings.Join(parts, ", "), quotedTable, quotedCol)

	row := i.conn.QueryRow(ctx, query)
	counts := make([]int64, numBuckets)
	ptrs := make([]any, numBuckets)
	for idx := range counts {
		ptrs[idx] = &counts[idx]
	}
	if err := row.Scan(ptrs...); err != nil {
		return
	}

	var nonNull int64
	for _, c := range counts {
		nonNull += c
	}
	if nonNull <= 0 {
		nonNull = 1
	}

	for idx, b := range buckets {
		if counts[idx] > 0 {
			cs.ValueBuckets = append(cs.ValueBuckets, Bucket{
				Range:   b.label,
				Count:   counts[idx],
				Percent: math.Round(float64(counts[idx])/float64(nonNull)*1000) / 10,
			})
		}
	}
}

// formatNum formats a float nicely — integer if whole, otherwise 1 decimal.
func formatNum(f float64) string {
	if f == math.Trunc(f) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%.1f", f)
}

// QueryFKValues returns up to `limit` distinct values from the given column,
// used to resolve foreign key references against pre-existing data.
func (i *Inspector) QueryFKValues(ctx context.Context, schemaName, tableName, columnName string, limit int) ([]any, error) {
	query := fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL LIMIT %d",
		pgx.Identifier{columnName}.Sanitize(),
		pgx.Identifier{schemaName, tableName}.Sanitize(),
		pgx.Identifier{columnName}.Sanitize(),
		limit,
	)

	rows, err := i.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying FK values from %s.%s.%s: %w", schemaName, tableName, columnName, err)
	}
	defer rows.Close()

	var vals []any
	for rows.Next() {
		var v any
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scanning FK value from %s.%s: %w", tableName, columnName, err)
		}
		vals = append(vals, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return vals, nil
}

// SampleRows returns a random sample of rows from the table.
// Returns nil if the table is empty.
func (i *Inspector) SampleRows(ctx context.Context, table *Table, n int) ([]map[string]any, error) {
	query := fmt.Sprintf("SELECT * FROM %s ORDER BY random() LIMIT %d",
		pgx.Identifier{table.Schema, table.Name}.Sanitize(), n)

	rows, err := i.conn.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("sampling rows from %s: %w", table.Name, err)
	}
	defer rows.Close()

	var result []map[string]any
	fieldDescs := rows.FieldDescriptions()
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return nil, fmt.Errorf("reading sample row from %s: %w", table.Name, err)
		}
		row := make(map[string]any, len(fieldDescs))
		for j, fd := range fieldDescs {
			row[string(fd.Name)] = values[j]
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return result, nil
}

// FormatTableForLLM returns a schema description limited to the target table
// and any tables referenced by its foreign keys.
func (sg *SchemaGraph) FormatTableForLLM(tableName string) string {
	target := sg.TableByName(tableName)
	if target == nil {
		return sg.FormatForLLM()
	}

	// Collect the target table plus referenced tables
	relevantTables := []*Table{target}
	seen := map[string]bool{tableName: true}
	for _, fk := range target.ForeignKeys {
		if !seen[fk.ReferencedTable] {
			if ref := sg.TableByName(fk.ReferencedTable); ref != nil {
				relevantTables = append(relevantTables, ref)
				seen[fk.ReferencedTable] = true
			}
		}
	}

	var b strings.Builder
	for _, t := range relevantTables {
		b.WriteString(formatTableDescription(t))
	}
	return b.String()
}

// FormatForLLM returns a human-readable schema description for LLM consumption.
func (sg *SchemaGraph) FormatForLLM() string {
	var b strings.Builder
	for _, t := range sg.Tables {
		b.WriteString(formatTableDescription(t))
	}
	return b.String()
}

// formatTableDescription returns a human-readable description of a single table.
func formatTableDescription(t *Table) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Table: %s\n", t.Name)
	fmt.Fprintf(&b, "Columns:\n")
	for _, c := range t.Columns {
		nullable := "NOT NULL"
		if c.IsNullable {
			nullable = "NULLABLE"
		}
		def := ""
		if c.DefaultValue != "" {
			def = " DEFAULT " + c.DefaultValue
		}
		comment := ""
		if c.Comment != "" {
			comment = " -- " + c.Comment
		}
		fmt.Fprintf(&b, "  - %s: %s %s%s%s\n", c.Name, c.DataType, nullable, def, comment)
	}
	if len(t.PrimaryKey) > 0 {
		fmt.Fprintf(&b, "Primary Key: (%s)\n", strings.Join(t.PrimaryKey, ", "))
	}
	for _, fk := range t.ForeignKeys {
		fmt.Fprintf(&b, "Foreign Key: (%s) REFERENCES %s(%s)\n",
			strings.Join(fk.Columns, ", "), fk.ReferencedTable, strings.Join(fk.ReferencedCols, ", "))
	}
	for _, ck := range t.Checks {
		fmt.Fprintf(&b, "Check: %s\n", ck.Expression)
	}
	b.WriteString("\n")
	return b.String()
}
