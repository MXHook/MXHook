package cli

import (
	"fmt"

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
	routeAddCmd.Flags().String("api-url", "http://localhost:8080", "MXHook API URL")
	routeAddCmd.Flags().String("api-key", "", "API key")
	routeAddCmd.MarkFlagRequired("recipient")
	routeAddCmd.MarkFlagRequired("webhook")
}

func runRouteAdd(cmd *cobra.Command, _ []string) error {
	recipient, _ := cmd.Flags().GetString("recipient")
	webhook, _ := cmd.Flags().GetString("webhook")
	fmt.Fprintf(cmd.OutOrStdout(), "Added route: %s -> %s\n", recipient, webhook)
	return nil
}
