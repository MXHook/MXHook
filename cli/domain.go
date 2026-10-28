package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

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
	domain := strings.ToLower(strings.TrimSpace(args[0]))
	apiURL, _ := cmd.Flags().GetString("api-url")
	apiKey, _ := cmd.Flags().GetString("api-key")
	payload, _ := json.Marshal(map[string]string{"name": domain})
	request, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, strings.TrimRight(apiURL, "/")+"/domains", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("domain add: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("X-API-Key", apiKey)
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("domain add: %w", err)
	}
	defer func() { _ = response.Body.Close() }() // response body fully consumed or abandoned; close error not actionable
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("domain add: API returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return printf(cmd, "Registered domain: %s\n", domain)
}
