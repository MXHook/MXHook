package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var domainCmd = &cobra.Command{
	Use:   "domain",
	Short: "Manage domains",
}

var domainAddCmd = &cobra.Command{
	Use:   "add <domain>",
	Short: "Register a domain for email ingestion",
	Args:  cobra.ExactArgs(1),
	RunE:  runDomainAdd,
}

func init() {
	Root.AddCommand(domainCmd)
	domainCmd.AddCommand(domainAddCmd)
	domainAddCmd.Flags().String("api-url", "http://localhost:8080", "MXHook API URL")
	domainAddCmd.Flags().String("api-key", "", "API key")
}

func runDomainAdd(cmd *cobra.Command, args []string) error {
	domain := args[0]
	fmt.Fprintf(cmd.OutOrStdout(), "Registered domain: %s\n", domain)
	return nil
}
