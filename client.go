package pushover

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"
)

const (
	// DefaultBaseURL is the base URL of the Pushover API.
	DefaultBaseURL = "https://api.pushover.net/1/"
	// DefaultMaxConcurrentRequests is the default limit of in-flight requests per client. Pushover asks API clients to
	// keep no more than 2 concurrent HTTP requests open.
	DefaultMaxConcurrentRequests = 2
	// DefaultUserAgent is the User-Agent header sent with each request unless overridden.
	DefaultUserAgent = "github.com/MicahParks/pushover"
	// MinRetryWait is the minimum time between retries. Pushover asks API clients to wait at least 5 seconds before
	// retrying a request that failed due to a server or network error.
	MinRetryWait = 5 * time.Second

	defaultHTTPTimeout  = time.Minute
	maxResponseBodySize = 1 << 20
)

// Limiter limits the rate of requests. Wait is called before every HTTP request and must block until the request is
// allowed or the context is done.
//
// A *rate.Limiter from golang.org/x/time/rate implements this interface.
type Limiter interface {
	Wait(ctx context.Context) error
}

// Options configure a Client or TeamClient. The zero value is valid and uses sensible defaults.
type Options struct {
	// BaseURL is the base URL of the Pushover API, including the version path. Defaults to DefaultBaseURL. This is
	// useful for testing against a local server.
	BaseURL string
	// HTTPClient is the HTTP client used for requests. Defaults to a client with a 1 minute timeout.
	HTTPClient *http.Client
	// Limiter, if non-nil, is waited on before each HTTP request, including retries. Pushover's monthly message quota
	// is enforced on their side, so this is only needed to smooth bursts from your application.
	Limiter Limiter
	// MaxConcurrentRequests limits the number of in-flight HTTP requests made by this client. Defaults to
	// DefaultMaxConcurrentRequests. Use a negative value to disable the limit.
	MaxConcurrentRequests int
	// RetryMax is the number of times a request is retried after a network error or an HTTP response that indicates a
	// temporary problem on Pushover's side, such as a 5xx status. Requests rejected because of invalid input (HTTP 4xx)
	// are never retried. Defaults to 0, no retries.
	//
	// Requests that would have a different effect if Pushover processed them twice, such as sending a message or
	// creating a group, are only retried when Pushover responded with a temporary error or the connection could not be
	// established. They are not retried after a timeout or a lost response, because the first attempt may have
	// succeeded. License assignments and Team user additions are never retried.
	RetryMax int
	// RetryWait is the time to wait between retries. Values less than MinRetryWait are raised to MinRetryWait.
	RetryWait time.Duration
	// UserAgent is the User-Agent header sent with each request. Defaults to DefaultUserAgent.
	UserAgent string
}

// Client is a client for the Pushover APIs that authenticate with an application API token. It is safe for concurrent
// use.
type Client struct {
	api *api
}

// New creates a new Client for the application with the given API token.
func New(token string, options Options) (*Client, error) {
	a, err := newAPI(token, options)
	if err != nil {
		return nil, err
	}
	return &Client{api: a}, nil
}

// api holds the HTTP plumbing shared by Client and TeamClient.
type api struct {
	baseURL      string
	httpClient   *http.Client
	limiter      Limiter
	minRetryWait time.Duration
	retryMax     int
	retryWait    time.Duration
	sem          chan struct{}
	token        string
	userAgent    string
}

func newAPI(token string, options Options) (*api, error) {
	if token == "" {
		return nil, fmt.Errorf("%w: API token is required", ErrInvalidRequest)
	}
	baseURL := options.BaseURL
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse base URL: %w", ErrInvalidRequest, err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, fmt.Errorf("%w: base URL must use the https or http scheme", ErrInvalidRequest)
	}
	if !strings.HasSuffix(baseURL, "/") {
		baseURL += "/"
	}
	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultHTTPTimeout}
	}
	var sem chan struct{}
	switch {
	case options.MaxConcurrentRequests == 0:
		sem = make(chan struct{}, DefaultMaxConcurrentRequests)
	case options.MaxConcurrentRequests > 0:
		sem = make(chan struct{}, options.MaxConcurrentRequests)
	}
	retryMax := max(options.RetryMax, 0)
	userAgent := options.UserAgent
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	a := &api{
		baseURL:      baseURL,
		httpClient:   httpClient,
		limiter:      options.Limiter,
		minRetryWait: MinRetryWait,
		retryMax:     retryMax,
		retryWait:    options.RetryWait,
		sem:          sem,
		token:        token,
		userAgent:    userAgent,
	}
	return a, nil
}

// apiRequest describes a single Pushover API call.
type apiRequest struct {
	method string
	// path is relative to the base URL. Dynamic path segments must already be escaped.
	path string
	// params are sent as the query string for GET requests and as the body for other methods. The token is added
	// automatically.
	params url.Values
	// attachment, if non-nil, causes the body to be encoded as multipart/form-data.
	attachment *Attachment
	// retry controls when the request may be retried. GET requests are always treated as retryIdempotent.
	retry retryPolicy
}

// retryPolicy controls which failures of a request may be retried.
type retryPolicy int

const (
	// retryNotIdempotent requests are retried only when they certainly were not processed: Pushover responded with a
	// temporary error, or the connection could not be established. This is the default.
	retryNotIdempotent retryPolicy = iota
	// retryIdempotent requests can be repeated without changing the result, so they're retried after any temporary
	// failure, including timeouts.
	retryIdempotent
	// retryNever requests are never retried.
	retryNever
)

// failure classifies an unsuccessful attempt.
type failure int

const (
	// failurePermanent must not be retried.
	failurePermanent failure = iota
	// failureNotProcessed means Pushover did not process the request, so any request may be retried.
	failureNotProcessed
	// failureUnknown means the request may or may not have been processed, so only idempotent requests may be retried.
	failureUnknown
)

// envelope holds the fields common to every Pushover API response.
type envelope struct {
	Status  int       `json:"status"`
	Request string    `json:"request"`
	Errors  errorList `json:"errors"`
}

// do performs the API call, retrying when allowed, and decodes a successful JSON response into out if out is non-nil.
// The response headers of the final attempt are returned.
func (a *api) do(ctx context.Context, r apiRequest, out any) (http.Header, error) {
	params := url.Values{}
	for k, v := range r.params {
		params[k] = v
	}
	params.Set("token", a.token)

	endpoint := a.baseURL + r.path
	var body []byte
	var contentType string
	if r.method == http.MethodGet {
		endpoint += "?" + params.Encode()
	} else if r.attachment != nil {
		var err error
		body, contentType, err = encodeMultipart(params, r.attachment)
		if err != nil {
			return nil, err
		}
	} else {
		body = []byte(params.Encode())
		contentType = "application/x-www-form-urlencoded"
	}

	policy := r.retry
	if r.method == http.MethodGet {
		policy = retryIdempotent
	}
	attempts := 1
	if policy != retryNever {
		attempts += a.retryMax
	}
	var err error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			wait := max(a.retryWait, a.minRetryWait)
			t := time.NewTimer(wait)
			select {
			case <-ctx.Done():
				t.Stop()
				return nil, fmt.Errorf("pushover: context done while waiting to retry: %w", errors.Join(ctx.Err(), err))
			case <-t.C:
			}
		}
		var header http.Header
		var f failure
		header, f, err = a.attempt(ctx, r.method, endpoint, contentType, body, out)
		retryable := f == failureNotProcessed || (f == failureUnknown && policy == retryIdempotent)
		if err == nil || !retryable {
			return header, err
		}
	}
	return nil, err
}

// attempt performs one HTTP request. On failure, it classifies whether the request may have been processed.
func (a *api) attempt(ctx context.Context, method, endpoint, contentType string, body []byte, out any) (http.Header, failure, error) {
	if a.limiter != nil {
		err := a.limiter.Wait(ctx)
		if err != nil {
			return nil, failurePermanent, fmt.Errorf("pushover: failed to wait for rate limiter: %w", err)
		}
	}
	if a.sem != nil {
		select {
		case a.sem <- struct{}{}:
			defer func() { <-a.sem }()
		case <-ctx.Done():
			return nil, failurePermanent, fmt.Errorf("pushover: context done while waiting for a concurrent request slot: %w", ctx.Err())
		}
	}

	var reqBody io.Reader
	if body != nil {
		reqBody = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reqBody)
	if err != nil {
		return nil, failurePermanent, fmt.Errorf("pushover: failed to create HTTP request: %w", redactURLError(err))
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", a.userAgent)

	resp, err := a.httpClient.Do(req)
	if err != nil {
		f := failureUnknown
		switch {
		case ctx.Err() != nil:
			f = failurePermanent
		case isDialError(err):
			f = failureNotProcessed
		}
		return nil, f, fmt.Errorf("pushover: failed to perform HTTP request: %w", redactURLError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		f := failureUnknown
		if ctx.Err() != nil {
			f = failurePermanent
		}
		return nil, f, fmt.Errorf("pushover: failed to read HTTP response body: %w", redactURLError(err))
	}

	var env envelope
	jsonErr := json.Unmarshal(raw, &env)
	if resp.StatusCode == http.StatusOK && jsonErr == nil && env.Status == 1 {
		if out != nil {
			err = json.Unmarshal(raw, out)
			if err != nil {
				return nil, failurePermanent, fmt.Errorf("pushover: request succeeded but the JSON response could not be decoded: %w", err)
			}
		}
		return resp.Header, failurePermanent, nil
	}

	apiErr := &APIError{
		StatusCode: resp.StatusCode,
		Status:     env.Status,
		Request:    env.Request,
		Errors:     env.Errors,
		Limits:     parseLimitHeaders(resp.Header),
	}
	f := failurePermanent
	if apiErr.Temporary() {
		f = failureNotProcessed
	}
	return resp.Header, f, apiErr
}

// isDialError reports whether err happened while establishing the connection, before the request was sent.
func isDialError(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// redactURLError removes the API token from the URL in a *url.Error. GET requests carry the token in the query string,
// and the net/http package includes the full URL in its errors.
func redactURLError(err error) error {
	var urlErr *url.Error
	if !errors.As(err, &urlErr) {
		return err
	}
	u, parseErr := url.Parse(urlErr.URL)
	if parseErr != nil {
		urlErr.URL = "(redacted)"
		return err
	}
	q := u.Query()
	if q.Has("token") {
		q.Set("token", "REDACTED")
		u.RawQuery = q.Encode()
	}
	urlErr.URL = u.String()
	return err
}

// errorList decodes the "errors" field of a Pushover API response. Most endpoints return an array of strings, but some,
// like the Licensing API, return an object that maps parameter names to arrays of messages.
type errorList []string

func (e *errorList) UnmarshalJSON(data []byte) error {
	var list []string
	if err := json.Unmarshal(data, &list); err == nil {
		*e = list
		return nil
	}
	var byField map[string]json.RawMessage
	if err := json.Unmarshal(data, &byField); err != nil {
		// Unknown format. Ignore it rather than failing the whole response.
		return nil
	}
	keys := make([]string, 0, len(byField))
	for k := range byField {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, field := range keys {
		var messages []string
		if err := json.Unmarshal(byField[field], &messages); err != nil {
			var message string
			if err = json.Unmarshal(byField[field], &message); err != nil {
				continue
			}
			messages = []string{message}
		}
		for _, message := range messages {
			*e = append(*e, field+" "+message)
		}
	}
	return nil
}

// flexBool decodes a JSON boolean that Pushover may send as true/false or as the integers 1/0.
type flexBool bool

func (b *flexBool) UnmarshalJSON(data []byte) error {
	switch string(data) {
	case "true", "1", `"1"`, `"true"`:
		*b = true
	default:
		*b = false
	}
	return nil
}

// unixTime converts a Unix timestamp from the API into a time.Time. Pushover uses 0 to mean "not set", which becomes
// the zero time.Time.
func unixTime(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}
