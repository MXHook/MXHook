package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"
)

var logsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Tail message lifecycle log",
	RunE:  runLogs,
}

func init() {
	Root.AddCommand(logsCmd)
	logsCmd.Flags().String("api-url", "http://localhost:8080", "MXHook API URL")
	logsCmd.Flags().String("api-key", "", "API key")
	logsCmd.Flags().String("message-id", "", "Filter by message ID")
}

func runLogs(cmd *cobra.Command, _ []string) error {
	apiURL, _ := cmd.Flags().GetString("api-url")
	apiKey, _ := cmd.Flags().GetString("api-key")

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, apiURL+"/messages", nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("logs: %w", err)
	}
	defer resp.Body.Close()

	var messages []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&messages); err != nil {
		return fmt.Errorf("logs: decode: %w", err)
	}

	if len(messages) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "No messages found.")
		return nil
	}
	for _, m := range messages {
		fmt.Fprintf(cmd.OutOrStdout(), "%v\n", m)
	}
	return nil
}
