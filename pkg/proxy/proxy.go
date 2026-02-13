package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/nvoxland/autodb/pkg/lang"
)

// Proxy is a PostgreSQL wire protocol proxy.
type Proxy struct {
	listenAddr string
	realAddr   string
	schemaName string
	state      *lang.State
	executor   *lang.Executor
	listener   net.Listener
	ready      chan struct{}
	mu         sync.Mutex
	sessions   map[net.Conn]*session
}

type session struct {
	mode        string   // per-session mode override ("" = use global)
	backendConn net.Conn // backend connection for search_path injection
	currentPath string   // current search_path setting
}

// Config holds proxy configuration.
type Config struct {
	ListenAddr string // e.g. ":5433"
	RealAddr   string // e.g. "localhost:5432"
	SchemaName string // e.g. "autodb_shadow"
}

// New creates a new proxy.
func New(cfg Config, state *lang.State, executor *lang.Executor) *Proxy {
	return &Proxy{
		listenAddr: cfg.ListenAddr,
		realAddr:   cfg.RealAddr,
		schemaName: cfg.SchemaName,
		state:      state,
		executor:   executor,
		ready:      make(chan struct{}),
		sessions:   make(map[net.Conn]*session),
	}
}

// Start starts the proxy server.
func (p *Proxy) Start(ctx context.Context) error {
	var err error
	p.listener, err = net.Listen("tcp", p.listenAddr)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", p.listenAddr, err)
	}
	close(p.ready)

	fmt.Printf("AutoDB proxy listening on %s\n", p.listenAddr)
	fmt.Printf("  Real DB: %s\n", p.realAddr)
	fmt.Printf("  Shadow schema: %s\n", p.schemaName)

	go func() {
		<-ctx.Done()
		p.listener.Close()
	}()

	for {
		clientConn, err := p.listener.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("accepting connection: %w", err)
			}
		}

		go p.handleConnection(ctx, clientConn)
	}
}

// Addr returns the listener's network address, useful when listening on ":0".
func (p *Proxy) Addr() net.Addr {
	return p.listener.Addr()
}

// Ready returns a channel that is closed when the proxy is listening.
func (p *Proxy) Ready() <-chan struct{} {
	return p.ready
}

func (p *Proxy) handleConnection(ctx context.Context, clientConn net.Conn) {
	defer clientConn.Close()

	sess := &session{}
	p.mu.Lock()
	p.sessions[clientConn] = sess
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		delete(p.sessions, clientConn)
		p.mu.Unlock()
	}()

	// Use Backend to receive startup messages from the client
	clientBackend := pgproto3.NewBackend(clientConn, clientConn)
	startupMsg, err := clientBackend.ReceiveStartupMessage()
	if err != nil {
		fmt.Printf("Error reading startup message: %v\n", err)
		return
	}

	// Handle SSL request
	if _, ok := startupMsg.(*pgproto3.SSLRequest); ok {
		// Deny SSL
		clientConn.Write([]byte("N"))
		// Re-read the actual startup message
		startupMsg, err = clientBackend.ReceiveStartupMessage()
		if err != nil {
			fmt.Printf("Error reading startup message after SSL: %v\n", err)
			return
		}
	}

	// Always connect to the real database
	backendConn, err := net.Dial("tcp", p.realAddr)
	if err != nil {
		fmt.Printf("Error connecting to backend %s: %v\n", p.realAddr, err)
		p.sendError(clientConn, fmt.Sprintf("could not connect to backend: %v", err))
		return
	}
	defer backendConn.Close()

	sess.backendConn = backendConn

	// Forward the startup message to the backend
	switch msg := startupMsg.(type) {
	case *pgproto3.StartupMessage:
		startupBytes, err := msg.Encode(nil)
		if err != nil {
			fmt.Printf("Error encoding startup message: %v\n", err)
			return
		}
		if _, err := backendConn.Write(startupBytes); err != nil {
			fmt.Printf("Error forwarding startup: %v\n", err)
			return
		}
	default:
		fmt.Printf("Unexpected startup message type: %T\n", msg)
		return
	}

	// Relay auth and initial parameter messages until ReadyForQuery
	if err := p.relayUntilReady(clientBackend, clientConn, backendConn); err != nil {
		fmt.Printf("Error during auth relay: %v\n", err)
		return
	}

	// Inject initial search_path based on current mode
	if err := p.injectSearchPath(sess); err != nil {
		fmt.Printf("Error injecting initial search_path: %v\n", err)
		return
	}

	// Main loop: intercept AUTODB commands, relay everything else
	p.mainLoop(ctx, clientConn, backendConn, sess)
}

// searchPathForMode returns the SET search_path SQL for the given mode.
func (p *Proxy) searchPathForMode(mode string) string {
	if mode == "synthetic" {
		return fmt.Sprintf("SET search_path TO %s, public", p.schemaName)
	}
	return "SET search_path TO public"
}

// injectSearchPath sends a SET search_path command to the backend.
// The response (CommandComplete + ReadyForQuery) is consumed and NOT forwarded to the client.
func (p *Proxy) injectSearchPath(sess *session) error {
	mode := sess.mode
	if mode == "" {
		mode = p.state.GlobalMode
	}

	newPath := p.searchPathForMode(mode)
	if newPath == sess.currentPath {
		return nil
	}

	// Build PG wire protocol Query message: 'Q' + int32(len) + query\0
	query := newPath
	queryBytes := append([]byte(query), 0)
	msgLen := uint32(4 + len(queryBytes))

	buf := make([]byte, 1+4+len(queryBytes))
	buf[0] = 'Q'
	binary.BigEndian.PutUint32(buf[1:5], msgLen)
	copy(buf[5:], queryBytes)

	if _, err := sess.backendConn.Write(buf); err != nil {
		return fmt.Errorf("sending search_path: %w", err)
	}

	// Consume the response (CommandComplete + ReadyForQuery)
	if err := p.consumeUntilReady(sess.backendConn); err != nil {
		return fmt.Errorf("consuming search_path response: %w", err)
	}

	sess.currentPath = newPath
	return nil
}

// consumeUntilReady reads and discards backend messages until ReadyForQuery.
func (p *Proxy) consumeUntilReady(backendConn net.Conn) error {
	frontend := pgproto3.NewFrontend(backendConn, backendConn)
	for {
		msg, err := frontend.Receive()
		if err != nil {
			return err
		}
		if _, ok := msg.(*pgproto3.ReadyForQuery); ok {
			return nil
		}
	}
}

func (p *Proxy) mainLoop(ctx context.Context, clientConn, backendConn net.Conn, sess *session) {
	// Client -> Backend relay (with AUTODB interception)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}

			data := buf[:n]

			// Check if this is a Query message ('Q') with AUTODB prefix
			if len(data) > 5 && data[0] == 'Q' {
				query := extractQuery(data)
				if lang.IsAutoDBCommand(query) {
					p.handleAutoDBCommand(ctx, clientConn, sess, query)
					continue
				}
			}

			// Check if this is a Parse message ('P') with AUTODB prefix
			if len(data) > 5 && data[0] == 'P' {
				query := extractParseQuery(data)
				if lang.IsAutoDBCommand(query) {
					p.handleAutoDBCommand(ctx, clientConn, sess, query)
					continue
				}
			}

			// Relay to backend
			if _, err := backendConn.Write(data); err != nil {
				return
			}
		}
	}()

	// Backend -> Client relay (passthrough)
	io.Copy(clientConn, backendConn)
}

func (p *Proxy) handleAutoDBCommand(ctx context.Context, clientConn net.Conn, sess *session, query string) {
	cmd, err := lang.Parse(query)
	if err != nil {
		p.sendError(clientConn, err.Error())
		return
	}

	result, err := p.executor.Execute(ctx, cmd)
	if err != nil {
		p.sendError(clientConn, err.Error())
		return
	}

	// If this was a mode change, re-inject the search_path
	if cmd.Mode != nil {
		if err := p.injectSearchPath(sess); err != nil {
			p.sendError(clientConn, fmt.Sprintf("failed to update search_path: %v", err))
			return
		}
	}

	p.sendResult(clientConn, result)
}

func (p *Proxy) sendResult(conn net.Conn, result *lang.Result) {
	var buf []byte
	var err error

	if len(result.Columns) > 0 {
		// Send RowDescription
		fields := make([]pgproto3.FieldDescription, len(result.Columns))
		for i, col := range result.Columns {
			fields[i] = pgproto3.FieldDescription{
				Name:         []byte(col),
				DataTypeOID:  25, // text
				DataTypeSize: -1,
				TypeModifier: -1,
			}
		}
		rd := &pgproto3.RowDescription{Fields: fields}
		buf, err = rd.Encode(buf)
		if err != nil {
			fmt.Printf("Error encoding RowDescription: %v\n", err)
			return
		}

		// Send DataRows
		for _, row := range result.Rows {
			values := make([][]byte, len(row))
			for i, v := range row {
				values[i] = []byte(v)
			}
			dr := &pgproto3.DataRow{Values: values}
			buf, err = dr.Encode(buf)
			if err != nil {
				fmt.Printf("Error encoding DataRow: %v\n", err)
				return
			}
		}
	}

	// Send CommandComplete
	cc := &pgproto3.CommandComplete{CommandTag: []byte(result.Tag)}
	buf, err = cc.Encode(buf)
	if err != nil {
		fmt.Printf("Error encoding CommandComplete: %v\n", err)
		return
	}

	// Send ReadyForQuery
	rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
	buf, err = rfq.Encode(buf)
	if err != nil {
		fmt.Printf("Error encoding ReadyForQuery: %v\n", err)
		return
	}

	conn.Write(buf)
}

func (p *Proxy) sendError(conn net.Conn, msg string) {
	var buf []byte
	errMsg := &pgproto3.ErrorResponse{
		Severity: "ERROR",
		Message:  msg,
	}
	buf, _ = errMsg.Encode(buf)

	rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
	buf, _ = rfq.Encode(buf)

	conn.Write(buf)
}

func (p *Proxy) relayUntilReady(clientBackend *pgproto3.Backend, clientConn, backendConn net.Conn) error {
	serverFrontend := pgproto3.NewFrontend(backendConn, backendConn)

	for {
		msg, err := serverFrontend.Receive()
		if err != nil {
			return fmt.Errorf("receiving from backend: %w", err)
		}

		// Encode and forward the server message to the client
		var buf []byte
		buf, err = msg.Encode(buf)
		if err != nil {
			return fmt.Errorf("encoding server message %T: %w", msg, err)
		}
		if _, err := clientConn.Write(buf); err != nil {
			return fmt.Errorf("writing to client: %w", err)
		}

		switch msg.(type) {
		case *pgproto3.ReadyForQuery:
			return nil
		case *pgproto3.ErrorResponse:
			return fmt.Errorf("backend authentication error")
		case *pgproto3.AuthenticationCleartextPassword:
			clientBackend.SetAuthType(pgproto3.AuthTypeCleartextPassword)
			if err := p.relayClientAuth(clientBackend, backendConn); err != nil {
				return err
			}
		case *pgproto3.AuthenticationMD5Password:
			clientBackend.SetAuthType(pgproto3.AuthTypeMD5Password)
			if err := p.relayClientAuth(clientBackend, backendConn); err != nil {
				return err
			}
		case *pgproto3.AuthenticationSASL:
			clientBackend.SetAuthType(pgproto3.AuthTypeSASL)
			if err := p.relayClientAuth(clientBackend, backendConn); err != nil {
				return err
			}
		case *pgproto3.AuthenticationSASLContinue:
			clientBackend.SetAuthType(pgproto3.AuthTypeSASLContinue)
			if err := p.relayClientAuth(clientBackend, backendConn); err != nil {
				return err
			}
		case *pgproto3.AuthenticationGSS:
			clientBackend.SetAuthType(pgproto3.AuthTypeGSS)
			if err := p.relayClientAuth(clientBackend, backendConn); err != nil {
				return err
			}
		case *pgproto3.AuthenticationGSSContinue:
			clientBackend.SetAuthType(pgproto3.AuthTypeGSSCont)
			if err := p.relayClientAuth(clientBackend, backendConn); err != nil {
				return err
			}
			// AuthenticationOk, AuthenticationSASLFinal, ParameterStatus, BackendKeyData: no client response needed
		}
	}
}

// relayClientAuth reads one auth response from the client and forwards it to the backend.
func (p *Proxy) relayClientAuth(clientBackend *pgproto3.Backend, backendConn net.Conn) error {
	msg, err := clientBackend.Receive()
	if err != nil {
		return fmt.Errorf("receiving auth response from client: %w", err)
	}
	var buf []byte
	buf, err = msg.Encode(buf)
	if err != nil {
		return fmt.Errorf("encoding client auth response: %w", err)
	}
	if _, err := backendConn.Write(buf); err != nil {
		return fmt.Errorf("forwarding auth response to backend: %w", err)
	}
	return nil
}

// extractQuery extracts the query string from a Query message.
// Format: 'Q' + int32(length) + string(query)\0
func extractQuery(data []byte) string {
	if len(data) < 6 || data[0] != 'Q' {
		return ""
	}
	// Skip 'Q' + 4 bytes length
	query := data[5:]
	// Remove null terminator
	if len(query) > 0 && query[len(query)-1] == 0 {
		query = query[:len(query)-1]
	}
	return string(query)
}

// extractParseQuery extracts the query from a Parse message.
// Format: 'P' + int32(length) + string(dest)\0 + string(query)\0 + int16(numParams) + ...
func extractParseQuery(data []byte) string {
	if len(data) < 6 || data[0] != 'P' {
		return ""
	}
	// Skip 'P' + 4 bytes length
	rest := data[5:]
	// Skip destination string (null-terminated)
	for i, b := range rest {
		if b == 0 {
			rest = rest[i+1:]
			break
		}
	}
	// Extract query string (null-terminated)
	for i, b := range rest {
		if b == 0 {
			return string(rest[:i])
		}
	}
	return string(rest)
}
