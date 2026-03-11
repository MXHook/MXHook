package cli_test

import (
	"testing"

	"github.com/mxhook/mxhook/cli"
)

func TestLogsCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range cli.Root.Commands() {
		if cmd.Use == "logs" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'logs' command to be registered")
	}
}

func TestReplayCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range cli.Root.Commands() {
		if cmd.Use == "replay <message-id>" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'replay' command to be registered")
	}
}

func TestStatusCmd_Registered(t *testing.T) {
	found := false
	for _, cmd := range cli.Root.Commands() {
		if cmd.Use == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected 'status' command to be registered")
	}
}
