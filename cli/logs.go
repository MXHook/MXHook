package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	messageID, _ := cmd.Flags().GetString("message-id")

	endpoint := strings.TrimRight(apiURL, "/") + "/messages"
	if messageID != "" {
		endpoint += "/" + url.PathEscape(messageID)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("logs: build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("logs: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // response body fully consumed or abandoned; close error not actionable
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("logs: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	decoder := json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024))
	if messageID != "" {
		var message map[string]any
		if err := decoder.Decode(&message); err != nil {
			return fmt.Errorf("logs: decode: %w", err)
		}
		return printf(cmd, "%v\n", message)
	}

	var messages []map[string]any
	if err := decoder.Decode(&messages); err != nil {
		return fmt.Errorf("logs: decode: %w", err)
	}

	if len(messages) == 0 {
		return printf(cmd, "No messages found.\n")
	}
	for _, m := range messages {
		if err := printf(cmd, "%v\n", m); err != nil {
			return err
		}
	}
	return nil
}
