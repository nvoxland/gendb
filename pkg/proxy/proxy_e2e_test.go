package proxy

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/nvoxland/gendb/pkg/config"
	"github.com/nvoxland/gendb/pkg/generator"
	"github.com/nvoxland/gendb/pkg/lang"
	"github.com/nvoxland/gendb/pkg/llm"
	"github.com/nvoxland/gendb/pkg/schema"
	"github.com/nvoxland/gendb/pkg/synthetic"
)

func testModel() string {
	if m := os.Getenv("GENDB_TEST_MODEL"); m != "" {
		return m
	}
	return "qwen2.5:3b"
}

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
	_ = server.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 100)
	_, err = server.Read(buf)
	if err == nil {
		t.Error("server received data that should have been discarded")
	}
	_ = server.SetReadDeadline(time.Time{})

	// Second write should pass through to the real conn.
	payload := []byte("real query")
	_, _ = vConn.Write(payload)

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
	_, _ = server.Write([]byte("after close"))
	// Drain remaining handshake data, then read the real data.
	drainBuf := make([]byte, len(handshakeData)+100)
	_ = client.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
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
	_ = client.SetReadDeadline(time.Time{})
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
		_ = ln.Close()
		t.Fatal(err)
	}

	server := <-ch
	_ = ln.Close()
	t.Cleanup(func() {
		_ = client.Close()
		_ = server.Close()
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
		_ = exec.Command("docker", "rm", "-f", cid).Run()
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
			_ = conn.Close(ctx)
			pgReady = true
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if !pgReady {
		t.Fatalf("postgres did not become ready within 30s at %s", pgAddr)
	}

	// Start the proxy
	p := New(Config{
		ListenAddr: ":0",
		RealAddr:   pgAddr,
	})

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
	defer func() { _ = conn.Close(ctx) }()

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
		_ = exec.Command("docker", "rm", "-f", cid).Run()
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
			_ = conn.Close(ctx)
			return pgAddr
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("postgres did not become ready within 30s at %s", pgAddr)
	return ""
}

// registerGenerateHandler registers the generate_data handler for E2E tests.
func registerGenerateHandler(t *testing.T) {
	t.Helper()
	t.Cleanup(lang.ResetHandlers)

	lang.RegisterHandler("generate_data", func(ctx context.Context, args map[string]string) (*lang.Result, error) {
		conn := ConnFromContext(ctx)
		if conn == nil {
			return nil, fmt.Errorf("no database connection available")
		}

		table := args["table_pattern"]
		rows, _ := strconv.Atoi(args["rows"])
		scenario := args["scenario"]

		inspector := schema.NewInspectorFromConn(conn)

		var err error
		var sg *schema.SchemaGraph
		if table != "" {
			sg, err = inspector.InspectTable(ctx, table)
		} else {
			sg, err = inspector.InspectWithOptions(ctx, schema.InspectOptions{
				ExcludeSchemas: []string{synthetic.SchemaName},
			})
		}
		if err != nil {
			return nil, fmt.Errorf("inspecting schema: %w", err)
		}

		for _, tbl := range sg.Tables {
			mapper := func(name string) string {
				return synthetic.SyntheticTableName(scenario, tbl.Schema, name)
			}

			singleTableGraph := &schema.SchemaGraph{}
			singleTableGraph.SetTables([]*schema.Table{tbl})
			ddl := schema.ReconstructDDLForSchemaWithMapping(singleTableGraph, synthetic.SchemaName, mapper)
			if _, err := conn.Exec(ctx, ddl); err != nil {
				return nil, fmt.Errorf("creating synthetic table %s.%s: %w", synthetic.SchemaName, synthetic.SyntheticTableName(scenario, tbl.Schema, tbl.Name), err)
			}

			genCfg := config.GenerationConfig{
				DefaultRows: 10,
			}
			if rows > 0 {
				genCfg.DefaultRows = rows
			}

			// Create a test LLM client pointing to a local Ollama instance
			testLLMClient := llm.NewClient("http://localhost:11434/v1", testModel(), "")
			gen, err := generator.New(testLLMClient, genCfg,
				generator.WithTargetSchema(synthetic.SchemaName),
				generator.WithTableNameMapper(mapper),
			)
			if err != nil {
				return nil, fmt.Errorf("creating generator: %w", err)
			}
			if err := gen.Generate(ctx, singleTableGraph, conn); err != nil {
				return nil, fmt.Errorf("generating data for %s: %w", tbl.Name, err)
			}
		}

		if table == "" {
			return &lang.Result{Tag: fmt.Sprintf("GENDB GENERATE DATA ROWS %d", rows)}, nil
		}
		return &lang.Result{Tag: fmt.Sprintf("GENDB GENERATE DATA FOR %s ROWS %d", table, rows)}, nil
	})
}

func TestGenerateDataWithVarcharConstraints(t *testing.T) {
	pgAddr := startPostgresContainer(t)
	ctx := context.Background()

	registerGenerateHandler(t)

	p := New(Config{
		ListenAddr: ":0",
		RealAddr:   pgAddr,
	})

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
	defer func() { _ = conn.Close(ctx) }()

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
	_, err = conn.Exec(ctx, "CALL gendb.generate_data(table_pattern => 'test1', rows => '10')")
	if err != nil {
		t.Fatalf("generate_data: %v", err)
	}

	// Verify rows were inserted in the synthetic table with the new naming convention.
	syntheticTable := synthetic.SyntheticTableName("", "public", "test1")
	var count int
	err = conn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.%s",
		synthetic.SchemaName, pgx.Identifier{syntheticTable}.Sanitize())).Scan(&count)
	if err != nil {
		t.Fatalf("counting rows in synthetic table: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected rows in synthetic table, got 0")
	}

	// Verify all values respect varchar constraints.
	rows, err := conn.Query(ctx, fmt.Sprintf("SELECT name, address, city, state, zip FROM %s.%s",
		synthetic.SchemaName, pgx.Identifier{syntheticTable}.Sanitize()))
	if err != nil {
		t.Fatalf("querying synthetic table: %v", err)
	}
	defer rows.Close()

	for rows.Next() {
		var name, address, city, state, zip *string
		if err := rows.Scan(&name, &address, &city, &state, &zip); err != nil {
			t.Fatalf("scanning row: %v", err)
		}
		if name != nil && len(*name) > 50 {
			t.Errorf("name too long: %d chars", len(*name))
		}
		if address != nil && len(*address) > 50 {
			t.Errorf("address too long: %d chars", len(*address))
		}
		if city != nil && len(*city) > 20 {
			t.Errorf("city too long: %d chars", len(*city))
		}
		if state != nil && len(*state) > 2 {
			t.Errorf("state too long: %d chars (%q)", len(*state), *state)
		}
		if zip != nil && len(*zip) > 5 {
			t.Errorf("zip too long: %d chars (%q)", len(*zip), *zip)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating rows: %v", err)
	}
}

func TestSyncE2E(t *testing.T) {
	pgAddr := startPostgresContainer(t)
	ctx := context.Background()

	registerGenerateHandler(t)

	// Register sync handler
	lang.RegisterHandler("sync", func(ctx context.Context, args map[string]string) (*lang.Result, error) {
		conn := ConnFromContext(ctx)
		if conn == nil {
			return nil, fmt.Errorf("no database connection available")
		}

		table := args["table_name"]
		scenario := args["scenario"]

		inspector := schema.NewInspectorFromConn(conn)

		rows, err := conn.Query(ctx, `
			SELECT table_name FROM information_schema.tables
			WHERE table_schema = $1 AND table_type = 'BASE TABLE'
		`, synthetic.SchemaName)
		if err != nil {
			return nil, fmt.Errorf("listing synthetic tables: %w", err)
		}

		var syntheticNames []string
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return nil, err
			}
			syntheticNames = append(syntheticNames, name)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}

		for _, syntheticName := range syntheticNames {
			sc, _, sourceTable, ok := synthetic.ParseSyntheticTableName(syntheticName)
			if !ok {
				continue
			}
			if table != "" && sourceTable != table {
				continue
			}
			if scenario != "" && sc != scenario {
				continue
			}

			qualifiedSynthetic := fmt.Sprintf("%s.%s",
				synthetic.SchemaName, pgx.Identifier{syntheticName}.Sanitize())

			origGraph, origErr := inspector.InspectTable(ctx, sourceTable)
			if origErr != nil {
				if _, err := conn.Exec(ctx, fmt.Sprintf("DROP TABLE %s", qualifiedSynthetic)); err != nil {
					return nil, err
				}
				continue
			}

			origTable := origGraph.Tables[0]

			syntheticGraph, err := inspector.InspectTable(ctx, syntheticName)
			if err != nil {
				return nil, err
			}
			syntheticTable := syntheticGraph.Tables[0]

			origCols := make(map[string]*schema.Column)
			for _, c := range origTable.Columns {
				origCols[c.Name] = c
			}
			syntheticCols := make(map[string]bool)
			for _, c := range syntheticTable.Columns {
				syntheticCols[c.Name] = true
			}

			for _, c := range syntheticTable.Columns {
				if _, exists := origCols[c.Name]; !exists {
					if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s DROP COLUMN %s",
						qualifiedSynthetic, pgx.Identifier{c.Name}.Sanitize())); err != nil {
						return nil, err
					}
				}
			}

			for _, origCol := range origTable.Columns {
				if syntheticCols[origCol.Name] {
					continue
				}
				if origCol.IsGenerated {
					continue
				}
				if strings.Contains(origCol.DefaultValue, "nextval(") ||
					strings.Contains(origCol.DefaultValue, "gen_random_uuid()") {
					continue
				}
				if strings.Contains(strings.ToLower(origCol.DataType), "serial") {
					continue
				}

				if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s",
					qualifiedSynthetic, pgx.Identifier{origCol.Name}.Sanitize(), origCol.DataType)); err != nil {
					return nil, err
				}

				genCfg := config.GenerationConfig{DefaultRows: 100}
				testLLMClient := llm.NewClient("http://localhost:11434/v1", testModel(), "")
				gen, err := generator.New(testLLMClient, genCfg)
				if err != nil {
					return nil, fmt.Errorf("creating generator: %w", err)
				}
				if err := gen.FillColumn(ctx, conn, qualifiedSynthetic, origCol, syntheticTable.PrimaryKey); err != nil {
					return nil, err
				}

				if !origCol.IsNullable {
					if _, err := conn.Exec(ctx, fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET NOT NULL",
						qualifiedSynthetic, pgx.Identifier{origCol.Name}.Sanitize())); err != nil {
						return nil, err
					}
				}
			}
		}

		if table != "" {
			return &lang.Result{Tag: fmt.Sprintf("GENDB SYNC %s", table)}, nil
		}
		return &lang.Result{Tag: "GENDB SYNC"}, nil
	})

	p := New(Config{
		ListenAddr: ":0",
		RealAddr:   pgAddr,
	})

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
	defer func() { _ = conn.Close(ctx) }()

	// Create test table
	_, err = conn.Exec(ctx, `CREATE TABLE test1 (
		id serial PRIMARY KEY,
		name varchar(50),
		email varchar(100)
	)`)
	if err != nil {
		t.Fatalf("CREATE TABLE: %v", err)
	}

	// Generate data
	_, err = conn.Exec(ctx, "CALL gendb.generate_data(table_pattern => 'test1', rows => '5')")
	if err != nil {
		t.Fatalf("generate_data: %v", err)
	}

	syntheticTable := synthetic.SyntheticTableName("", "public", "test1")

	// Verify initial synthetic table
	var count int
	err = conn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.%s",
		synthetic.SchemaName, pgx.Identifier{syntheticTable}.Sanitize())).Scan(&count)
	if err != nil {
		t.Fatalf("counting initial rows: %v", err)
	}
	if count == 0 {
		t.Fatalf("expected rows in synthetic table, got 0")
	}

	// Alter original: add column, drop column
	_, err = conn.Exec(ctx, "ALTER TABLE test1 ADD COLUMN age integer")
	if err != nil {
		t.Fatalf("ALTER TABLE ADD COLUMN: %v", err)
	}
	_, err = conn.Exec(ctx, "ALTER TABLE test1 DROP COLUMN email")
	if err != nil {
		t.Fatalf("ALTER TABLE DROP COLUMN: %v", err)
	}

	// Run sync
	_, err = conn.Exec(ctx, "CALL gendb.sync(table_name => 'test1')")
	if err != nil {
		t.Fatalf("sync: %v", err)
	}

	// Verify synthetic table has 'age' column and no 'email' column
	var hasAge, hasEmail bool
	colRows, err := conn.Query(ctx, `
		SELECT column_name FROM information_schema.columns
		WHERE table_schema = $1 AND table_name = $2
	`, synthetic.SchemaName, syntheticTable)
	if err != nil {
		t.Fatalf("querying synthetic columns: %v", err)
	}
	for colRows.Next() {
		var colName string
		if err := colRows.Scan(&colName); err != nil {
			t.Fatalf("scanning column: %v", err)
		}
		if colName == "age" {
			hasAge = true
		}
		if colName == "email" {
			hasEmail = true
		}
	}
	colRows.Close()

	if !hasAge {
		t.Error("expected synthetic table to have 'age' column after sync")
	}
	if hasEmail {
		t.Error("expected synthetic table NOT to have 'email' column after sync")
	}

	// Verify age column has data (not all nulls)
	var nullCount int
	err = conn.QueryRow(ctx, fmt.Sprintf("SELECT count(*) FROM %s.%s WHERE age IS NULL",
		synthetic.SchemaName, pgx.Identifier{syntheticTable}.Sanitize())).Scan(&nullCount)
	if err != nil {
		t.Fatalf("counting nulls: %v", err)
	}
	if nullCount == 5 {
		t.Error("expected age column to have generated data, but all values are NULL")
	}

	// Drop original table and sync to clean up orphaned synthetic table
	_, err = conn.Exec(ctx, "DROP TABLE test1 CASCADE")
	if err != nil {
		t.Fatalf("DROP TABLE: %v", err)
	}

	_, err = conn.Exec(ctx, "CALL gendb.sync()")
	if err != nil {
		t.Fatalf("sync after drop: %v", err)
	}

	// Verify synthetic table no longer exists
	var tableExists bool
	err = conn.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM information_schema.tables
			WHERE table_schema = $1 AND table_name = $2
		)
	`, synthetic.SchemaName, syntheticTable).Scan(&tableExists)
	if err != nil {
		t.Fatalf("checking synthetic table existence: %v", err)
	}
	if tableExists {
		t.Error("expected synthetic table to be dropped after original was dropped and sync was run")
	}
}
