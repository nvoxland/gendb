package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/nvoxland/gendb/pkg/config"
	"github.com/nvoxland/gendb/pkg/generator"
	"github.com/nvoxland/gendb/pkg/lang"
	"github.com/nvoxland/gendb/pkg/proxy"
	"github.com/nvoxland/gendb/pkg/schema"
	"github.com/nvoxland/gendb/pkg/shadow"
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
)

func init() {
	serveCmd.Flags().StringVar(&dbHostname, "db-hostname", "localhost", "PostgreSQL server hostname")
	serveCmd.Flags().IntVar(&dbPort, "db-port", 5432, "PostgreSQL server port")
	serveCmd.Flags().IntVar(&servePort, "port", 5433, "Proxy listen port")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	realAddr := fmt.Sprintf("%s:%d", dbHostname, dbPort)

	executor := lang.NewExecutor()

	// Wire up OnGenerate callback
	executor.OnGenerate = func(ctx context.Context, table string, rows int, seed *int64, scenario string) error {
		conn := proxy.ConnFromContext(ctx)
		if conn == nil {
			return fmt.Errorf("no database connection available")
		}
		// Do NOT close conn — the proxy manages its lifecycle.

		inspector := schema.NewInspectorFromConn(conn)

		var err error
		var sg *schema.SchemaGraph
		if table != "" {
			sg, err = inspector.InspectTable(ctx, table)
		} else {
			// Exclude the gendb schema when inspecting all tables
			sg, err = inspector.InspectWithOptions(ctx, schema.InspectOptions{
				ExcludeSchemas: []string{shadow.SchemaName},
			})
		}
		if err != nil {
			return fmt.Errorf("inspecting schema: %w", err)
		}

		// For each table, create the shadow table in the gendb schema and generate data
		for _, t := range sg.Tables {
			mapper := func(name string) string {
				return shadow.ShadowTableName(scenario, t.Schema, name)
			}

			// Build and execute DDL for this single table
			singleTableGraph := &schema.SchemaGraph{}
			singleTableGraph.SetTables([]*schema.Table{t})
			ddl := schema.ReconstructDDLForSchemaWithMapping(singleTableGraph, shadow.SchemaName, mapper)
			if _, err := conn.Exec(ctx, ddl); err != nil {
				return fmt.Errorf("creating shadow table %s.%s: %w", shadow.SchemaName, shadow.ShadowTableName(scenario, t.Schema, t.Name), err)
			}

			// Build generator config
			genCfg := config.GenerationConfig{
				DefaultRows: 100,
				Seed:        42,
			}
			if rows > 0 {
				genCfg.DefaultRows = rows
			}
			if seed != nil {
				genCfg.Seed = *seed
			}

			gen := generator.New(nil, genCfg,
				generator.WithTargetSchema(shadow.SchemaName),
				generator.WithTableNameMapper(mapper),
			)
			if err := gen.Generate(ctx, singleTableGraph, conn); err != nil {
				return fmt.Errorf("generating data for %s: %w", t.Name, err)
			}
		}

		return nil
	}

	// Wire up OnSync callback
	executor.OnSync = func(ctx context.Context, table string, scenario string) error {
		conn := proxy.ConnFromContext(ctx)
		if conn == nil {
			return fmt.Errorf("no database connection available")
		}

		inspector := schema.NewInspectorFromConn(conn)

		// Step 1: List all shadow tables
		rows, err := conn.Query(ctx, `
			SELECT table_name FROM information_schema.tables
			WHERE table_schema = $1 AND table_type = 'BASE TABLE'
		`, shadow.SchemaName)
		if err != nil {
			return fmt.Errorf("listing shadow tables: %w", err)
		}

		var shadowNames []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return fmt.Errorf("scanning shadow table name: %w", err)
			}
			shadowNames = append(shadowNames, name)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}

		for _, shadowName := range shadowNames {
			sc, sourceSchema, sourceTable, ok := shadow.ParseShadowTableName(shadowName)
			if !ok {
				continue
			}

			// Step 2: Filter by table and scenario
			if table != "" && sourceTable != table {
				continue
			}
			if scenario != "" && sc != scenario {
				continue
			}

			qualifiedShadow := pgx.Identifier{shadow.SchemaName, shadowName}.Sanitize()

			// Step 3: Check if original table still exists
			origGraph, origErr := inspector.InspectTable(ctx, sourceTable)
			if origErr != nil {
				// Original table missing — drop the shadow table
				fmt.Printf("Dropping orphaned shadow table %s (original %s.%s not found)\n", shadowName, sourceSchema, sourceTable)
				if _, err := conn.Exec(ctx, fmt.Sprintf("DROP TABLE %s", qualifiedShadow)); err != nil {
					return fmt.Errorf("dropping orphaned shadow table %s: %w", shadowName, err)
				}
				continue
			}

			origTable := origGraph.Tables[0]

			// Inspect the shadow table
			shadowGraph, err := inspector.InspectTable(ctx, shadowName)
			if err != nil {
				return fmt.Errorf("inspecting shadow table %s: %w", shadowName, err)
			}
			shadowTable := shadowGraph.Tables[0]

			// Build column sets
			origCols := make(map[string]*schema.Column)
			for _, c := range origTable.Columns {
				origCols[c.Name] = c
			}
			shadowCols := make(map[string]bool)
			for _, c := range shadowTable.Columns {
				shadowCols[c.Name] = true
			}

			// Columns in shadow but NOT in original → DROP COLUMN
			for _, c := range shadowTable.Columns {
				if _, exists := origCols[c.Name]; !exists {
					fmt.Printf("  Dropping column %s from %s\n", c.Name, shadowName)
					if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
						qualifiedShadow, pgx.Identifier{c.Name}.Sanitize())); err != nil {
						return fmt.Errorf("dropping column %s from %s: %w", c.Name, shadowName, err)
					}
				}
			}

			// Columns in original but NOT in shadow → ADD COLUMN + fill data
			for _, origCol := range origTable.Columns {
				if shadowCols[origCol.Name] {
					continue
				}

				// Skip auto-generated columns
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

				fmt.Printf("  Adding column %s to %s\n", origCol.Name, shadowName)

				// Add as nullable first so we can fill data
				if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
					qualifiedShadow, pgx.Identifier{origCol.Name}.Sanitize(), origCol.DataType)); err != nil {
					return fmt.Errorf("adding column %s to %s: %w", origCol.Name, shadowName, err)
				}

				// Fill data for existing rows
				genCfg := config.GenerationConfig{
					DefaultRows: 100,
					Seed:        42,
				}
				gen := generator.New(nil, genCfg)
				if err := gen.FillColumn(ctx, conn, qualifiedShadow, origCol, shadowTable.PrimaryKey); err != nil {
					return fmt.Errorf("filling column %s in %s: %w", origCol.Name, shadowName, err)
				}

				// If original column is NOT NULL, set NOT NULL after filling
				if !origCol.IsNullable {
					if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
						qualifiedShadow, pgx.Identifier{origCol.Name}.Sanitize())); err != nil {
						return fmt.Errorf("setting NOT NULL on %s.%s: %w", shadowName, origCol.Name, err)
					}
				}
			}
		}

		return nil
	}

	// Wire up OnReturnGenerated callback
	executor.OnReturnGenerated = func(ctx context.Context, table string, scenario string) error {
		conn := proxy.ConnFromContext(ctx)
		if conn == nil {
			return fmt.Errorf("no database connection available")
		}
		shadowTable := shadow.ShadowTableName(scenario, "public", table)
		_, err := conn.Exec(ctx, fmt.Sprintf(
			"CREATE OR REPLACE TEMP VIEW %s AS SELECT * FROM %s",
			pgx.Identifier{table}.Sanitize(),
			pgx.Identifier{shadow.SchemaName, shadowTable}.Sanitize(),
		))
		return err
	}

	// Wire up OnReturnActual callback
	executor.OnReturnActual = func(ctx context.Context, table string, scenario string) error {
		conn := proxy.ConnFromContext(ctx)
		if conn == nil {
			return fmt.Errorf("no database connection available")
		}
		_, err := conn.Exec(ctx, fmt.Sprintf(
			"DROP VIEW IF EXISTS pg_temp.%s",
			pgx.Identifier{table}.Sanitize(),
		))
		return err
	}

	p := proxy.New(proxy.Config{
		ListenAddr: fmt.Sprintf(":%d", servePort),
		RealAddr:   realAddr,
	}, executor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down proxy...")
		cancel()
	}()

	return p.Start(ctx)
}
