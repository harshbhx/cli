package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
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

func TestRequestNonJSONSuccessStatusIsAnError(t *testing.T) {
	// A captive portal can answer 200 with a login page.
	client := newTestClient(t, http.StatusOK, "text/html", "<html>\n  <body>Please sign in</body>\n</html>\n")

	err := client.Get("/org/test", nil)
	if err == nil {
		t.Fatal("Get returned nil error for a non-JSON 200 response")
	}

	msg := err.Error()
	for _, want := range []string{"HTTP 200", "not valid JSON", "<html> <body>Please sign in</body> </html>"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q does not mention %q", msg, want)
		}
	}
}

func TestRequestNonJSONErrorIsNotAnAPIErrorResponse(t *testing.T) {
	// Callers treat *ErrorResponse{Status: 404} as "the resource does not
	// exist"; a proxy's 404 page must not be mistaken for that.
	client := newTestClient(t, http.StatusNotFound, "text/plain", "404 page not found")

	err := client.Get("/org/test", nil)

	var apiErr *ErrorResponse
	if errors.As(err, &apiErr) {
		t.Fatalf("non-JSON response was reported as *ErrorResponse: %v", err)
	}

	var nonJSON *NonJSONResponseError
	if !errors.As(err, &nonJSON) {
		t.Fatalf("error %T is not a *NonJSONResponseError: %v", err, err)
	}
	if nonJSON.StatusCode != http.StatusNotFound {
		t.Fatalf("StatusCode = %d, want %d", nonJSON.StatusCode, http.StatusNotFound)
	}
	if nonJSON.Unwrap() == nil {
		t.Fatal("the JSON decoding error was not kept for errors.Unwrap")
	}
}

func TestRequestJSONErrorBodyStillReturnsAPIErrorResponse(t *testing.T) {
	client := newTestClient(t, http.StatusNotFound, "application/json",
		`{"success":false,"error":true,"message":"Org not found","status":404}`)

	err := client.Get("/org/test", nil)

	var apiErr *ErrorResponse
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %T is not a *ErrorResponse: %v", err, err)
	}
	if apiErr.Status != http.StatusNotFound || apiErr.Message != "Org not found" {
		t.Fatalf("got %+v, want status 404 and message %q", apiErr, "Org not found")
	}
}

func TestRequestEmptyBodyIsStillAccepted(t *testing.T) {
	client := newTestClient(t, http.StatusOK, "", "")

	if err := client.Get("/org/test", nil); err != nil {
		t.Fatalf("Get returned error for an empty body: %v", err)
	}
}

func TestLoginAndDeviceAuthReportNonJSONResponses(t *testing.T) {
	client := newTestClient(t, http.StatusNotFound, "text/plain", "404 page not found")

	calls := map[string]func() error{
		"LoginWithCookie": func() error {
			_, _, err := LoginWithCookie(client, LoginRequest{Email: "a@example.com", Password: "pw"})
			return err
		},
		"StartDeviceWebAuth": func() error {
			_, err := StartDeviceWebAuth(client, DeviceWebAuthStartRequest{})
			return err
		},
		"PollDeviceWebAuth": func() error {
			_, _, err := PollDeviceWebAuth(client, "code")
			return err
		},
	}

	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()
			if err == nil {
				t.Fatal("call returned nil error for a 404 response")
			}
			if strings.Contains(err.Error(), "invalid character") {
				t.Fatalf("error leaks the JSON syntax error: %q", err.Error())
			}
			if !strings.Contains(err.Error(), "HTTP 404") {
				t.Fatalf("error %q does not mention HTTP 404", err.Error())
			}
		})
	}
}

func TestBodySnippet(t *testing.T) {
	long := strings.Repeat("a", maxBodySnippetLen+50)
	// "é" is two bytes, so a cut at an odd offset would split a rune.
	multibyte := strings.Repeat("é", maxBodySnippetLen)

	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"trims surrounding whitespace", "\n  404 page not found\n", "404 page not found"},
		{"collapses newlines and runs of spaces", "<h1>\n\t502   Bad Gateway\n</h1>", "<h1> 502 Bad Gateway </h1>"},
		{"drops invalid utf-8", "bad\xff\xfebytes", "badbytes"},
		{"truncates long bodies", long, strings.Repeat("a", maxBodySnippetLen) + "..."},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bodySnippet([]byte(tt.in)); got != tt.want {
				t.Fatalf("bodySnippet(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	t.Run("never splits a multibyte rune", func(t *testing.T) {
		got := bodySnippet([]byte(multibyte))
		if !utf8.ValidString(got) {
			t.Fatalf("bodySnippet returned invalid UTF-8: %q", got)
		}
		if !strings.HasSuffix(got, "...") {
			t.Fatalf("bodySnippet(%d bytes) = %q, want a truncated result", len(multibyte), got)
		}
	})
}
