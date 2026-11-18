package kindeapi

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	// maxRetries is how many times a 429 response is retried.
	maxRetries = 3
	// maxRetryWait caps how long one Retry-After wait can be.
	maxRetryWait = 60 * time.Second
	// maxErrorBody caps how much of an error response is kept.
	maxErrorBody = 64 << 10
)

type captureKey struct{}

// capture holds the final error response seen during one adapter call.
type capture struct {
	status int
	body   []byte
}

func withCapture(ctx context.Context) (context.Context, *capture) {
	c := &capture{}
	return context.WithValue(ctx, captureKey{}, c), c
}

// captureTransport records the status and body of error responses for the
// call whose context carries a capture. The generated SDK discards the body
// of undeclared statuses, so this is the only place the error code survives.
type captureTransport struct {
	next http.RoundTripper
}

func (t captureTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.next.RoundTrip(req)
	if err != nil || resp.StatusCode < http.StatusBadRequest {
		return resp, err
	}
	c, ok := req.Context().Value(captureKey{}).(*capture)
	if !ok {
		return resp, nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	resp.Body.Close()
	if err != nil {
		return nil, err
	}
	c.status, c.body = resp.StatusCode, body
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return resp, nil
}

// retryTransport retries 429 responses. Kinde does not process a request it
// rate-limits, so retrying is safe for every method.
type retryTransport struct {
	next    http.RoundTripper
	retries int
	sleep   func(ctx context.Context, d time.Duration) error
}

func (t retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil && req.Body != http.NoBody {
		b, err := io.ReadAll(req.Body)
		req.Body.Close()
		if err != nil {
			return nil, err
		}
		body = b
	}

	for attempt := 0; ; attempt++ {
		r := req.Clone(req.Context())
		if body != nil {
			r.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err := t.next.RoundTrip(r)
		if err != nil || resp.StatusCode != http.StatusTooManyRequests || attempt == t.retries {
			return resp, err
		}
		wait := retryAfter(resp.Header.Get("Retry-After"), attempt)
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if err := t.sleep(req.Context(), wait); err != nil {
			return nil, err
		}
	}
}

// retryAfter returns how long to wait before retrying after the given
// attempt (0 for the first). It honors Retry-After in seconds or as an HTTP
// date, capped at maxRetryWait, and otherwise backs off 1s, 2s, then 4s.
func retryAfter(header string, attempt int) time.Duration {
	header = strings.TrimSpace(header)
	if secs, err := strconv.Atoi(header); err == nil && secs >= 0 {
		return min(time.Duration(secs)*time.Second, maxRetryWait)
	}
	if at, err := http.ParseTime(header); err == nil {
		return min(max(time.Until(at), 0), maxRetryWait)
	}
	return time.Second << attempt
}

// sleepCtx waits for d or until ctx ends, whichever comes first.
func sleepCtx(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
