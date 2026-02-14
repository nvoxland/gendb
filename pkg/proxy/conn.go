package proxy

import (
	"io"
	"net"
	"time"

	"github.com/jackc/pgx/v5/pgproto3"
)

// virtualHandshakeConn wraps an already-authenticated net.Conn so that
// pgx.ConnectConfig can perform its startup handshake without touching the
// real backend. The first Write (pgx's StartupMessage) is silently discarded.
// Reads come first from a pre-built handshake buffer (AuthenticationOk +
// ParameterStatus messages + BackendKeyData + ReadyForQuery), then from
// the real connection.
type virtualHandshakeConn struct {
	real         net.Conn
	reader       io.Reader
	startupEaten bool
}

func newVirtualHandshakeConn(real net.Conn, handshakeData []byte) *virtualHandshakeConn {
	return &virtualHandshakeConn{
		real:   real,
		reader: io.MultiReader(newBytesReader(handshakeData), real),
	}
}

// bytesReader is a minimal io.Reader over a byte slice (avoids importing bytes
// just for bytes.NewReader).
type bytesReader struct {
	data []byte
	pos  int
}

func newBytesReader(data []byte) *bytesReader {
	return &bytesReader{data: data}
}

func (r *bytesReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.pos:])
	r.pos += n
	return n, nil
}

func (c *virtualHandshakeConn) Read(b []byte) (int, error) {
	return c.reader.Read(b)
}

func (c *virtualHandshakeConn) Write(b []byte) (int, error) {
	if !c.startupEaten {
		// Discard the first write — pgx's StartupMessage.
		c.startupEaten = true
		return len(b), nil
	}
	return c.real.Write(b)
}

func (c *virtualHandshakeConn) Close() error {
	// No-op: the proxy manages the real connection's lifecycle.
	return nil
}

func (c *virtualHandshakeConn) LocalAddr() net.Addr                { return c.real.LocalAddr() }
func (c *virtualHandshakeConn) RemoteAddr() net.Addr               { return c.real.RemoteAddr() }
func (c *virtualHandshakeConn) SetDeadline(t time.Time) error      { return c.real.SetDeadline(t) }
func (c *virtualHandshakeConn) SetReadDeadline(t time.Time) error  { return c.real.SetReadDeadline(t) }
func (c *virtualHandshakeConn) SetWriteDeadline(t time.Time) error { return c.real.SetWriteDeadline(t) }

// buildHandshakeData encodes the fake PG startup response that pgx expects:
// AuthenticationOk, ParameterStatus messages, BackendKeyData, ReadyForQuery.
func buildHandshakeData(params map[string]string, pid uint32, secretKey uint32) []byte {
	var buf []byte

	authOk := &pgproto3.AuthenticationOk{}
	buf, _ = authOk.Encode(buf)

	for name, value := range params {
		ps := &pgproto3.ParameterStatus{Name: name, Value: value}
		buf, _ = ps.Encode(buf)
	}

	bkd := &pgproto3.BackendKeyData{ProcessID: pid, SecretKey: secretKey}
	buf, _ = bkd.Encode(buf)

	rfq := &pgproto3.ReadyForQuery{TxStatus: 'I'}
	buf, _ = rfq.Encode(buf)

	return buf
}
