package proxy

import (
	"fmt"
	"net"

	"github.com/jackc/pgx/v5/pgproto3"
)

const setupGenDBSchemaSQL = `
CREATE SCHEMA IF NOT EXISTS gendb;

CREATE OR REPLACE PROCEDURE gendb.generate_data(
    table_name text DEFAULT NULL,
    rows integer DEFAULT 100,
    seed bigint DEFAULT NULL,
    scenario text DEFAULT 'default'
) LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'This procedure is handled by the GenDB proxy. Connect through the proxy on port 5433.'; END; $$;

CREATE OR REPLACE PROCEDURE gendb.regenerate_data(
    table_name text DEFAULT NULL,
    rows integer DEFAULT 100,
    seed bigint DEFAULT NULL,
    scenario text DEFAULT 'default'
) LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'This procedure is handled by the GenDB proxy. Connect through the proxy on port 5433.'; END; $$;

CREATE OR REPLACE PROCEDURE gendb.return_generated(
    table_name text,
    scenario text DEFAULT 'default'
) LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'This procedure is handled by the GenDB proxy. Connect through the proxy on port 5433.'; END; $$;

CREATE OR REPLACE PROCEDURE gendb.return_actual(
    table_name text,
    scenario text DEFAULT 'default'
) LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'This procedure is handled by the GenDB proxy. Connect through the proxy on port 5433.'; END; $$;

COMMENT ON PROCEDURE gendb.generate_data IS 'Generate synthetic data into the shadow schema. Args: table_name (optional), rows (default 100), seed (optional).';
COMMENT ON PROCEDURE gendb.regenerate_data IS 'Alias for generate_data.';
COMMENT ON PROCEDURE gendb.return_generated IS 'Route queries for a table to generated (shadow) data.';
COMMENT ON PROCEDURE gendb.return_actual IS 'Restore a table to return real data.';

CREATE OR REPLACE PROCEDURE gendb.sync(
    table_name text DEFAULT NULL,
    scenario text DEFAULT NULL
) LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'This procedure is handled by the GenDB proxy. Connect through the proxy on port 5433.'; END; $$;

COMMENT ON PROCEDURE gendb.sync IS 'Sync shadow table schemas with their original tables.';

CREATE TABLE IF NOT EXISTS gendb.generation_status (
    id               SERIAL PRIMARY KEY,
    command          TEXT NOT NULL,
    status           TEXT NOT NULL DEFAULT 'pending',
    total_tables     INT NOT NULL DEFAULT 0,
    completed_tables INT NOT NULL DEFAULT 0,
    current_table    TEXT,
    total_rows       INT NOT NULL DEFAULT 0,
    completed_rows   INT NOT NULL DEFAULT 0,
    error_message    TEXT,
    started_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_update      TIMESTAMPTZ NOT NULL DEFAULT now()
);
`

// ensureGenDBSchema sends the setup SQL over a raw backend connection to create
// stub procedures in the gendb schema. These stubs provide intellisense/autocomplete
// in SQL clients. They never actually execute because the proxy intercepts CALL gendb.*
// before it reaches PostgreSQL.
func ensureGenDBSchema(backendConn net.Conn) error {
	// Send a simple Query message with the setup SQL
	query := &pgproto3.Query{String: setupGenDBSchemaSQL}
	buf, err := query.Encode(nil)
	if err != nil {
		return fmt.Errorf("encoding setup query: %w", err)
	}
	if _, err := backendConn.Write(buf); err != nil {
		return fmt.Errorf("sending setup query: %w", err)
	}

	// Read responses until ReadyForQuery
	frontend := pgproto3.NewFrontend(backendConn, backendConn)
	for {
		msg, err := frontend.Receive()
		if err != nil {
			return fmt.Errorf("reading setup response: %w", err)
		}
		switch m := msg.(type) {
		case *pgproto3.ReadyForQuery:
			return nil
		case *pgproto3.ErrorResponse:
			return fmt.Errorf("backend error: %s", m.Message)
		}
	}
}
