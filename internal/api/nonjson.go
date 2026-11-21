package api

import (
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

// maxBodySnippetLen caps how much of an unexpected response body is echoed
// back in an error message.
const maxBodySnippetLen = 200

// NonJSONResponseError is returned when the server answers with a body that
// is not the JSON envelope the API normally sends, for example the plain-text
// "404 page not found" of a reverse proxy with a missing upstream, an HTML
// 502 page, or a captive portal.
//
// It is deliberately not an *ErrorResponse: that type carries an error the
// Pangolin API itself reported, and callers branch on its Status (for
// instance to treat a 404 as "resource does not exist"). A response from
// something in front of the API says nothing about the resource.
type NonJSONResponseError struct {
	// StatusCode is the HTTP status of the response.
	StatusCode int
	// Snippet is a shortened, single-line copy of the response body.
	Snippet string
	// Err is the JSON decoding error.
	Err error
}

// Error implements the error interface.
func (e *NonJSONResponseError) Error() string {
	status := fmt.Sprintf("HTTP %d", e.StatusCode)
	if text := http.StatusText(e.StatusCode); text != "" {
		status += " " + text
	}

	if e.StatusCode >= 200 && e.StatusCode < 300 {
		if e.Snippet == "" {
			return fmt.Sprintf("unexpected response (%s): body is not valid JSON", status)
		}
		return fmt.Sprintf("unexpected response (%s): body is not valid JSON: %s", status, e.Snippet)
	}

	if e.Snippet == "" {
		return status
	}
	return fmt.Sprintf("%s: %s", status, e.Snippet)
}

// Unwrap returns the underlying JSON decoding error.
func (e *NonJSONResponseError) Unwrap() error {
	return e.Err
}

// newNonJSONResponseError builds a NonJSONResponseError for a response whose
// body could not be decoded as JSON.
func newNonJSONResponseError(statusCode int, body []byte, err error) *NonJSONResponseError {
	return &NonJSONResponseError{
		StatusCode: statusCode,
		Snippet:    bodySnippet(body),
		Err:        err,
	}
}

// bodySnippet returns a short, printable, single-line version of body:
// invalid UTF-8 is dropped, runs of whitespace (including the newlines of an
// HTML page) collapse to one space, and the result is cut to
// maxBodySnippetLen bytes on a rune boundary.
func bodySnippet(body []byte) string {
	s := strings.ToValidUTF8(string(body), "")
	s = strings.Join(strings.Fields(s), " ")

	if len(s) <= maxBodySnippetLen {
		return s
	}

	cut := maxBodySnippetLen
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}
