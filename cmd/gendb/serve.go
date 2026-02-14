package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
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
