package pushover

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewValidatesInput(t *testing.T) {
	_, err := New("", Options{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("Expected ErrInvalidRequest for empty token, got %v.", err)
	}
	for _, baseURL := range []string{"ftp://example.com/", "://bad", "example.com"} {
		_, err = New(testToken, Options{BaseURL: baseURL})
		if !errors.Is(err, ErrInvalidRequest) {
			t.Errorf("Expected ErrInvalidRequest for base URL %q, got %v.", baseURL, err)
		}
	}
	_, err = NewTeamClient("", Options{})
	if !errors.Is(err, ErrInvalidRequest) {
		t.Errorf("Expected ErrInvalidRequest for empty team token, got %v.", err)
	}
}

func TestNewDefaults(t *testing.T) {
	c, err := New(testToken, Options{})
	if err != nil {
		t.Fatalf("Failed to create client.\nError: %s", err)
	}
	if c.api.baseURL != DefaultBaseURL {
		t.Errorf("Expected base URL %q, got %q.", DefaultBaseURL, c.api.baseURL)
	}
	if cap(c.api.sem) != DefaultMaxConcurrentRequests {
		t.Errorf("Expected %d concurrent requests, got %d.", DefaultMaxConcurrentRequests, cap(c.api.sem))
	}
	if c.api.httpClient == http.DefaultClient || c.api.httpClient.Timeout == 0 {
		t.Error("Expected a dedicated HTTP client with a timeout.")
	}
	if c.api.retryMax != 0 {
		t.Errorf("Expected no retries by default, got %d.", c.api.retryMax)
	}

	c, err = New(testToken, Options{BaseURL: "http://localhost:1234/1", MaxConcurrentRequests: -1})
	if err != nil {
		t.Fatalf("Failed to create client.\nError: %s", err)
	}
	if c.api.baseURL != "http://localhost:1234/1/" {
		t.Errorf("Expected a trailing slash to be added to the base URL, got %q.", c.api.baseURL)
	}
	if c.api.sem != nil {
		t.Error("Expected no concurrency limit with a negative MaxConcurrentRequests.")
	}
}

func TestAPIErrorArray(t *testing.T) {
	c, _ := newFakeAPI(t, http.StatusBadRequest, `{"user":"invalid","errors":["user identifier is invalid"],"status":0,"request":"5042853c-402d-4a18-abcb-168734a801de"}`)
	_, err := c.Send(t.Context(), validMessage())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T: %v.", err, err)
	}
	if apiErr.StatusCode != http.StatusBadRequest || apiErr.Status != 0 {
		t.Errorf("Unexpected status codes %d and %d.", apiErr.StatusCode, apiErr.Status)
	}
	if apiErr.Request != "5042853c-402d-4a18-abcb-168734a801de" {
		t.Errorf("Unexpected request ID %q.", apiErr.Request)
	}
	if len(apiErr.Errors) != 1 || apiErr.Errors[0] != "user identifier is invalid" {
		t.Errorf("Unexpected errors %q.", apiErr.Errors)
	}
	if apiErr.Temporary() {
		t.Error("Expected a 400 response to not be temporary.")
	}
	if errors.Is(err, ErrQuotaExceeded) {
		t.Error("Expected a 400 response to not match ErrQuotaExceeded.")
	}
	want := "pushover: API error: HTTP 400 Bad Request: user identifier is invalid (request 5042853c-402d-4a18-abcb-168734a801de)"
	if err.Error() != want {
		t.Errorf("Unexpected error string.\nExpected: %s\nGot:      %s", want, err.Error())
	}
}

func TestAPIErrorObject(t *testing.T) {
	c, _ := newFakeAPI(t, http.StatusBadRequest, `{"status":0,"request":"r","errors":{"user":["is invalid"],"token":["is out of available license credits"]}}`)
	_, err := c.AssignLicense(t.Context(), LicenseAssignment{User: testUser})
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T: %v.", err, err)
	}
	want := []string{"token is out of available license credits", "user is invalid"}
	if strings.Join(apiErr.Errors, "|") != strings.Join(want, "|") {
		t.Errorf("Expected errors %q, got %q.", want, apiErr.Errors)
	}
}

func TestAPIErrorStatusZeroWithHTTP200(t *testing.T) {
	c, _ := newFakeAPI(t, http.StatusOK, `{"status":0,"request":"r","errors":["something is wrong"]}`)
	_, err := c.Send(t.Context(), validMessage())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T: %v.", err, err)
	}
	if apiErr.Temporary() {
		t.Error("Expected a status 0 response to not be temporary.")
	}
}

func TestAPIErrorQuotaExceeded(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusTooManyRequests, `{"status":0,"request":"r","errors":["application has exceeded its monthly limit"]}`)
	f.setHeader("X-Limit-App-Limit", "10000")
	f.setHeader("X-Limit-App-Remaining", "0")
	f.setHeader("X-Limit-App-Reset", "1393653600")
	c.api.retryMax = 3
	_, err := c.Send(t.Context(), validMessage())
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("Expected ErrQuotaExceeded, got %v.", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Limits == nil || apiErr.Limits.Remaining != 0 {
		t.Fatalf("Expected limits on the API error, got %v.", err)
	}
	if f.count() != 1 {
		t.Errorf("Expected a 429 response to not be retried, got %d requests.", f.count())
	}
}

func TestAPIErrorNonJSON(t *testing.T) {
	c, _ := newFakeAPI(t, http.StatusBadGateway, `<html>Bad Gateway</html>`)
	_, err := c.Sounds(t.Context())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Expected *APIError, got %T: %v.", err, err)
	}
	if !apiErr.Temporary() {
		t.Error("Expected a 502 response to be temporary.")
	}
	if apiErr.Error() != "pushover: API error: HTTP 502 Bad Gateway" {
		t.Errorf("Unexpected error string %q.", apiErr.Error())
	}
}

func TestMalformedSuccessResponse(t *testing.T) {
	c, _ := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r","sounds":["not","a","map"]}`)
	_, err := c.Sounds(t.Context())
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("Expected a decoding error, got %v.", err)
	}
}

func TestRetryAfterServerError(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), "message=hello") {
			t.Errorf("Expected the body to be resent on retry, got %q.", body)
		}
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		_, _ = io.WriteString(w, `{"status":1,"request":"ok"}`)
	}))
	t.Cleanup(server.Close)
	c := newTestClient(t, server.URL, Options{RetryMax: 2})
	c.api.minRetryWait = time.Millisecond

	msg := validMessage()
	msg.Message = "hello"
	resp, err := c.Send(t.Context(), msg)
	if err != nil {
		t.Fatalf("Expected success after retries.\nError: %s", err)
	}
	if resp.Request != "ok" || calls.Load() != 3 {
		t.Errorf("Expected 3 attempts, got %d.", calls.Load())
	}
}

func TestRetryExhausted(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusServiceUnavailable, `{"status":0,"request":"r"}`)
	c.api.retryMax = 2
	c.api.minRetryWait = time.Millisecond
	_, err := c.Send(t.Context(), validMessage())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("Expected a 503 *APIError, got %v.", err)
	}
	if f.count() != 3 {
		t.Errorf("Expected 3 attempts, got %d.", f.count())
	}
}

func TestRetryWaitRespectsMinimum(t *testing.T) {
	c, err := New(testToken, Options{RetryMax: 1, RetryWait: time.Millisecond})
	if err != nil {
		t.Fatalf("Failed to create client.\nError: %s", err)
	}
	if got := max(c.api.retryWait, c.api.minRetryWait); got != MinRetryWait {
		t.Errorf("Expected the retry wait to be raised to %s, got %s.", MinRetryWait, got)
	}
}

func TestNoRetryOnClientError(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusBadRequest, `{"status":0,"request":"r","errors":["application token is invalid"]}`)
	c.api.retryMax = 3
	c.api.minRetryWait = time.Millisecond
	_, err := c.Send(t.Context(), validMessage())
	if err == nil {
		t.Fatal("Expected an error.")
	}
	if f.count() != 1 {
		t.Errorf("Expected 1 attempt for a 400 response, got %d.", f.count())
	}
}

func TestNoRetryForUnsafeRequests(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusInternalServerError, ``)
	c.api.retryMax = 3
	c.api.minRetryWait = time.Millisecond
	_, err := c.AssignLicense(t.Context(), LicenseAssignment{Email: "user@example.com"})
	if err == nil {
		t.Fatal("Expected an error.")
	}
	if f.count() != 1 {
		t.Errorf("Expected license assignment to not be retried, got %d attempts.", f.count())
	}

	team, err := NewTeamClient(testToken, Options{BaseURL: c.api.baseURL, RetryMax: 3})
	if err != nil {
		t.Fatalf("Failed to create team client.\nError: %s", err)
	}
	team.api.minRetryWait = time.Millisecond
	err = team.AddUser(t.Context(), TeamInvitation{Email: "user@example.com"})
	if err == nil {
		t.Fatal("Expected an error.")
	}
	if f.count() != 2 {
		t.Errorf("Expected adding a team user to not be retried, got %d attempts.", f.count()-1)
	}
}

// newSlowServer starts a server that answers the first request after delay, so it times out, and later requests
// immediately.
func newSlowServer(t *testing.T, delay time.Duration) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
			}
		}
		_, _ = io.WriteString(w, `{"status":1,"request":"r","receipt":"x","sounds":{}}`)
	}))
	t.Cleanup(server.Close)
	return server, &calls
}

func TestNoRetryAfterTimeoutForNonIdempotentRequests(t *testing.T) {
	server, calls := newSlowServer(t, time.Second)
	c := newTestClient(t, server.URL, Options{RetryMax: 2, HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})
	c.api.minRetryWait = time.Millisecond
	msg := validEmergency()
	_, err := c.Send(t.Context(), msg)
	if err == nil {
		t.Fatal("Expected a timeout error.")
	}
	if calls.Load() != 1 {
		t.Errorf("Expected a message to not be resent after a timeout, got %d attempts.", calls.Load())
	}
}

func TestRetryAfterTimeoutForIdempotentRequests(t *testing.T) {
	server, calls := newSlowServer(t, time.Second)
	c := newTestClient(t, server.URL, Options{RetryMax: 2, HTTPClient: &http.Client{Timeout: 50 * time.Millisecond}})
	c.api.minRetryWait = time.Millisecond
	_, err := c.Sounds(t.Context())
	if err != nil {
		t.Fatalf("Expected a GET request to succeed after retrying a timeout.\nError: %s", err)
	}
	if calls.Load() != 2 {
		t.Errorf("Expected 2 attempts, got %d.", calls.Load())
	}
}

func TestRetrySendAfterDialError(t *testing.T) {
	var dials atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"status":1,"request":"ok"}`)
	}))
	t.Cleanup(server.Close)
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if dials.Add(1) == 1 {
				return nil, &net.OpError{Op: "dial", Net: network, Err: errors.New("connection refused")}
			}
			return (&net.Dialer{}).DialContext(ctx, network, addr)
		},
	}
	c := newTestClient(t, server.URL, Options{RetryMax: 1, HTTPClient: &http.Client{Transport: transport}})
	c.api.minRetryWait = time.Millisecond
	resp, err := c.Send(t.Context(), validMessage())
	if err != nil {
		t.Fatalf("Expected a message to be resent after a connection failure.\nError: %s", err)
	}
	if resp.Request != "ok" || dials.Load() != 2 {
		t.Errorf("Expected 2 dials, got %d.", dials.Load())
	}
}

func TestRetryNetworkError(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	c := newTestClient(t, server.URL, Options{RetryMax: 1})
	c.api.minRetryWait = time.Millisecond
	_, err := c.Limits(t.Context())
	if err == nil {
		t.Fatal("Expected a network error.")
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Errorf("Expected a network error, got an *APIError: %v.", err)
	}
}

func TestRetryWaitCanceledByContext(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusInternalServerError, ``)
	c.api.retryMax = 1
	c.api.minRetryWait = time.Hour
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.Send(ctx, validMessage())
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Expected context.DeadlineExceeded, got %v.", err)
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Errorf("Expected the last *APIError to be included, got %v.", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("Expected the retry wait to end when the context was done.")
	}
	if f.count() != 1 {
		t.Errorf("Expected 1 attempt, got %d.", f.count())
	}
}

func TestTokenRedactedFromErrors(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	server.Close()
	c := newTestClient(t, server.URL, Options{})
	_, err := c.Sounds(t.Context())
	if err == nil {
		t.Fatal("Expected a network error.")
	}
	if strings.Contains(err.Error(), testToken) {
		t.Fatalf("The error revealed the API token: %s", err)
	}
	if !strings.Contains(err.Error(), "token=REDACTED") {
		t.Errorf("Expected the token to be redacted in the error: %s", err)
	}
}

func TestMaxConcurrentRequests(t *testing.T) {
	const limit = 2
	var inFlight, peak atomic.Int32
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := inFlight.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		<-release
		inFlight.Add(-1)
		_, _ = io.WriteString(w, `{"status":1,"request":"r"}`)
	}))
	t.Cleanup(server.Close)
	c := newTestClient(t, server.URL, Options{MaxConcurrentRequests: limit})

	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := c.Send(t.Context(), validMessage())
			if err != nil {
				t.Errorf("Failed to send message.\nError: %s", err)
			}
		}()
	}
	deadline := time.Now().Add(5 * time.Second)
	for inFlight.Load() < limit && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	if peak.Load() != limit {
		t.Errorf("Expected at most %d concurrent requests, got a peak of %d.", limit, peak.Load())
	}
}

type countingLimiter struct {
	calls atomic.Int32
	err   error
}

func (l *countingLimiter) Wait(context.Context) error {
	l.calls.Add(1)
	return l.err
}

func TestLimiter(t *testing.T) {
	c, f := newFakeAPI(t, http.StatusOK, `{"status":1,"request":"r"}`)
	limiter := &countingLimiter{}
	c.api.limiter = limiter
	_, err := c.Send(t.Context(), validMessage())
	if err != nil {
		t.Fatalf("Failed to send message.\nError: %s", err)
	}
	if limiter.calls.Load() != 1 {
		t.Errorf("Expected the limiter to be called once, got %d.", limiter.calls.Load())
	}

	limiter.err = errors.New("rate limited")
	_, err = c.Send(t.Context(), validMessage())
	if !errors.Is(err, limiter.err) {
		t.Fatalf("Expected the limiter error, got %v.", err)
	}
	if f.count() != 1 {
		t.Errorf("Expected no request when the limiter fails, got %d requests.", f.count())
	}
}

func TestCustomHTTPClientAndUserAgent(t *testing.T) {
	var used atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") != "my-app/1.0" {
			t.Errorf("Unexpected User-Agent %q.", r.Header.Get("User-Agent"))
		}
		_, _ = io.WriteString(w, `{"status":1,"request":"r"}`)
	}))
	t.Cleanup(server.Close)
	httpClient := &http.Client{Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		used.Store(true)
		return http.DefaultTransport.RoundTrip(r)
	})}
	c := newTestClient(t, server.URL, Options{HTTPClient: httpClient, UserAgent: "my-app/1.0"})
	_, err := c.Send(t.Context(), validMessage())
	if err != nil {
		t.Fatalf("Failed to send message.\nError: %s", err)
	}
	if !used.Load() {
		t.Error("Expected the custom HTTP client to be used.")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
