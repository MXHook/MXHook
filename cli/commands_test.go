package cli_test

import (
	"bytes"
	"testing"

	"github.com/mxhook/mxhook/cli"
)

func TestDomainAdd_PrintsDomain(t *testing.T) {
	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetArgs([]string{"domain", "add", "example.com"})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("example.com")) {
		t.Errorf("expected output to contain 'example.com', got: %q", buf.String())
	}
}

func TestRouteAdd_PrintsRoute(t *testing.T) {
	buf := &bytes.Buffer{}
	cli.Root.SetOut(buf)
	cli.Root.SetArgs([]string{"route", "add", "--recipient", "*@example.com", "--webhook", "https://x.com/hook"})
	if err := cli.Root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("*@example.com")) {
		t.Errorf("expected output to contain '*@example.com', got: %q", buf.String())
	}
}
