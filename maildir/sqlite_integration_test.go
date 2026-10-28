package maildir

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"
	"time"

	sqlitequeue "github.com/mxhook/mxhook/queue/sqlite"
)

func TestMaildirEnqueuesIntoSQLite(t *testing.T) {
	root := newTestMaildir(t)
	writeMessage(t, root, "new", "sqlite-delivery", testMessage)

	adapter, err := sqlitequeue.New(context.Background(), filepath.Join(t.TempDir(), "mxhook.db"))
	if err != nil {
		t.Fatalf("sqlite New: %v", err)
	}
	defer func() { _ = adapter.Close() }()

	ingester, err := New(Config{Path: root, IDNamespace: "integration"}, adapter)
	if err != nil {
		t.Fatalf("maildir New: %v", err)
	}
	if processed, err := ingester.ProcessOnce(context.Background()); err != nil || processed != 1 {
		t.Fatalf("ProcessOnce = (%d, %v), want (1, nil)", processed, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	delivery, err := adapter.Claim(ctx)
	if err != nil {
		t.Fatalf("Claim: %v", err)
	}
	msg := delivery.Message
	if !bytes.Equal(msg.Body, []byte(testMessage)) {
		t.Error("SQLite queue changed raw Maildir bytes")
	}
	if len(msg.EnvelopeTo) != 1 || msg.EnvelopeTo[0] != "inbox@mxhook.exe.xyz" || msg.Source != "maildir" {
		t.Errorf("envelope = %#v", msg)
	}
}
