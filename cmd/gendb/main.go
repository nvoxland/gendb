package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "gendb",
	Short: "GenDB — synthetic database for development & testing",
	Long: `GenDB creates a shadow PostgreSQL database with LLM-analyzed synthetic data.
Developers can work against realistic data without production PII.`,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
