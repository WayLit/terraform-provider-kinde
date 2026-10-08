package kindeapi

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type okResult struct{}

type badRequestResult struct{}

// testHTTPClient returns a client with the adapter's transports. It records
// each retry wait in sleeps (when non-nil) instead of sleeping.
func testHTTPClient(sleeps *[]time.Duration) *http.Client {
	return &http.Client{Transport: captureTransport{next: retryTransport{
		next:    http.DefaultTransport,
		retries: maxRetries,
		sleep: func(_ context.Context, d time.Duration) error {
			if sleeps != nil {
				*sleeps = append(*sleeps, d)
			}
			return nil
		},
	}}}
}

// doGet mimics generated SDK code: 200 is the success type, a declared 400
// is a typed result with a nil error, and any other status is an error.
func doGet(hc *http.Client, url string) func(context.Context) (any, error) {
	return func(ctx context.Context) (any, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return nil, err
		}
		resp, err := hc.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		switch resp.StatusCode {
		case http.StatusOK:
			return &okResult{}, nil
		case http.StatusBadRequest:
			return &badRequestResult{}, nil
		default:
			return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
		}
	}
}

func jsonServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCallReturnsSuccessType(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, `{}`)
	got, err := call[*okResult](t.Context(), "GetRole", doGet(testHTTPClient(nil), srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("expected a result")
	}
}

func TestCallTurnsTypedErrorResultIntoAPIError(t *testing.T) {
	srv := jsonServer(t, http.StatusBadRequest, `{"errors":[{"code":"INVALID_KEY","message":"Key is invalid"}]}`)
	_, err := call[*okResult](t.Context(), "CreateRole", doGet(testHTTPClient(nil), srv.URL))
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("expected *APIError, got %v", err)
	}
	want := APIError{Op: "CreateRole", StatusCode: 400, Code: "INVALID_KEY", Message: "Key is invalid"}
	if *apiErr != want {
		t.Fatalf("got %+v, want %+v", *apiErr, want)
	}
}

func TestCallTurnsUndeclaredStatusIntoAPIError(t *testing.T) {
	srv := jsonServer(t, http.StatusNotFound, `{"errors":{"code":"ROUTE_NOT_FOUND","message":"Not found"}}`)
	_, err := call[*okResult](t.Context(), "GetRole", doGet(testHTTPClient(nil), srv.URL))
	if !IsNotFound(err) {
		t.Fatalf("expected a not-found *APIError, got %v", err)
	}
	if !HasCode(err, "ROUTE_NOT_FOUND") {
		t.Fatalf("expected the body's code to survive, got %v", err)
	}
}

func TestCallWrapsTransportErrors(t *testing.T) {
	dial := errors.New("dial tcp: connection refused")
	_, err := call[*okResult](t.Context(), "GetRole", func(context.Context) (any, error) { return nil, dial })
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		t.Fatalf("transport errors must not become *APIError, got %v", err)
	}
	if !errors.Is(err, dial) || !strings.Contains(err.Error(), "kinde GetRole") {
		t.Fatalf("expected a wrapped error naming the operation, got %v", err)
	}
}

func TestCallRejectsUnexpectedSuccessType(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, `{}`)
	_, err := call[*badRequestResult](t.Context(), "GetRole", doGet(testHTTPClient(nil), srv.URL))
	if err == nil || !strings.Contains(err.Error(), "unexpected response type *kindeapi.okResult") {
		t.Fatalf("expected an unexpected-type error, got %v", err)
	}
}

func TestRetryTransportReplaysBodyAfter429(t *testing.T) {
	var mu sync.Mutex
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		bodies = append(bodies, string(b))
		n := len(bodies)
		mu.Unlock()
		if n <= 2 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
		}
	}))
	defer srv.Close()

	var sleeps []time.Duration
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL, strings.NewReader(`{"name":"admin"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := testHTTPClient(&sleeps).Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{`{"name":"admin"}`, `{"name":"admin"}`, `{"name":"admin"}`}
	if !slices.Equal(bodies, want) {
		t.Fatalf("bodies = %q, want %q", bodies, want)
	}
	if !slices.Equal(sleeps, []time.Duration{0, 0}) {
		t.Fatalf("sleeps = %v, want [0 0]", sleeps)
	}
}

func TestCallReturnsAPIErrorWhenRetriesRunOut(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts.Add(1)
		w.Header().Set("Retry-After", "0")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"errors":[{"code":"TOO_MANY_REQUESTS","message":"Slow down"}]}`)
	}))
	defer srv.Close()

	_, err := call[*okResult](t.Context(), "GetRole", doGet(testHTTPClient(nil), srv.URL))
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected a 429 *APIError, got %v", err)
	}
	if got := attempts.Load(); got != maxRetries+1 {
		t.Fatalf("attempts = %d, want %d", got, maxRetries+1)
	}
}

func TestRetryAfter(t *testing.T) {
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	tests := []struct {
		header  string
		attempt int
		want    time.Duration
	}{
		{"5", 0, 5 * time.Second},
		{"120", 0, maxRetryWait},
		{"", 0, time.Second},
		{"", 1, 2 * time.Second},
		{"", 2, 4 * time.Second},
		{past, 0, 0},
		{"soon", 1, 2 * time.Second},
	}
	for _, tt := range tests {
		if got := retryAfter(tt.header, tt.attempt); got != tt.want {
			t.Errorf("retryAfter(%q, %d) = %v, want %v", tt.header, tt.attempt, got, tt.want)
		}
	}
}

func TestRetryStopsWhenContextIsCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	hc := &http.Client{Transport: captureTransport{next: retryTransport{
		next: http.DefaultTransport, retries: maxRetries, sleep: sleepCtx,
	}}}
	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := call[*okResult](ctx, "GetRole", doGet(hc, srv.URL))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v, want context.DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("call took %v; it must stop when the context ends", elapsed)
	}
}
