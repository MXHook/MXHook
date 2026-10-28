package cli

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

var replayCmd = &cobra.Command{
	Use:   "replay <message-id>",
	Short: "Re-deliver a message by ID",
	Args:  cobra.ExactArgs(1),
	RunE:  runReplay,
}

func init() {
	Root.AddCommand(replayCmd)
	replayCmd.Flags().String("api-url", "http://localhost:8080", "MXHook API URL")
	replayCmd.Flags().String("api-key", "", "API key")
}

func runReplay(cmd *cobra.Command, args []string) error {
	messageID := args[0]
	apiURL, _ := cmd.Flags().GetString("api-url")
	apiKey, _ := cmd.Flags().GetString("api-key")

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, strings.TrimRight(apiURL, "/")+"/replay/"+url.PathEscape(messageID), nil)
	if err != nil {
		return fmt.Errorf("replay: build request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	defer func() { _ = resp.Body.Close() }() // response body fully consumed or abandoned; close error not actionable
	if resp.StatusCode != http.StatusAccepted {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("replay: API returned %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	return printf(cmd, "Replay queued for message %s (status %d)\n", messageID, resp.StatusCode)
}
