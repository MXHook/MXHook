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

var routeCmd = &cobra.Command{
	Use:   "route",
	Short: "Manage routing rules",
}

var routeAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a routing rule",
	RunE:  runRouteAdd,
}

func init() {
	Root.AddCommand(routeCmd)
	routeCmd.AddCommand(routeAddCmd)
	routeAddCmd.Flags().String("recipient", "", "Recipient pattern (e.g. *@example.com)")
	routeAddCmd.Flags().String("webhook", "", "Webhook URL")
	routeAddCmd.Flags().String("secret", "", "Webhook HMAC signing secret")
	routeAddCmd.Flags().String("domain", "", "Recipient domain (derived from pattern when omitted)")
	routeAddCmd.Flags().String("api-url", "http://localhost:8080", "MXHook API URL")
	routeAddCmd.Flags().String("api-key", "", "API key")
	for _, name := range []string{"recipient", "webhook", "secret"} {
		if err := routeAddCmd.MarkFlagRequired(name); err != nil {
			// Only fails for an undefined flag: a programming error.
			panic(fmt.Sprintf("cli: mark --%s required: %v", name, err))
		}
	}
}

func runRouteAdd(cmd *cobra.Command, _ []string) error {
	recipient, _ := cmd.Flags().GetString("recipient")
	webhookURL, _ := cmd.Flags().GetString("webhook")
	secret, _ := cmd.Flags().GetString("secret")
	domain, _ := cmd.Flags().GetString("domain")
	if domain == "" {
		if separator := strings.LastIndexByte(recipient, '@'); separator >= 0 {
			domain = recipient[separator+1:]
		}
	}
	if strings.TrimSpace(domain) == "" {
		return fmt.Errorf("route add: domain cannot be derived from recipient pattern")
	}
	apiURL, _ := cmd.Flags().GetString("api-url")
	apiKey, _ := cmd.Flags().GetString("api-key")
	payload, _ := json.Marshal(map[string]string{
		"domain": strings.ToLower(domain), "recipient_pattern": recipient,
		"webhook_url": webhookURL, "webhook_secret": secret,
	})
	request, err := http.NewRequestWithContext(cmd.Context(), http.MethodPost, strings.TrimRight(apiURL, "/")+"/routes", bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("route add: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		request.Header.Set("X-API-Key", apiKey)
	}
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return fmt.Errorf("route add: %w", err)
	}
	defer func() { _ = response.Body.Close() }() // response body fully consumed or abandoned; close error not actionable
	if response.StatusCode != http.StatusCreated {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
		return fmt.Errorf("route add: API returned %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return printf(cmd, "Added route: %s -> %s\n", recipient, webhookURL)
}
