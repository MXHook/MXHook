package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start MXHook services (SMTP receiver + worker)",
	RunE:  runStart,
}

func init() {
	Root.AddCommand(startCmd)
	startCmd.Flags().String("smtp-addr", "0.0.0.0:25", "SMTP listen address")
	startCmd.Flags().String("api-addr", "0.0.0.0:8080", "API listen address")
	startCmd.Flags().String("db-url", "", "Postgres connection string")
	viper.BindPFlag("smtp.addr", startCmd.Flags().Lookup("smtp-addr"))
	viper.BindPFlag("api.addr", startCmd.Flags().Lookup("api-addr"))
	viper.BindPFlag("database.url", startCmd.Flags().Lookup("db-url"))
}

func runStart(cmd *cobra.Command, _ []string) error {
	smtpAddr := viper.GetString("smtp.addr")
	apiAddr := viper.GetString("api.addr")
	fmt.Fprintf(cmd.OutOrStdout(), "Starting MXHook SMTP on %s, API on %s\n", smtpAddr, apiAddr)
	// Full wiring happens in a later integration task
	select {}
}
