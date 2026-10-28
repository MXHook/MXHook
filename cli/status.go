package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show health of all MXHook components",
	RunE:  runStatus,
}

func init() {
	Root.AddCommand(statusCmd)
	statusCmd.Flags().String("api-url", "http://localhost:8080", "MXHook API URL")
}

func runStatus(cmd *cobra.Command, _ []string) error {
	apiURL, _ := cmd.Flags().GetString("api-url")

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, apiURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("status: build request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		// The unreachable error is the command result; a failed write of the
		// human-readable line adds nothing to it.
		_ = printf(cmd, "MXHook API: UNREACHABLE (%v)\n", err)
		return fmt.Errorf("status: API unreachable: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // response body fully consumed; close error not actionable

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return fmt.Errorf("status: decode response: %w", err)
	}
	if err := printf(cmd, "MXHook API: %s (HTTP %d)\n", body["status"], resp.StatusCode); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("status: API returned %d", resp.StatusCode)
	}
	return nil
}
