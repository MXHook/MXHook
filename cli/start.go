package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mxhook/mxhook/queue"
	cfqueue "github.com/mxhook/mxhook/queue/cloudflare"
	"github.com/mxhook/mxhook/smtp"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start MXHook services (SMTP receiver + worker)",
	RunE:  runStart,
}

func init() {
	Root.AddCommand(startCmd)
	startCmd.Flags().String("smtp-addr", "0.0.0.0:25", "SMTP listen address")
	startCmd.Flags().String("smtp-domain", "localhost", "SMTP server domain")
	startCmd.Flags().String("api-addr", "0.0.0.0:8080", "API listen address")
	startCmd.Flags().String("db-url", "", "Postgres connection string")
	viper.BindPFlag("smtp.addr", startCmd.Flags().Lookup("smtp-addr"))
	viper.BindPFlag("smtp.domain", startCmd.Flags().Lookup("smtp-domain"))
	viper.BindPFlag("api.addr", startCmd.Flags().Lookup("api-addr"))
	viper.BindPFlag("database.url", startCmd.Flags().Lookup("db-url"))
}

func runStart(cmd *cobra.Command, _ []string) error {
	smtpAddr := viper.GetString("smtp.addr")
	smtpDomain := viper.GetString("smtp.domain")
	apiAddr := viper.GetString("api.addr")

	enqueuer := buildEnqueuer()

	smtpCfg := smtp.Config{
		Addr:            smtpAddr,
		Domain:          smtpDomain,
		MaxMessageBytes: 10 * 1024 * 1024,
		MaxRecipients:   50,
	}
	srv := smtp.NewServer(smtpCfg, enqueuer)

	fmt.Fprintf(cmd.OutOrStdout(), "Starting MXHook SMTP on %s, API on %s\n", smtpAddr, apiAddr)

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil {
			errCh <- err
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-quit:
		slog.Info("start: shutting down", "signal", sig)
		return srv.Close()
	case err := <-errCh:
		return fmt.Errorf("start: smtp: %w", err)
	}
}

// buildEnqueuer returns a Cloudflare Queue publisher when env vars are set,
// otherwise falls back to a no-op enqueuer.
func buildEnqueuer() queue.Enqueuer {
	accountID := os.Getenv("CF_QUEUE_ACCOUNT_ID")
	queueID := os.Getenv("CF_QUEUE_ID")
	token := os.Getenv("CF_QUEUE_TOKEN")

	if accountID == "" || queueID == "" || token == "" {
		slog.Warn("start: CF_QUEUE_* env vars not set; using noop enqueuer")
		return &queue.NoopEnqueuer{}
	}

	slog.Info("start: using Cloudflare Queue publisher", "queue_id", queueID)
	return cfqueue.New(cfqueue.Config{
		AccountID: accountID,
		QueueID:   queueID,
		Token:     token,
	})
}
