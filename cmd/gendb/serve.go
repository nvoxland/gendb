package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"sort"

	"github.com/nvoxland/gendb/pkg/config"
	"github.com/nvoxland/gendb/pkg/generator"
	"github.com/nvoxland/gendb/pkg/lang"
	"github.com/nvoxland/gendb/pkg/llm"
	"github.com/nvoxland/gendb/pkg/proxy"
	"github.com/nvoxland/gendb/pkg/schema"
	"github.com/nvoxland/gendb/pkg/shadow"

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
	appLLMClient = llm.NewClient(cfg.LLM.BaseURL, cfg.LLM.Model, cfg.LLM.APIKey)
	slog.Info("LLM configured", "model", cfg.LLM.Model, "base_url", cfg.LLM.BaseURL)

	realAddr := fmt.Sprintf("%s:%d", dbHostname, dbPort)

	lang.RegisterHandler("generate_data", handleGenerate)
	lang.RegisterHandler("sync", handleSync)
	lang.RegisterHandler("return_generated", handleReturnGenerated)
	lang.RegisterHandler("return_actual", handleReturnActual)
	lang.RegisterHandler("drop_scenario", handleDropScenario)

	p := proxy.New(proxy.Config{
		ListenAddr: fmt.Sprintf(":%d", servePort),
		RealAddr:   realAddr,
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

// connectToDB creates an independent *pgx.Conn by dialing the real database directly.
func connectToDB(realAddr, user, database string) (*pgx.Conn, error) {
	slog.Debug("Creating independent DB connection", "addr", realAddr, "user", user, "database", database)
	host, port, _ := strings.Cut(realAddr, ":")
	connStr := fmt.Sprintf("host=%s port=%s user=%s database=%s sslmode=disable",
		host, port, user, database)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		return nil, fmt.Errorf("connecting to %s: %w", realAddr, err)
	}
	slog.Debug("Independent DB connection established", "addr", realAddr)
	return conn, nil
}

func handleGenerate(ctx context.Context, args map[string]string) (*lang.Result, error) {
	conn := proxy.ConnFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	pattern := args["table_pattern"]
	rows, _ := strconv.Atoi(args["rows"]) // zero if missing or invalid
	scenario := args["scenario"]

	slog.Info("Handling generate_data", "pattern", pattern, "rows", rows, "scenario", scenario)

	// 1. Schema inspection (sync, using session pgxConn)
	inspector := schema.NewInspectorFromConn(conn)

	var err error
	var sg *schema.SchemaGraph
	if pattern != "" && !strings.ContainsAny(pattern, "*?") {
		// Exact table name — use direct inspection
		sg, err = inspector.InspectTable(ctx, pattern)
	} else {
		sg, err = inspector.InspectWithOptions(ctx, schema.InspectOptions{
			ExcludeSchemas: []string{shadow.SchemaName},
		})
		if err == nil && pattern != "" {
			// Filter tables to those matching the glob pattern
			var matched []*schema.Table
			for _, t := range sg.Tables {
				if generator.MatchPattern(t.Name, pattern) {
					matched = append(matched, t)
				}
			}
			sg.SetTables(matched)
		}
	}
	if err != nil {
		return nil, fmt.Errorf("inspecting schema: %w", err)
	}

	slog.Info("Schema inspected", "tables", len(sg.Tables))

	// 2. Create shadow tables (sync, using session pgxConn)
	for _, t := range sg.Tables {
		mapper := func(name string) string {
			return shadow.ShadowTableName(scenario, t.Schema, name)
		}

		singleTableGraph := &schema.SchemaGraph{}
		singleTableGraph.SetTables([]*schema.Table{t})
		ddl := schema.ReconstructDDLForSchemaWithMapping(singleTableGraph, shadow.SchemaName, mapper)
		if _, err := conn.Exec(ctx, ddl); err != nil {
			return nil, fmt.Errorf("creating shadow table %s.%s: %w", shadow.SchemaName, shadow.ShadowTableName(scenario, t.Schema, t.Name), err)
		}
		slog.Debug("Created shadow table", "shadow_table", shadow.ShadowTableName(scenario, t.Schema, t.Name))
	}

	// 3. Create an independent DB connection for background work
	realAddr := proxy.RealAddrFromContext(ctx)
	connParams := proxy.ConnParamsFromContext(ctx)
	if realAddr == "" || connParams == nil {
		return nil, fmt.Errorf("missing connection parameters for background generation")
	}

	bgConn, err := connectToDB(realAddr, connParams["user"], connParams["database"])
	if err != nil {
		return nil, fmt.Errorf("creating background connection: %w", err)
	}

	// 4. Insert status row
	command := "generate_data"
	if pattern != "" {
		command = fmt.Sprintf("generate_data(%s)", pattern)
	}

	var statusID int
	err = conn.QueryRow(ctx,
		`INSERT INTO gendb.generation_status (command, status, total_tables)
		 VALUES ($1, 'pending', $2)
		 RETURNING id`,
		command, len(sg.Tables),
	).Scan(&statusID)
	if err != nil {
		bgConn.Close(context.Background())
		return nil, fmt.Errorf("inserting generation status: %w", err)
	}

	// 5. Launch background goroutine
	slog.Info("Launching background data generation", "status_id", statusID, "tables", len(sg.Tables))
	go runBackgroundGeneration(bgConn, sg, scenario, rows, statusID)

	return &lang.Result{Tag: fmt.Sprintf("GENDB GENERATE DATA STARTED (status id: %d)", statusID)}, nil
}

func runBackgroundGeneration(bgConn *pgx.Conn, sg *schema.SchemaGraph, scenario string, rows, statusID int) {
	defer bgConn.Close(context.Background())
	ctx := context.Background()

	slog.Info("Background generation started", "status_id", statusID, "tables", len(sg.Tables))

	// Update status to in_progress
	updateGenerationStatus(ctx, bgConn, statusID, "in_progress", "", "", 0, 0, 0, 0)

	completedTables := 0
	totalTables := len(sg.Tables)

	for _, t := range sg.Tables {
		slog.Info("Background generation: processing table", "status_id", statusID, "table", t.Name,
			"progress", fmt.Sprintf("%d/%d", completedTables+1, totalTables))

		mapper := func(name string) string {
			return shadow.ShadowTableName(scenario, t.Schema, name)
		}

		singleTableGraph := &schema.SchemaGraph{}
		singleTableGraph.SetTables([]*schema.Table{t})

		genCfg := appConfig.Generation
		if rows > 0 {
			genCfg.DefaultRows = rows
		}

		progressFn := func(tableName string, compTables, totTables, compRows, totRows int) {
			updateGenerationStatus(ctx, bgConn, statusID, "in_progress", "", tableName,
				totTables, compTables, totRows, compRows)
		}

		gen, err := generator.New(appLLMClient, genCfg,
			generator.WithTargetSchema(shadow.SchemaName),
			generator.WithTableNameMapper(mapper),
			generator.WithProgressFunc(progressFn),
		)
		if err != nil {
			slog.Error("Background generation: failed to create generator", "status_id", statusID, "table", t.Name, "error", err)
			updateGenerationStatus(ctx, bgConn, statusID, "error", fmt.Sprintf("creating generator: %v", err), "", 0, 0, 0, 0)
			return
		}

		if err := gen.Generate(ctx, singleTableGraph, bgConn); err != nil {
			slog.Error("Background generation: failed", "status_id", statusID, "table", t.Name, "error", err)
			updateGenerationStatus(ctx, bgConn, statusID, "error", fmt.Sprintf("generating data for %s: %v", t.Name, err), t.Name,
				totalTables, completedTables, 0, 0)
			return
		}

		completedTables++
		slog.Info("Background generation: table complete", "status_id", statusID, "table", t.Name,
			"completed", completedTables, "total", totalTables)
		updateGenerationStatus(ctx, bgConn, statusID, "in_progress", "", t.Name,
			totalTables, completedTables, 0, 0)
	}

	slog.Info("Background generation completed", "status_id", statusID, "tables", completedTables)
	updateGenerationStatus(ctx, bgConn, statusID, "completed", "", "",
		totalTables, completedTables, 0, 0)
}

func updateGenerationStatus(ctx context.Context, conn *pgx.Conn, statusID int, status, errMsg, currentTable string, totalTables, completedTables, totalRows, completedRows int) {
	_, err := conn.Exec(ctx,
		`UPDATE gendb.generation_status SET
			status = $1,
			error_message = $2,
			current_table = $3,
			total_tables = CASE WHEN $4 > 0 THEN $4 ELSE total_tables END,
			completed_tables = CASE WHEN $5 > 0 THEN $5 ELSE completed_tables END,
			total_rows = CASE WHEN $6 > 0 THEN $6 ELSE total_rows END,
			completed_rows = CASE WHEN $7 > 0 THEN $7 ELSE completed_rows END,
			last_update = now()
		 WHERE id = $8`,
		status, errMsg, currentTable, totalTables, completedTables, totalRows, completedRows, statusID,
	)
	if err != nil {
		slog.Warn("Failed to update generation status", "status_id", statusID, "error", err)
	}
}

func handleSync(ctx context.Context, args map[string]string) (*lang.Result, error) {
	conn := proxy.ConnFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	table := args["table_name"]
	scenario := args["scenario"]

	inspector := schema.NewInspectorFromConn(conn)

	rows, err := conn.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
	`, shadow.SchemaName)
	if err != nil {
		return nil, fmt.Errorf("listing shadow tables: %w", err)
	}

	var shadowNames []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning shadow table name: %w", err)
		}
		shadowNames = append(shadowNames, name)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for _, shadowName := range shadowNames {
		sc, sourceSchema, sourceTable, ok := shadow.ParseShadowTableName(shadowName)
		if !ok {
			continue
		}

		if table != "" && sourceTable != table {
			continue
		}
		if scenario != "" && sc != scenario {
			continue
		}

		qualifiedShadow := pgx.Identifier{shadow.SchemaName, shadowName}.Sanitize()

		origGraph, origErr := inspector.InspectTable(ctx, sourceTable)
		if origErr != nil {
			slog.Info("Dropping orphaned shadow table", "shadow_table", shadowName, "original_schema", sourceSchema, "original_table", sourceTable)
			if _, err := conn.Exec(ctx, fmt.Sprintf("DROP TABLE %s", qualifiedShadow)); err != nil {
				return nil, fmt.Errorf("dropping orphaned shadow table %s: %w", shadowName, err)
			}
			continue
		}

		origTable := origGraph.Tables[0]

		shadowGraph, err := inspector.InspectTable(ctx, shadowName)
		if err != nil {
			return nil, fmt.Errorf("inspecting shadow table %s: %w", shadowName, err)
		}
		shadowTable := shadowGraph.Tables[0]

		origCols := make(map[string]*schema.Column)
		for _, c := range origTable.Columns {
			origCols[c.Name] = c
		}
		shadowCols := make(map[string]bool)
		for _, c := range shadowTable.Columns {
			shadowCols[c.Name] = true
		}

		for _, c := range shadowTable.Columns {
			if _, exists := origCols[c.Name]; !exists {
				slog.Info("Dropping removed column from shadow table", "column", c.Name, "shadow_table", shadowName)
				if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
					qualifiedShadow, pgx.Identifier{c.Name}.Sanitize())); err != nil {
					return nil, fmt.Errorf("dropping column %s from %s: %w", c.Name, shadowName, err)
				}
			}
		}

		for _, origCol := range origTable.Columns {
			if shadowCols[origCol.Name] {
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

			slog.Info("Adding new column to shadow table", "column", origCol.Name, "shadow_table", shadowName)

			if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
				qualifiedShadow, pgx.Identifier{origCol.Name}.Sanitize(), origCol.DataType)); err != nil {
				return nil, fmt.Errorf("adding column %s to %s: %w", origCol.Name, shadowName, err)
			}

			gen, err := generator.New(appLLMClient, appConfig.Generation)
			if err != nil {
				return nil, fmt.Errorf("creating generator for fill: %w", err)
			}
			if err := gen.FillColumn(ctx, conn, qualifiedShadow, origCol, shadowTable.PrimaryKey); err != nil {
				return nil, fmt.Errorf("filling column %s in %s: %w", origCol.Name, shadowName, err)
			}

			if !origCol.IsNullable {
				if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
					qualifiedShadow, pgx.Identifier{origCol.Name}.Sanitize())); err != nil {
					return nil, fmt.Errorf("setting NOT NULL on %s.%s: %w", shadowName, origCol.Name, err)
				}
			}
		}

		// --- Index sync ---
		origIndexes := make(map[string]*schema.Index)
		for _, idx := range origTable.Indexes {
			origIndexes[indexSignature(idx)] = idx
		}
		shadowIndexes := make(map[string]*schema.Index)
		for _, idx := range shadowTable.Indexes {
			shadowIndexes[indexSignature(idx)] = idx
		}

		// Drop shadow indexes whose signature no longer exists in the original
		for sig, idx := range shadowIndexes {
			if _, exists := origIndexes[sig]; !exists {
				slog.Info("Dropping removed index from shadow table", "index", idx.Name, "shadow_table", shadowName)
				if _, err := conn.Exec(ctx, fmt.Sprintf("DROP INDEX %s.%s",
					pgx.Identifier{shadow.SchemaName}.Sanitize(), pgx.Identifier{idx.Name}.Sanitize())); err != nil {
					return nil, fmt.Errorf("dropping index %s from %s: %w", idx.Name, shadowName, err)
				}
			}
		}

		// Create indexes present on original but missing from shadow
		for _, idx := range origIndexes {
			if _, exists := shadowIndexes[indexSignature(idx)]; !exists {
				idxName := shadow.ShadowTableName(sc, sourceSchema, idx.Name)
				cols := make([]string, len(idx.Columns))
				for i, c := range idx.Columns {
					cols[i] = pgx.Identifier{c}.Sanitize()
				}
				unique := ""
				if idx.IsUnique {
					unique = "UNIQUE "
				}
				slog.Info("Creating new index on shadow table", "index", idxName, "shadow_table", shadowName)
				if _, err := conn.Exec(ctx, fmt.Sprintf("CREATE %sINDEX %s ON %s (%s)",
					unique, pgx.Identifier{idxName}.Sanitize(), qualifiedShadow, strings.Join(cols, ", "))); err != nil {
					return nil, fmt.Errorf("creating index %s on %s: %w", idxName, shadowName, err)
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
	shadowTable := shadow.ShadowTableName(scenario, "public", table)
	_, err := conn.Exec(ctx, fmt.Sprintf(
		"CREATE OR REPLACE TEMP VIEW %s AS SELECT * FROM %s",
		pgx.Identifier{table}.Sanitize(),
		pgx.Identifier{shadow.SchemaName, shadowTable}.Sanitize(),
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

func handleDropScenario(ctx context.Context, args map[string]string) (*lang.Result, error) {
	conn := proxy.ConnFromContext(ctx)
	if conn == nil {
		return nil, fmt.Errorf("no database connection available")
	}

	scenario := args["scenario"]
	schemaFilter := args["schema"]

	slog.Info("Handling drop_scenario", "scenario", scenario, "schema", schemaFilter)

	// List all tables in the gendb schema
	rows, err := conn.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
	`, shadow.SchemaName)
	if err != nil {
		return nil, fmt.Errorf("listing shadow tables: %w", err)
	}

	var toDrop []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			rows.Close()
			return nil, fmt.Errorf("scanning shadow table name: %w", err)
		}

		sc, sourceSchema, _, ok := shadow.ParseShadowTableName(name)
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
		qualified := pgx.Identifier{shadow.SchemaName, name}.Sanitize()
		slog.Info("Dropping shadow table", "table", qualified)
		if _, err := conn.Exec(ctx, fmt.Sprintf("DROP TABLE %s", qualified)); err != nil {
			return nil, fmt.Errorf("dropping table %s: %w", name, err)
		}
	}

	slog.Info("drop_scenario complete", "scenario", scenario, "tables_dropped", len(toDrop))
	return &lang.Result{Tag: fmt.Sprintf("GENDB DROP SCENARIO %s (%d tables)", scenario, len(toDrop))}, nil
}
