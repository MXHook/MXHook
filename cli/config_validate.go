package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"golang.org/x/sys/unix"
)

var configCmd = &cobra.Command{Use: "config", Short: "Inspect and validate configuration"}

var configValidateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate effective configuration without opening listeners",
	RunE:  runConfigValidate,
}

func init() {
	Root.AddCommand(configCmd)
	configCmd.AddCommand(configValidateCmd)
	configValidateCmd.Flags().Bool("json", false, "Print machine-readable output")
}

type validationSummary struct {
	Valid        bool     `json:"valid"`
	Role         string   `json:"role"`
	QueueBackend string   `json:"queue_backend"`
	Ingress      []string `json:"ingress"`
	APIEnabled   bool     `json:"api_enabled"`
	TLSRequired  bool     `json:"smtp_tls_required"`
}

func runConfigValidate(cmd *cobra.Command, _ []string) error {
	summary, err := validateEffectiveConfig()
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		if encodeErr := json.NewEncoder(cmd.OutOrStdout()).Encode(summary); encodeErr != nil {
			return fmt.Errorf("config validate: write JSON: %w", encodeErr)
		}
	} else if err == nil {
		if werr := printf(cmd, "Configuration valid: role=%s queue=%s ingress=%s api=%t tls_required=%t\n",
			summary.Role, summary.QueueBackend, strings.Join(summary.Ingress, ","), summary.APIEnabled, summary.TLSRequired); werr != nil {
			return werr
		}
	}
	return err
}

func validateEffectiveConfig() (validationSummary, error) {
	role := strings.ToLower(strings.TrimSpace(viper.GetString("runtime.role")))
	smtpAddr := strings.TrimSpace(viper.GetString("smtp.addr"))
	maildirPath := strings.TrimSpace(viper.GetString("maildir.path"))
	backend := strings.ToLower(strings.TrimSpace(viper.GetString("queue.backend")))
	if backend == "" {
		backend = backendSQLite
	}
	summary := validationSummary{
		Role: role, QueueBackend: backend,
		APIEnabled:  strings.TrimSpace(viper.GetString("api.addr")) != "",
		TLSRequired: viper.GetBool("smtp.require_tls"),
	}
	if smtpAddr != "" {
		summary.Ingress = append(summary.Ingress, "smtp")
	}
	if maildirPath != "" {
		summary.Ingress = append(summary.Ingress, "maildir")
	}
	if err := validateRuntime(role, smtpAddr, maildirPath); err != nil {
		return summary, err
	}
	// start.go refuses the API outside all and worker roles: only those
	// build the durable state the API serves.
	if role == roleEdge && summary.APIEnabled {
		return summary, errors.New("config: API requires all or worker role; set api.addr empty for edge")
	}
	switch backend {
	case backendSQLite:
		databasePath := strings.TrimSpace(viper.GetString("database.url"))
		if databasePath == "" {
			return summary, errors.New("config: SQLite database path is required")
		}
		if err := validateCreatableDir(filepath.Dir(databasePath)); err != nil {
			return summary, fmt.Errorf("config: SQLite database directory: %w", err)
		}
	case backendPostgres:
		databaseURL := viper.GetString("database.url")
		parsed, err := url.Parse(databaseURL)
		if err != nil || parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" || parsed.Host == "" {
			return summary, errors.New("config: valid Postgres database URL is required")
		}
	case backendCloudflare:
		if role != roleEdge {
			return summary, errors.New("config: Cloudflare Queue supports edge role only")
		}
		for _, key := range []string{"CF_QUEUE_ACCOUNT_ID", "CF_QUEUE_ID", "CF_QUEUE_TOKEN"} {
			if os.Getenv(key) == "" {
				return summary, fmt.Errorf("config: %s is required", key)
			}
		}
	default:
		return summary, fmt.Errorf("config: unsupported queue backend %q", backend)
	}
	if role == roleAll || role == roleWorker {
		webhookRawURL := strings.TrimSpace(viper.GetString("webhook.url"))
		webhookSecret := viper.GetString("webhook.secret")
		if (webhookRawURL == "") != (webhookSecret == "") {
			return summary, errors.New("config: static webhook URL and signing secret must be configured together")
		}
		if webhookRawURL != "" {
			webhookURL, err := url.Parse(webhookRawURL)
			if err != nil || webhookURL.Scheme != "https" || webhookURL.Hostname() == "" || webhookURL.User != nil {
				return summary, errors.New("config: webhook URL must be an HTTPS URL without user information")
			}
		}
		if _, err := decodeStateKey(viper.GetString("state.encryption_key")); err != nil {
			return summary, err
		}
		if summary.APIEnabled && viper.GetString("api.key") == "" {
			return summary, errors.New("config: API key is required when API is enabled")
		}
		// Mirror runtime.NewDeliveryWorker: zero values take constructor
		// defaults; the survivors must satisfy the same bounds.
		deliveryMaxAttempts := viper.GetInt("delivery.max_attempts")
		if deliveryMaxAttempts == 0 {
			deliveryMaxAttempts = 8
		}
		baseRetry := viper.GetDuration("delivery.base_retry")
		if baseRetry == 0 {
			baseRetry = 5 * time.Second
		}
		maxRetry := viper.GetDuration("delivery.max_retry")
		if maxRetry == 0 {
			maxRetry = time.Hour
		}
		if deliveryMaxAttempts < 1 || baseRetry <= 0 || maxRetry < baseRetry {
			return summary, errors.New("config: delivery retry bounds are invalid: max_attempts must be at least 1, base_retry positive, and max_retry at least base_retry")
		}
		// Mirror runtime.NewWorker: zero defaults, negatives refused.
		if viper.GetInt("worker.max_attempts") < 0 {
			return summary, errors.New("config: worker max attempts must not be negative")
		}
		if err := validateCreatableDir(viper.GetString("storage.raw_dir")); err != nil {
			return summary, fmt.Errorf("config: raw storage directory: %w", err)
		}
	}
	if smtpAddr != "" {
		if len(allowedDomains()) == 0 {
			return summary, errors.New("config: SMTP requires at least one recipient domain")
		}
		if viper.GetInt64("smtp.max_message_bytes") <= 0 || viper.GetInt("smtp.max_recipients") <= 0 || viper.GetInt("smtp.max_connections") <= 0 {
			return summary, errors.New("config: SMTP limits must be positive")
		}
		certificate := viper.GetString("smtp.tls_cert")
		key := viper.GetString("smtp.tls_key")
		if (certificate == "") != (key == "") || viper.GetBool("smtp.require_tls") && certificate == "" {
			return summary, errors.New("config: SMTP TLS certificate and key must be configured together; TLS-required mode needs both")
		}
		for _, path := range []string{certificate, key} {
			if path != "" {
				if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
					return summary, fmt.Errorf("config: SMTP TLS file %q is not readable and regular", path)
				}
			}
		}
		// smtp.NewServer refuses negative timeouts (zero takes defaults).
		if viper.GetDuration("smtp.read_timeout") < 0 || viper.GetDuration("smtp.write_timeout") < 0 {
			return summary, errors.New("config: SMTP timeouts must not be negative")
		}
	}
	if maildirPath != "" {
		for _, name := range []string{"new", "cur", "tmp"} {
			path := filepath.Join(maildirPath, name)
			info, err := os.Lstat(path)
			if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return summary, fmt.Errorf("config: Maildir %s must be a real directory", path)
			}
		}
		// maildir.New refuses a negative poll interval (zero takes default).
		if viper.GetDuration("maildir.poll_interval") < 0 {
			return summary, errors.New("config: Maildir poll interval must not be negative")
		}
	}
	for _, value := range []time.Duration{
		viper.GetDuration("webhook.timeout"), viper.GetDuration("worker.lease_extension"),
		viper.GetDuration("runtime.shutdown_grace_period"),
	} {
		if value <= 0 {
			return summary, errors.New("config: timeouts and lease extensions must be positive")
		}
	}
	summary.Valid = true
	return summary, nil
}

// validateCreatableDir reports, without writing, whether path is a writable
// directory or could be created by os.MkdirAll at startup: the nearest
// existing ancestor must be a writable, searchable directory. This mirrors
// storage.FilesystemStorer.Prepare and the SQLite backends, which MkdirAll
// their target directories before use.
func validateCreatableDir(path string) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("path is required")
	}
	current, err := filepath.Abs(path)
	if err != nil {
		return fmt.Errorf("resolve %q: %w", path, err)
	}
	for {
		info, statErr := os.Stat(current)
		if statErr == nil {
			if !info.IsDir() {
				return fmt.Errorf("%s is not a directory", current)
			}
			if err := unix.Access(current, unix.W_OK|unix.X_OK); err != nil {
				return fmt.Errorf("%s is not writable: %w", current, err)
			}
			return nil
		}
		if !errors.Is(statErr, fs.ErrNotExist) {
			return fmt.Errorf("check writable ancestor: %w", statErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return fmt.Errorf("check writable ancestor: %w", statErr)
		}
		current = parent
	}
}
