package generator

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/jackc/pgx/v5"
	"github.com/nvoxland/gendb/pkg/config"
	"github.com/nvoxland/gendb/pkg/llm"
	"github.com/nvoxland/gendb/pkg/schema"
)

// Generator produces synthetic data for a schema.
type Generator struct {
	llmClient    *llm.Client
	cfg          config.GenerationConfig
	faker        *gofakeit.Faker
	targetSchema string
}

// Option configures a Generator.
type Option func(*Generator)

// WithTargetSchema sets the schema name for insert operations.
func WithTargetSchema(schema string) Option {
	return func(g *Generator) {
		g.targetSchema = schema
	}
}

// New creates a new Generator.
func New(llmClient *llm.Client, cfg config.GenerationConfig, opts ...Option) *Generator {
	g := &Generator{
		llmClient: llmClient,
		cfg:       cfg,
		faker:     gofakeit.New(uint64(cfg.Seed)),
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// Generate generates synthetic data for all tables and inserts into the target database.
func (g *Generator) Generate(ctx context.Context, sg *schema.SchemaGraph, targetConn *pgx.Conn) error {
	var plan *llm.GenerationPlan

	if g.llmClient != nil {
		// Phase A: Get generation plan from LLM
		fmt.Println("Analyzing schema with LLM...")
		var err error
		plan, err = g.llmClient.AnalyzeSchema(ctx, sg)
		if err != nil {
			return fmt.Errorf("analyzing schema: %w", err)
		}
	} else {
		// No LLM configured — build a type-based fallback plan
		fmt.Println("No LLM configured, using type-based generation...")
		plan = g.buildFallbackPlan(sg)
	}

	// Apply config overrides
	g.applyConfigOverrides(plan, sg)

	// Phase B: Generate and insert data in topological order
	// Track generated PKs for FK resolution
	pkValues := make(map[string][]map[string]any) // table -> list of PK value maps

	for _, table := range sg.Tables {
		tablePlan, ok := plan.Tables[table.Name]
		if !ok {
			fmt.Printf("Skipping table %s (no generation plan)\n", table.Name)
			continue
		}

		rows := g.rowCount(table.Name)
		fmt.Printf("Generating %d rows for %s...\n", rows, table.Name)

		generatedRows, err := g.generateTable(ctx, table, tablePlan, rows, pkValues)
		if err != nil {
			return fmt.Errorf("generating data for %s: %w", table.Name, err)
		}

		if err := g.insertRows(ctx, targetConn, table, generatedRows); err != nil {
			return fmt.Errorf("inserting data for %s: %w", table.Name, err)
		}

		// Track PK values for FK resolution
		pkValues[table.Name] = extractPKValues(table, generatedRows)
		fmt.Printf("  Inserted %d rows into %s\n", len(generatedRows), table.Name)
	}

	return nil
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

func (g *Generator) applyConfigOverrides(plan *llm.GenerationPlan, sg *schema.SchemaGraph) {
	if plan.Tables == nil {
		plan.Tables = make(map[string]llm.TablePlan)
	}

	for _, table := range sg.Tables {
		tablePlan, ok := plan.Tables[table.Name]
		if !ok {
			tablePlan = llm.TablePlan{Columns: make(map[string]llm.ColumnPlan)}
			plan.Tables[table.Name] = tablePlan
		}
		if tablePlan.Columns == nil {
			tablePlan.Columns = make(map[string]llm.ColumnPlan)
			plan.Tables[table.Name] = tablePlan
		}

		// Apply table-level config overrides
		if tc, ok := g.cfg.Tables[table.Name]; ok {
			for colName, colCfg := range tc.Columns {
				tablePlan.Columns[colName] = llm.ColumnPlan{
					Generator: colCfg.Generator,
					Template:  colCfg.Template,
					Values:    colCfg.Values,
					Format:    colCfg.Format,
				}
			}
			plan.Tables[table.Name] = tablePlan
		}

		// Apply column rules (pattern matching)
		for _, rule := range g.cfg.ColumnRules {
			for _, col := range table.Columns {
				if matchPattern(col.Name, rule.Pattern) {
					tablePlan.Columns[col.Name] = llm.ColumnPlan{
						Generator: rule.Generator,
						Format:    rule.Format,
						Template:  rule.Template,
					}
				}
			}
			plan.Tables[table.Name] = tablePlan
		}
	}
}

// buildFallbackPlan creates a generation plan using type-based heuristics when no LLM is available.
func (g *Generator) buildFallbackPlan(sg *schema.SchemaGraph) *llm.GenerationPlan {
	plan := &llm.GenerationPlan{
		Tables: make(map[string]llm.TablePlan),
	}
	for _, table := range sg.Tables {
		tp := llm.TablePlan{Columns: make(map[string]llm.ColumnPlan)}
		for _, col := range table.Columns {
			// Skip columns with defaults that look auto-generated, generated columns, or serial types
			if col.IsGenerated {
				tp.Columns[col.Name] = llm.ColumnPlan{Generator: "skip"}
				continue
			}
			if strings.Contains(col.DefaultValue, "nextval(") || strings.Contains(col.DefaultValue, "gen_random_uuid()") {
				tp.Columns[col.Name] = llm.ColumnPlan{Generator: "skip"}
				continue
			}
			if strings.Contains(strings.ToLower(col.DataType), "serial") {
				tp.Columns[col.Name] = llm.ColumnPlan{Generator: "skip"}
				continue
			}
			// FK columns are handled by the FK resolver
			if fkTarget(table, col.Name) != "" {
				tp.Columns[col.Name] = llm.ColumnPlan{Generator: "skip"}
				continue
			}
			// Default: use type-based generation (the "default" case in generateValue)
			tp.Columns[col.Name] = llm.ColumnPlan{Generator: "type_based"}
		}
		plan.Tables[table.Name] = tp
	}
	return plan
}

func (g *Generator) generateTable(ctx context.Context, table *schema.Table, plan llm.TablePlan, rowCount int, pkValues map[string][]map[string]any) ([]map[string]any, error) {
	rows := make([]map[string]any, 0, rowCount)
	uniqueTracker := newUniqueTracker(table)

	for i := 0; i < rowCount; i++ {
		row := make(map[string]any)
		for _, col := range table.Columns {
			colPlan, ok := plan.Columns[col.Name]
			if !ok || colPlan.Generator == "skip" {
				continue
			}

			// Handle FK columns
			if fkTable := fkTarget(table, col.Name); fkTable != "" {
				if refs, ok := pkValues[fkTable]; ok && len(refs) > 0 {
					refRow := refs[g.faker.IntN(len(refs))]
					refCol := fkRefCol(table, col.Name)
					row[col.Name] = refRow[refCol]
					continue
				}
			}

			val, err := g.generateValue(ctx, col, colPlan, row)
			if err != nil {
				return nil, fmt.Errorf("generating value for %s.%s: %w", table.Name, col.Name, err)
			}
			row[col.Name] = val
		}

		// Handle UNIQUE constraint enforcement with retry
		if !uniqueTracker.isEmpty() {
			for attempt := 0; attempt < 100; attempt++ {
				if uniqueTracker.isUnique(row) {
					break
				}
				// Regenerate unique columns
				for _, cols := range table.UniqueColumns() {
					for _, colName := range cols {
						colPlan, ok := plan.Columns[colName]
						if !ok || colPlan.Generator == "skip" {
							continue
						}
						col := table.ColumnByName(colName)
						if col == nil {
							continue
						}
						val, err := g.generateValue(ctx, col, colPlan, row)
						if err != nil {
							return nil, err
						}
						row[colName] = val
					}
				}
				if attempt == 99 {
					return nil, fmt.Errorf("could not generate unique row for %s after 100 attempts", table.Name)
				}
			}
			uniqueTracker.add(row)
		}

		rows = append(rows, row)
	}

	return rows, nil
}

func (g *Generator) generateValue(ctx context.Context, col *schema.Column, plan llm.ColumnPlan, currentRow map[string]any) (any, error) {
	switch plan.Generator {
	case "person.first_name":
		return g.faker.FirstName(), nil
	case "person.last_name":
		return g.faker.LastName(), nil
	case "person.full_name":
		return g.faker.Name(), nil
	case "internet.email":
		if plan.Template != "" {
			return g.expandTemplate(plan.Template, currentRow), nil
		}
		return g.faker.Email(), nil
	case "internet.url":
		return g.faker.URL(), nil
	case "internet.image_url":
		return fmt.Sprintf("https://picsum.photos/seed/%s/200/200", g.faker.LetterN(8)), nil
	case "internet.domain":
		return g.faker.DomainName(), nil
	case "phone.national":
		return g.faker.Phone(), nil
	case "phone.international":
		return g.faker.PhoneFormatted(), nil
	case "address.street":
		return g.faker.Street(), nil
	case "address.city":
		return g.faker.City(), nil
	case "address.state":
		return g.faker.State(), nil
	case "address.zip":
		return g.faker.Zip(), nil
	case "address.country":
		return g.faker.Country(), nil
	case "company.name":
		return g.faker.Company(), nil
	case "company.bs":
		return g.faker.BS(), nil
	case "company.suffix":
		return g.faker.CompanySuffix(), nil
	case "lorem.sentence":
		return g.faker.Sentence(10), nil
	case "lorem.paragraph":
		sentences := 3
		if s, ok := plan.Params["sentences"]; ok {
			if n, err := strconv.Atoi(s); err == nil {
				sentences = n
			}
		}
		return g.faker.Paragraph(1, sentences, 10, " "), nil
	case "time.recent":
		days := 365
		if d, ok := plan.Params["days"]; ok {
			if n, err := strconv.Atoi(d); err == nil {
				days = n
			}
		}
		return time.Now().Add(-time.Duration(g.faker.IntN(days*24)) * time.Hour), nil
	case "time.past":
		return g.faker.DateRange(time.Now().AddDate(-5, 0, 0), time.Now()), nil
	case "time.future":
		return g.faker.DateRange(time.Now(), time.Now().AddDate(1, 0, 0)), nil
	case "number.int":
		min, max := 0, 1000000
		if plan.Min != nil {
			min = int(*plan.Min)
		}
		if plan.Max != nil {
			max = int(*plan.Max)
		}
		return g.faker.IntN(max-min) + min, nil
	case "number.float":
		min, max := 0.0, 1000000.0
		if plan.Min != nil {
			min = *plan.Min
		}
		if plan.Max != nil {
			max = *plan.Max
		}
		return g.faker.Float64Range(min, max), nil
	case "number.price":
		return g.faker.Price(1.0, 999.99), nil
	case "uuid":
		return g.faker.UUID(), nil
	case "boolean":
		return g.faker.Bool(), nil
	case "one_of":
		if len(plan.Values) == 0 {
			return nil, fmt.Errorf("one_of generator requires values")
		}
		return plan.Values[g.faker.IntN(len(plan.Values))], nil
	case "regex":
		if plan.Format == "" {
			return nil, fmt.Errorf("regex generator requires format")
		}
		return g.faker.Regex(plan.Format), nil
	case "llm":
		// Direct LLM generation for text columns
		vars := make(map[string]string)
		for k, v := range currentRow {
			vars[k] = fmt.Sprintf("%v", v)
		}
		return g.llmClient.GenerateText(ctx, plan.Template, vars)
	default:
		// Fallback based on column type
		return g.generateByType(col)
	}
}

var maxLengthRe = regexp.MustCompile(`\((\d+)\)`)

// parseMaxLength extracts the numeric length from type strings like
// "character varying(20)" or "varchar(50)". Returns 0 if no length is found.
func parseMaxLength(dataType string) int {
	m := maxLengthRe.FindStringSubmatch(dataType)
	if len(m) < 2 {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0
	}
	return n
}

func (g *Generator) generateByType(col *schema.Column) (any, error) {
	dt := strings.ToLower(col.DataType)
	switch {
	case strings.Contains(dt, "int"):
		return g.faker.IntN(100000), nil
	case strings.Contains(dt, "serial"):
		return nil, nil // skip serial columns
	case strings.Contains(dt, "bool"):
		return g.faker.Bool(), nil
	case strings.Contains(dt, "text"), strings.Contains(dt, "varchar"), strings.Contains(dt, "char"):
		maxLen := parseMaxLength(col.DataType)
		if maxLen > 0 && maxLen <= 5 {
			return g.faker.LetterN(uint(maxLen)), nil
		}
		val := g.faker.Sentence(5)
		if maxLen > 0 && len(val) > maxLen {
			val = val[:maxLen]
		}
		return val, nil
	case strings.Contains(dt, "numeric"), strings.Contains(dt, "decimal"), strings.Contains(dt, "money"):
		return g.faker.Price(1.0, 999.99), nil
	case strings.Contains(dt, "float"), strings.Contains(dt, "double"), strings.Contains(dt, "real"):
		return g.faker.Float64Range(0, 1000), nil
	case strings.Contains(dt, "timestamp"), strings.Contains(dt, "date"):
		return g.faker.DateRange(time.Now().AddDate(-2, 0, 0), time.Now()), nil
	case strings.Contains(dt, "uuid"):
		return g.faker.UUID(), nil
	case strings.Contains(dt, "json"):
		return "{}", nil
	default:
		return g.faker.Word(), nil
	}
}

func (g *Generator) expandTemplate(tmpl string, row map[string]any) string {
	result := tmpl
	for k, v := range row {
		result = strings.ReplaceAll(result, "{"+k+"}", fmt.Sprintf("%v", v))
	}
	// Replace any remaining placeholders with random values
	for {
		start := strings.Index(result, "{")
		if start == -1 {
			break
		}
		end := strings.Index(result[start:], "}")
		if end == -1 {
			break
		}
		result = result[:start] + g.faker.Word() + result[start+end+1:]
	}
	return result
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

	tableIdent := pgx.Identifier{table.Name}
	if g.targetSchema != "" {
		tableIdent = pgx.Identifier{g.targetSchema, table.Name}
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

func matchPattern(name, pattern string) bool {
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
