package cli

import (
	"os"
	"testing"

	"github.com/mxhook/mxhook/queue"
)

func TestBuildEnqueuer_Noop_WhenEnvMissing(t *testing.T) {
	os.Unsetenv("CF_QUEUE_ACCOUNT_ID")
	os.Unsetenv("CF_QUEUE_ID")
	os.Unsetenv("CF_QUEUE_TOKEN")

	enqueuer := buildEnqueuer()
	if _, ok := enqueuer.(*queue.NoopEnqueuer); !ok {
		t.Errorf("expected *queue.NoopEnqueuer, got %T", enqueuer)
	}
}

func TestBuildEnqueuer_Noop_WhenPartialEnv(t *testing.T) {
	t.Setenv("CF_QUEUE_ACCOUNT_ID", "acct-123")
	// QueueID and Token missing → should fall back to noop
	os.Unsetenv("CF_QUEUE_ID")
	os.Unsetenv("CF_QUEUE_TOKEN")

	enqueuer := buildEnqueuer()
	if _, ok := enqueuer.(*queue.NoopEnqueuer); !ok {
		t.Errorf("expected *queue.NoopEnqueuer for partial env, got %T", enqueuer)
	}
}

func TestBuildEnqueuer_CF_WhenAllEnvSet(t *testing.T) {
	t.Setenv("CF_QUEUE_ACCOUNT_ID", "acct-123")
	t.Setenv("CF_QUEUE_ID", "queue-456")
	t.Setenv("CF_QUEUE_TOKEN", "tok-789")

	enqueuer := buildEnqueuer()
	if _, ok := enqueuer.(*queue.NoopEnqueuer); ok {
		t.Error("expected non-noop enqueuer when all CF env vars are set")
	}
	if enqueuer == nil {
		t.Error("expected non-nil enqueuer")
	}
}
