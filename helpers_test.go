package pushover

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
)

const (
	testToken = "azGDORePK8gMaC0QOYAMyEEuzJnyUi"
	testUser  = "uQiRzpo4DXghDmr9QzzfQu27cmVRsG"
	testGroup = "gznej3rKEVAvPUxu9vvNnqpmZpokzF"
)

// capturedRequest is a request received by the fake Pushover API.
type capturedRequest struct {
	Method      string
	Path        string
	Params      url.Values
	Header      http.Header
	File        []byte
	FileName    string
	FileType    string
	ContentType string
}

// fakeAPI is a stand-in for the Pushover API that records requests and replies with a canned response.
type fakeAPI struct {
	mu       sync.Mutex
	requests []capturedRequest
	status   int
	body     string
	header   http.Header
}

func (f *fakeAPI) last(t *testing.T) capturedRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.requests) == 0 {
		t.Fatal("The fake Pushover API received no requests.")
	}
	return f.requests[len(f.requests)-1]
}

func (f *fakeAPI) setBody(body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.body = body
}

func (f *fakeAPI) setHeader(key, value string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.header.Set(key, value)
}

func (f *fakeAPI) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

// newFakeAPI starts a fake Pushover API that responds with the given status and body, and returns a Client for it.
func newFakeAPI(t *testing.T, status int, body string) (*Client, *fakeAPI) {
	t.Helper()
	f := &fakeAPI{status: status, body: body, header: http.Header{}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := captureRequest(t, r)
		f.mu.Lock()
		f.requests = append(f.requests, c)
		status, body := f.status, f.body
		for k, v := range f.header {
			w.Header()[k] = v
		}
		f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(server.Close)
	return newTestClient(t, server.URL, Options{}), f
}

func newTestClient(t *testing.T, serverURL string, options Options) *Client {
	t.Helper()
	options.BaseURL = serverURL + "/1/"
	c, err := New(testToken, options)
	if err != nil {
		t.Fatalf("Failed to create client.\nError: %s", err)
	}
	return c
}

func captureRequest(t *testing.T, r *http.Request) capturedRequest {
	c := capturedRequest{
		Method:      r.Method,
		Path:        r.URL.EscapedPath(),
		Header:      r.Header.Clone(),
		ContentType: r.Header.Get("Content-Type"),
	}
	switch {
	case r.Method == http.MethodGet:
		c.Params = r.URL.Query()
	case strings.HasPrefix(c.ContentType, "multipart/form-data"):
		err := r.ParseMultipartForm(10 << 20)
		if err != nil {
			t.Errorf("Failed to parse multipart form.\nError: %s", err)
			return c
		}
		c.Params = url.Values(r.MultipartForm.Value)
		if files := r.MultipartForm.File["attachment"]; len(files) > 0 {
			fh := files[0]
			c.FileName = fh.Filename
			c.FileType = fh.Header.Get("Content-Type")
			f, err := fh.Open()
			if err != nil {
				t.Errorf("Failed to open multipart file.\nError: %s", err)
				return c
			}
			defer func() { _ = f.Close() }()
			c.File, _ = io.ReadAll(f)
		}
	default:
		err := r.ParseForm()
		if err != nil {
			t.Errorf("Failed to parse form.\nError: %s", err)
			return c
		}
		c.Params = r.PostForm
	}
	return c
}

// assertParams checks that the request has exactly the expected parameters, ignoring the token, which is checked
// separately.
func assertParams(t *testing.T, got url.Values, want map[string]string) {
	t.Helper()
	if got.Get("token") != testToken {
		t.Errorf("Expected token %q, got %q.", testToken, got.Get("token"))
	}
	for k, v := range want {
		if got.Get(k) != v {
			t.Errorf("Expected parameter %q to be %q, got %q.", k, v, got.Get(k))
		}
	}
	for k := range got {
		if _, ok := want[k]; !ok && k != "token" {
			t.Errorf("Unexpected parameter %q with value %q.", k, got.Get(k))
		}
	}
}

func assertRequest(t *testing.T, got capturedRequest, method, path string) {
	t.Helper()
	if got.Method != method {
		t.Errorf("Expected method %s, got %s.", method, got.Method)
	}
	if got.Path != path {
		t.Errorf("Expected path %q, got %q.", path, got.Path)
	}
}

func ptr[T any](v T) *T {
	return &v
}
