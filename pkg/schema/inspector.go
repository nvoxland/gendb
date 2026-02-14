package schema

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

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

func (i *Inspector) getTables(ctx context.Context) ([]*Table, error) {
	return i.getTablesWithOptions(ctx, InspectOptions{})
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

// FormatForLLM returns a human-readable schema description for LLM consumption.
func (sg *SchemaGraph) FormatForLLM() string {
	var b strings.Builder
	for _, t := range sg.Tables {
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
	}
	return b.String()
}
