package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/nvoxland/gendb/pkg/config"
	"github.com/nvoxland/gendb/pkg/generator"
	"github.com/nvoxland/gendb/pkg/lang"
	"github.com/nvoxland/gendb/pkg/llm"
	"github.com/nvoxland/gendb/pkg/proxy"
	"github.com/nvoxland/gendb/pkg/schema"
	"github.com/nvoxland/gendb/pkg/synthetic"

	"github.com/jackc/pgx/v5"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the PostgreSQL wire protocol proxy",
	RunE:  runServe,
}

var (
	dbHostname string
	dbPort     int
	servePort  int
	logLevel   string
)

// Package-level variables for config and LLM client, set at startup.
var (
	appConfig    *config.Config
	appLLMClient *llm.Client
)

func init() {
	serveCmd.Flags().StringVar(&dbHostname, "db-hostname", "localhost", "PostgreSQL server hostname")
	serveCmd.Flags().IntVar(&dbPort, "db-port", 5432, "PostgreSQL server port")
	serveCmd.Flags().IntVar(&servePort, "port", 5433, "Proxy listen port")
	serveCmd.Flags().StringVar(&logLevel, "log-level", "", "Log level: debug, info, warn, error (overrides config file)")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	// Load config
	cfg, err := config.Load(configFile)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	appConfig = cfg

	// Configure logging: CLI flag overrides config file
	level := cfg.LogLevel
	if logLevel != "" {
		level = logLevel
	}
	initLogging(level)

	// Create LLM client
	appLLMClient = llm.NewClient(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKey,
		llm.WithTemperature(cfg.LLM.Temperature),
		llm.WithStructuredOutput(cfg.LLM.StructuredOutput),
		llm.WithProvider(cfg.LLM.Provider),
		llm.WithChunkSize(cfg.LLM.ChunkSize),
	)
	slog.Info("LLM configured", "model", cfg.LLM.Model, "base_url", cfg.LLM.BaseURL)

	realAddr := fmt.Sprintf("%s:%d", dbHostname, dbPort)

	lang.RegisterHandler("generate_data", handleGenerate)
	lang.RegisterHandler("sync", handleSync)
	lang.RegisterHandler("return_generated", handleReturnGenerated)
	lang.RegisterHandler("return_actual", handleReturnActual)
	lang.RegisterHandler("drop_scenario", handleDropScenario)

	p := proxy.New(proxy.Config{
		ListenAddr:       fmt.Sprintf(":%d", servePort),
		RealAddr:         realAddr,
		Version:          Version,
		LLMModel:         cfg.LLM.Model,
		LLMProvider:      cfg.LLM.Provider,
		StructuredOutput: cfg.LLM.StructuredOutput,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		slog.Info("Shutting down proxy...")
		cancel()
	}()

	go logTokenUsage(ctx, appLLMClient)

	return p.Start(ctx)
}

func initLogging(level string) {
	var slogLevel slog.Level
	switch strings.ToLower(level) {
	case "debug":
		slogLevel = slog.LevelDebug
	case "warn":
		slogLevel = slog.LevelWarn
	case "error":
		slogLevel = slog.LevelError
	default:
		slogLevel = slog.LevelInfo
	}
	handler := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slogLevel})
	slog.SetDefault(slog.New(handler))
}

func handleGenerate(ctx context.Context, args map[string]string) (result *lang.Result, retErr error) {
	startTime := time.Now()
	conn := proxy.ConnFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	include := args["include_tables"]
	exclude := args["exclude_tables"]
	rows, _ := strconv.Atoi(args["rows"]) // zero if missing or invalid
	scenario := args["scenario"]

	defer func() {
		status, errMsg := "success", ""
		if retErr != nil {
			status, errMsg = "error", retErr.Error()
		}
		details := map[string]any{
			"include_tables": include,
			"exclude_tables": exclude,
			"rows":           rows,
		}
		recordHistory(ctx, conn, "generate_data", scenario, details, status, errMsg, time.Since(startTime))
	}()
	includeSampleData := true
	if v, ok := args["include_sample_data"]; ok && (v == "false" || v == "f") {
		includeSampleData = false
	}

	slog.Info("Handling generate_data", "include_tables", include, "exclude_tables", exclude, "rows", rows, "scenario", scenario, "include_sample_data", includeSampleData)

	// 1. Schema inspection (sync, using session pgxConn)
	inspector := schema.NewInspectorFromConn(conn)

	var err error
	var sg *schema.SchemaGraph
	if include != "" && !strings.ContainsAny(include, "*?") {
		// Exact table name — use direct inspection
		sg, err = inspector.InspectTable(ctx, include)
		if err == nil && exclude != "" {
			// Apply exclude filter even for exact include
			var matched []*schema.Table
			for _, t := range sg.Tables {
				if !generator.MatchPattern(t.Name, exclude) {
					matched = append(matched, t)
				}
			}
			sg.SetTables(matched)
		}
	} else {
		sg, err = inspector.InspectWithOptions(ctx, schema.InspectOptions{
			ExcludeSchemas: []string{synthetic.SchemaName},
		})
		if err == nil {
			var matched []*schema.Table
			for _, t := range sg.Tables {
				if include != "" && !generator.MatchPattern(t.Name, include) {
					continue
				}
				if exclude != "" && generator.MatchPattern(t.Name, exclude) {
					continue
				}
				matched = append(matched, t)
			}
			sg.SetTables(matched)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("inspecting schema: %w", err)
	}

	slog.Info("Schema inspected", "tables", len(sg.Tables))

	// 2. Create synthetic tables (sync, using session pgxConn)
	for _, t := range sg.Tables {
		mapper := func(name string) string {
			return synthetic.SyntheticTableName(scenario, t.Schema, name)
		}

		singleTableGraph := &schema.SchemaGraph{}
		singleTableGraph.SetTables([]*schema.Table{t})
		ddl := schema.ReconstructDDLForSchemaWithMapping(singleTableGraph, synthetic.SchemaName, mapper)
		if _, err := conn.Exec(ctx, ddl); err != nil {
			return nil, fmt.Errorf("creating synthetic table %s.%s: %w", synthetic.SchemaName, synthetic.SyntheticTableName(scenario, t.Schema, t.Name), err)
		}
		slog.Debug("Created synthetic table", "synthetic_table", synthetic.SyntheticTableName(scenario, t.Schema, t.Name))
	}

	// 3. Generate data synchronously with NOTICE progress
	clientConn := proxy.ClientConnFromContext(ctx)
	totalTables := len(sg.Tables)
	totalRows := 0

	for i, t := range sg.Tables {
		proxy.SendNotice(clientConn, fmt.Sprintf("gendb: generating data for %s (%d/%d)", t.Name, i+1, totalTables))

		mapper := func(name string) string {
			return synthetic.SyntheticTableName(scenario, t.Schema, name)
		}

		singleTableGraph := &schema.SchemaGraph{}
		singleTableGraph.SetTables([]*schema.Table{t})

		genCfg := appConfig.Generation
		if rows > 0 {
			genCfg.DefaultRows = rows
		}

		gen, err := generator.New(appLLMClient, genCfg,
			generator.WithTargetSchema(synthetic.SchemaName),
			generator.WithTableNameMapper(mapper),
			generator.WithInspector(inspector),
			generator.WithSampleData(includeSampleData),
		)
		if err != nil {
			return nil, fmt.Errorf("creating generator: %w", err)
		}

		if err := gen.Generate(ctx, singleTableGraph, conn); err != nil {
			return nil, fmt.Errorf("generating data for %s: %w", t.Name, err)
		}

		totalRows += genCfg.DefaultRows
		slog.Info("Generated data for table", "table", t.Name, "progress", fmt.Sprintf("%d/%d", i+1, totalTables))
	}

	return &lang.Result{Tag: fmt.Sprintf("GENDB GENERATE DATA (%d tables, %d rows)", totalTables, totalRows)}, nil
}

func handleSync(ctx context.Context, args map[string]string) (result *lang.Result, retErr error) {
	startTime := time.Now()
	conn := proxy.ConnFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	table := args["table_name"]
	scenario := args["scenario"]

	defer func() {
		status, errMsg := "success", ""
		if retErr != nil {
			status, errMsg = "error", retErr.Error()
		}
		details := map[string]any{
			"table_name": table,
		}
		recordHistory(ctx, conn, "sync", scenario, details, status, errMsg, time.Since(startTime))
	}()

	inspector := schema.NewInspectorFromConn(conn)

	rows, err := conn.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
	`, synthetic.SchemaName)
	if err != nil {
		return nil, fmt.Errorf("listing synthetic tables: %w", err)
	}

	var syntheticNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning synthetic table name: %w", err)
		}
		syntheticNames = append(syntheticNames, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, syntheticName := range syntheticNames {
		sc, sourceSchema, sourceTable, ok := synthetic.ParseSyntheticTableName(syntheticName)
		if !ok {
			continue
		}

		if table != "" && sourceTable != table {
			continue
		}
		if scenario != "" && sc != scenario {
			continue
		}

		qualifiedSynthetic := pgx.Identifier{synthetic.SchemaName, syntheticName}.Sanitize()

		origGraph, origErr := inspector.InspectTable(ctx, sourceTable)
		if origErr != nil {
			slog.Info("Dropping orphaned synthetic table", "synthetic_table", syntheticName, "original_schema", sourceSchema, "original_table", sourceTable)
			if _, err := conn.Exec(ctx, fmt.Sprintf("DROP TABLE %s", qualifiedSynthetic)); err != nil {
				return nil, fmt.Errorf("dropping orphaned synthetic table %s: %w", syntheticName, err)
			}
			continue
		}

		origTable := origGraph.Tables[0]

		syntheticGraph, err := inspector.InspectTable(ctx, syntheticName)
		if err != nil {
			return nil, fmt.Errorf("inspecting synthetic table %s: %w", syntheticName, err)
		}
		syntheticTable := syntheticGraph.Tables[0]

		origCols := make(map[string]*schema.Column)
		for _, c := range origTable.Columns {
			origCols[c.Name] = c
		}
		syntheticCols := make(map[string]bool)
		for _, c := range syntheticTable.Columns {
			syntheticCols[c.Name] = true
		}

		for _, c := range syntheticTable.Columns {
			if _, exists := origCols[c.Name]; !exists {
				slog.Info("Dropping removed column from synthetic table", "column", c.Name, "synthetic_table", syntheticName)
				if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
					qualifiedSynthetic, pgx.Identifier{c.Name}.Sanitize())); err != nil {
					return nil, fmt.Errorf("dropping column %s from %s: %w", c.Name, syntheticName, err)
				}
			}
		}

		for _, origCol := range origTable.Columns {
			if syntheticCols[origCol.Name] {
				continue
			}
			if origCol.IsGenerated {
				continue
			}
			if strings.Contains(origCol.DefaultValue, "nextval(") ||
				strings.Contains(origCol.DefaultValue, "gen_random_uuid()") {
				continue
			}
			if strings.Contains(strings.ToLower(origCol.DataType), "serial") {
				continue
			}

			slog.Info("Adding new column to synthetic table", "column", origCol.Name, "synthetic_table", syntheticName)

			if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
				qualifiedSynthetic, pgx.Identifier{origCol.Name}.Sanitize(), origCol.DataType)); err != nil {
				return nil, fmt.Errorf("adding column %s to %s: %w", origCol.Name, syntheticName, err)
			}

			gen, err := generator.New(appLLMClient, appConfig.Generation)
			if err != nil {
				return nil, fmt.Errorf("creating generator for fill: %w", err)
			}
			if err := gen.FillColumn(ctx, conn, qualifiedSynthetic, origCol, syntheticTable.PrimaryKey); err != nil {
				return nil, fmt.Errorf("filling column %s in %s: %w", origCol.Name, syntheticName, err)
			}

			if !origCol.IsNullable {
				if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
					qualifiedSynthetic, pgx.Identifier{origCol.Name}.Sanitize())); err != nil {
					return nil, fmt.Errorf("setting NOT NULL on %s.%s: %w", syntheticName, origCol.Name, err)
				}
			}
		}

		// --- Index sync ---
		origIndexes := make(map[string]*schema.Index)
		for _, idx := range origTable.Indexes {
			origIndexes[indexSignature(idx)] = idx
		}
		syntheticIndexes := make(map[string]*schema.Index)
		for _, idx := range syntheticTable.Indexes {
			syntheticIndexes[indexSignature(idx)] = idx
		}

		// Drop synthetic indexes whose signature no longer exists in the original
		for sig, idx := range syntheticIndexes {
			if _, exists := origIndexes[sig]; !exists {
				slog.Info("Dropping removed index from synthetic table", "index", idx.Name, "synthetic_table", syntheticName)
				if _, err := conn.Exec(ctx, fmt.Sprintf("DROP INDEX %s.%s",
					pgx.Identifier{synthetic.SchemaName}.Sanitize(), pgx.Identifier{idx.Name}.Sanitize())); err != nil {
					return nil, fmt.Errorf("dropping index %s from %s: %w", idx.Name, syntheticName, err)
				}
			}
		}

		// Create indexes present on original but missing from synthetic
		for _, idx := range origIndexes {
			if _, exists := syntheticIndexes[indexSignature(idx)]; !exists {
				idxName := synthetic.SyntheticTableName(sc, sourceSchema, idx.Name)
				cols := make([]string, len(idx.Columns))
				for i, c := range idx.Columns {
					cols[i] = pgx.Identifier{c}.Sanitize()
				}
				unique := ""
				if idx.IsUnique {
					unique = "UNIQUE "
				}
				slog.Info("Creating new index on synthetic table", "index", idxName, "synthetic_table", syntheticName)
				if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
					unique, pgx.Identifier{idxName}.Sanitize(), qualifiedSynthetic, strings.Join(cols, ", "))); err != nil {
					return nil, fmt.Errorf("creating index %s on %s: %w", idxName, syntheticName, err)
				}
			}
		}
	}

	if table != "" {
		return &lang.Result{Tag: fmt.Sprintf("GENDB SYNC %s", table)}, nil
	}
	return &lang.Result{Tag: "GENDB SYNC"}, nil
}

// indexSignature returns a canonical string for an index based on its functional
// properties (columns and uniqueness), ignoring the name.
func indexSignature(idx *schema.Index) string {
	cols := make([]string, len(idx.Columns))
	copy(cols, idx.Columns)
	sort.Strings(cols)
	return fmt.Sprintf("unique:%t|cols:%s", idx.IsUnique, strings.Join(cols, ","))
}

func handleReturnGenerated(ctx context.Context, args map[string]string) (*lang.Result, error) {
	conn := proxy.ConnFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	table := args["table_name"]
	scenario := args["scenario"]
	syntheticTable := synthetic.SyntheticTableName(scenario, "public", table)
	_, err := conn.Exec(ctx, fmt.Sprintf(
		"CREATE OR REPLACE TEMP VIEW %s AS SELECT * FROM %s",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{synthetic.SchemaName, syntheticTable}.Sanitize(),
	))
	if err != nil {
		return nil, err
	}
	return &lang.Result{Tag: fmt.Sprintf("GENDB RETURN GENERATED %s", table)}, nil
}

func handleReturnActual(ctx context.Context, args map[string]string) (*lang.Result, error) {
	conn := proxy.ConnFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	table := args["table_name"]
	_, err := conn.Exec(ctx, fmt.Sprintf(
		"DROP VIEW IF EXISTS pg_temp.%s",
		pgx.Identifier{table}.Sanitize(),
	))
	if err != nil {
		return nil, err
	}
	return &lang.Result{Tag: fmt.Sprintf("GENDB RETURN ACTUAL %s", table)}, nil
}

func logTokenUsage(ctx context.Context, client *llm.Client) {
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prompt, completion, total, requests := client.SnapshotUsage()
			if requests > 0 {
				slog.Info("LLM token usage", "prompt_tokens", prompt, "completion_tokens", completion, "total_tokens", total, "requests", requests)
			}
		}
	}
}

func recordHistory(ctx context.Context, conn *pgx.Conn, operation, scenario string, details map[string]any, status string, errMsg string, duration time.Duration) {
	detailsJSON, _ := json.Marshal(details)
	_, err := conn.Exec(ctx,
		`INSERT INTO gendb.history (operation, scenario, details, status, error_message, duration_ms)
		 VALUES ($1, $2, $3, $4, $5, $6)`,
		operation, scenario, detailsJSON, status, errMsg, duration.Milliseconds())
	if err != nil {
		slog.Warn("Failed to record history", "error", err)
	}
}

func handleDropScenario(ctx context.Context, args map[string]string) (result *lang.Result, retErr error) {
	startTime := time.Now()
	conn := proxy.ConnFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	scenario := args["scenario"]
	schemaFilter := args["schema"]

	slog.Info("Handling drop_scenario", "scenario", scenario, "schema", schemaFilter)

	var toDrop []string
	defer func() {
		status, errMsg := "success", ""
		if retErr != nil {
			status, errMsg = "error", retErr.Error()
		}
		details := map[string]any{
			"schema":         schemaFilter,
			"tables_dropped": len(toDrop),
		}
		recordHistory(ctx, conn, "drop_scenario", scenario, details, status, errMsg, time.Since(startTime))
	}()

	// List all tables in the gendb schema
	rows, err := conn.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
	`, synthetic.SchemaName)
	if err != nil {
		return nil, fmt.Errorf("listing synthetic tables: %w", err)
	}

	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning synthetic table name: %w", err)
		}

		sc, sourceSchema, _, ok := synthetic.ParseSyntheticTableName(name)
		if !ok {
			continue
		}
		if sc != scenario {
			continue
		}
		if schemaFilter != "" && sourceSchema != schemaFilter {
			continue
		}
		toDrop = append(toDrop, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, name := range toDrop {
		qualified := pgx.Identifier{synthetic.SchemaName, name}.Sanitize()
		slog.Info("Dropping synthetic table", "table", qualified)
		if _, err := conn.Exec(ctx, fmt.Sprintf("DROP TABLE %s", qualified)); err != nil {
			return nil, fmt.Errorf("dropping table %s: %w", name, err)
		}
	}

	slog.Info("drop_scenario complete", "scenario", scenario, "tables_dropped", len(toDrop))
	return &lang.Result{Tag: fmt.Sprintf("GENDB DROP SCENARIO %s (%d tables)", scenario, len(toDrop))}, nil
}
