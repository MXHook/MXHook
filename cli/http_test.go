package cli_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mxhook/mxhook/cli"
	"github.com/spf13/cobra"
)

// ---- status command ----

func TestStatusCmd_Reachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/health" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"status", "--api-url", srv.URL})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "ok") {
		t.Errorf("expected 'ok' in output, got: %q", out)
	}
	if !strings.Contains(out, "200") {
		t.Errorf("expected HTTP status 200 in output, got: %q", out)
	}
}

func TestStatusCmd_Unreachable(t *testing.T) {
	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	// Port 1 is unlikely to be listening; connection refused → UNREACHABLE path
	cli.Root.SetArgs([]string{"status", "--api-url", "http://127.0.0.1:1"})
	if err := cli.Root.Execute(); err == nil {
		t.Fatal("status command reported unreachable API as success")
	}

	out := buf.String()
	if !strings.Contains(out, "UNREACHABLE") {
		t.Errorf("expected UNREACHABLE in output, got: %q", out)
	}
}

// ---- logs command ----

func TestLogsCmd_WithMessages(t *testing.T) {
	messages := []map[string]any{
		{"id": "msg-1", "subject": "Hello"},
		{"id": "msg-2", "subject": "World"},
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(messages)
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"logs", "--api-url", srv.URL})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "msg-1") {
		t.Errorf("expected 'msg-1' in output, got: %q", out)
	}
}

func TestLogsCmd_EmptyMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"logs", "--api-url", srv.URL})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "No messages found") {
		t.Errorf("expected 'No messages found' in output, got: %q", out)
	}
}

func TestLogsCmd_WithAPIKey(t *testing.T) {
	var receivedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-API-Key")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"logs", "--api-url", srv.URL, "--api-key", "test-key-123"})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if receivedKey != "test-key-123" {
		t.Errorf("expected X-API-Key='test-key-123', got %q", receivedKey)
	}
}

func TestLogsCmd_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"logs", "--api-url", srv.URL})
	err := cli.Root.Execute()
	// Command returns an error on bad JSON decode
	if err == nil {
		t.Error("expected error for bad JSON response")
	}
}

func resetLogsMessageIDFlag(t *testing.T) {
	t.Helper()
	t.Cleanup(func() {
		for _, sub := range cli.Root.Commands() {
			if sub.Use == "logs" {
				_ = sub.Flags().Set("message-id", "")
			}
		}
	})
}

func TestLogsCmd_MessageIDFetchesSingleMessage(t *testing.T) {
	resetLogsMessageIDFlag(t)
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/messages/msg-42" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "msg-42", "subject": "Target"})
			return
		}
		_ = json.NewEncoder(w).Encode([]map[string]any{{"id": "other-message"}})
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"logs", "--api-url", srv.URL, "--message-id", "msg-42"})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if requestedPath != "/messages/msg-42" {
		t.Errorf("requested path = %q, want /messages/msg-42", requestedPath)
	}
	out := buf.String()
	if !strings.Contains(out, "msg-42") {
		t.Errorf("expected 'msg-42' in output, got: %q", out)
	}
	if strings.Contains(out, "other-message") {
		t.Errorf("expected filtered output, got full listing: %q", out)
	}
}

func TestLogsCmd_TrailingSlashAPIURL(t *testing.T) {
	resetLogsMessageIDFlag(t)
	var requestedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode([]map[string]any{})
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"logs", "--api-url", srv.URL + "/"})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if requestedPath != "/messages" {
		t.Errorf("requested path = %q, want /messages", requestedPath)
	}
}

// ---- replay command ----

func TestReplayCmd_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/replay/abc-123") {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"replay", "abc-123", "--api-url", srv.URL})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "abc-123") {
		t.Errorf("expected message ID in output, got: %q", out)
	}
	if !strings.Contains(out, "202") {
		t.Errorf("expected status 202 in output, got: %q", out)
	}
}

func TestReplayCmd_WithAPIKey(t *testing.T) {
	var receivedKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"replay", "msg-456", "--api-url", srv.URL, "--api-key", "my-key"})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if receivedKey != "my-key" {
		t.Errorf("expected X-API-Key='my-key', got %q", receivedKey)
	}
}

func TestReplayCmd_MissingArg(t *testing.T) {
	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"replay"})
	err := cli.Root.Execute()
	if err == nil {
		t.Error("expected error when message-id arg is missing")
	}
}

// ---- domain command ----

func TestDomainAdd_MissingArg(t *testing.T) {
	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"domain", "add"})
	err := cli.Root.Execute()
	if err == nil {
		t.Error("expected error when domain arg is missing")
	}
}

func TestDomainCmd_SubcommandHelp(t *testing.T) {
	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetErr(buf)
	cli.Root.SetArgs([]string{"domain", "--help"})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("help returned error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "domain") {
		t.Errorf("expected 'domain' in help output, got: %q", out)
	}
}

// ---- route command ----

func TestRouteCmd_FlagsRegistered(t *testing.T) {
	// Verify that --recipient and --webhook flags are registered on route add.
	var routeAdd *cobra.Command
	for _, sub := range cli.Root.Commands() {
		if sub.Use == "route" {
			for _, s := range sub.Commands() {
				if s.Use == "add" {
					routeAdd = s
				}
			}
		}
	}
	if routeAdd == nil {
		t.Fatal("route add command not found")
	}
	if routeAdd.Flags().Lookup("recipient") == nil {
		t.Error("expected --recipient flag on route add")
	}
	if routeAdd.Flags().Lookup("webhook") == nil {
		t.Error("expected --webhook flag on route add")
	}
	if routeAdd.Flags().Lookup("secret") == nil {
		t.Error("expected --secret flag on route add")
	}
}
