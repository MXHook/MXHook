package cli_test

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mxhook/mxhook/cli"
)

func TestDomainAdd_PrintsDomain(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }))
	defer server.Close()
	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetArgs([]string{"domain", "add", "example.com", "--api-url", server.URL})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("example.com")) {
		t.Errorf("expected output to contain 'example.com', got: %q", buf.String())
	}
}

func TestRouteAdd_PrintsRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusCreated) }))
	defer server.Close()
	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetArgs([]string{"route", "add", "--recipient", "*@example.com", "--webhook", "https://x.com/hook", "--secret", "test-secret", "--api-url", server.URL})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("*@example.com")) {
		t.Errorf("expected output to contain '*@example.com', got: %q", buf.String())
	}
}
