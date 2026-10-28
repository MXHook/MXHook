package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"

	"github.com/mxhook/mxhook/queue/cloudflare"
	queuesqlite "github.com/mxhook/mxhook/queue/sqlite"
	"github.com/mxhook/mxhook/router"
)

func TestBuildEnqueuerDefaultsToSQLite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mxhook.db")
	viper.Set("queue.backend", "sqlite")
	viper.Set("database.url", path)
	t.Cleanup(func() {
		viper.Set("queue.backend", nil)
		viper.Set("database.url", nil)
	})

	enqueuer, err := buildEnqueuer(context.Background())
	if err != nil {
		t.Fatalf("buildEnqueuer: %v", err)
	}
	adapter, ok := enqueuer.(*queuesqlite.Adapter)
	if !ok {
		t.Fatalf("enqueuer = %T, want SQLite", enqueuer)
	}
	if err := adapter.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("SQLite database not created: %v", err)
	}
}

func TestBuildEnqueuerRejectsPartialCloudflareConfig(t *testing.T) {
	viper.Set("queue.backend", "cloudflare")
	t.Cleanup(func() { viper.Set("queue.backend", nil) })
	t.Setenv("CF_QUEUE_ACCOUNT_ID", "acct-123")
	for _, name := range []string{"CF_QUEUE_ID", "CF_QUEUE_TOKEN"} {
		t.Setenv(name, "") // registers restore of the caller's value
		if err := os.Unsetenv(name); err != nil {
			t.Fatalf("unset %s: %v", name, err)
		}
	}

	if _, err := buildEnqueuer(context.Background()); err == nil {
		t.Fatal("buildEnqueuer accepted partial Cloudflare configuration")
	}
}

func TestBuildEnqueuerCloudflareWhenConfigured(t *testing.T) {
	viper.Set("queue.backend", "cloudflare")
	t.Cleanup(func() { viper.Set("queue.backend", nil) })
	t.Setenv("CF_QUEUE_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_QUEUE_ID", "queue-456")
	t.Setenv("CF_QUEUE_TOKEN", "tok-789")

	enqueuer, err := buildEnqueuer(context.Background())
	if err != nil {
		t.Fatalf("buildEnqueuer: %v", err)
	}
	if _, ok := enqueuer.(*cloudflare.Publisher); !ok {
		t.Fatalf("enqueuer = %T, want Cloudflare", enqueuer)
	}
}

func TestBuildEnqueuerRejectsUnknownBackend(t *testing.T) {
	viper.Set("queue.backend", "mystery")
	t.Cleanup(func() { viper.Set("queue.backend", nil) })
	if _, err := buildEnqueuer(context.Background()); err == nil {
		t.Fatal("buildEnqueuer accepted unknown backend")
	}
}

func TestValidateIngress(t *testing.T) {
	if err := validateIngress("", ""); err == nil {
		t.Fatal("validateIngress accepted no ingress source")
	}
	if err := validateIngress("127.0.0.1:2525", ""); err != nil {
		t.Fatalf("SMTP ingress rejected: %v", err)
	}
	if err := validateIngress("", "/maildir"); err != nil {
		t.Fatalf("Maildir ingress rejected: %v", err)
	}
}

func TestValidateConfiguredRoutesRequiresRouteBeforeIngress(t *testing.T) {
	if err := validateConfiguredRoutes("all", nil); err == nil {
		t.Fatal("all role accepted zero routes")
	}
	if err := validateConfiguredRoutes("worker", nil); err != nil {
		t.Fatalf("worker bootstrap rejected zero routes: %v", err)
	}
	if err := validateConfiguredRoutes("all", []router.Rule{{ID: "route"}}); err != nil {
		t.Fatalf("all role rejected configured route: %v", err)
	}
}

func TestMaildirEnvironmentKeyMapping(t *testing.T) {
	t.Setenv("MXHOOK_MAILDIR_PATH", "/srv/Maildir")
	if got := viper.GetString("maildir.path"); got != "/srv/Maildir" {
		t.Fatalf("maildir.path = %q, want environment value", got)
	}
}

func TestComposeUsesCanonicalAllowedDomainsEnvironmentKey(t *testing.T) {
	compose, err := os.ReadFile("../docker-compose.yml")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(compose, []byte("MXHOOK_SMTP_ALLOWED_DOMAINS")) ||
		bytes.Contains(compose, []byte("MXHOOK_RECIPIENT_DOMAIN")) {
		t.Fatal("Compose must use MXHOOK_SMTP_ALLOWED_DOMAINS")
	}
}

func TestAllowedDomainsEnvironmentKeyMapping(t *testing.T) {
	t.Setenv("MXHOOK_SMTP_ALLOWED_DOMAINS", "example.com")
	domains := viper.GetStringSlice("smtp.allowed_domains")
	if len(domains) != 1 || domains[0] != "example.com" {
		t.Fatalf("smtp.allowed_domains = %#v", domains)
	}
}

func TestAllowedDomainsEnvironmentSeparators(t *testing.T) {
	cases := []struct {
		name  string
		value string
	}{
		{"comma", "example.com,example.net"},
		{"comma and space", "example.com, example.net"},
		{"space", "example.com example.net"},
	}
	want := []string{"example.com", "example.net"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MXHOOK_SMTP_ALLOWED_DOMAINS", tc.value)
			config := smtpConfigFromViper("127.0.0.1:2525", "mx.example.com")
			if !reflect.DeepEqual(config.AllowedDomains, want) {
				t.Fatalf("AllowedDomains = %#v, want %#v", config.AllowedDomains, want)
			}
		})
	}
}

func TestValidateEffectiveConfigRejectsCommaOnlyAllowedDomains(t *testing.T) {
	settings := map[string]any{
		"runtime.role":                  "all",
		"queue.backend":                 "sqlite",
		"database.url":                  filepath.Join(t.TempDir(), "mxhook.db"),
		"smtp.addr":                     "127.0.0.1:2525",
		"smtp.max_message_bytes":        int64(1024),
		"smtp.max_recipients":           5,
		"smtp.max_connections":          10,
		"webhook.url":                   "https://hooks.example.com/mxhook",
		"webhook.secret":                "signing-secret",
		"webhook.timeout":               5 * time.Second,
		"worker.lease_extension":        30 * time.Second,
		"runtime.shutdown_grace_period": 30 * time.Second,
		"state.encryption_key":          base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
		"api.addr":                      "127.0.0.1:8080",
		"api.key":                       "api-secret",
		"maildir.path":                  "",
	}
	for key, value := range settings {
		viper.Set(key, value)
		defer viper.Set(key, nil)
	}
	t.Setenv("MXHOOK_SMTP_ALLOWED_DOMAINS", " , ")
	if _, err := validateEffectiveConfig(); err == nil || !strings.Contains(err.Error(), "recipient domain") {
		t.Fatalf("comma-only allowed domains error = %v", err)
	}
}

func TestEmptySMTPEnvironmentDisablesSMTP(t *testing.T) {
	t.Setenv("MXHOOK_SMTP_ADDR", "")
	if got := viper.GetString("smtp.addr"); got != "" {
		t.Fatalf("smtp.addr = %q, want empty environment value", got)
	}
}

func TestSMTPConfigMapsEveryProductionSetting(t *testing.T) {
	settings := map[string]any{
		"smtp.max_message_bytes": int64(12345),
		"smtp.max_recipients":    17,
		"smtp.allowed_domains":   []string{"example.com", "example.net"},
		"smtp.tls_cert":          "/cert.pem",
		"smtp.tls_key":           "/key.pem",
		"smtp.require_tls":       true,
		"smtp.read_timeout":      11 * time.Second,
		"smtp.write_timeout":     13 * time.Second,
		"smtp.max_connections":   23,
	}
	for key, value := range settings {
		viper.Set(key, value)
		defer viper.Set(key, nil)
	}
	config := smtpConfigFromViper("127.0.0.1:2525", "mx.example.com")
	if config.Addr != "127.0.0.1:2525" || config.Domain != "mx.example.com" ||
		config.MaxMessageBytes != 12345 || config.MaxRecipients != 17 ||
		len(config.AllowedDomains) != 2 || config.AllowedDomains[1] != "example.net" ||
		config.TLSCertFile != "/cert.pem" || config.TLSKeyFile != "/key.pem" || !config.RequireTLS ||
		config.ReadTimeout != 11*time.Second || config.WriteTimeout != 13*time.Second || config.MaxConnections != 23 {
		t.Fatalf("SMTP config mapping = %#v", config)
	}
}

func TestWebhookConfigFromViperWiresPolicyOptIns(t *testing.T) {
	viper.Set("webhook.timeout", 7*time.Second)
	t.Cleanup(func() { viper.Set("webhook.timeout", nil) })

	config := webhookConfigFromViper()
	if config.AllowHTTP || config.AllowPrivateNetwork {
		t.Fatalf("policy opt-ins must default off: %#v", config)
	}
	if config.MaxAttempts != 1 || config.TimeoutSecs != 7 {
		t.Fatalf("webhook config = %#v", config)
	}

	t.Setenv("MXHOOK_WEBHOOK_ALLOW_HTTP", "true")
	t.Setenv("MXHOOK_WEBHOOK_ALLOW_PRIVATE_NETWORK", "true")
	config = webhookConfigFromViper()
	if !config.AllowHTTP || !config.AllowPrivateNetwork {
		t.Fatalf("policy opt-ins not wired from environment: %#v", config)
	}
}

func TestDecodeStateKey(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32))
	if key, err := decodeStateKey(encoded); err != nil || len(key) != 32 {
		t.Fatalf("decodeStateKey valid result = %x, %v", key, err)
	}
	for _, invalid := range []string{"", "not-base64", base64.StdEncoding.EncodeToString([]byte("short"))} {
		if _, err := decodeStateKey(invalid); err == nil {
			t.Fatalf("decodeStateKey accepted %q", invalid)
		}
	}
}

func TestValidateEffectiveProductionConfig(t *testing.T) {
	settings := map[string]any{
		"runtime.role":                  "all",
		"queue.backend":                 "sqlite",
		"database.url":                  filepath.Join(t.TempDir(), "mxhook.db"),
		"smtp.addr":                     "127.0.0.1:2525",
		"smtp.allowed_domains":          []string{"example.com"},
		"smtp.max_message_bytes":        int64(1024),
		"smtp.max_recipients":           5,
		"smtp.max_connections":          10,
		"webhook.url":                   "https://hooks.example.com/mxhook",
		"webhook.secret":                "signing-secret",
		"webhook.timeout":               5 * time.Second,
		"worker.lease_extension":        30 * time.Second,
		"runtime.shutdown_grace_period": 30 * time.Second,
		"state.encryption_key":          base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
		"api.addr":                      "127.0.0.1:8080",
		"api.key":                       "api-secret",
		"maildir.path":                  "",
	}
	for key, value := range settings {
		viper.Set(key, value)
		defer viper.Set(key, nil)
	}
	summary, err := validateEffectiveConfig()
	if err != nil || !summary.Valid || summary.QueueBackend != "sqlite" || len(summary.Ingress) != 1 {
		t.Fatalf("validation result = %#v, %v", summary, err)
	}
	viper.Set("webhook.secret", "")
	if _, err := validateEffectiveConfig(); err == nil {
		t.Fatal("validation accepted missing webhook secret")
	}
}

func TestValidateEffectiveConfigAllowsRoutesToBeConfiguredThroughAPI(t *testing.T) {
	settings := map[string]any{
		"runtime.role":                  "all",
		"queue.backend":                 "sqlite",
		"database.url":                  filepath.Join(t.TempDir(), "mxhook.db"),
		"smtp.addr":                     "127.0.0.1:2525",
		"smtp.allowed_domains":          []string{"example.com"},
		"smtp.max_message_bytes":        int64(1024),
		"smtp.max_recipients":           5,
		"smtp.max_connections":          10,
		"webhook.url":                   "",
		"webhook.secret":                "",
		"webhook.timeout":               5 * time.Second,
		"worker.lease_extension":        30 * time.Second,
		"runtime.shutdown_grace_period": 30 * time.Second,
		"state.encryption_key":          base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
		"api.addr":                      "127.0.0.1:8080",
		"api.key":                       "api-secret",
		"maildir.path":                  "",
	}
	for key, value := range settings {
		viper.Set(key, value)
		defer viper.Set(key, nil)
	}
	summary, err := validateEffectiveConfig()
	if err != nil || !summary.Valid {
		t.Fatalf("validation result = %#v, %v", summary, err)
	}
}

func TestValidateEffectiveConfigRejectsPartialStaticWebhook(t *testing.T) {
	settings := map[string]any{
		"runtime.role":                  "all",
		"queue.backend":                 "sqlite",
		"database.url":                  filepath.Join(t.TempDir(), "mxhook.db"),
		"smtp.addr":                     "127.0.0.1:2525",
		"smtp.allowed_domains":          []string{"example.com"},
		"smtp.max_message_bytes":        int64(1024),
		"smtp.max_recipients":           5,
		"smtp.max_connections":          10,
		"webhook.url":                   "https://hooks.example.com/mxhook",
		"webhook.secret":                "",
		"webhook.timeout":               5 * time.Second,
		"worker.lease_extension":        30 * time.Second,
		"runtime.shutdown_grace_period": 30 * time.Second,
		"state.encryption_key":          base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{2}, 32)),
		"api.addr":                      "127.0.0.1:8080",
		"api.key":                       "api-secret",
	}
	for key, value := range settings {
		viper.Set(key, value)
		defer viper.Set(key, nil)
	}
	if _, err := validateEffectiveConfig(); err == nil || !strings.Contains(err.Error(), "configured together") {
		t.Fatalf("partial static webhook error = %v", err)
	}
}

// The edge role runs ingress without durable state, so validate must accept a
// configuration that carries no state key or API key, and start must reach the
// same verdict rather than diverging at runtime.
func TestEdgeRoleValidationMatchesStartRequirements(t *testing.T) {
	settings := map[string]any{
		"runtime.role":                  "edge",
		"queue.backend":                 "sqlite",
		"database.url":                  filepath.Join(t.TempDir(), "mxhook.db"),
		"smtp.addr":                     "127.0.0.1:2525",
		"smtp.max_message_bytes":        int64(1024),
		"smtp.max_recipients":           5,
		"smtp.max_connections":          10,
		"smtp.allowed_domains":          []string{"example.com"},
		"webhook.timeout":               5 * time.Second,
		"worker.lease_extension":        30 * time.Second,
		"runtime.shutdown_grace_period": 30 * time.Second,
		"maildir.path":                  "",
		// Deliberately absent: state.encryption_key and api.key. The edge role
		// runs no worker and no API, so neither is required.
		"api.addr":             "",
		"state.encryption_key": "",
		"api.key":              "",
	}
	for key, value := range settings {
		viper.Set(key, value)
		defer viper.Set(key, nil)
	}

	summary, err := validateEffectiveConfig()
	if err != nil {
		t.Fatalf("validateEffectiveConfig for edge role: %v", err)
	}
	if summary.Role != "edge" {
		t.Fatalf("summary role = %q, want edge", summary.Role)
	}
	if summary.APIEnabled {
		t.Fatal("API reported enabled with an empty listen address")
	}

	// start applies the same runtime rule for the role/ingress combination.
	if err := validateRuntime("edge", "127.0.0.1:2525", ""); err != nil {
		t.Fatalf("validateRuntime disagrees with validateEffectiveConfig: %v", err)
	}
}

// A worker role must refuse ingress, and both entry points must agree on that.
func TestWorkerRoleRejectsIngressInBothValidators(t *testing.T) {
	if err := validateRuntime("worker", "127.0.0.1:2525", ""); err == nil {
		t.Fatal("validateRuntime accepted SMTP ingress for worker role")
	}
	if err := validateRuntime("worker", "", "/maildir"); err == nil {
		t.Fatal("validateRuntime accepted Maildir ingress for worker role")
	}
	if err := validateRuntime("worker", "", ""); err != nil {
		t.Fatalf("validateRuntime rejected zero-ingress worker: %v", err)
	}
}
