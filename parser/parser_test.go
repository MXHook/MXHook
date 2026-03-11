package parser_test

import (
	"strings"
	"testing"

	"github.com/mxhook/mxhook/parser"
)

const sampleEML = `From: sender@example.com
To: recipient@example.com
Subject: Test email
Message-ID: <test-123@example.com>
MIME-Version: 1.0
Content-Type: text/plain

Hello, world!
`

func TestParser_ParsesBasicEmail(t *testing.T) {
	p := parser.New()
	event, err := p.Parse("msg-1", "sender@example.com", []string{"recipient@example.com"}, strings.NewReader(sampleEML))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if event.Subject != "Test email" {
		t.Errorf("Subject: got %q, want %q", event.Subject, "Test email")
	}
	if event.TextBody != "Hello, world!\n" {
		t.Errorf("TextBody: got %q", event.TextBody)
	}
	if event.MessageID != "<test-123@example.com>" {
		t.Errorf("MessageID: got %q", event.MessageID)
	}
}
