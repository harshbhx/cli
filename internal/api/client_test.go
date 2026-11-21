package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestClient returns a Client pointed at a test server that answers every
// request with the given status code, content type and body.
func newTestClient(t *testing.T, status int, contentType, body string) *Client {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(ClientConfig{BaseURL: server.URL})
	if err != nil {
		t.Fatalf("NewClient returned error: %v", err)
	}
	return client
}

func TestRequestNonJSONErrorBodyReportsStatusAndBody(t *testing.T) {
	// A reverse proxy with a missing upstream answers with a plain-text 404.
	client := newTestClient(t, http.StatusNotFound, "text/plain", "404 page not found\n")

	err := client.Get("/org/test", nil)
	if err == nil {
		t.Fatal("Get returned nil error for a 404 response")
	}

	msg := err.Error()
	if strings.Contains(msg, "invalid character") {
		t.Fatalf("error leaks the JSON syntax error instead of the HTTP failure: %q", msg)
	}
	for _, want := range []string{"404", "404 page not found"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}
}

func TestRequestNonJSONBadGatewayReportsStatus(t *testing.T) {
	client := newTestClient(t, http.StatusBadGateway, "text/html", "<html><body><h1>502 Bad Gateway</h1></body></html>")

	err := client.Get("/org/test", nil)
	if err == nil {
		t.Fatal("Get returned nil error for a 502 response")
	}

	msg := err.Error()
	if strings.Contains(msg, "invalid character") {
		t.Fatalf("error leaks the JSON syntax error instead of the HTTP failure: %q", msg)
	}
	if !strings.Contains(msg, "502") {
		t.Fatalf("error %q does not mention the 502 status", msg)
	}
}
