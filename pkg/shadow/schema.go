package shadow

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// SchemaManager manages the shadow schema within the real database.
type SchemaManager struct {
	realURL    string
	schemaName string
}

// NewSchemaManager creates a new schema-based shadow manager.
func NewSchemaManager(realURL string, schemaName string) *SchemaManager {
	if schemaName == "" {
		schemaName = "autodb_shadow"
	}
	return &SchemaManager{
		realURL:    realURL,
		schemaName: schemaName,
	}
}

// Start creates the shadow schema if it doesn't exist.
func (m *SchemaManager) Start(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, m.realURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, fmt.Sprintf("CREATE SCHEMA IF NOT EXISTS %s", pgx.Identifier{m.schemaName}.Sanitize()))
	if err != nil {
		return fmt.Errorf("creating shadow schema: %w", err)
	}
	return nil
}

// Destroy drops the shadow schema and all its contents.
func (m *SchemaManager) Destroy(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, m.realURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", pgx.Identifier{m.schemaName}.Sanitize()))
	if err != nil {
		return fmt.Errorf("dropping shadow schema: %w", err)
	}
	return nil
}

// IsRunning checks if the shadow schema exists.
func (m *SchemaManager) IsRunning(ctx context.Context) (bool, error) {
	conn, err := pgx.Connect(ctx, m.realURL)
	if err != nil {
		return false, fmt.Errorf("connecting to database: %w", err)
	}
	defer conn.Close(ctx)

	var exists bool
	err = conn.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM information_schema.schemata WHERE schema_name = $1)",
		m.schemaName,
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("checking schema existence: %w", err)
	}
	return exists, nil
}

// Connect returns a connection to the real database.
func (m *SchemaManager) Connect(ctx context.Context) (*pgx.Conn, error) {
	return pgx.Connect(ctx, m.realURL)
}

// ApplySchema executes DDL against the real database (DDL should create objects in the shadow schema).
func (m *SchemaManager) ApplySchema(ctx context.Context, ddl string) error {
	conn, err := pgx.Connect(ctx, m.realURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, ddl)
	if err != nil {
		return fmt.Errorf("applying schema: %w", err)
	}
	return nil
}

// TruncateAll truncates all tables in the shadow schema.
func (m *SchemaManager) TruncateAll(ctx context.Context) error {
	conn, err := pgx.Connect(ctx, m.realURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer conn.Close(ctx)

	rows, err := conn.Query(ctx, `
		SELECT table_name FROM information_schema.tables
		WHERE table_schema = $1 AND table_type = 'BASE TABLE'
	`, m.schemaName)
	if err != nil {
		return fmt.Errorf("listing tables: %w", err)
	}
	defer rows.Close()

	var tables []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		tables = append(tables, pgx.Identifier{m.schemaName, name}.Sanitize())
	}

	if len(tables) > 0 {
		_, err = conn.Exec(ctx, "TRUNCATE "+strings.Join(tables, ", ")+" CASCADE")
		if err != nil {
			return fmt.Errorf("truncating tables: %w", err)
		}
	}

	return nil
}

// DropTable drops a single table in the shadow schema.
func (m *SchemaManager) DropTable(ctx context.Context, name string) error {
	conn, err := pgx.Connect(ctx, m.realURL)
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer conn.Close(ctx)

	_, err = conn.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", pgx.Identifier{m.schemaName, name}.Sanitize()))
	if err != nil {
		return fmt.Errorf("dropping table %s: %w", name, err)
	}
	return nil
}

// SchemaName returns the shadow schema name.
func (m *SchemaManager) SchemaName() string {
	return m.schemaName
}
