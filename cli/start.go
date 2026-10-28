package cli

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/mxhook/mxhook/api"
	"github.com/mxhook/mxhook/internal/metrics"
	mxruntime "github.com/mxhook/mxhook/internal/runtime"
	"github.com/mxhook/mxhook/internal/state"
	"github.com/mxhook/mxhook/maildir"
	"github.com/mxhook/mxhook/parser"
	"github.com/mxhook/mxhook/queue"
	cfqueue "github.com/mxhook/mxhook/queue/cloudflare"
	pgqueue "github.com/mxhook/mxhook/queue/postgres"
	sqlitequeue "github.com/mxhook/mxhook/queue/sqlite"
	"github.com/mxhook/mxhook/router"
	"github.com/mxhook/mxhook/smtp"
	"github.com/mxhook/mxhook/storage"
	"github.com/mxhook/mxhook/webhook"
)

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start MXHook ingress and durable queue publication",
	RunE:  runStart,
}

func init() {
	Root.AddCommand(startCmd)
	startCmd.Flags().String("smtp-addr", "0.0.0.0:25", "SMTP listen address")
	startCmd.Flags().String("smtp-domain", "localhost", "SMTP server domain")
	startCmd.Flags().String("api-addr", "0.0.0.0:8080", "API listen address")
	startCmd.Flags().String("api-key", "", "API key required for control-plane routes")
	startCmd.Flags().String("runtime-role", roleAll, "Runtime role: all, edge, or worker")
	startCmd.Flags().StringSlice("recipient-domain", nil, "Accepted recipient domain; repeat for multiple domains")
	startCmd.Flags().Int64("smtp-max-message-bytes", 10*1024*1024, "Maximum SMTP message size")
	startCmd.Flags().Int("smtp-max-recipients", 50, "Maximum SMTP recipients per message")
	startCmd.Flags().String("smtp-tls-cert", "", "PEM certificate for SMTP STARTTLS")
	startCmd.Flags().String("smtp-tls-key", "", "PEM private key for SMTP STARTTLS")
	startCmd.Flags().Bool("smtp-require-tls", false, "Reject SMTP MAIL before STARTTLS")
	startCmd.Flags().Duration("smtp-read-timeout", 60*time.Second, "SMTP connection read timeout")
	startCmd.Flags().Duration("smtp-write-timeout", 60*time.Second, "SMTP connection write timeout")
	startCmd.Flags().Int("smtp-max-connections", 1000, "Maximum concurrent SMTP connections")
	startCmd.Flags().String("raw-dir", "./data/raw", "Durable raw-message directory")
	startCmd.Flags().String("webhook-url", "", "Webhook destination for accepted recipient domains")
	startCmd.Flags().String("webhook-secret", "", "Webhook HMAC secret")
	startCmd.Flags().Duration("webhook-timeout", 10*time.Second, "Timeout for one webhook attempt")
	startCmd.Flags().Int("worker-max-attempts", 5, "Maximum durable processing attempts before terminal nack")
	startCmd.Flags().Duration("worker-lease-extension", 30*time.Second, "Visibility lease extension while processing")
	startCmd.Flags().Int("delivery-max-attempts", 8, "Maximum durable webhook attempts")
	startCmd.Flags().Duration("delivery-base-retry", 5*time.Second, "Initial durable webhook retry delay")
	startCmd.Flags().Duration("delivery-max-retry", time.Hour, "Maximum durable webhook retry delay")
	startCmd.Flags().String("state-encryption-key", "", "Base64-encoded 32-byte state encryption key")
	startCmd.Flags().Duration("retention-ttl", 0, "Delete terminal state rows and raw files older than this age; 0 disables")
	startCmd.Flags().Duration("retention-interval", time.Hour, "Retention sweep interval")
	startCmd.Flags().Bool("migrate", true, "Apply supported database migrations at startup")
	startCmd.Flags().Duration("shutdown-grace-period", 30*time.Second, "Maximum worker shutdown grace period")
	startCmd.Flags().String("maildir", "", "Maildir root containing new, cur, and tmp")
	startCmd.Flags().Duration("maildir-poll-interval", 2*time.Second, "Maildir recovery scan interval")
	startCmd.Flags().Int64("maildir-max-message-bytes", 10*1024*1024, "Maximum Maildir message size")
	startCmd.Flags().String("maildir-recipient-header", "Delivered-To", "Maildir envelope recipient header")
	startCmd.Flags().String("maildir-default-recipient", "", "Fallback recipient when the header is absent")
	startCmd.Flags().String("maildir-id-namespace", "", "Stable namespace for Maildir delivery IDs")
	startCmd.Flags().Bool("maildir-adopt-existing", false, "Adopt a nonempty Maildir cur directory")
	startCmd.Flags().String("queue-backend", backendSQLite, "Queue backend: sqlite, postgres, or cloudflare")
	startCmd.Flags().String("db-url", "./data/mxhook.db", "SQLite path or Postgres connection string")
	bindStartFlags(map[string]string{
		"smtp.addr":                     "smtp-addr",
		"smtp.domain":                   "smtp-domain",
		"api.addr":                      "api-addr",
		"api.key":                       "api-key",
		"runtime.role":                  "runtime-role",
		"smtp.allowed_domains":          "recipient-domain",
		"smtp.max_message_bytes":        "smtp-max-message-bytes",
		"smtp.max_recipients":           "smtp-max-recipients",
		"smtp.tls_cert":                 "smtp-tls-cert",
		"smtp.tls_key":                  "smtp-tls-key",
		"smtp.require_tls":              "smtp-require-tls",
		"smtp.read_timeout":             "smtp-read-timeout",
		"smtp.write_timeout":            "smtp-write-timeout",
		"smtp.max_connections":          "smtp-max-connections",
		"storage.raw_dir":               "raw-dir",
		"webhook.url":                   "webhook-url",
		"webhook.secret":                "webhook-secret",
		"webhook.timeout":               "webhook-timeout",
		"worker.max_attempts":           "worker-max-attempts",
		"worker.lease_extension":        "worker-lease-extension",
		"delivery.max_attempts":         "delivery-max-attempts",
		"delivery.base_retry":           "delivery-base-retry",
		"delivery.max_retry":            "delivery-max-retry",
		"state.encryption_key":          "state-encryption-key",
		"retention.ttl":                 "retention-ttl",
		"retention.interval":            "retention-interval",
		"database.migrate":              "migrate",
		"runtime.shutdown_grace_period": "shutdown-grace-period",
		"maildir.path":                  "maildir",
		"maildir.poll_interval":         "maildir-poll-interval",
		"maildir.max_message_bytes":     "maildir-max-message-bytes",
		"maildir.recipient_header":      "maildir-recipient-header",
		"maildir.default_recipient":     "maildir-default-recipient",
		"maildir.id_namespace":          "maildir-id-namespace",
		"maildir.adopt_existing":        "maildir-adopt-existing",
		"queue.backend":                 "queue-backend",
		"database.url":                  "db-url",
	})
}

// bindStartFlags binds viper keys to start flags. A failed bind means a flag
// name in the table is wrong, a programming error caught at process start.
func bindStartFlags(bindings map[string]string) {
	for key, flag := range bindings {
		if err := viper.BindPFlag(key, startCmd.Flags().Lookup(flag)); err != nil {
			panic(fmt.Sprintf("cli: bind %s to --%s: %v", key, flag, err))
		}
	}
}

func runStart(cmd *cobra.Command, _ []string) (retErr error) {
	role := strings.ToLower(strings.TrimSpace(viper.GetString("runtime.role")))
	smtpAddr := viper.GetString("smtp.addr")
	smtpDomain := viper.GetString("smtp.domain")
	maildirPath := viper.GetString("maildir.path")
	if err := validateRuntime(role, smtpAddr, maildirPath); err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(cmd.Context())
	defer cancel()
	enqueuer, err := buildEnqueuer(ctx)
	if err != nil {
		return err
	}
	defer closeEnqueuer(enqueuer)

	var worker *mxruntime.Worker
	var deliveryWorker *mxruntime.DeliveryWorker
	var stateStore *state.Store
	var webhookDeliverer *webhook.Deliverer
	var retentionJanitor *state.Janitor
	if role == roleAll || role == roleWorker {
		dequeuer, ok := enqueuer.(queue.Dequeuer)
		if !ok {
			return fmt.Errorf("start: queue backend %q cannot run a worker", viper.GetString("queue.backend"))
		}
		webhookURL := strings.TrimSpace(viper.GetString("webhook.url"))
		webhookSecret := viper.GetString("webhook.secret")
		if (webhookURL == "") != (webhookSecret == "") {
			return errors.New("start: static webhook URL and signing secret must be configured together")
		}
		encryptionKey, err := decodeStateKey(viper.GetString("state.encryption_key"))
		if err != nil {
			return err
		}
		driver := strings.ToLower(viper.GetString("queue.backend"))
		if driver == "" {
			driver = backendSQLite
		}
		stateStore, err = state.Open(state.Config{
			Driver: driver, DSN: viper.GetString("database.url"), EncryptionKey: encryptionKey,
			LeaseDuration: viper.GetDuration("worker.lease_extension"),
			RunMigrations: viper.GetBool("database.migrate"),
		})
		if err != nil {
			return fmt.Errorf("start: state database: %w", err)
		}
		defer func() {
			if cerr := stateStore.Close(); cerr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("start: close state database: %w", cerr))
			}
		}()
		webhookDeliverer = webhook.NewDeliverer(webhookConfigFromViper())
		if webhookURL != "" {
			rules := staticWebhookRules(allowedDomains(), webhookURL, webhookSecret)
			for _, rule := range rules {
				if err := webhookDeliverer.ValidateEndpoint(ctx, webhook.Endpoint{URL: rule.WebhookURL, Secret: rule.WebhookSecret}); err != nil {
					return fmt.Errorf("start: webhook target: %w", err)
				}
				if err := stateStore.EnsureRoute(ctx, rule); err != nil {
					return fmt.Errorf("start: configure route: %w", err)
				}
			}
		}
		configuredRules, err := stateStore.Load()
		if err != nil {
			return fmt.Errorf("start: load configured routes: %w", err)
		}
		if err := validateConfiguredRoutes(role, configuredRules); err != nil {
			return err
		}
		rawStore := storage.NewFilesystemStorer(viper.GetString("storage.raw_dir"))
		if err := rawStore.Prepare(); err != nil {
			return fmt.Errorf("start: raw storage: %w", err)
		}
		if ttl := viper.GetDuration("retention.ttl"); ttl > 0 {
			retentionJanitor, err = state.NewJanitor(stateStore, rawStore, state.RetentionConfig{
				TTL:      ttl,
				Interval: viper.GetDuration("retention.interval"),
			})
			if err != nil {
				return fmt.Errorf("start: retention janitor: %w", err)
			}
		}
		processor, err := mxruntime.NewIntakeProcessor(
			parser.New(),
			rawStore,
			stateStore,
			stateStore,
		)
		if err != nil {
			return fmt.Errorf("start: processor: %w", err)
		}
		if source, ok := dequeuer.(state.QueueDepthSource); ok {
			stateStore.SetQueueDepthSource(source)
		}
		worker, err = mxruntime.NewWorker(dequeuer, processor, mxruntime.WorkerConfig{
			MaxAttempts:    viper.GetInt("worker.max_attempts"),
			LeaseExtension: viper.GetDuration("worker.lease_extension"),
			FailureSink:    stateStore,
		})
		if err != nil {
			return fmt.Errorf("start: worker: %w", err)
		}
		deliveryWorker, err = mxruntime.NewDeliveryWorker(stateStore, webhookDeliverer, mxruntime.DeliveryWorkerConfig{
			MaxAttempts:    viper.GetInt("delivery.max_attempts"),
			LeaseExtension: viper.GetDuration("worker.lease_extension"),
			BaseRetry:      viper.GetDuration("delivery.base_retry"),
			MaxRetry:       viper.GetDuration("delivery.max_retry"),
		})
		if err != nil {
			return fmt.Errorf("start: delivery worker: %w", err)
		}
	}

	var smtpServer *smtp.Server
	if smtpAddr != "" {
		var smtpOpts []smtp.Option
		if stateStore != nil {
			// Recipient acceptance consults the durable domain registry, so a
			// domain added through the control plane is accepted without a
			// restart. Statically configured domains remain accepted.
			smtpOpts = append(smtpOpts, smtp.WithDomainSource(stateStore))
		}
		smtpServer, err = smtp.NewServer(smtpConfigFromViper(smtpAddr, smtpDomain), enqueuer, smtpOpts...)
		if err != nil {
			return fmt.Errorf("start: %w", err)
		}
	}

	var maildirIngester *maildir.Ingester
	if maildirPath != "" {
		maildirIngester, err = maildir.New(maildir.Config{
			Path:             maildirPath,
			PollInterval:     viper.GetDuration("maildir.poll_interval"),
			MaxMessageBytes:  viper.GetInt64("maildir.max_message_bytes"),
			RecipientHeader:  viper.GetString("maildir.recipient_header"),
			DefaultRecipient: viper.GetString("maildir.default_recipient"),
			IDNamespace:      viper.GetString("maildir.id_namespace"),
			AdoptExisting:    viper.GetBool("maildir.adopt_existing"),
		}, enqueuer)
		if err != nil {
			return fmt.Errorf("start: %w", err)
		}
		defer func() {
			if err := maildirIngester.Close(); err != nil {
				slog.Error("start: close Maildir", "err", err)
			}
		}()
	}

	var apiServer *api.Server
	apiAddr := strings.TrimSpace(viper.GetString("api.addr"))
	if apiAddr != "" {
		if stateStore == nil {
			return errors.New("start: API requires all or worker role with durable state")
		}
		apiKey := viper.GetString("api.key")
		if apiKey == "" {
			return errors.New("start: API key is required when API is enabled")
		}
		apiOpts := []api.Option{
			api.WithRepository(stateStore), api.WithReadinessChecker(stateStore),
			api.WithMetricsHandler(metrics.HandlerWithSnapshot(stateStore)),
			api.WithEndpointValidator(webhookDeliverer),
		}
		if smtpServer != nil {
			// Same-process domain writes take effect on the next recipient
			// check; other processes converge within the cache TTL.
			apiOpts = append(apiOpts, api.WithDomainInvalidator(smtpServer))
		}
		apiServer = api.NewServer(api.Config{ListenAddr: apiAddr, APIKey: apiKey}, apiOpts...)
	}

	var sources []string
	if smtpServer != nil {
		sources = append(sources, "SMTP "+smtpAddr)
	}
	if maildirIngester != nil {
		sources = append(sources, "Maildir "+maildirPath)
	}
	banner := fmt.Sprintf("Starting MXHook role %s", role)
	if len(sources) > 0 {
		banner += ": " + strings.Join(sources, ", ")
	}
	if err := printf(cmd, "%s\n", banner); err != nil {
		return err
	}

	// One slot per potential sender: worker, API, delivery worker, SMTP, and
	// Maildir. Every send also selects on ctx.Done, so a smaller buffer would
	// not deadlock, but sizing it to the sender count keeps a component's
	// first error from being dropped while the shutdown path runs.
	errCh := make(chan error, 5)
	var ingress sync.WaitGroup
	if worker != nil {
		ingress.Add(1)
		go func() {
			defer ingress.Done()
			if err := worker.Run(ctx); err != nil {
				select {
				case errCh <- fmt.Errorf("worker: %w", err):
				case <-ctx.Done():
				}
			}
		}()
	}
	if apiServer != nil {
		ingress.Add(1)
		go func() {
			defer ingress.Done()
			if err := apiServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				select {
				case errCh <- fmt.Errorf("API: %w", err):
				case <-ctx.Done():
				}
			}
		}()
	}
	if deliveryWorker != nil {
		ingress.Add(1)
		go func() {
			defer ingress.Done()
			if err := deliveryWorker.Run(ctx); err != nil {
				select {
				case errCh <- fmt.Errorf("delivery worker: %w", err):
				case <-ctx.Done():
				}
			}
		}()
	}
	if retentionJanitor != nil {
		ingress.Add(1)
		go func() {
			defer ingress.Done()
			retentionJanitor.Run(ctx)
		}()
	}
	if smtpServer != nil {
		ingress.Add(1)
		go func() {
			defer ingress.Done()
			if err := smtpServer.ListenAndServe(); err != nil {
				select {
				case errCh <- fmt.Errorf("smtp: %w", err):
				case <-ctx.Done():
				}
			}
		}()
	}
	if maildirIngester != nil {
		ingress.Add(1)
		go func() {
			defer ingress.Done()
			if err := maildirIngester.Run(ctx); err != nil {
				select {
				case errCh <- fmt.Errorf("maildir: %w", err):
				case <-ctx.Done():
				}
			}
		}()
	}

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(quit)
	select {
	case sig := <-quit:
		slog.Info("start: shutting down", "signal", sig)
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), viper.GetDuration("runtime.shutdown_grace_period"))
		defer shutdownCancel()
		if apiServer != nil {
			_ = apiServer.Shutdown(shutdownCtx)
		}
		var closeErr error
		if smtpServer != nil {
			if err := smtpServer.Shutdown(shutdownCtx); err != nil {
				closeErr = fmt.Errorf("start: smtp shutdown: %w", err)
			}
		}
		cancel()
		ingress.Wait()
		return closeErr
	case err := <-errCh:
		if apiServer != nil {
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			_ = apiServer.Shutdown(shutdownCtx)
			shutdownCancel()
		}
		cancel()
		if smtpServer != nil {
			_ = smtpServer.Close()
		}
		ingress.Wait()
		return fmt.Errorf("start: component: %w", err)
	}
}

// allowedDomains resolves smtp.allowed_domains with comma-separated entries
// split apart, surrounding whitespace trimmed, and empty values dropped. The
// environment path hands viper one raw string split on whitespace only, so a
// documented value such as "example.com,example.net" arrives as one entry.
func allowedDomains() []string {
	entries := viper.GetStringSlice("smtp.allowed_domains")
	domains := make([]string, 0, len(entries))
	for _, entry := range entries {
		for _, domain := range strings.Split(entry, ",") {
			if domain = strings.TrimSpace(domain); domain != "" {
				domains = append(domains, domain)
			}
		}
	}
	return domains
}

func smtpConfigFromViper(addr, domain string) smtp.Config {
	return smtp.Config{
		Addr: addr, Domain: domain,
		MaxMessageBytes: viper.GetInt64("smtp.max_message_bytes"),
		MaxRecipients:   viper.GetInt("smtp.max_recipients"),
		AllowedDomains:  allowedDomains(),
		TLSCertFile:     viper.GetString("smtp.tls_cert"),
		TLSKeyFile:      viper.GetString("smtp.tls_key"),
		RequireTLS:      viper.GetBool("smtp.require_tls"),
		ReadTimeout:     viper.GetDuration("smtp.read_timeout"),
		WriteTimeout:    viper.GetDuration("smtp.write_timeout"),
		MaxConnections:  viper.GetInt("smtp.max_connections"),
	}
}

func decodeStateKey(encoded string) ([]byte, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, errors.New("start: state encryption key is required for all and worker roles")
	}
	key, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("start: state encryption key must be valid base64")
	}
	if len(key) != 32 {
		return nil, errors.New("start: state encryption key must decode to exactly 32 bytes")
	}
	return key, nil
}

func validateRuntime(role, smtpAddr, maildirPath string) error {
	switch role {
	case roleAll, roleEdge:
		if strings.TrimSpace(smtpAddr) == "" && strings.TrimSpace(maildirPath) == "" {
			return errors.New("start: enable SMTP, Maildir, or both")
		}
	case roleWorker:
		if strings.TrimSpace(smtpAddr) != "" || strings.TrimSpace(maildirPath) != "" {
			return errors.New("start: worker role cannot enable ingress")
		}
	default:
		return fmt.Errorf("start: unsupported runtime role %q", role)
	}
	return nil
}

func validateIngress(smtpAddr, maildirPath string) error {
	return validateRuntime(roleAll, smtpAddr, maildirPath)
}

func validateConfiguredRoutes(role string, rules []router.Rule) error {
	if role == roleAll && len(rules) == 0 {
		return errors.New("start: configure at least one durable or static route before enabling ingress")
	}
	return nil
}

func staticWebhookRules(domains []string, url, secret string) []router.Rule {
	if len(domains) == 0 {
		return []router.Rule{{
			ID: "configured-webhook", RecipientPattern: "*", Target: router.TargetWebhook,
			WebhookURL: url, WebhookSecret: secret, Priority: -1000,
		}}
	}
	rules := make([]router.Rule, 0, len(domains))
	for _, domain := range domains {
		domain = strings.ToLower(strings.TrimSpace(domain))
		if domain == "" {
			continue
		}
		rules = append(rules, router.Rule{
			ID:     "configured-webhook-" + domain,
			Domain: domain, RecipientPattern: "*@" + domain, Target: router.TargetWebhook,
			WebhookURL: url, WebhookSecret: secret,
			Priority: -1000,
		})
	}
	return rules
}

// webhookConfigFromViper builds the delivery policy for the runtime deliverer.
// webhook.allow_http and webhook.allow_private_network are dangerous opt-ins
// that weaken SSRF protection; they default off and must be enabled
// explicitly, e.g. for a receiver on an internal Docker network.
func webhookConfigFromViper() webhook.Config {
	return webhook.Config{
		MaxAttempts:         1,
		TimeoutSecs:         durationSeconds(viper.GetDuration("webhook.timeout")),
		AllowHTTP:           viper.GetBool("webhook.allow_http"),
		AllowPrivateNetwork: viper.GetBool("webhook.allow_private_network"),
	}
}

func durationSeconds(duration time.Duration) int {
	seconds := int(duration / time.Second)
	if seconds < 1 {
		return 1
	}
	return seconds
}

func buildEnqueuer(ctx context.Context) (queue.Enqueuer, error) {
	switch backend := strings.ToLower(viper.GetString("queue.backend")); backend {
	case "", backendSQLite:
		adapter, err := sqlitequeue.NewWithConfig(ctx, sqlitequeue.Config{
			Path:          viper.GetString("database.url"),
			RunMigrations: viper.GetBool("database.migrate"),
		})
		if err != nil {
			return nil, fmt.Errorf("start: sqlite queue: %w", err)
		}
		return adapter, nil
	case backendPostgres:
		dsn := viper.GetString("database.url")
		if dsn == "" {
			return nil, errors.New("start: Postgres database URL is required")
		}
		adapter, err := pgqueue.NewWithConfig(ctx, pgqueue.Config{
			DSN:           dsn,
			RunMigrations: viper.GetBool("database.migrate"),
		})
		if err != nil {
			return nil, fmt.Errorf("start: postgres queue: %w", err)
		}
		return adapter, nil
	case backendCloudflare:
		accountID := os.Getenv("CF_QUEUE_ACCOUNT_ID")
		queueID := os.Getenv("CF_QUEUE_ID")
		token := os.Getenv("CF_QUEUE_TOKEN")
		if accountID == "" || queueID == "" || token == "" {
			return nil, errors.New("start: CF_QUEUE_ACCOUNT_ID, CF_QUEUE_ID, and CF_QUEUE_TOKEN are required")
		}
		publisher, err := cfqueue.New(cfqueue.Config{
			AccountID: accountID,
			QueueID:   queueID,
			Token:     token,
		})
		if err != nil {
			return nil, fmt.Errorf("start: Cloudflare queue: %w", err)
		}
		return publisher, nil
	default:
		return nil, fmt.Errorf("start: unsupported queue backend %q", backend)
	}
}

func closeEnqueuer(enqueuer queue.Enqueuer) {
	switch closer := enqueuer.(type) {
	case interface{ Close() error }:
		if err := closer.Close(); err != nil {
			slog.Error("start: close queue", "err", err)
		}
	case interface{ Close() }:
		closer.Close()
	}
}
