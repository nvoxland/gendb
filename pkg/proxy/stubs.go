package proxy

import (
	"fmt"
	"log/slog"
	"net"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/nvoxland/gendb/pkg/lang"
)

// ensureGenDBSchema sends the setup SQL over a raw backend connection to create
// stub procedures in the gendb schema. These stubs provide intellisense/autocomplete
// in SQL clients. They never actually execute because the proxy intercepts CALL gendb.*
// before it reaches PostgreSQL.
//
// infoSQL creates a session-scoped temp table that the gendb.info view selects from,
// so it must run before the main setup SQL.
func ensureGenDBSchema(backendConn net.Conn, infoSQL string) error {
	frontend := pgproto3.NewFrontend(backendConn, backendConn)

	// 1. Create the per-session _gendb_info temp table first (the view depends on it).
	if err := sendAndDrain(backendConn, frontend, infoSQL); err != nil {
		return fmt.Errorf("info table setup: %w", err)
	}

	// 2. Check if stubs are already up to date by comparing the schema comment fingerprint.
	wantTag := "gendb-stubs:" + lang.SetupSQLFingerprint()
	existing, found, err := sendAndReadScalar(backendConn, frontend,
		"SELECT obj_description(n.oid) FROM pg_namespace n WHERE n.nspname = 'gendb'")
	if err != nil {
		return fmt.Errorf("fingerprint check: %w", err)
	}
	if found && existing == wantTag {
		slog.Debug("gendb stubs up to date, skipping")
		return nil
	}

	// 3. Create schema, stub procedures, and the gendb.info view.
	if err := sendAndDrain(backendConn, frontend, lang.BuildSetupSQL()); err != nil {
		return fmt.Errorf("schema setup: %w", err)
	}

	slog.Debug("Created gendb schema stubs")
	return nil
}

// sendAndReadScalar sends a simple Query and returns the first column of the
// first DataRow as a string. Returns ("", false, nil) if no rows are returned.
func sendAndReadScalar(backendConn net.Conn, frontend *pgproto3.Frontend, sql string) (string, bool, error) {
	query := &pgproto3.Query{String: sql}
	buf, err := query.Encode(nil)
	if err != nil {
		return "", false, fmt.Errorf("encoding query: %w", err)
	}
	if _, err := backendConn.Write(buf); err != nil {
		return "", false, fmt.Errorf("sending query: %w", err)
	}

	var value string
	var found bool
	for {
		msg, err := frontend.Receive()
		if err != nil {
			return "", false, fmt.Errorf("reading response: %w", err)
		}
		switch m := msg.(type) {
		case *pgproto3.DataRow:
			if !found && len(m.Values) > 0 && m.Values[0] != nil {
				value = string(m.Values[0])
				found = true
			}
		case *pgproto3.ReadyForQuery:
			return value, found, nil
		case *pgproto3.ErrorResponse:
			return "", false, fmt.Errorf("backend error: %s", m.Message)
		}
	}
}

// sendAndDrain sends a simple Query and reads responses until ReadyForQuery.
func sendAndDrain(backendConn net.Conn, frontend *pgproto3.Frontend, sql string) error {
	query := &pgproto3.Query{String: sql}
	buf, err := query.Encode(nil)
	if err != nil {
		return fmt.Errorf("encoding query: %w", err)
	}
	if _, err := backendConn.Write(buf); err != nil {
		return fmt.Errorf("sending query: %w", err)
	}

	for {
		msg, err := frontend.Receive()
		if err != nil {
			return fmt.Errorf("reading response: %w", err)
		}
		switch m := msg.(type) {
		case *pgproto3.ReadyForQuery:
			return nil
		case *pgproto3.ErrorResponse:
			return fmt.Errorf("backend error: %s", m.Message)
		}
	}
}
