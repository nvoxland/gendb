package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/nvoxland/autodb/pkg/lang"
	"github.com/nvoxland/autodb/pkg/proxy"
	"github.com/spf13/cobra"
)

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the PostgreSQL wire protocol proxy",
	RunE:  runServe,
}

var (
	dbHostname string
	dbPort     int
	servePort  int
)

func init() {
	serveCmd.Flags().StringVar(&dbHostname, "db-hostname", "localhost", "PostgreSQL server hostname")
	serveCmd.Flags().IntVar(&dbPort, "db-port", 5432, "PostgreSQL server port")
	serveCmd.Flags().IntVar(&servePort, "port", 5433, "Proxy listen port")
	rootCmd.AddCommand(serveCmd)
}

func runServe(cmd *cobra.Command, args []string) error {
	realAddr := fmt.Sprintf("%s:%d", dbHostname, dbPort)

	state := lang.NewState()
	executor := lang.NewExecutor(state)

	p := proxy.New(proxy.Config{
		ListenAddr: fmt.Sprintf(":%d", servePort),
		RealAddr:   realAddr,
		SchemaName: "autodb_shadow",
	}, state, executor)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nShutting down proxy...")
		cancel()
	}()

	return p.Start(ctx)
}
