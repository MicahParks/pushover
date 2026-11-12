package pushover

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func callbackRequest(method string, form url.Values) *http.Request {
	r := httptest.NewRequest(method, "/pushover/callback?secret=abc", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func callbackForm() url.Values {
	return url.Values{
		"receipt":                {"rLqVuqTRh62UzxtmqiaLzQmVcPgiCy"},
		"acknowledged":           {"1"},
		"acknowledged_at":        {"1360019238"},
		"acknowledged_by":        {testUser},
		"acknowledged_by_device": {"iphone"},
	}
}

func TestParseCallback(t *testing.T) {
	cb, err := ParseCallback(callbackRequest(http.MethodPost, callbackForm()))
	if err != nil {
		t.Fatalf("Failed to parse callback.\nError: %s", err)
	}
	want := Callback{
		Receipt:              "rLqVuqTRh62UzxtmqiaLzQmVcPgiCy",
		AcknowledgedAt:       time.Unix(1360019238, 0),
		AcknowledgedBy:       testUser,
		AcknowledgedByDevice: "iphone",
	}
	if cb != want {
		t.Errorf("Unexpected callback.\nExpected: %+v\nGot:      %+v", want, cb)
	}

	form := callbackForm()
	form.Del("receipt")
	_, err = ParseCallback(callbackRequest(http.MethodPost, form))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("Expected ErrInvalidRequest for a missing receipt, got %v.", err)
	}

	form = callbackForm()
	form.Set("acknowledged_at", "yesterday")
	_, err = ParseCallback(callbackRequest(http.MethodPost, form))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("Expected ErrInvalidRequest for an invalid timestamp, got %v.", err)
	}

	_, err = ParseCallback(callbackRequest(http.MethodGet, callbackForm()))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("Expected ErrInvalidRequest for a GET request, got %v.", err)
	}
}

func TestCallbackHandler(t *testing.T) {
	var got Callback
	var fail bool
	handler := CallbackHandler(func(ctx context.Context, cb Callback) error {
		got = cb
		if fail {
			return errors.New("database unavailable")
		}
		return nil
	})

	w := httptest.NewRecorder()
	handler.ServeHTTP(w, callbackRequest(http.MethodPost, callbackForm()))
	if w.Code != http.StatusOK {
		t.Errorf("Expected HTTP 200, got %d.", w.Code)
	}
	if got.Receipt != "rLqVuqTRh62UzxtmqiaLzQmVcPgiCy" {
		t.Errorf("Expected the callback to be passed to fn, got %+v.", got)
	}

	fail = true
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, callbackRequest(http.MethodPost, callbackForm()))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected HTTP 500 so Pushover retries, got %d.", w.Code)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, callbackRequest(http.MethodPost, url.Values{}))
	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected HTTP 400 for an invalid callback, got %d.", w.Code)
	}

	w = httptest.NewRecorder()
	handler.ServeHTTP(w, callbackRequest(http.MethodGet, nil))
	if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != http.MethodPost {
		t.Errorf("Expected HTTP 405 with an Allow header, got %d.", w.Code)
	}

	form := callbackForm()
	form.Set("padding", strings.Repeat("a", 1<<20))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, callbackRequest(http.MethodPost, form))
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("Expected HTTP 413 for a large body, got %d.", w.Code)
	}
}
