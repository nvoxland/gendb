package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/nvoxland/gendb/pkg/lang"
)

// Proxy is a PostgreSQL wire protocol proxy.
type Proxy struct {
	listenAddr string
	realAddr   string
	infoSQL    string // per-session temp table SQL for gendb.info
	listener   net.Listener
	ready      chan struct{}
	mu         sync.Mutex
	sessions   map[net.Conn]*session
}

type session struct {
	backendConn   net.Conn          // backend connection
	clientConn    net.Conn          // client connection (for sending NOTICEs)
	startupParams map[string]string // from StartupMessage.Parameters
	pgxConn       *pgx.Conn         // reused across generate_data calls

	// Captured from handshake for virtual conn replay
	backendPID    uint32
	backendSecret uint32
	backendParams map[string]string // ParameterStatus values

	// Relay pause/resume
	mu           sync.Mutex
	relayPaused  bool
	pauseConfirm chan struct{}
	resumeCh     chan struct{}

	// Transaction state
	txStatus atomic.Int32 // 'I' = idle, 'T' = in transaction, 'E' = failed transaction
}

func (sess *session) getTxStatus() byte {
	return byte(sess.txStatus.Load())
}

func (sess *session) setTxStatus(b byte) {
	sess.txStatus.Store(int32(b))
}

// Config holds proxy configuration.
type Config struct {
	ListenAddr       string // e.g. ":5433"
	RealAddr         string // e.g. "localhost:5432"
	Version          string
	LLMModel         string
	LLMProvider      string
	StructuredOutput bool
}

// New creates a new proxy.
func New(cfg Config) *Proxy {
	return &Proxy{
		listenAddr: cfg.ListenAddr,
		realAddr:   cfg.RealAddr,
		infoSQL:    lang.BuildInfoSQL(cfg.Version, cfg.LLMModel, cfg.LLMProvider, cfg.StructuredOutput),
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

	slog.Info("GenDB proxy started", "listen", p.listenAddr, "backend", p.realAddr)

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

	sess := &session{
		backendParams: make(map[string]string),
		clientConn:    clientConn,
	}
	sess.setTxStatus('I')
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
		slog.Error("Failed to read startup message", "error", err)
		return
	}

	// Handle SSL request
	if _, ok := startupMsg.(*pgproto3.SSLRequest); ok {
		// Deny SSL
		clientConn.Write([]byte("N"))
		slog.Debug("Denied SSL request, waiting for plaintext startup")
		// Re-read the actual startup message
		startupMsg, err = clientBackend.ReceiveStartupMessage()
		if err != nil {
			slog.Error("Failed to read startup message after SSL denial", "error", err)
			return
		}
	}

	// Always connect to the real database
	backendConn, err := net.Dial("tcp", p.realAddr)
	if err != nil {
		slog.Error("Failed to connect to backend", "backend", p.realAddr, "error", err)
		p.sendError(clientConn, fmt.Sprintf("could not connect to backend: %v", err))
		return
	}
	defer backendConn.Close()
	defer func() {
		if sess.pgxConn != nil {
			sess.pgxConn.PgConn().Hijack()
		}
	}()

	sess.backendConn = backendConn

	// Forward the startup message to the backend
	switch msg := startupMsg.(type) {
	case *pgproto3.StartupMessage:
		sess.startupParams = msg.Parameters
		slog.Info("New client connection", "user", msg.Parameters["user"], "database", msg.Parameters["database"])
		startupBytes, err := msg.Encode(nil)
		if err != nil {
			slog.Error("Failed to encode startup message", "error", err)
			return
		}
		if _, err := backendConn.Write(startupBytes); err != nil {
			slog.Error("Failed to forward startup message to backend", "error", err)
			return
		}
	default:
		slog.Error("Unexpected startup message type", "type", fmt.Sprintf("%T", msg))
		return
	}

	// Relay auth and initial parameter messages until ReadyForQuery
	if err := p.relayUntilReady(clientBackend, clientConn, backendConn, sess); err != nil {
		slog.Error("Authentication relay failed", "error", err)
		return
	}
	slog.Debug("Client authenticated successfully", "user", sess.startupParams["user"])

	// Recreate stub procedures on every connection so signatures stay current
	if err := ensureGenDBSchema(backendConn, p.infoSQL); err != nil {
		slog.Warn("Could not create gendb schema stubs", "error", err)
	} else {
		slog.Debug("Created gendb schema stubs for intellisense")
	}

	// Main loop: intercept GENDB commands, relay everything else
	p.mainLoop(ctx, clientConn, backendConn, sess)
}

func (p *Proxy) mainLoop(ctx context.Context, clientConn, backendConn net.Conn, sess *session) {
	// Client -> Backend relay (with GENDB interception)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := clientConn.Read(buf)
			if err != nil {
				return
			}

			data := buf[:n]

			// Check if this is a Query message ('Q') with GENDB prefix
			if len(data) > 5 && data[0] == 'Q' {
				query := extractQuery(data)
				if lang.IsGenDBCommand(query) {
					p.handleGenDBCommand(ctx, clientConn, sess, query)
					continue
				}
			}

			// Check if this is a Parse message ('P') with GENDB prefix
			if len(data) > 5 && data[0] == 'P' {
				query := extractParseQuery(data)
				if lang.IsGenDBCommand(query) {
					// Drain remaining extended query protocol messages (Bind, Describe,
					// Execute, Sync) to prevent them from leaking to the backend.
					p.drainExtendedQuery(clientConn, data, n)
					p.handleGenDBCommand(ctx, clientConn, sess, query)
					continue
				}
			}

			// Relay to backend
			if _, err := backendConn.Write(data); err != nil {
				return
			}
		}
	}()

	// Backend -> Client relay (pausable, tracks transaction state)
	buf := make([]byte, 32*1024)
	for {
		n, err := backendConn.Read(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				sess.mu.Lock()
				if sess.relayPaused {
					confirm := sess.pauseConfirm
					resume := sess.resumeCh
					sess.mu.Unlock()
					close(confirm) // signal: "I've paused"
					<-resume       // wait for resume
					continue
				}
				sess.mu.Unlock()
				continue
			}
			return
		}

		// Scan for ReadyForQuery messages to track transaction state.
		// ReadyForQuery: 'Z' + int32(5) + byte(txStatus)
		data := buf[:n]
		for i := 0; i < len(data); {
			if data[i] == 'Z' && i+6 <= len(data) {
				// Verify length field is 4 (int32 big-endian)
				msgLen := int(data[i+1])<<24 | int(data[i+2])<<16 | int(data[i+3])<<8 | int(data[i+4])
				if msgLen == 5 {
					sess.setTxStatus(data[i+5])
					i += 6
					continue
				}
			}
			i++
		}

		if _, err := clientConn.Write(buf[:n]); err != nil {
			return
		}
	}
}

// pauseRelay pauses the backend->client relay goroutine.
// It sets a read deadline to interrupt any blocked Read, then waits until
// the relay goroutine confirms it has paused.
func (sess *session) pauseRelay() {
	sess.mu.Lock()
	sess.relayPaused = true
	sess.pauseConfirm = make(chan struct{})
	sess.resumeCh = make(chan struct{})
	sess.mu.Unlock()

	// Interrupt any blocked read on backendConn.
	sess.backendConn.SetReadDeadline(time.Now())

	// Wait for relay goroutine to acknowledge pause.
	<-sess.pauseConfirm

	// Clear the expired deadline so the connection is usable for queries.
	sess.backendConn.SetReadDeadline(time.Time{})
}

// resumeRelay resumes the backend->client relay goroutine after a pause.
func (sess *session) resumeRelay() {
	sess.backendConn.SetReadDeadline(time.Time{}) // clear deadline
	sess.mu.Lock()
	sess.relayPaused = false
	ch := sess.resumeCh
	sess.mu.Unlock()
	close(ch) // unblock relay goroutine
}

func (p *Proxy) handleGenDBCommand(ctx context.Context, clientConn net.Conn, sess *session, query string) {
	slog.Info("Intercepted GenDB command", "query", query)

	cmd, err := lang.Parse(query)
	if err != nil {
		slog.Error("Failed to parse GenDB command", "query", query, "error", err)
		p.sendError(clientConn, err.Error())
		return
	}

	// Pause the relay so we can safely use backendConn.
	sess.pauseRelay()
	defer sess.resumeRelay()

	// Inject client connection into context for NOTICE messages.
	ctx = context.WithValue(ctx, clientConnKey{}, clientConn)

	// If the command needs a DB connection, lazily create a *pgx.Conn and reuse it.
	if cmd.NeedsConn() {
		if sess.pgxConn == nil {
			slog.Debug("Creating pgx connection for session")
			pgxConn, err := p.createPgxConn(ctx, sess)
			if err != nil {
				slog.Error("Failed to create database connection", "error", err)
				p.sendError(clientConn, fmt.Sprintf("creating database connection: %v", err))
				return
			}
			sess.pgxConn = pgxConn
		}
		ctx = withConn(ctx, sess.pgxConn)
	}

	result, err := lang.Execute(ctx, cmd)
	if err != nil {
		slog.Error("Command execution failed", "query", query, "error", err)
		p.sendError(clientConn, err.Error())
		return
	}

	slog.Info("Command completed", "tag", result.Tag)
	p.sendResult(clientConn, result)
}

// createPgxConn builds a *pgx.Conn over the existing authenticated backendConn
// using a virtual handshake connection.
func (p *Proxy) createPgxConn(ctx context.Context, sess *session) (*pgx.Conn, error) {
	handshakeData := buildHandshakeData(sess.backendParams, sess.backendPID, sess.backendSecret)
	vConn := newVirtualHandshakeConn(sess.backendConn, handshakeData)

	connStr := fmt.Sprintf("host=localhost user=%s database=%s sslmode=disable",
		sess.startupParams["user"], sess.startupParams["database"])
	config, err := pgx.ParseConfig(connStr)
	if err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	config.Config.DialFunc = func(ctx context.Context, network, addr string) (net.Conn, error) {
		return vConn, nil
	}
	pgxConn, err := pgx.ConnectConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("connecting via virtual handshake: %w", err)
	}
	return pgxConn, nil
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
			slog.Error("Failed to encode RowDescription", "error", err)
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
				slog.Error("Failed to encode DataRow", "error", err)
				return
			}
		}
	}

	// Send CommandComplete
	cc := &pgproto3.CommandComplete{CommandTag: []byte(result.Tag)}
	buf, err = cc.Encode(buf)
	if err != nil {
		slog.Error("Failed to encode CommandComplete", "error", err)
		return
	}

	// Send ReadyForQuery
	rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
	buf, err = rfq.Encode(buf)
	if err != nil {
		slog.Error("Failed to encode ReadyForQuery", "error", err)
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

func (p *Proxy) relayUntilReady(clientBackend *pgproto3.Backend, clientConn, backendConn net.Conn, sess *session) error {
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

		switch m := msg.(type) {
		case *pgproto3.ReadyForQuery:
			return nil
		case *pgproto3.ErrorResponse:
			return fmt.Errorf("backend authentication error")
		case *pgproto3.BackendKeyData:
			sess.backendPID = m.ProcessID
			sess.backendSecret = m.SecretKey
		case *pgproto3.ParameterStatus:
			sess.backendParams[m.Name] = m.Value
		case *pgproto3.AuthenticationCleartextPassword:
			clientBackend.SetAuthType(pgproto3.AuthTypeCleartextPassword)
			if err := p.relayClientAuth(clientBackend, backendConn, sess); err != nil {
				return err
			}
		case *pgproto3.AuthenticationMD5Password:
			clientBackend.SetAuthType(pgproto3.AuthTypeMD5Password)
			if err := p.relayClientAuth(clientBackend, backendConn, sess); err != nil {
				return err
			}
		case *pgproto3.AuthenticationSASL:
			clientBackend.SetAuthType(pgproto3.AuthTypeSASL)
			if err := p.relayClientAuth(clientBackend, backendConn, sess); err != nil {
				return err
			}
		case *pgproto3.AuthenticationSASLContinue:
			clientBackend.SetAuthType(pgproto3.AuthTypeSASLContinue)
			if err := p.relayClientAuth(clientBackend, backendConn, sess); err != nil {
				return err
			}
		case *pgproto3.AuthenticationGSS:
			clientBackend.SetAuthType(pgproto3.AuthTypeGSS)
			if err := p.relayClientAuth(clientBackend, backendConn, sess); err != nil {
				return err
			}
		case *pgproto3.AuthenticationGSSContinue:
			clientBackend.SetAuthType(pgproto3.AuthTypeGSSCont)
			if err := p.relayClientAuth(clientBackend, backendConn, sess); err != nil {
				return err
			}
			// AuthenticationOk, AuthenticationSASLFinal, ParameterStatus, BackendKeyData: no client response needed
		}
	}
}

// relayClientAuth reads one auth response from the client and forwards it to the backend.
func (p *Proxy) relayClientAuth(clientBackend *pgproto3.Backend, backendConn net.Conn, sess *session) error {
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

type connKey struct{}
type clientConnKey struct{}

func withConn(ctx context.Context, conn *pgx.Conn) context.Context {
	return context.WithValue(ctx, connKey{}, conn)
}

// ConnFromContext returns the *pgx.Conn stored in the context by the proxy,
// or nil if not present.
func ConnFromContext(ctx context.Context) *pgx.Conn {
	if v, ok := ctx.Value(connKey{}).(*pgx.Conn); ok {
		return v
	}
	return nil
}

// ClientConnFromContext returns the client net.Conn from the context, for sending NOTICEs.
func ClientConnFromContext(ctx context.Context) net.Conn {
	if v, ok := ctx.Value(clientConnKey{}).(net.Conn); ok {
		return v
	}
	return nil
}

// SendNotice sends a PostgreSQL NOTICE message to the client connection.
func SendNotice(conn net.Conn, message string) {
	if conn == nil {
		return
	}
	notice := &pgproto3.NoticeResponse{
		Severity: "NOTICE",
		Message:  message,
	}
	buf, err := notice.Encode(nil)
	if err != nil {
		slog.Debug("Failed to encode NOTICE", "error", err)
		return
	}
	conn.Write(buf)
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

// drainExtendedQuery consumes remaining extended query protocol messages after
// intercepting a Parse message. This prevents Bind/Describe/Execute/Sync from
// leaking to the backend when a GENDB command is sent via the extended protocol.
func (p *Proxy) drainExtendedQuery(clientConn net.Conn, data []byte, n int) {
	// Check if a Sync ('S') message is already in the current buffer.
	// Walk through the messages in the buffer by parsing their lengths.
	pos := 0
	for pos < n {
		if pos+5 > n {
			break
		}
		msgType := data[pos]
		msgLen := int(data[pos+1])<<24 | int(data[pos+2])<<16 | int(data[pos+3])<<8 | int(data[pos+4])
		totalLen := 1 + msgLen // type byte + length (which includes itself)
		if msgType == 'S' {
			// Found Sync in current buffer, nothing more to drain
			return
		}
		if totalLen <= 0 || pos+totalLen > n {
			break
		}
		pos += totalLen
	}

	// Sync wasn't in the current buffer — read more until we find it.
	drainBuf := make([]byte, 4096)
	for {
		clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		dn, err := clientConn.Read(drainBuf)
		clientConn.SetReadDeadline(time.Time{})
		if err != nil {
			slog.Debug("drainExtendedQuery: read error while draining", "error", err)
			return
		}
		// Scan for Sync message type
		for i := 0; i < dn; {
			if i+5 > dn {
				break
			}
			msgType := drainBuf[i]
			msgLen := int(drainBuf[i+1])<<24 | int(drainBuf[i+2])<<16 | int(drainBuf[i+3])<<8 | int(drainBuf[i+4])
			totalLen := 1 + msgLen
			if msgType == 'S' {
				return
			}
			if totalLen <= 0 || i+totalLen > dn {
				break
			}
			i += totalLen
		}
	}
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
