package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// Version is set at build time via -ldflags "-X main.Version=x.y.z".
var Version = "dev"

var (
	configFile string
)

var rootCmd = &cobra.Command{
	Use:     "gendb",
	Short:   "GenDB — synthetic database for development & testing",
	Version: Version,
	Long: `GenDB creates a synthetic PostgreSQL database with LLM-analyzed synthetic data.
Developers can work against realistic data without production PII.`,
}

func init() {
	rootCmd.PersistentFlags().StringVar(&configFile, "config", "gendb.yaml", "Path to config file")
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
