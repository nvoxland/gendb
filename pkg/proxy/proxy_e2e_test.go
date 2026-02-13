package proxy

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/nvoxland/autodb/pkg/lang"
)

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
	state := lang.NewState()
	executor := lang.NewExecutor(state)
	p := New(Config{
		ListenAddr: ":0",
		RealAddr:   pgAddr,
		SchemaName: "autodb_shadow",
	}, state, executor)

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
