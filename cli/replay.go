package cli

import (
	"fmt"
	"net/http"
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
	req, err := http.NewRequest(http.MethodPost, apiURL+"/replay/"+messageID, nil)
	if err != nil {
		return err
	}
	if apiKey != "" {
		req.Header.Set("X-API-Key", apiKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	defer resp.Body.Close()

	fmt.Fprintf(cmd.OutOrStdout(), "Replay queued for message %s (status %d)\n", messageID, resp.StatusCode)
	return nil
}
