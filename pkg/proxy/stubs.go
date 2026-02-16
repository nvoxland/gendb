package proxy

import (
	"fmt"
	"net"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/nvoxland/gendb/pkg/lang"
)

// ensureGenDBSchema sends the setup SQL over a raw backend connection to create
// stub procedures in the gendb schema. These stubs provide intellisense/autocomplete
// in SQL clients. They never actually execute because the proxy intercepts CALL gendb.*
// before it reaches PostgreSQL.
func ensureGenDBSchema(backendConn net.Conn) error {
	// Send a simple Query message with the setup SQL
	query := &pgproto3.Query{String: lang.BuildSetupSQL()}
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
