package generator

import (
	"context"
	"fmt"
	"log/slog"
	"math/rand"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/nvoxland/gendb/pkg/config"
	"github.com/nvoxland/gendb/pkg/llm"
	"github.com/nvoxland/gendb/pkg/schema"
)

// ProgressFunc is called after each table's rows are inserted to report progress.
type ProgressFunc func(tableName string, completedTables, totalTables, completedRows, totalRows int)

// Generator produces synthetic data for a schema.
type Generator struct {
	llmClient       *llm.Client
	cfg             config.GenerationConfig
	targetSchema    string
	tableNameMapper func(string) string
	progressFunc    ProgressFunc
}

// Option configures a Generator.
type Option func(*Generator)

// WithTargetSchema sets the schema name for insert operations.
func WithTargetSchema(schema string) Option {
	return func(g *Generator) {
		g.targetSchema = schema
	}
}

// WithTableNameMapper sets a function that maps original table names to target table names.
func WithTableNameMapper(mapper func(string) string) Option {
	return func(g *Generator) {
		g.tableNameMapper = mapper
	}
}

// WithProgressFunc sets a callback that is invoked after each table's rows are inserted.
func WithProgressFunc(fn ProgressFunc) Option {
	return func(g *Generator) {
		g.progressFunc = fn
	}
}

// New creates a new Generator. An LLM client is required.
func New(llmClient *llm.Client, cfg config.GenerationConfig, opts ...Option) (*Generator, error) {
	if llmClient == nil {
		return nil, fmt.Errorf("LLM client is required for data generation")
	}
	g := &Generator{
		llmClient: llmClient,
		cfg:       cfg,
	}
	for _, opt := range opts {
		opt(g)
	}
	return g, nil
}

// Generate generates synthetic data for all tables and inserts into the target database.
func (g *Generator) Generate(ctx context.Context, sg *schema.SchemaGraph, targetConn *pgx.Conn) error {
	// Track generated PKs for FK resolution
	pkValues := make(map[string][]map[string]any) // table -> list of PK value maps

	totalRows := 0
	for _, t := range sg.Tables {
		totalRows += g.rowCount(t.Name)
	}
	completedTables := 0
	completedRows := 0

	slog.Info("Starting data generation", "tables", len(sg.Tables), "total_rows", totalRows)

	for _, table := range sg.Tables {
		rows := g.rowCount(table.Name)
		slog.Info("Generating rows via LLM", "table", table.Name, "rows", rows)

		// Build schema context for this table only (plus FK-referenced tables)
		schemaContext := sg.FormatTableForLLM(table.Name)

		// Identify skip columns
		skipCols := g.skipColumns(table)

		// Build column instructions from config
		colInstructions := g.columnInstructions(table)

		// Resolve FK values
		fkValues := make(map[string][]any)
		for _, col := range table.Columns {
			if fkTable := fkTarget(table, col.Name); fkTable != "" {
				if refs, ok := pkValues[fkTable]; ok && len(refs) > 0 {
					refCol := fkRefCol(table, col.Name)
					vals := make([]any, 0, len(refs))
					for _, ref := range refs {
						if v, ok := ref[refCol]; ok {
							vals = append(vals, v)
						}
					}
					fkValues[col.Name] = vals
				}
			}
		}

		req := llm.TableDataRequest{
			SchemaContext:      schemaContext,
			Table:              table,
			RowCount:           rows,
			FKValues:           fkValues,
			ColumnInstructions: colInstructions,
			SkipColumns:        skipCols,
			UniqueColumns:      table.UniqueColumns(),
		}

		generatedRows, err := g.llmClient.GenerateTableData(ctx, req)
		if err != nil {
			return fmt.Errorf("generating data for %s: %w", table.Name, err)
		}

		// Coerce types
		for i := range generatedRows {
			if err := coerceRow(generatedRows[i], table); err != nil {
				return fmt.Errorf("coercing row %d for %s: %w", i, table.Name, err)
			}
		}

		// Post-process: validate FK values
		for colName, validVals := range fkValues {
			if len(validVals) == 0 {
				continue
			}
			for _, row := range generatedRows {
				if v, ok := row[colName]; ok {
					if !containsValue(validVals, v) {
						row[colName] = validVals[rand.Intn(len(validVals))]
					}
				}
			}
		}

		// Post-process: enforce uniqueness with regeneration
		uniqueTracker := newUniqueTracker(table)
		if !uniqueTracker.isEmpty() {
			var uniqueRows []map[string]any
			for _, row := range generatedRows {
				if uniqueTracker.isUnique(row) {
					uniqueTracker.add(row)
					uniqueRows = append(uniqueRows, row)
				}
			}

			lost := len(generatedRows) - len(uniqueRows)
			if lost > 0 {
				slog.Warn("Rows lost to unique constraint violations, requesting replacements",
					"table", table.Name, "lost", lost)

				for attempt := 0; attempt < 2 && lost > 0; attempt++ {
					retryReq := llm.TableDataRequest{
						SchemaContext:      schemaContext,
						Table:              table,
						RowCount:           lost,
						FKValues:           fkValues,
						ColumnInstructions: colInstructions,
						SkipColumns:        skipCols,
						UniqueColumns:      table.UniqueColumns(),
						PreviousRows:       uniqueRows,
					}
					extraRows, err := g.llmClient.GenerateTableData(ctx, retryReq)
					if err != nil {
						slog.Warn("Failed to regenerate rows for unique violations",
							"table", table.Name, "attempt", attempt, "error", err)
						break
					}
					// Coerce and filter extra rows
					for i := range extraRows {
						if err := coerceRow(extraRows[i], table); err != nil {
							continue
						}
						// Validate FK values on extra rows
						for colName, validVals := range fkValues {
							if len(validVals) == 0 {
								continue
							}
							if v, ok := extraRows[i][colName]; ok {
								if !containsValue(validVals, v) {
									extraRows[i][colName] = validVals[rand.Intn(len(validVals))]
								}
							}
						}
						if uniqueTracker.isUnique(extraRows[i]) {
							uniqueTracker.add(extraRows[i])
							uniqueRows = append(uniqueRows, extraRows[i])
							lost--
						}
					}
				}
				if lost > 0 {
					slog.Warn("Could not fully recover unique rows",
						"table", table.Name, "still_missing", lost)
				}
			}
			generatedRows = uniqueRows
		}

		// Remove skip columns from rows (in case LLM included them)
		skipSet := make(map[string]bool)
		for _, s := range skipCols {
			skipSet[s] = true
		}
		for _, row := range generatedRows {
			for k := range row {
				if skipSet[k] {
					delete(row, k)
				}
			}
		}

		if err := g.insertRows(ctx, targetConn, table, generatedRows); err != nil {
			return fmt.Errorf("inserting data for %s: %w", table.Name, err)
		}

		pkValues[table.Name] = extractPKValues(table, generatedRows)
		slog.Info("Inserted rows", "table", table.Name, "rows", len(generatedRows))

		completedTables++
		completedRows += len(generatedRows)
		if g.progressFunc != nil {
			g.progressFunc(table.Name, completedTables, len(sg.Tables), completedRows, totalRows)
		}
	}

	return nil
}

// skipColumns returns columns that should not be generated (auto-generated, serial, FK).
func (g *Generator) skipColumns(table *schema.Table) []string {
	var skip []string
	for _, col := range table.Columns {
		if col.IsGenerated {
			skip = append(skip, col.Name)
			continue
		}
		if strings.Contains(col.DefaultValue, "nextval(") || strings.Contains(col.DefaultValue, "gen_random_uuid()") {
			skip = append(skip, col.Name)
			continue
		}
		if strings.Contains(strings.ToLower(col.DataType), "serial") {
			skip = append(skip, col.Name)
			continue
		}
		if fkTarget(table, col.Name) != "" {
			// FK columns are included in FKValues, not skip — LLM picks from the list
			// But we do skip them from the "columns to generate" since they're constrained
			continue
		}
		// Check config for skip
		if tc, ok := g.cfg.Tables[table.Name]; ok {
			if cc, ok := tc.Columns[col.Name]; ok && cc.Generator == "skip" {
				skip = append(skip, col.Name)
				continue
			}
		}
		for _, rule := range g.cfg.ColumnRules {
			if MatchPattern(col.Name, rule.Pattern) && rule.Generator == "skip" {
				skip = append(skip, col.Name)
				break
			}
		}
	}
	return skip
}

// columnInstructions builds LLM prompt instructions from config overrides.
func (g *Generator) columnInstructions(table *schema.Table) map[string]string {
	instructions := make(map[string]string)

	if tc, ok := g.cfg.Tables[table.Name]; ok {
		for colName, colCfg := range tc.Columns {
			if instr := configToInstruction(colCfg); instr != "" {
				instructions[colName] = instr
			}
		}
	}

	// Apply column rules
	for _, rule := range g.cfg.ColumnRules {
		for _, col := range table.Columns {
			if MatchPattern(col.Name, rule.Pattern) {
				if rule.Generator == "skip" {
					continue
				}
				if rule.Format != "" {
					instructions[col.Name] = fmt.Sprintf("must match the regex pattern: %s", rule.Format)
				}
			}
		}
	}

	return instructions
}

func configToInstruction(cc config.ColumnConfig) string {
	switch cc.Generator {
	case "one_of":
		if len(cc.Values) > 0 {
			return fmt.Sprintf("must be one of: [%s]", strings.Join(cc.Values, ", "))
		}
	case "regex":
		if cc.Format != "" {
			return fmt.Sprintf("must match the regex pattern: %s", cc.Format)
		}
	case "skip":
		return ""
	}
	if cc.Prompt != "" {
		return cc.Prompt
	}
	return ""
}

func (g *Generator) rowCount(tableName string) int {
	if tc, ok := g.cfg.Tables[tableName]; ok && tc.Rows > 0 {
		return tc.Rows
	}
	if g.cfg.DefaultRows > 0 {
		return g.cfg.DefaultRows
	}
	return 100
}

func (g *Generator) insertRows(ctx context.Context, conn *pgx.Conn, table *schema.Table, rows []map[string]any) error {
	if len(rows) == 0 {
		return nil
	}

	// Determine which columns have data
	colSet := make(map[string]bool)
	for _, row := range rows {
		for k := range row {
			colSet[k] = true
		}
	}

	var columns []string
	for _, col := range table.Columns {
		if colSet[col.Name] {
			columns = append(columns, col.Name)
		}
	}

	if len(columns) == 0 {
		return nil
	}

	// Use COPY protocol for batch insert
	copyRows := make([][]any, len(rows))
	for i, row := range rows {
		vals := make([]any, len(columns))
		for j, col := range columns {
			vals[j] = row[col]
		}
		copyRows[i] = vals
	}

	tableName := table.Name
	if g.tableNameMapper != nil {
		tableName = g.tableNameMapper(table.Name)
	}
	tableIdent := pgx.Identifier{tableName}
	if g.targetSchema != "" {
		tableIdent = pgx.Identifier{g.targetSchema, tableName}
	}

	copyCount, err := conn.CopyFrom(
		ctx,
		tableIdent,
		columns,
		pgx.CopyFromRows(copyRows),
	)
	if err != nil {
		return fmt.Errorf("COPY insert: %w", err)
	}

	if int(copyCount) != len(rows) {
		return fmt.Errorf("expected to insert %d rows, inserted %d", len(rows), copyCount)
	}

	return nil
}

// fkTarget returns the referenced table for a column, or "" if not a FK.
func fkTarget(table *schema.Table, colName string) string {
	for _, fk := range table.ForeignKeys {
		for _, c := range fk.Columns {
			if c == colName {
				return fk.ReferencedTable
			}
		}
	}
	return ""
}

// fkRefCol returns the referenced column for a FK column.
func fkRefCol(table *schema.Table, colName string) string {
	for _, fk := range table.ForeignKeys {
		for i, c := range fk.Columns {
			if c == colName {
				return fk.ReferencedCols[i]
			}
		}
	}
	return ""
}

func extractPKValues(table *schema.Table, rows []map[string]any) []map[string]any {
	if len(table.PrimaryKey) == 0 {
		return nil
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		pkRow := make(map[string]any)
		for _, pk := range table.PrimaryKey {
			if v, ok := row[pk]; ok {
				pkRow[pk] = v
			}
		}
		if len(pkRow) > 0 {
			result = append(result, pkRow)
		}
	}
	return result
}

// FillColumn generates data for a single new column across existing rows.
func (g *Generator) FillColumn(ctx context.Context, conn *pgx.Conn, qualifiedTable string, col *schema.Column, pkColumns []string) error {
	// Build a minimal schema context
	schemaContext := fmt.Sprintf("Table: %s\nColumn: %s (%s)\n", qualifiedTable, col.Name, col.DataType)

	// Count rows
	var rowCount int
	err := conn.QueryRow(ctx, fmt.Sprintf("SELECT COUNT(*) FROM %s", qualifiedTable)).Scan(&rowCount)
	if err != nil {
		return fmt.Errorf("counting rows: %w", err)
	}
	if rowCount == 0 {
		return nil
	}

	// Create a temporary table object for the LLM call
	tempTable := &schema.Table{Name: qualifiedTable, Columns: []*schema.Column{col}}

	values, err := g.llmClient.GenerateColumnValues(ctx, schemaContext, tempTable, col, rowCount)
	if err != nil {
		return fmt.Errorf("generating values for column %s: %w", col.Name, err)
	}

	if len(pkColumns) == 0 {
		// No PK: set all rows to the first generated value
		if len(values) > 0 {
			coerced, err := coerceValue(values[0], col)
			if err != nil {
				return fmt.Errorf("coercing value for column %s: %w", col.Name, err)
			}
			_, err = conn.Exec(ctx, fmt.Sprintf("UPDATE %s SET %s = $1",
				qualifiedTable, pgx.Identifier{col.Name}.Sanitize()), coerced)
			return err
		}
		return nil
	}

	// Build SELECT for PK columns
	pkIdents := make([]string, len(pkColumns))
	for i, pk := range pkColumns {
		pkIdents[i] = pgx.Identifier{pk}.Sanitize()
	}
	selectSQL := fmt.Sprintf("SELECT %s FROM %s", strings.Join(pkIdents, ", "), qualifiedTable)
	rows, err := conn.Query(ctx, selectSQL)
	if err != nil {
		return fmt.Errorf("reading primary keys: %w", err)
	}
	defer rows.Close()

	type pkRow struct {
		values []any
	}
	var pkRows []pkRow
	for rows.Next() {
		vals := make([]any, len(pkColumns))
		ptrs := make([]any, len(pkColumns))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return fmt.Errorf("scanning primary key: %w", err)
		}
		pkRows = append(pkRows, pkRow{values: vals})
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Build WHERE clause for PK
	whereParts := make([]string, len(pkColumns))
	for i, pk := range pkColumns {
		whereParts[i] = fmt.Sprintf("%s = $%d", pgx.Identifier{pk}.Sanitize(), i+2)
	}
	updateSQL := fmt.Sprintf("UPDATE %s SET %s = $1 WHERE %s",
		qualifiedTable, pgx.Identifier{col.Name}.Sanitize(), strings.Join(whereParts, " AND "))

	// Batch update each row with generated values
	batch := &pgx.Batch{}
	for i, pk := range pkRows {
		var val any
		if i < len(values) {
			coerced, err := coerceValue(values[i], col)
			if err != nil {
				return fmt.Errorf("coercing value for column %s: %w", col.Name, err)
			}
			val = coerced
		} else if len(values) > 0 {
			// Reuse last value if we ran short
			coerced, err := coerceValue(values[len(values)-1], col)
			if err != nil {
				return fmt.Errorf("coercing value for column %s: %w", col.Name, err)
			}
			val = coerced
		}
		args := make([]any, 0, 1+len(pk.values))
		args = append(args, val)
		args = append(args, pk.values...)
		batch.Queue(updateSQL, args...)
	}

	br := conn.SendBatch(ctx, batch)
	defer func() { _ = br.Close() }()
	for range pkRows {
		if _, err := br.Exec(); err != nil {
			return fmt.Errorf("updating column %s: %w", col.Name, err)
		}
	}

	return nil
}

func MatchPattern(name, pattern string) bool {
	// Simple glob matching: only supports * prefix/suffix
	if pattern == "*" {
		return true
	}
	if strings.HasPrefix(pattern, "*") && strings.HasSuffix(pattern, "*") {
		return strings.Contains(name, pattern[1:len(pattern)-1])
	}
	if strings.HasPrefix(pattern, "*") {
		return strings.HasSuffix(name, pattern[1:])
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(name, pattern[:len(pattern)-1])
	}
	return name == pattern
}

func containsValue(vals []any, v any) bool {
	target := fmt.Sprintf("%v", v)
	for _, val := range vals {
		if fmt.Sprintf("%v", val) == target {
			return true
		}
	}
	return false
}
