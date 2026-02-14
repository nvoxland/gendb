package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/nvoxland/autodb/pkg/config"
	"github.com/nvoxland/autodb/pkg/generator"
	"github.com/nvoxland/autodb/pkg/lang"
	"github.com/nvoxland/autodb/pkg/proxy"
	"github.com/nvoxland/autodb/pkg/schema"
	"github.com/nvoxland/autodb/pkg/shadow"
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
	key        string
)

func init() {
	serveCmd.Flags().StringVar(&dbHostname, "db-hostname", "localhost", "PostgreSQL server hostname")
	serveCmd.Flags().IntVar(&dbPort, "db-port", 5432, "PostgreSQL server port")
	serveCmd.Flags().IntVar(&servePort, "port", 5433, "Proxy listen port")
	serveCmd.Flags().StringVar(&key, "key", "autodb", "Key for shadow schema naming: {source_schema}_{key}")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	realAddr := fmt.Sprintf("%s:%d", dbHostname, dbPort)

	executor := lang.NewExecutor()

	// Wire up OnGenerate callback
	executor.OnGenerate = func(ctx context.Context, table string, rows int, seed *int64) error {
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
			// Exclude shadow schemas when inspecting all tables
			sg, err = inspector.InspectWithOptions(ctx, schema.InspectOptions{
				ExcludeSchemas: []string{shadow.DeriveSchemaName("public", key)},
			})
		}
		if err != nil {
			return fmt.Errorf("inspecting schema: %w", err)
		}

		// For each table, create the shadow schema + table and generate data
		for _, t := range sg.Tables {
			targetSchema := shadow.DeriveSchemaName(t.Schema, key)

			// Build and execute DDL for this single table
			singleTableGraph := &schema.SchemaGraph{}
			singleTableGraph.SetTables([]*schema.Table{t})
			ddl := schema.ReconstructDDLForSchema(singleTableGraph, targetSchema)
			if _, err := conn.Exec(ctx, ddl); err != nil {
				return fmt.Errorf("creating shadow table %s.%s: %w", targetSchema, t.Name, err)
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

			gen := generator.New(nil, genCfg, generator.WithTargetSchema(targetSchema))
			if err := gen.Generate(ctx, singleTableGraph, conn); err != nil {
				return fmt.Errorf("generating data for %s: %w", t.Name, err)
			}
		}

		return nil
	}

	// Wire up OnReturnGenerated callback
	executor.OnReturnGenerated = func(ctx context.Context, table string) error {
		conn := proxy.ConnFromContext(ctx)
		if conn == nil {
			return fmt.Errorf("no database connection available")
		}
		shadowSchema := shadow.DeriveSchemaName("public", key)
		_, err := conn.Exec(ctx, fmt.Sprintf(
			"CREATE OR REPLACE TEMP VIEW %s AS SELECT * FROM %s",
			pgx.Identifier{table}.Sanitize(),
			pgx.Identifier{shadowSchema, table}.Sanitize(),
		))
		return err
	}

	// Wire up OnReturnActual callback
	executor.OnReturnActual = func(ctx context.Context, table string) error {
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
		Key:        key,
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
