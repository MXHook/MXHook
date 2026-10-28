package api_test

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mxhook/mxhook/api"
	"github.com/mxhook/mxhook/internal/state"
	"github.com/mxhook/mxhook/queue"
	"github.com/mxhook/mxhook/types"
)

func TestPersistentAPIStateMessagesDLQAndReplay(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mxhook.db")
	key := bytes.Repeat([]byte{5}, 32)
	store := openAPIStore(t, path, key)
	server := api.NewServer(api.Config{APIKey: "api-secret"}, api.WithRepository(store), api.WithReadinessChecker(store))

	domainResponse := requestAPI(t, server, http.MethodPost, "/domains", `{"name":"Example.COM"}`, true)
	if domainResponse.Code != http.StatusCreated {
		t.Fatalf("create domain status = %d, body=%s", domainResponse.Code, domainResponse.Body.String())
	}
	routeResponse := requestAPI(t, server, http.MethodPost, "/routes",
		`{"domain":"example.com","recipient_pattern":"*@example.com","webhook_url":"https://example.net/hook","webhook_secret":"route-secret"}`, true)
	if routeResponse.Code != http.StatusCreated {
		t.Fatalf("create route status = %d, body=%s", routeResponse.Code, routeResponse.Body.String())
	}
	if strings.Contains(routeResponse.Body.String(), "route-secret") || strings.Contains(routeResponse.Body.String(), "webhook_secret") {
		t.Fatalf("route response disclosed secret: %s", routeResponse.Body.String())
	}
	rules, err := store.Load()
	if err != nil || len(rules) != 1 || rules[0].WebhookSecret != "route-secret" {
		t.Fatalf("worker route load = %#v, err=%v", rules, err)
	}
	event := &types.EmailEvent{
		SchemaVersion: types.EmailEventSchemaV1, ID: "api-message", TraceID: "trace",
		ReceivedAt: time.Now().UTC(), EnvelopeFrom: "sender@example.net", EnvelopeTo: []string{"inbox@example.com"},
		Headers: map[string][]string{}, Subject: "persistent", RawPath: "/raw/api-message.eml",
	}
	if err := store.Commit(context.Background(), event, rules); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	secondEvent := *event
	secondEvent.ID = "api-message-2"
	secondEvent.MessageID = "second@example.net"
	secondEvent.ReceivedAt = event.ReceivedAt.Add(time.Second)
	if err := store.Commit(context.Background(), &secondEvent, nil); err != nil {
		t.Fatalf("Commit second event: %v", err)
	}
	delivery, err := store.ClaimDelivery(context.Background())
	if err != nil {
		t.Fatalf("ClaimDelivery: %v", err)
	}
	if err := store.DeadDelivery(context.Background(), delivery, "receiver rejected request"); err != nil {
		t.Fatalf("DeadDelivery: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	store = openAPIStore(t, path, key)
	t.Cleanup(func() { _ = store.Close() })
	server = api.NewServer(api.Config{APIKey: "api-secret"}, api.WithRepository(store), api.WithReadinessChecker(store))
	if response := requestAPI(t, server, http.MethodGet, "/domains", "", false); response.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d, want 401", response.Code)
	}
	domains := requestAPI(t, server, http.MethodGet, "/domains", "", true)
	if domains.Code != http.StatusOK || !strings.Contains(domains.Body.String(), "example.com") {
		t.Fatalf("persistent domains response = %d %s", domains.Code, domains.Body.String())
	}
	messages := requestAPI(t, server, http.MethodGet, "/messages", "", true)
	if messages.Code != http.StatusOK || !strings.Contains(messages.Body.String(), "api-message") {
		t.Fatalf("persistent messages response = %d %s", messages.Code, messages.Body.String())
	}
	pagedMessages := requestAPI(t, server, http.MethodGet, "/messages?limit=1&offset=1", "", true)
	if pagedMessages.Code != http.StatusOK || strings.Count(pagedMessages.Body.String(), `"state"`) != 1 {
		t.Fatalf("paged messages response = %d %s", pagedMessages.Code, pagedMessages.Body.String())
	}
	dlq := requestAPI(t, server, http.MethodGet, "/dlq", "", true)
	if dlq.Code != http.StatusOK || !strings.Contains(dlq.Body.String(), "receiver rejected request") {
		t.Fatalf("persistent DLQ response = %d %s", dlq.Code, dlq.Body.String())
	}
	replay := requestAPI(t, server, http.MethodPost, "/replay/api-message", "", true)
	if replay.Code != http.StatusAccepted {
		t.Fatalf("replay response = %d %s", replay.Code, replay.Body.String())
	}
	replayed, err := store.ClaimDelivery(context.Background())
	if err != nil || replayed.Attempt != 1 || replayed.Generation != 1 {
		t.Fatalf("replayed delivery = %#v, err=%v (replay must grant a fresh attempt budget)", replayed, err)
	}
	ready := requestAPI(t, server, http.MethodGet, "/readyz", "", false)
	if ready.Code != http.StatusOK {
		t.Fatalf("readiness status = %d", ready.Code)
	}
}

func TestPersistentAPIDLQIngressFailureDelete(t *testing.T) {
	store := openAPIStore(t, filepath.Join(t.TempDir(), "mxhook.db"), bytes.Repeat([]byte{6}, 32))
	t.Cleanup(func() { _ = store.Close() })
	server := api.NewServer(api.Config{APIKey: "api-secret"}, api.WithRepository(store), api.WithReadinessChecker(store))
	message := queue.NewMessage("stale-ingress", "smtp", "", "sender@example.net",
		[]string{"inbox@example.com"}, time.Now(), []byte("Subject: x\r\n\r\nbody"))
	if err := store.RecordIngressFailure(context.Background(), message, "unparsable payload"); err != nil {
		t.Fatalf("RecordIngressFailure: %v", err)
	}
	dlq := requestAPI(t, server, http.MethodGet, "/dlq", "", true)
	if dlq.Code != http.StatusOK || !strings.Contains(dlq.Body.String(), "ingress:stale-ingress") {
		t.Fatalf("DLQ response = %d %s", dlq.Code, dlq.Body.String())
	}
	deleted := requestAPI(t, server, http.MethodDelete, "/dlq/ingress:stale-ingress", "", true)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("delete ingress DLQ entry status = %d %s, want 204", deleted.Code, deleted.Body.String())
	}
	dlq = requestAPI(t, server, http.MethodGet, "/dlq", "", true)
	if dlq.Code != http.StatusOK || strings.Contains(dlq.Body.String(), "ingress:stale-ingress") {
		t.Fatalf("DLQ still lists deleted entry: %d %s", dlq.Code, dlq.Body.String())
	}
	if again := requestAPI(t, server, http.MethodDelete, "/dlq/ingress:stale-ingress", "", true); again.Code != http.StatusNotFound {
		t.Fatalf("repeat delete status = %d, want 404", again.Code)
	}
	if dead := requestAPI(t, server, http.MethodDelete, "/dlq/delivery-id", "", true); dead.Code != http.StatusMethodNotAllowed {
		t.Fatalf("dead delivery delete status = %d, want 405 (retained; replay instead)", dead.Code)
	}
}

func openAPIStore(t *testing.T, path string, key []byte) *state.Store {
	t.Helper()
	store, err := state.Open(state.Config{
		Driver: "sqlite", DSN: path, EncryptionKey: key,
		LeaseDuration: time.Second, PollInterval: 5 * time.Millisecond, RunMigrations: true,
	})
	if err != nil {
		t.Fatalf("open state store: %v", err)
	}
	return store
}

func requestAPI(t *testing.T, server *api.Server, method, path, body string, authenticated bool) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if authenticated {
		request.Header.Set("X-API-Key", "api-secret")
	}
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	return response
}
