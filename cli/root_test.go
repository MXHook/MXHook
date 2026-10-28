package cli_test

import (
	"testing"

	"github.com/mxhook/mxhook/cli"
)

func TestRoot_HasCorrectUse(t *testing.T) {
	if cli.Root.Use != "mxhook" {
		t.Errorf("expected Use=mxhook, got %q", cli.Root.Use)
	}
}
