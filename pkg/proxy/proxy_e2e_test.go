package proxy

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/nvoxland/gendb/pkg/config"
	"github.com/nvoxland/gendb/pkg/generator"
	"github.com/nvoxland/gendb/pkg/lang"
	"github.com/nvoxland/gendb/pkg/schema"
	"github.com/nvoxland/gendb/pkg/shadow"
)

func TestConnFromContext(t *testing.T) {
	ctx := context.Background()
	if got := ConnFromContext(ctx); got != nil {
		t.Errorf("ConnFromContext on empty ctx = %v, want nil", got)
	}
}

func TestBuildHandshakeData(t *testing.T) {
	params := map[string]string{
		"server_version":  "16.1",
		"server_encoding": "UTF8",
	}
	data := buildHandshakeData(params, 12345, 67890)

	if len(data) == 0 {
		t.Fatal("buildHandshakeData returned empty data")
	}

	// Parse the data back using pgproto3 Frontend to verify wire format.
	r := bytes.NewReader(data)
	frontend := pgproto3.NewFrontend(r, nil)

	// First message: AuthenticationOk
	msg, err := frontend.Receive()
	if err != nil {
		t.Fatalf("reading AuthenticationOk: %v", err)
	}
	if _, ok := msg.(*pgproto3.AuthenticationOk); !ok {
		t.Fatalf("expected AuthenticationOk, got %T", msg)
	}

	// Next: ParameterStatus messages (order may vary due to map iteration)
	gotParams := make(map[string]string)
	for i := 0; i < len(params); i++ {
		msg, err = frontend.Receive()
		if err != nil {
			t.Fatalf("reading ParameterStatus %d: %v", i, err)
		}
		ps, ok := msg.(*pgproto3.ParameterStatus)
		if !ok {
			t.Fatalf("expected ParameterStatus, got %T", msg)
		}
		gotParams[ps.Name] = ps.Value
	}
	for k, v := range params {
		if gotParams[k] != v {
			t.Errorf("param %q = %q, want %q", k, gotParams[k], v)
		}
	}

	// BackendKeyData
	msg, err = frontend.Receive()
	if err != nil {
		t.Fatalf("reading BackendKeyData: %v", err)
	}
	bkd, ok := msg.(*pgproto3.BackendKeyData)
	if !ok {
		t.Fatalf("expected BackendKeyData, got %T", msg)
	}
	if bkd.ProcessID != 12345 || bkd.SecretKey != 67890 {
		t.Errorf("BackendKeyData = {%d, %d}, want {12345, 67890}", bkd.ProcessID, bkd.SecretKey)
	}

	// ReadyForQuery
	msg, err = frontend.Receive()
	if err != nil {
		t.Fatalf("reading ReadyForQuery: %v", err)
	}
	rfq, ok := msg.(*pgproto3.ReadyForQuery)
	if !ok {
		t.Fatalf("expected ReadyForQuery, got %T", msg)
	}
	if rfq.TxStatus != 'I' {
		t.Errorf("TxStatus = %c, want I", rfq.TxStatus)
	}
}

func TestVirtualHandshakeConn(t *testing.T) {
	// Create a connected TCP pair so SetReadDeadline works.
	server, client := newTCPPair(t)

	handshakeData := buildHandshakeData(map[string]string{"server_version": "16.1"}, 1, 2)
	vConn := newVirtualHandshakeConn(client, handshakeData)

	// First write should be silently discarded (pgx's StartupMessage).
	n, err := vConn.Write([]byte("startup message"))
	if err != nil {
		t.Fatalf("first Write: %v", err)
	}
	if n != len("startup message") {
		t.Errorf("first Write returned %d, want %d", n, len("startup message"))
	}

	// Verify the server did NOT receive the discarded write.
	server.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 100)
	_, err = server.Read(buf)
	if err == nil {
		t.Error("server received data that should have been discarded")
	}
	server.SetReadDeadline(time.Time{})

	// Second write should pass through to the real conn.
	payload := []byte("real query")
	vConn.Write(payload)

	n, err = server.Read(buf)
	if err != nil {
		t.Fatalf("server Read: %v", err)
	}
	if string(buf[:n]) != "real query" {
		t.Errorf("server got %q, want %q", buf[:n], "real query")
	}

	// Read should return handshake data first.
	// First byte of AuthenticationOk is 'R'.
	readBuf := make([]byte, 1)
	_, err = vConn.Read(readBuf)
	if err != nil {
		t.Fatalf("vConn Read: %v", err)
	}
	if readBuf[0] != 'R' {
		t.Errorf("first read byte = %c, want R", readBuf[0])
	}

	// Close should be a no-op (not close the real conn).
	if err := vConn.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	// Real conn should still be usable after vConn.Close().
	server.Write([]byte("after close"))
	// Drain remaining handshake data, then read the real data.
	drainBuf := make([]byte, len(handshakeData)+100)
	client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	total := 0
	for {
		n, err := client.Read(drainBuf[total:])
		total += n
		if err != nil {
			break
		}
		if bytes.Contains(drainBuf[:total], []byte("after close")) {
			break
		}
	}
	client.SetReadDeadline(time.Time{})
	if !bytes.Contains(drainBuf[:total], []byte("after close")) {
		t.Error("real conn not usable after vConn.Close()")
	}
}

// newTCPPair creates a connected pair of net.Conn via a local TCP listener.
func newTCPPair(t *testing.T) (net.Conn, net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	ch := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		ch <- c
	}()

	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		ln.Close()
		t.Fatal(err)
	}

	server := <-ch
	ln.Close()
	t.Cleanup(func() {
		client.Close()
		server.Close()
	})
	return server, client
}

func TestProxyE2E(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not in PATH, skipping E2E test")
	}

	// Start postgres:16 container with a dynamic host port
	containerID, err := exec.Command("docker", "run", "-d",
		"-p", "0:5432",
		"-e", "POSTGRES_PASSWORD=testpass",
		"postgres:16",
	).Output()
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	cid := strings.TrimSpace(string(containerID))
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", cid).Run()
	})

	// Discover the mapped host port
	portOut, err := exec.Command("docker", "port", cid, "5432/tcp").Output()
	if err != nil {
		t.Fatalf("getting container port: %v", err)
	}
	// Output looks like "0.0.0.0:XXXXX\n" or "[::]:XXXXX\n"
	hostPort := strings.TrimSpace(string(portOut))
	// Take the last entry (may have multiple lines for IPv4 + IPv6)
	lines := strings.Split(hostPort, "\n")
	hostPort = strings.TrimSpace(lines[len(lines)-1])
	// Extract just host:port — docker port output is "addr:port"
	pgAddr := "localhost:" + hostPort[strings.LastIndex(hostPort, ":")+1:]

	// Wait for postgres to be ready
	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	var pgReady bool
	for time.Now().Before(deadline) {
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://postgres:testpass@%s/postgres?sslmode=disable", pgAddr))
		if err == nil {
			conn.Close(ctx)
			pgReady = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !pgReady {
		t.Fatalf("postgres did not become ready within 30s at %s", pgAddr)
	}

	// Start the proxy
	executor := lang.NewExecutor()
	p := New(Config{
		ListenAddr: ":0",
		RealAddr:   pgAddr,
		Key:        "gendb",
	}, executor)

	proxyCtx, proxyCancel := context.WithCancel(ctx)
	defer proxyCancel()

	proxyErr := make(chan error, 1)
	go func() {
		proxyErr <- p.Start(proxyCtx)
	}()

	// Wait for proxy to be listening
	select {
	case <-p.Ready():
	case err := <-proxyErr:
		t.Fatalf("proxy failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for proxy to be ready")
	}

	proxyAddr := p.Addr().String()

	// Connect through the proxy
	connStr := fmt.Sprintf("postgres://postgres:testpass@%s/postgres?sslmode=disable", proxyAddr)
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connecting through proxy: %v", err)
	}
	defer conn.Close(ctx)

	// Run SELECT 1
	var result int
	err = conn.QueryRow(ctx, "SELECT 1").Scan(&result)
	if err != nil {
		t.Fatalf("SELECT 1 through proxy: %v", err)
	}
	if result != 1 {
		t.Fatalf("expected 1, got %d", result)
	}
}

// startPostgresContainer starts a postgres:16 Docker container and returns
// the container ID and the host:port address. The container is cleaned up
// when the test finishes.
func startPostgresContainer(t *testing.T) (pgAddr string) {
	t.Helper()

	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker not in PATH, skipping E2E test")
	}

	containerID, err := exec.Command("docker", "run", "-d",
		"-p", "0:5432",
		"-e", "POSTGRES_PASSWORD=testpass",
		"postgres:16",
	).Output()
	if err != nil {
		t.Fatalf("starting postgres container: %v", err)
	}
	cid := strings.TrimSpace(string(containerID))
	t.Cleanup(func() {
		exec.Command("docker", "rm", "-f", cid).Run()
	})

	portOut, err := exec.Command("docker", "port", cid, "5432/tcp").Output()
	if err != nil {
		t.Fatalf("getting container port: %v", err)
	}
	hostPort := strings.TrimSpace(string(portOut))
	lines := strings.Split(hostPort, "\n")
	hostPort = strings.TrimSpace(lines[len(lines)-1])
	pgAddr = "localhost:" + hostPort[strings.LastIndex(hostPort, ":")+1:]

	// Wait for postgres to be ready
	ctx := context.Background()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := pgx.Connect(ctx, fmt.Sprintf("postgres://postgres:testpass@%s/postgres?sslmode=disable", pgAddr))
		if err == nil {
			conn.Close(ctx)
			return pgAddr
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres did not become ready within 30s at %s", pgAddr)
	return ""
}

func TestGenerateDataWithVarcharConstraints(t *testing.T) {
	pgAddr := startPostgresContainer(t)
	ctx := context.Background()

	const testKey = "gendb"

	executor := lang.NewExecutor()

	// Wire up OnGenerate the same way serve.go does.
	executor.OnGenerate = func(ctx context.Context, table string, rows int, seed *int64) error {
		conn := ConnFromContext(ctx)
		if conn == nil {
			return fmt.Errorf("no database connection available")
		}

		inspector := schema.NewInspectorFromConn(conn)

		var err error
		var sg *schema.SchemaGraph
		if table != "" {
			sg, err = inspector.InspectTable(ctx, table)
		} else {
			sg, err = inspector.InspectWithOptions(ctx, schema.InspectOptions{
				ExcludeSchemas: []string{shadow.DeriveSchemaName("public", testKey)},
			})
		}
		if err != nil {
			return fmt.Errorf("inspecting schema: %w", err)
		}

		for _, tbl := range sg.Tables {
			targetSchema := shadow.DeriveSchemaName(tbl.Schema, testKey)
			singleTableGraph := &schema.SchemaGraph{}
			singleTableGraph.SetTables([]*schema.Table{tbl})
			ddl := schema.ReconstructDDLForSchema(singleTableGraph, targetSchema)
			if _, err := conn.Exec(ctx, ddl); err != nil {
				return fmt.Errorf("creating shadow table %s.%s: %w", targetSchema, tbl.Name, err)
			}

			genCfg := config.GenerationConfig{
				DefaultRows: 10,
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
				return fmt.Errorf("generating data for %s: %w", tbl.Name, err)
			}
		}
		return nil
	}

	p := New(Config{
		ListenAddr: ":0",
		RealAddr:   pgAddr,
		Key:        testKey,
	}, executor)

	proxyCtx, proxyCancel := context.WithCancel(ctx)
	defer proxyCancel()

	proxyErr := make(chan error, 1)
	go func() {
		proxyErr <- p.Start(proxyCtx)
	}()

	select {
	case <-p.Ready():
	case err := <-proxyErr:
		t.Fatalf("proxy failed to start: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for proxy to be ready")
	}

	proxyAddr := p.Addr().String()
	connStr := fmt.Sprintf("postgres://postgres:testpass@%s/postgres?sslmode=disable", proxyAddr)
	conn, err := pgx.Connect(ctx, connStr)
	if err != nil {
		t.Fatalf("connecting through proxy: %v", err)
	}
	defer conn.Close(ctx)

	// Create a table with tight varchar constraints.
	_, err = conn.Exec(ctx, `CREATE TABLE test1 (
		id serial PRIMARY KEY,
		name varchar(50),
		address varchar(50),
		city varchar(20),
		state varchar(2),
		zip varchar(5)
	)`)
	if err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	// Generate data via the proxy's CALL interception.
	_, err = conn.Exec(ctx, "CALL gendb.generate_data(table_name => 'test1', rows => '10')")
	if err != nil {
		t.Fatalf("generate_data: %v", err)
	}

	// Verify rows were inserted in the shadow schema.
	shadowSchema := shadow.DeriveSchemaName("public", testKey)
	var count int
	err = conn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.test1", shadowSchema)).Scan(&count)
	if err != nil {
		t.Fatalf("counting rows in shadow table: %v", err)
	}
	if count != 10 {
		t.Fatalf("expected 10 rows, got %d", count)
	}

	// Verify all values respect varchar constraints.
	rows, err := conn.Query(ctx, fmt.Sprintf("SELECT name, address, city, state, zip FROM %s.test1", shadowSchema))
	if err != nil {
		t.Fatalf("querying shadow table: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name, address, city, state, zip string
		if err := rows.Scan(&name, &address, &city, &state, &zip); err != nil {
			t.Fatalf("scanning row: %v", err)
		}
		if len(name) > 50 {
			t.Errorf("name too long: %d chars", len(name))
		}
		if len(address) > 50 {
			t.Errorf("address too long: %d chars", len(address))
		}
		if len(city) > 20 {
			t.Errorf("city too long: %d chars", len(city))
		}
		if len(state) > 2 {
			t.Errorf("state too long: %d chars (%q)", len(state), state)
		}
		if len(zip) > 5 {
			t.Errorf("zip too long: %d chars (%q)", len(zip), zip)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating rows: %v", err)
	}
}
