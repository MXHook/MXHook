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
	resp, err := client.Get(apiURL + "/health")
	if err != nil {
		fmt.Fprintf(cmd.OutOrStdout(), "MXHook API: UNREACHABLE (%v)\n", err)
		return nil
	}
	defer resp.Body.Close()

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	fmt.Fprintf(cmd.OutOrStdout(), "MXHook API: %s (HTTP %d)\n", body["status"], resp.StatusCode)
	return nil
}
