package api_test

import (
	"net/http"

	"github.com/mxhook/mxhook/api"
)

// testAPIKey is the credential every control-plane test authenticates with.
// The control plane fails closed without a configured key, so handler tests
// must supply one to reach the handler under test.
const testAPIKey = "test-api-key"

// newTestServer builds a server whose control routes are reachable with
// testAPIKey. Use authed to attach the credential to each request.
func newTestServer(opts ...api.Option) *api.Server {
	return api.NewServer(api.Config{APIKey: testAPIKey}, opts...)
}

// authed attaches the test credential and returns the request for chaining.
func authed(req *http.Request) *http.Request {
	req.Header.Set("X-API-Key", testAPIKey)
	return req
}
