package pushover

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	// ErrInvalidRequest is returned when a request is rejected before it is sent because its input is invalid, such as
	// a missing required parameter or a value that exceeds a documented Pushover limit. Pushover temporarily blocks IP
	// addresses that send too many invalid requests, so these are caught locally where possible.
	ErrInvalidRequest = errors.New("pushover: invalid request")
	// ErrQuotaExceeded matches an *APIError with errors.Is when Pushover rejected a message because the account's
	// monthly message limit was reached (HTTP 429). Requests will keep failing until the limit resets, see
	// APIError.Limits.
	ErrQuotaExceeded = errors.New("pushover: monthly message limit reached")
)

// APIError is returned when the Pushover API responds with an error. Use errors.As to access it.
type APIError struct {
	// StatusCode is the HTTP status code of the response.
	StatusCode int
	// Status is the "status" field of the JSON response body. Pushover uses 1 for success.
	Status int
	// Request is the unique request identifier assigned by Pushover. Include it when contacting Pushover support.
	Request string
	// Errors are the human-readable error messages from the response body, if any.
	Errors []string
	// Limits are the account's message limits from the response headers, if present. This is useful when
	// StatusCode is 429 to know when the limit resets.
	Limits *Limits
}

// Error implements the error interface.
func (e *APIError) Error() string {
	var b strings.Builder
	b.WriteString("pushover: API error: HTTP ")
	fmt.Fprintf(&b, "%d", e.StatusCode)
	if text := http.StatusText(e.StatusCode); text != "" {
		b.WriteString(" ")
		b.WriteString(text)
	}
	if len(e.Errors) > 0 {
		b.WriteString(": ")
		b.WriteString(strings.Join(e.Errors, "; "))
	}
	if e.Request != "" {
		b.WriteString(" (request ")
		b.WriteString(e.Request)
		b.WriteString(")")
	}
	return b.String()
}

// Is reports whether the error matches target. It matches ErrQuotaExceeded when the HTTP status is 429.
func (e *APIError) Is(target error) bool {
	return target == ErrQuotaExceeded && e.StatusCode == http.StatusTooManyRequests
}

// Temporary reports whether the error is likely caused by a temporary problem on Pushover's side, so repeating the
// same request later may succeed. Pushover documents that requests rejected with an HTTP 4xx status, or with an HTTP
// 200 status and a non-1 status in the body, will fail no matter how many times they are repeated.
func (e *APIError) Temporary() bool {
	if e.StatusCode == http.StatusOK {
		return false
	}
	return e.StatusCode < 400 || e.StatusCode >= 500
}
