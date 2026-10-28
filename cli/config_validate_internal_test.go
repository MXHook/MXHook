package cli

import (
	"bytes"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/viper"
)

// validBaseSettings returns a complete configuration that must validate for
// the all role. Tests override individual keys to probe one invariant each.
func validBaseSettings(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"runtime.role":                  "all",
		"queue.backend":                 "sqlite",
		"database.url":                  filepath.Join(t.TempDir(), "mxhook.db"),
		"storage.raw_dir":               filepath.Join(t.TempDir(), "raw"),
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
}

func applySettings(t *testing.T, settings map[string]any) {
	t.Helper()
	for key, value := range settings {
		viper.Set(key, value)
	}
	t.Cleanup(func() {
		for key := range settings {
			viper.Set(key, nil)
		}
	})
}

func maildirRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, name := range []string{"new", "cur", "tmp"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func readOnlyDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) }) //nolint:gosec // G302: directory mode; the file threshold does not apply to os.Chmod on a directory
	return dir
}

func TestValidateEffectiveConfigMirrorsStartInvariants(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission probes are meaningless as root")
	}
	cases := []struct {
		name     string
		override func(t *testing.T, settings map[string]any)
		wantErr  string
	}{
		{
			name: "edge role with API enabled",
			override: func(_ *testing.T, settings map[string]any) {
				settings["runtime.role"] = "edge"
				settings["api.addr"] = "127.0.0.1:8080"
			},
			wantErr: "API requires all or worker role",
		},
		{
			name: "edge role without API is valid",
			override: func(_ *testing.T, settings map[string]any) {
				settings["runtime.role"] = "edge"
				settings["api.addr"] = ""
			},
		},
		{
			name: "delivery max retry below base retry",
			override: func(_ *testing.T, settings map[string]any) {
				settings["delivery.base_retry"] = time.Minute
				settings["delivery.max_retry"] = time.Second
			},
			wantErr: "delivery retry",
		},
		{
			name: "negative delivery max attempts",
			override: func(_ *testing.T, settings map[string]any) {
				settings["delivery.max_attempts"] = -1
			},
			wantErr: "delivery retry",
		},
		{
			name: "negative delivery base retry",
			override: func(_ *testing.T, settings map[string]any) {
				settings["delivery.base_retry"] = -time.Second
			},
			wantErr: "delivery retry",
		},
		{
			name: "negative worker max attempts",
			override: func(_ *testing.T, settings map[string]any) {
				settings["worker.max_attempts"] = -1
			},
			wantErr: "worker max attempts",
		},
		{
			name: "negative SMTP read timeout",
			override: func(_ *testing.T, settings map[string]any) {
				settings["smtp.read_timeout"] = -time.Second
			},
			wantErr: "SMTP timeouts",
		},
		{
			name: "negative SMTP write timeout",
			override: func(_ *testing.T, settings map[string]any) {
				settings["smtp.write_timeout"] = -time.Second
			},
			wantErr: "SMTP timeouts",
		},
		{
			name: "negative Maildir poll interval",
			override: func(t *testing.T, settings map[string]any) {
				settings["maildir.path"] = maildirRoot(t)
				settings["maildir.poll_interval"] = -time.Second
			},
			wantErr: "poll interval",
		},
		{
			name: "raw storage under unwritable directory",
			override: func(t *testing.T, settings map[string]any) {
				settings["storage.raw_dir"] = filepath.Join(readOnlyDir(t), "raw")
			},
			wantErr: "raw storage",
		},
		{
			name: "raw storage path occupied by a file",
			override: func(t *testing.T, settings map[string]any) {
				path := filepath.Join(t.TempDir(), "raw")
				if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				settings["storage.raw_dir"] = path
			},
			wantErr: "raw storage",
		},
		{
			name: "sqlite database under unwritable parent",
			override: func(t *testing.T, settings map[string]any) {
				settings["database.url"] = filepath.Join(readOnlyDir(t), "data", "mxhook.db")
			},
			wantErr: "database directory",
		},
		{
			name: "sqlite database parent is a file",
			override: func(t *testing.T, settings map[string]any) {
				parent := filepath.Join(t.TempDir(), "data")
				if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				settings["database.url"] = filepath.Join(parent, "mxhook.db")
			},
			wantErr: "database directory",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			settings := validBaseSettings(t)
			tc.override(t, settings)
			applySettings(t, settings)
			summary, err := validateEffectiveConfig()
			if tc.wantErr == "" {
				if err != nil || !summary.Valid {
					t.Fatalf("expected valid config, got %#v, %v", summary, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validation passed but start would refuse (%s)", tc.name)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error %q does not mention %q", err, tc.wantErr)
			}
		})
	}
}
