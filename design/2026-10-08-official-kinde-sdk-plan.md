# Official Kinde SDK Migration Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace `github.com/nxt-fwd/kinde-go` with `github.com/kinde-oss/kinde-go` behind an internal adapter, and run every acceptance test against an in-memory fake Kinde.

**Architecture:** `internal/kindeapi` wraps the generated SDK client. It builds the authenticated client, turns error responses into `*APIError`, retries 429s, and returns every page of list endpoints. `internal/kindefake` implements the endpoints the provider calls on the SDK's generated server. Resources move from the old library to `kindeapi` one domain at a time: `Configure` hands both clients to every resource until the last one moves.

**Tech Stack:** Go 1.26, Terraform Plugin Framework, `github.com/kinde-oss/kinde-go` v0.3.0 (ogen v1.14.0), `golang.org/x/oauth2`, `terraform-plugin-testing`.

**Spec:** `design/2026-10-08-official-kinde-sdk.md`

## Global Constraints

- The `go` directive must be at least `1.24.4`, the SDK's floor. Task 2's `go get` raises it to `1.26.0`, because `golang.org/x/oauth2` v0.37.0 requires Go 1.26. Run Go commands with `GOTOOLCHAIN=auto` (the default) so the toolchain downloads; CI's `setup-go` reads the version from `go.mod`.
- Import the generated package as `mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"`. Never call `kinde.NewManagementAPI`.
- Resources and data sources call only `*kindeapi.Client` methods. They may use the SDK types those methods return.
- The module path stays `github.com/nxt-fwd/terraform-provider-kinde`. Renaming it is separate work.
- Resource type names, IDs, and import ID formats do not change.
- Lint (`.golangci.yml`) runs in CI:
  - comments end with a period (`godot`);
  - type assertions use the comma-ok form (`forcetypeassert`);
  - tests use `t.Context()` and `t.Setenv` (`usetesting`);
  - no unused functions, fields, or parameters at the end of a task (`unused`, `unparam`). Add a helper in the task that first uses it.
- Acceptance tests run against `kindefake` with `TF_ACC=1`, never against a live tenant. A test that calls `testAccFake` must not call `t.Parallel`.
- The Terraform CLI must be on `PATH` for acceptance tests. If it is not, `terraform-plugin-testing` downloads it.
- Commit messages follow the existing history: an imperative summary such as "Add zizmor workflow security checks", with no type prefix. End every commit message with this line:
  `Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro`

## Review Focus

These failure modes are implied by the spec, but no acceptance test exercises them. Each has a pinned test in the task named. They are listed most likely first.

1. **Removing an optional attribute after it was set.** Kinde keeps some values once set, such as a permission's description. Removing the attribute keeps the value; it does not fail with "inconsistent result after apply". Pinned in Task 6 by the last two steps of `TestAccPermissionResource_NoDescription`.
2. **Empty string versus omitted.** An attribute set to `""` is sent as `""`; an attribute left out of the config is not sent at all. Pinned in Task 6 by `TestOptString`, which checks the encoded request body.
3. **Persistent rate limiting.** If Kinde keeps answering 429 past the retries, the apply fails with an HTTP 429 error instead of treating the call as a success. Pinned in Task 1 by `TestCallReturnsAPIErrorWhenRetriesRunOut`.
4. **Runs that outlive the access token.** A long apply fetches a new token when the old one expires. Pinned in Task 2 by `TestTokenIsRefreshedWhenExpired`.
5. **Error bodies that aren't JSON.** A proxy's HTML page or a bare 502 becomes a readable error, with no panic and no decode error. Pinned in Task 1 by the `html` and `empty` rows of `TestNewAPIErrorParsesBodyShapes`.

Two neighbors are also pinned: cancelling during a `Retry-After` wait (`TestRetryStopsWhenContextIsCancelled`, Task 1), and lists longer than one page (each domain's adapter tests).

## File Structure

```
internal/kindeapi/                adapter over the generated SDK
  doc.go errors.go transport.go call.go client.go    core (Tasks 1-2)
  pagination.go                   allPages (Task 5), allCursorPages (Task 10)
  permissions.go roles.go apis.go connections.go applications.go
  organizations.go users.go phone.go organization_users.go
                                  one file per domain (Tasks 5-18), each with a _test.go
                                  that runs against kindefake
internal/kindefake/               in-memory fake Kinde for tests
  fake.go errors.go handler.go    core (Task 3); raw routes added in Task 10
  permissions.go ... organization_users.go
                                  one file per domain: state, handlers, Remove<Thing> hooks
internal/provider/
  provider.go provider_data.go    Configure builds kindeapi (plus the old client until Task 20)
  values.go                       OptString/types.String helpers (Task 6, extended later)
  not_found_test.go               Read-on-missing-object table (rows added per domain)
  pagination.go                   deleted in Task 8
  *_resource.go *_data_source.go  migrated one domain at a time (Tasks 6-19)
```

Three list endpoints bypass the SDK through `getJSON`, because the SDK cannot decode what the live API returns:
- `ListConnections` (Task 10) and `ListApplicationConnections` (Task 12): the spec wraps each item in an envelope the API doesn't send (kinde-go issue #53, PR #63).
- `GetUserIdentities` (Task 16): the spec allows a `null` `is_confirmed`, but the SDK's type is non-nullable.

The fake also serves connection create and update (Task 10) itself, because the generated server cannot decode social-connection options.

Task order is fixed. A task's acceptance tests use only resources migrated by earlier tasks:
- 5–6 permissions;
- 7–8 roles;
- 9 APIs;
- 10–11 connections;
- 12–13 applications;
- 14–15 organizations;
- 16–17 users;
- 18–19 organization users and user roles;
- 20 removes the old library;
- 21 adds CI.

---

### Task 1: Adapter errors, transports, and `call[T]`

**Files:**
- Create: `internal/kindeapi/doc.go`
- Create: `internal/kindeapi/errors.go`
- Create: `internal/kindeapi/transport.go`
- Create: `internal/kindeapi/call.go`
- Test: `internal/kindeapi/errors_test.go`
- Test: `internal/kindeapi/transport_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces:
  - `type APIError struct { Op string; StatusCode int; Code string; Message string }`, which implements `error`.
  - `func IsNotFound(err error) bool` and `func HasCode(err error, code string) bool`.
  - `func newAPIError(op string, status int, body []byte) *APIError`.
  - `type captureTransport struct{ next http.RoundTripper }` and `type retryTransport struct { next http.RoundTripper; retries int; sleep func(context.Context, time.Duration) error }`.
  - `const maxRetries = 3`, `func sleepCtx(ctx context.Context, d time.Duration) error`.
  - `func call[T any](ctx context.Context, op string, fn func(context.Context) (any, error)) (T, error)`.

- [ ] **Step 1: Write the failing error tests**

`internal/kindeapi/errors_test.go`:

```go
package kindeapi

import (
	"errors"
	"fmt"
	"testing"
)

func TestNewAPIErrorParsesBodyShapes(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCode    string
		wantMessage string
	}{
		{"list", `{"errors":[{"code":"ROLE_NOT_FOUND","message":"Role not found"}]}`, "ROLE_NOT_FOUND", "Role not found"},
		{"object", `{"errors":{"code":"ROUTE_NOT_FOUND","message":"Not found"}}`, "ROUTE_NOT_FOUND", "Not found"},
		{"bare", `{"code":"INVALID_CREDENTIALS","message":"Bad token"}`, "INVALID_CREDENTIALS", "Bad token"},
		{"html", "<html><body>502 Bad Gateway</body></html>\n", "", "<html><body>502 Bad Gateway</body></html>"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newAPIError("GetRole", 502, []byte(tt.body))
			want := APIError{Op: "GetRole", StatusCode: 502, Code: tt.wantCode, Message: tt.wantMessage}
			if *got != want {
				t.Fatalf("got %+v, want %+v", *got, want)
			}
		})
	}
}

func TestNewAPIErrorTruncatesLongBodies(t *testing.T) {
	body := make([]byte, 2*maxMessage)
	for i := range body {
		body[i] = 'x'
	}
	if got := newAPIError("GetRole", 500, body); len(got.Message) != maxMessage {
		t.Fatalf("message length = %d, want %d", len(got.Message), maxMessage)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &APIError{Op: "GetRole", StatusCode: 404, Code: "ROLE_NOT_FOUND", Message: "Role not found"}
	if got, want := err.Error(), "kinde GetRole: HTTP 404 ROLE_NOT_FOUND: Role not found"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestIsNotFound(t *testing.T) {
	notFound := &APIError{Op: "GetRole", StatusCode: 404}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"404", notFound, true},
		{"wrapped 404", fmt.Errorf("reading role: %w", notFound), true},
		{"400", &APIError{Op: "GetRole", StatusCode: 400}, false},
		{"plain error", errors.New("dial tcp: connection refused"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Fatalf("IsNotFound = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasCodeIgnoresCase(t *testing.T) {
	err := fmt.Errorf("listing roles: %w", &APIError{StatusCode: 400, Code: "USER_NOT_IN_ORGANIZATION"})
	if !HasCode(err, "user_not_in_organization") {
		t.Fatal("expected HasCode to match regardless of case")
	}
	if HasCode(err, "ROLE_NOT_FOUND") {
		t.Fatal("expected HasCode not to match a different code")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/ -run 'APIError|IsNotFound|HasCode'`
Expected: FAIL to compile with `undefined: newAPIError`.

- [ ] **Step 3: Implement the package doc and errors**

`internal/kindeapi/doc.go`:

```go
// Package kindeapi adapts Kinde's official Go SDK for the provider. It builds
// an authenticated client, turns error responses into *APIError, retries rate
// limits, and returns every page of list endpoints.
package kindeapi
```

`internal/kindeapi/errors.go`:

```go
package kindeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError is an error response from the Kinde management API.
type APIError struct {
	// Op names the operation, such as "GetRole".
	Op         string
	StatusCode int
	// Code is Kinde's error code, such as "USER_NOT_IN_ORGANIZATION". It is
	// empty when the response body carried none.
	Code    string
	Message string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("kinde %s: HTTP %d", e.Op, e.StatusCode)
	if e.Code != "" {
		msg += " " + e.Code
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// IsNotFound reports whether err is an *APIError for an object that does not
// exist. Kinde's spec documents no 400 error codes for missing IDs on the
// endpoints this provider calls, so only HTTP 404 counts.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// HasCode reports whether err is an *APIError with the given Kinde error
// code, ignoring case.
func HasCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && strings.EqualFold(apiErr.Code, code)
}

// maxMessage caps how much of a non-JSON error body goes into Message.
const maxMessage = 500

type errorItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// newAPIError builds an *APIError from an error response body. Kinde uses
// three shapes: {"errors":[{...}]}, {"errors":{...}}, and a bare {...}.
func newAPIError(op string, status int, body []byte) *APIError {
	apiErr := &APIError{Op: op, StatusCode: status}

	var parsed struct {
		Errors  json.RawMessage `json:"errors"`
		Code    string          `json:"code"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		msg := strings.TrimSpace(string(body))
		if len(msg) > maxMessage {
			msg = msg[:maxMessage]
		}
		apiErr.Message = msg
		return apiErr
	}

	item := errorItem{Code: parsed.Code, Message: parsed.Message}
	var list []errorItem
	var single errorItem
	if json.Unmarshal(parsed.Errors, &list) == nil && len(list) > 0 {
		item = list[0]
	} else if json.Unmarshal(parsed.Errors, &single) == nil && single != (errorItem{}) {
		item = single
	}
	apiErr.Code, apiErr.Message = item.Code, item.Message
	return apiErr
}
```

- [ ] **Step 4: Run the error tests to verify they pass**

Run: `go test ./internal/kindeapi/ -run 'APIError|IsNotFound|HasCode' -v`
Expected: PASS.

- [ ] **Step 5: Write the failing transport and `call` tests**

`internal/kindeapi/transport_test.go`:

```go
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
```

- [ ] **Step 6: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/`
Expected: FAIL to compile with `undefined: captureTransport`.

- [ ] **Step 7: Implement the transports**

`internal/kindeapi/transport.go`:

```go
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
```

- [ ] **Step 8: Implement `call`**

`internal/kindeapi/call.go`:

```go
package kindeapi

import (
	"context"
	"fmt"
)

// call runs one SDK operation and returns its result as the success type T.
// The SDK reports declared error statuses as typed results with a nil error
// and undeclared ones as errors without the body; both become *APIError,
// built from the response captured during the call. Transport and decoding
// failures are wrapped and returned as they are.
func call[T any](ctx context.Context, op string, fn func(context.Context) (any, error)) (T, error) {
	var zero T
	ctx, c := withCapture(ctx)
	res, err := fn(ctx)
	if c.status != 0 {
		return zero, newAPIError(op, c.status, c.body)
	}
	if err != nil {
		return zero, fmt.Errorf("kinde %s: %w", op, err)
	}
	v, ok := res.(T)
	if !ok {
		return zero, fmt.Errorf("kinde %s: unexpected response type %T", op, res)
	}
	return v, nil
}
```

- [ ] **Step 9: Run the tests to verify they pass**

Run: `go test ./internal/kindeapi/ -race -v`
Expected: PASS, including `TestRetryStopsWhenContextIsCancelled` finishing in well under a second.

- [ ] **Step 10: Commit**

```bash
git add internal/kindeapi
git commit -m "Add kindeapi error handling and rate-limit retries" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 2: Adapter client construction and tokens

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/kindeapi/client.go`
- Test: `internal/kindeapi/client_test.go`

**Interfaces:**
- Consumes: `captureTransport`, `retryTransport`, `maxRetries`, `sleepCtx`, and `newAPIError` from Task 1.
- Produces:
  - `type Config struct { Domain, Audience, ClientID, ClientSecret string }`.
  - `type Client struct { api *mgmt.Client; http *http.Client; domain string; tokens oauth2.TokenSource }`.
  - `func New(cfg Config) (*Client, error)`, which makes no network calls.
  - `func (c *Client) CheckCredentials() error`.
  - `func (c *Client) getJSON(ctx context.Context, op, path string, query url.Values, out any) error`, an authenticated raw GET for endpoints the SDK cannot decode.

- [ ] **Step 1: Add the dependencies**

Run:

```bash
go get github.com/kinde-oss/kinde-go@v0.3.0 golang.org/x/oauth2
```

Expected: `go.mod` requires `github.com/kinde-oss/kinde-go v0.3.0` and `golang.org/x/oauth2 v0.37.0` (or newer), and its `go` directive is `1.26.0`. Leave the `nxt-fwd/kinde-go` requirement in place; Task 20 removes it.

- [ ] **Step 2: Write the failing client tests**

`internal/kindeapi/client_test.go`:

```go
package kindeapi

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// tokenServer is a minimal Kinde: a token endpoint that accepts client ID
// "id" and secret "secret", plus two API routes.
type tokenServer struct {
	srv       *httptest.Server
	tokens    atomic.Int32
	expiresIn int

	mu       sync.Mutex
	audience string
}

func newTokenServer(t *testing.T, expiresIn int) *tokenServer {
	t.Helper()
	k := &tokenServer{expiresIn: expiresIn}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		n := k.tokens.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if r.FormValue("grant_type") != "client_credentials" || r.FormValue("client_id") != "id" || r.FormValue("client_secret") != "secret" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = io.WriteString(w, `{"error":"invalid_client"}`)
			return
		}
		k.mu.Lock()
		k.audience = r.FormValue("audience")
		k.mu.Unlock()
		_, _ = fmt.Fprintf(w, `{"access_token":"tok-%d","token_type":"bearer","expires_in":%d}`, n, k.expiresIn)
	})
	mux.HandleFunc("GET /api/v1/things", func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer tok-") {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"`+r.URL.Query().Get("name")+`"}`)
	})
	mux.HandleFunc("GET /api/v1/missing", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"errors":[{"code":"ROLE_NOT_FOUND","message":"Role not found"}]}`)
	})
	k.srv = httptest.NewServer(mux)
	t.Cleanup(k.srv.Close)
	return k
}

func (k *tokenServer) lastAudience() string {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.audience
}

// clearKindeEnv stops a developer's own KINDE_* settings leaking into tests.
func clearKindeEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"KINDE_DOMAIN", "KINDE_AUDIENCE", "KINDE_CLIENT_ID", "KINDE_CLIENT_SECRET"} {
		t.Setenv(name, "")
	}
}

func mustNew(t *testing.T, cfg Config) *Client {
	t.Helper()
	c, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewDefaultsAudienceToDomainAPI(t *testing.T) {
	clearKindeEnv(t)
	k := newTokenServer(t, 3600)
	c := mustNew(t, Config{Domain: k.srv.URL + "/", ClientID: "id", ClientSecret: "secret"})
	if err := c.CheckCredentials(); err != nil {
		t.Fatal(err)
	}
	if got, want := k.lastAudience(), k.srv.URL+"/api"; got != want {
		t.Fatalf("audience = %q, want %q", got, want)
	}
}

func TestNewUsesConfiguredAudience(t *testing.T) {
	clearKindeEnv(t)
	k := newTokenServer(t, 3600)
	c := mustNew(t, Config{Domain: k.srv.URL, Audience: "https://custom.example/api", ClientID: "id", ClientSecret: "secret"})
	if err := c.CheckCredentials(); err != nil {
		t.Fatal(err)
	}
	if got := k.lastAudience(); got != "https://custom.example/api" {
		t.Fatalf("audience = %q, want the configured one", got)
	}
}

func TestNewFallsBackToEnvironment(t *testing.T) {
	k := newTokenServer(t, 3600)
	t.Setenv("KINDE_DOMAIN", k.srv.URL)
	t.Setenv("KINDE_AUDIENCE", "")
	t.Setenv("KINDE_CLIENT_ID", "id")
	t.Setenv("KINDE_CLIENT_SECRET", "secret")
	if err := mustNew(t, Config{}).CheckCredentials(); err != nil {
		t.Fatal(err)
	}
}

func TestNewAddsHTTPSScheme(t *testing.T) {
	clearKindeEnv(t)
	c := mustNew(t, Config{Domain: "example.kinde.com", ClientID: "id", ClientSecret: "secret"})
	if c.domain != "https://example.kinde.com" {
		t.Fatalf("domain = %q, want https://example.kinde.com", c.domain)
	}
}

func TestNewRequiresDomainAndCredentials(t *testing.T) {
	clearKindeEnv(t)
	if _, err := New(Config{Domain: "https://example.kinde.com"}); err == nil {
		t.Fatal("expected an error without client credentials")
	}
}

func TestCheckCredentialsRejectsWrongSecret(t *testing.T) {
	clearKindeEnv(t)
	k := newTokenServer(t, 3600)
	c := mustNew(t, Config{Domain: k.srv.URL, ClientID: "id", ClientSecret: "wrong"})
	if err := c.CheckCredentials(); err == nil {
		t.Fatal("expected an error for a wrong secret")
	}
}

func TestTokenIsReusedUntilExpiry(t *testing.T) {
	clearKindeEnv(t)
	k := newTokenServer(t, 3600)
	c := mustNew(t, Config{Domain: k.srv.URL, ClientID: "id", ClientSecret: "secret"})
	var out struct{ Name string }
	for range 2 {
		if err := c.getJSON(t.Context(), "ListThings", "/api/v1/things", nil, &out); err != nil {
			t.Fatal(err)
		}
	}
	if got := k.tokens.Load(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func TestTokenIsRefreshedWhenExpired(t *testing.T) {
	clearKindeEnv(t)
	// oauth2 treats a token as expired 10 seconds early, so a 1-second token
	// is stale as soon as it arrives.
	k := newTokenServer(t, 1)
	c := mustNew(t, Config{Domain: k.srv.URL, ClientID: "id", ClientSecret: "secret"})
	var out struct{ Name string }
	for range 2 {
		if err := c.getJSON(t.Context(), "ListThings", "/api/v1/things", nil, &out); err != nil {
			t.Fatal(err)
		}
	}
	if got := k.tokens.Load(); got != 2 {
		t.Fatalf("token requests = %d, want 2", got)
	}
}

func TestGetJSONDecodesBodyAndSendsQuery(t *testing.T) {
	clearKindeEnv(t)
	k := newTokenServer(t, 3600)
	c := mustNew(t, Config{Domain: k.srv.URL, ClientID: "id", ClientSecret: "secret"})
	var out struct {
		Name string `json:"name"`
	}
	if err := c.getJSON(t.Context(), "ListThings", "/api/v1/things", map[string][]string{"name": {"widget"}}, &out); err != nil {
		t.Fatal(err)
	}
	if out.Name != "widget" {
		t.Fatalf("name = %q, want widget", out.Name)
	}
}

func TestGetJSONReturnsAPIError(t *testing.T) {
	clearKindeEnv(t)
	k := newTokenServer(t, 3600)
	c := mustNew(t, Config{Domain: k.srv.URL, ClientID: "id", ClientSecret: "secret"})
	var out struct{}
	err := c.getJSON(t.Context(), "GetMissing", "/api/v1/missing", nil, &out)
	if !IsNotFound(err) || !HasCode(err, "ROLE_NOT_FOUND") {
		t.Fatalf("expected a ROLE_NOT_FOUND 404, got %v", err)
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/ -run 'New|Token|CheckCredentials|GetJSON'`
Expected: FAIL to compile with `undefined: New`.

- [ ] **Step 4: Implement the client**

`internal/kindeapi/client.go`:

```go
package kindeapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/clientcredentials"
)

// Config holds the settings for one Kinde business. Empty fields fall back to
// the KINDE_DOMAIN, KINDE_AUDIENCE, KINDE_CLIENT_ID, and KINDE_CLIENT_SECRET
// environment variables.
type Config struct {
	Domain       string
	Audience     string
	ClientID     string
	ClientSecret string
}

func (c Config) withEnv() Config {
	if c.Domain == "" {
		c.Domain = os.Getenv("KINDE_DOMAIN")
	}
	if c.Audience == "" {
		c.Audience = os.Getenv("KINDE_AUDIENCE")
	}
	if c.ClientID == "" {
		c.ClientID = os.Getenv("KINDE_CLIENT_ID")
	}
	if c.ClientSecret == "" {
		c.ClientSecret = os.Getenv("KINDE_CLIENT_SECRET")
	}
	return c
}

// Client calls the Kinde management API.
type Client struct {
	api    *mgmt.Client
	http   *http.Client
	domain string
	tokens oauth2.TokenSource
}

// New builds a client without making network calls. Call CheckCredentials to
// verify the settings.
func New(cfg Config) (*Client, error) {
	cfg = cfg.withEnv()
	if cfg.Domain == "" || cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("kinde: domain, client_id, and client_secret are required; set them in the provider block or with KINDE_DOMAIN, KINDE_CLIENT_ID, and KINDE_CLIENT_SECRET")
	}

	domain := strings.TrimRight(cfg.Domain, "/")
	if !strings.Contains(domain, "://") {
		domain = "https://" + domain
	}
	audience := cfg.Audience
	if audience == "" {
		audience = domain + "/api"
	}

	cc := clientcredentials.Config{
		ClientID:       cfg.ClientID,
		ClientSecret:   cfg.ClientSecret,
		TokenURL:       domain + "/oauth2/token",
		EndpointParams: url.Values{"audience": {audience}},
		AuthStyle:      oauth2.AuthStyleInParams,
	}
	// The token source outlives the RPC that configured the provider, so it
	// must not be bound to that RPC's context.
	tokens := cc.TokenSource(context.Background())

	httpClient := &http.Client{Transport: captureTransport{next: retryTransport{
		next:    http.DefaultTransport,
		retries: maxRetries,
		sleep:   sleepCtx,
	}}}
	api, err := mgmt.NewClient(domain, tokenSecurity{tokens: tokens}, mgmt.WithClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("kinde: %w", err)
	}
	return &Client{api: api, http: httpClient, domain: domain, tokens: tokens}, nil
}

// CheckCredentials fetches an access token. A wrong domain, client ID,
// secret, or audience fails here instead of on the first API call.
func (c *Client) CheckCredentials() error {
	if _, err := c.tokens.Token(); err != nil {
		return fmt.Errorf("kinde: fetching access token: %w", err)
	}
	return nil
}

// tokenSecurity supplies the generated client with the current access token.
type tokenSecurity struct {
	tokens oauth2.TokenSource
}

func (s tokenSecurity) KindeBearerAuth(_ context.Context, _ mgmt.OperationName) (mgmt.KindeBearerAuth, error) {
	tok, err := s.tokens.Token()
	if err != nil {
		return mgmt.KindeBearerAuth{}, err
	}
	return mgmt.KindeBearerAuth{Token: tok.AccessToken}, nil
}

// getJSON sends an authenticated GET to path and decodes a 2xx JSON body
// into out. It exists for endpoints whose real response the SDK cannot
// decode; error responses become *APIError.
func (c *Client) getJSON(ctx context.Context, op, path string, query url.Values, out any) error {
	tok, err := c.tokens.Token()
	if err != nil {
		return fmt.Errorf("kinde %s: fetching access token: %w", op, err)
	}
	u := c.domain + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return fmt.Errorf("kinde %s: %w", op, err)
	}
	req.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kinde %s: %w", op, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("kinde %s: %w", op, err)
	}
	if resp.StatusCode >= http.StatusMultipleChoices {
		return newAPIError(op, resp.StatusCode, body)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("kinde %s: decoding response: %w", op, err)
	}
	return nil
}
```

`Client.api` is first read in Task 5. Until then, `unused` may flag it; that is expected for this one field and clears in Task 5.

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/kindeapi/ -race -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/kindeapi
git commit -m "Build the Kinde client from the official SDK" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 3: Fake Kinde core

**Files:**
- Create: `internal/kindefake/fake.go`
- Create: `internal/kindefake/errors.go`
- Create: `internal/kindefake/handler.go`
- Test: `internal/kindefake/fake_test.go`
- Test: `internal/kindeapi/fake_test.go`

**Interfaces:**
- Consumes: `kindeapi.New`, `kindeapi.Config`, and `(*kindeapi.Client).CheckCredentials` from Task 2 (tests only; `kindefake` itself never imports `kindeapi`).
- Produces:
  - `type Fake struct { URL, ClientID, ClientSecret, Audience string; ... }`.
  - `func New(t testing.TB) *Fake`, `func (f *Fake) Throttle(n int)`, `func (f *Fake) TokenRequests() int`.
  - `type apiError struct { status int; code, message string }` and `func writeAPIError(w http.ResponseWriter, e *apiError)`.
  - `type handler struct { mgmt.UnimplementedHandler; f *Fake }`. Domain tasks add operation methods with value receivers, for example `func (h handler) GetRole(ctx context.Context, params mgmt.GetRoleParams) (mgmt.GetRoleRes, error)`.
  - In package `kindeapi_test`: `func newFakeClient(t *testing.T) (*kindefake.Fake, *kindeapi.Client)`.
- Domain tasks extend `Fake`. They add state fields under the `// Domain state.` comment, initialize them under `// Initialize domain state.` in `New`, and add exported test hooks named `Remove<Thing>(id string)` that delete an object behind the provider's back.

- [ ] **Step 1: Write the failing fake tests**

`internal/kindefake/fake_test.go`:

```go
package kindefake_test

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

func fetchToken(t *testing.T, f *kindefake.Fake) string {
	t.Helper()
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {f.ClientID},
		"client_secret": {f.ClientSecret},
		"audience":      {f.Audience},
	}
	resp, err := http.PostForm(f.URL+"/oauth2/token", form)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body.AccessToken
}

func get(t *testing.T, f *kindefake.Fake, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, f.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func errorCode(t *testing.T, resp *http.Response) string {
	t.Helper()
	var body struct {
		Errors []struct {
			Code string `json:"code"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || len(body.Errors) == 0 {
		t.Fatalf("expected a Kinde error body: %v", err)
	}
	return body.Errors[0].Code
}

func TestRejectsMissingToken(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/business", "")
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestRejectsWrongToken(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/business", "not-the-token")
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, resp) != "UNAUTHORIZED" {
		t.Fatalf("status = %d, want 401 UNAUTHORIZED", resp.StatusCode)
	}
}

func TestUnimplementedOperationsReturn501(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/business", fetchToken(t, f))
	if resp.StatusCode != http.StatusNotImplemented || errorCode(t, resp) != "NOT_IMPLEMENTED" {
		t.Fatalf("status = %d, want 501 NOT_IMPLEMENTED", resp.StatusCode)
	}
}

func TestUnknownRoutesReturn501(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/no-such-route", fetchToken(t, f))
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", resp.StatusCode)
	}
}

func TestThrottleAnswers429ThenRecovers(t *testing.T) {
	f := kindefake.New(t)
	token := fetchToken(t, f)
	f.Throttle(1)

	resp := get(t, f, "/api/v1/business", token)
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") != "0" {
		t.Fatalf("status = %d, Retry-After = %q; want 429 and 0", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
	if resp := get(t, f, "/api/v1/business", token); resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status after throttling = %d, want 501", resp.StatusCode)
	}
}

func TestTokenEndpointRejectsWrongSecret(t *testing.T) {
	f := kindefake.New(t)
	resp, err := http.Post(f.URL+"/oauth2/token", "application/x-www-form-urlencoded",
		strings.NewReader(url.Values{"grant_type": {"client_credentials"}, "client_id": {f.ClientID}, "client_secret": {"wrong"}, "audience": {f.Audience}}.Encode()))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}
```

`internal/kindeapi/fake_test.go`:

```go
package kindeapi_test

import (
	"testing"

	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

// newFakeClient starts a fake Kinde and returns a client configured for it.
func newFakeClient(t *testing.T) (*kindefake.Fake, *kindeapi.Client) {
	t.Helper()
	f := kindefake.New(t)
	c, err := kindeapi.New(kindeapi.Config{
		Domain:       f.URL,
		Audience:     f.Audience,
		ClientID:     f.ClientID,
		ClientSecret: f.ClientSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

func TestCheckCredentialsAgainstFake(t *testing.T) {
	f, c := newFakeClient(t)
	if err := c.CheckCredentials(); err != nil {
		t.Fatal(err)
	}
	if got := f.TokenRequests(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func TestCheckCredentialsRejectedByFake(t *testing.T) {
	f := kindefake.New(t)
	c, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CheckCredentials(); err == nil {
		t.Fatal("expected the fake to reject a wrong secret")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindefake/ ./internal/kindeapi/`
Expected: FAIL with `no required module provides package .../internal/kindefake` or `undefined: kindefake.New`.

- [ ] **Step 3: Implement the fake core**

`internal/kindefake/fake.go`:

```go
// Package kindefake is an in-memory fake of the Kinde management API for
// tests. It runs the server ogen generated from Kinde's OpenAPI spec, so it
// encodes our reading of that spec rather than the live API's behavior.
package kindefake

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// Fake is a running fake Kinde. Create one with New.
type Fake struct {
	// URL is the fake's base URL. Use it as the provider's domain.
	URL string
	// ClientID, ClientSecret, and Audience are the only credentials the
	// token endpoint accepts.
	ClientID     string
	ClientSecret string
	Audience     string

	token string

	mu            sync.Mutex
	tokenRequests int
	throttle      int

	// Domain state.
}

// New starts a fake Kinde that shuts down when the test ends.
func New(t testing.TB) *Fake {
	t.Helper()
	f := &Fake{
		ClientID:     "kindefake-client",
		ClientSecret: "kindefake-secret",
		token:        "kindefake-token",
	}
	// Initialize domain state.

	api, err := mgmt.NewServer(handler{f: f}, security{f: f},
		mgmt.WithErrorHandler(writeError),
		mgmt.WithNotFound(func(w http.ResponseWriter, r *http.Request) {
			writeAPIError(w, &apiError{status: http.StatusNotImplemented, code: "NOT_IMPLEMENTED", message: "kindefake: no route for " + r.Method + " " + r.URL.Path})
		}),
	)
	if err != nil {
		t.Fatalf("kindefake: creating server: %s", err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", f.serveToken)
	mux.Handle("/", api)

	srv := httptest.NewServer(f.throttled(mux))
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	f.Audience = srv.URL + "/api"
	return f
}

// Throttle makes the next n API requests fail with HTTP 429 and
// Retry-After: 0. Token requests are never throttled.
func (f *Fake) Throttle(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.throttle = n
}

// TokenRequests returns how many token requests the fake has served.
func (f *Fake) TokenRequests() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tokenRequests
}

func (f *Fake) throttled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth2/token" && f.takeThrottle() {
			w.Header().Set("Retry-After", "0")
			writeAPIError(w, &apiError{status: http.StatusTooManyRequests, code: "TOO_MANY_REQUESTS", message: "kindefake: throttled"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (f *Fake) takeThrottle() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.throttle == 0 {
		return false
	}
	f.throttle--
	return true
}

func (f *Fake) serveToken(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	f.tokenRequests++
	f.mu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	if r.FormValue("grant_type") != "client_credentials" ||
		r.FormValue("client_id") != f.ClientID ||
		r.FormValue("client_secret") != f.ClientSecret ||
		r.FormValue("audience") != f.Audience {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid_client"})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": f.token,
		"token_type":   "bearer",
		"expires_in":   3600,
	})
}
```

`internal/kindefake/errors.go`:

```go
package kindefake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
)

// apiError makes the fake answer with a Kinde-shaped error response.
// Handlers return it as their error.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%d %s: %s", e.status, e.code, e.message)
}

// writeError renders handler, security, and request-decoding errors.
func writeError(_ context.Context, w http.ResponseWriter, r *http.Request, err error) {
	var e *apiError
	var secErr *ogenerrors.SecurityError
	switch {
	case errors.As(err, &e):
	case errors.As(err, &secErr):
		e = &apiError{status: http.StatusUnauthorized, code: "UNAUTHORIZED", message: "kindefake: missing access token"}
	case errors.Is(err, ht.ErrNotImplemented):
		e = &apiError{status: http.StatusNotImplemented, code: "NOT_IMPLEMENTED", message: "kindefake: " + r.Method + " " + r.URL.Path + " is not implemented"}
	default:
		e = &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: err.Error()}
	}
	writeAPIError(w, e)
}

// writeAPIError writes e in Kinde's {"errors":[{"code","message"}]} shape.
func writeAPIError(w http.ResponseWriter, e *apiError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"code": e.code, "message": e.message}},
	})
}
```

`internal/kindefake/handler.go`:

```go
package kindefake

import (
	"context"
	"net/http"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// handler implements the generated server. Operations the provider does not
// call fall through to UnimplementedHandler and answer HTTP 501.
type handler struct {
	mgmt.UnimplementedHandler
	f *Fake
}

// security accepts only the token the fake issued.
type security struct {
	f *Fake
}

func (s security) HandleKindeBearerAuth(ctx context.Context, _ mgmt.OperationName, t mgmt.KindeBearerAuth) (context.Context, error) {
	if t.Token != s.f.token {
		return ctx, &apiError{status: http.StatusUnauthorized, code: "UNAUTHORIZED", message: "kindefake: invalid access token"}
	}
	return ctx, nil
}
```

Run `go mod tidy`. It moves `github.com/ogen-go/ogen` to a direct requirement.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/kindefake/ ./internal/kindeapi/ -race -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/kindefake internal/kindeapi/fake_test.go
git commit -m "Add an in-memory fake Kinde for tests" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 4: Provider wiring and test harness

**Files:**
- Create: `internal/provider/provider_data.go`
- Modify: `internal/provider/provider.go` (`Schema` and `Configure`)
- Modify the `Configure` method of every resource and data source:
  - `api_data_source.go`, `api_resource.go`
  - `application_connection_resource.go`, `application_data_source.go`, `application_resource.go`
  - `connection_resource.go`, `connections_data_source.go`
  - `organization_resource.go`, `organization_user_resource.go`
  - `permission_resource.go`, `role_resource.go`
  - `user_resource.go`, `user_role_resource.go`
- Modify: `internal/provider/provider_test.go`
- Create: `internal/provider/not_found_test.go`

**Interfaces:**
- Consumes: `kindeapi.New`, `kindeapi.Config`, `(*kindeapi.Client).CheckCredentials`, `kindefake.New`, and `Fake` fields.
- Produces:
  - `type providerData struct { api *kindeapi.Client; legacy *kinde.Client }`.
  - `func providerDataFrom(data any, diags *diag.Diagnostics) *providerData`, which returns nil, without adding diagnostics, when the provider is not configured yet.
  - `func testAccFake(t *testing.T) *kindefake.Fake` (tests).
  - In `not_found_test.go`:
    - `type notFoundCase struct { name string; resource func() resource.Resource; attrs map[string]string }`;
    - `func TestReadRemovesMissingObjects(t *testing.T)`, whose `tests` slice each domain task extends;
    - `func stateWithAttrs(t *testing.T, s schema.Schema, attrs map[string]string) tfsdk.State`.
- Every migrated resource follows this `Configure` pattern:

  ```go
  pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
  if pd == nil {
  	return
  }
  r.client = pd.api
  ```

- [ ] **Step 1: Write the failing provider tests**

Add to `internal/provider/provider_test.go`. Keep `testAccProtoV6ProviderFactories` and `testAccPreCheck`; Task 20 deletes `testAccPreCheck`.

```go
// testAccFake starts a fake Kinde and points the provider at it through the
// KINDE_* environment variables. Tests that call it must not run in parallel.
func testAccFake(t *testing.T) *kindefake.Fake {
	t.Helper()
	f := kindefake.New(t)
	t.Setenv("KINDE_DOMAIN", f.URL)
	t.Setenv("KINDE_AUDIENCE", f.Audience)
	t.Setenv("KINDE_CLIENT_ID", f.ClientID)
	t.Setenv("KINDE_CLIENT_SECRET", f.ClientSecret)
	return f
}

func TestProviderSchemaMarksClientSecretSensitive(t *testing.T) {
	var resp provider.SchemaResponse
	New("test")().Schema(t.Context(), provider.SchemaRequest{}, &resp)
	attr, ok := resp.Schema.Attributes["client_secret"]
	if !ok || !attr.IsSensitive() {
		t.Fatal("client_secret must be marked sensitive")
	}
}

func TestAccProviderRejectsBadCredentials(t *testing.T) {
	testAccFake(t)
	t.Setenv("KINDE_CLIENT_SECRET", "wrong")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      `data "kinde_api" "test" { id = "api_0001" }`,
			ExpectError: regexp.MustCompile(`Unable to Create Kinde Client`),
		}},
	})
}
```

The new imports are:
- `regexp`
- `github.com/hashicorp/terraform-plugin-framework/provider`
- `github.com/hashicorp/terraform-plugin-testing/helper/resource`
- `github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake`

Create `internal/provider/not_found_test.go`:

```go
package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

// notFoundCase reads a resource whose Kinde object does not exist.
type notFoundCase struct {
	name     string
	resource func() resource.Resource
	// attrs are the state attributes Read needs, such as "id". Every other
	// attribute is null.
	attrs map[string]string
}

// TestReadRemovesMissingObjects checks that Read removes a resource from
// state, without an error, when its object no longer exists in Kinde.
func TestReadRemovesMissingObjects(t *testing.T) {
	tests := []notFoundCase{
		// Each domain task adds its resources here as they move to kindeapi.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := kindefake.New(t)
			client, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: f.ClientSecret})
			if err != nil {
				t.Fatal(err)
			}

			r := tt.resource()
			if rc, ok := r.(resource.ResourceWithConfigure); ok {
				var cresp resource.ConfigureResponse
				rc.Configure(t.Context(), resource.ConfigureRequest{ProviderData: &providerData{api: client}}, &cresp)
				requireNoErrors(t, cresp.Diagnostics)
			}
			var sresp resource.SchemaResponse
			r.Schema(t.Context(), resource.SchemaRequest{}, &sresp)

			state := stateWithAttrs(t, sresp.Schema, tt.attrs)
			resp := resource.ReadResponse{State: tfsdk.State{Schema: sresp.Schema, Raw: state.Raw.Copy()}}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			requireNoErrors(t, resp.Diagnostics)
			if !resp.State.Raw.IsNull() {
				t.Fatalf("expected %s to be removed from state", tt.name)
			}
		})
	}
}

// stateWithAttrs builds state for s with the given string attributes set and
// every other attribute null.
func stateWithAttrs(t *testing.T, s schema.Schema, attrs map[string]string) tfsdk.State {
	t.Helper()
	objType, ok := s.Type().TerraformType(t.Context()).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}
	for name := range attrs {
		if _, ok := objType.AttributeTypes[name]; !ok {
			t.Fatalf("schema has no attribute %q", name)
		}
	}
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		if v, ok := attrs[name]; ok {
			values[name] = tftypes.NewValue(typ, v)
		} else {
			values[name] = tftypes.NewValue(typ, nil)
		}
	}
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, values)}
}

func requireNoErrors(t *testing.T, diags diag.Diagnostics) {
	t.Helper()
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run 'ProviderSchema|ReadRemovesMissingObjects'`
Expected: FAIL to compile with `undefined: providerData`.

- [ ] **Step 3: Add `providerData`**

`internal/provider/provider_data.go`:

```go
package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/nxt-fwd/kinde-go"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// providerData is what Configure hands to every resource and data source.
type providerData struct {
	api *kindeapi.Client
	// legacy serves resources that have not moved to api yet.
	legacy *kinde.Client
}

// providerDataFrom extracts providerData in a resource or data source
// Configure. It returns nil without diagnostics before the provider is
// configured.
func providerDataFrom(data any, diags *diag.Diagnostics) *providerData {
	if data == nil {
		return nil
	}
	pd, ok := data.(*providerData)
	if !ok {
		diags.AddError(
			"Unexpected Configure Type",
			fmt.Sprintf("Expected *providerData, got: %T. Please report this issue to the provider developers.", data),
		)
		return nil
	}
	return pd
}
```

- [ ] **Step 4: Update the provider schema and `Configure`**

In `provider.go`, replace the `audience` and `client_secret` attributes:

```go
"audience": schema.StringAttribute{
	MarkdownDescription: "Kinde M2M application audience, also set by KINDE_AUDIENCE. Defaults to `<domain>/api`.",
	Optional:            true,
},
```

```go
"client_secret": schema.StringAttribute{
	MarkdownDescription: "Kinde M2M application client secret, also set by KINDE_CLIENT_SECRET",
	Optional:            true,
	Sensitive:           true,
},
```

Replace `Configure`'s body from `opts := kinde.NewClientOptions()` to the end of the function. Unknown values read as `""` and fall back to the environment, as before.

```go
	client, err := kindeapi.New(kindeapi.Config{
		Domain:       data.Domain.ValueString(),
		Audience:     data.Audience.ValueString(),
		ClientID:     data.ClientID.ValueString(),
		ClientSecret: data.ClientSecret.ValueString(),
	})
	if err == nil {
		err = client.CheckCredentials()
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Kinde Client",
			fmt.Sprintf("Failed to authenticate with Kinde API: %v\n"+
				"Please verify your domain, client_id, client_secret, and audience are correct.", err),
		)
		return
	}

	opts := kinde.NewClientOptions()
	if !data.Domain.IsNull() && !data.Domain.IsUnknown() {
		opts.WithDomain(data.Domain.ValueString())
	}
	if !data.Audience.IsNull() && !data.Audience.IsUnknown() {
		opts.WithAudience(data.Audience.ValueString())
	}
	if !data.ClientID.IsNull() && !data.ClientID.IsUnknown() {
		opts.WithClientID(data.ClientID.ValueString())
	}
	if !data.ClientSecret.IsNull() && !data.ClientSecret.IsUnknown() {
		opts.WithClientSecret(data.ClientSecret.ValueString())
	}
	legacy := kinde.New(ctx, opts)

	pd := &providerData{api: client, legacy: &legacy}
	resp.DataSourceData = pd
	resp.ResourceData = pd
```

Remove the `github.com/nxt-fwd/kinde-go/api/users` import and add `github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi`. `kinde.New` makes no network calls, so building the legacy client costs nothing.

- [ ] **Step 5: Point every `Configure` at `providerData`**

In each file below, replace the `Configure` body from `if req.ProviderData == nil {` through the line that assigns the client with the block shown. Use `d` instead of `r` in data sources. Keep any lines after the assignment, such as the `tflog.Debug` call in `application_resource.go`. In `application_resource.go`, also delete the `client.Applications == nil` check.

```go
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.legacy.<Field>
```

| File | `<Field>` |
|---|---|
| `api_data_source.go`, `api_resource.go` | `APIs` |
| `application_connection_resource.go`, `application_data_source.go`, `application_resource.go` | `Applications` |
| `connection_resource.go`, `connections_data_source.go` | `Connections` |
| `organization_resource.go`, `organization_user_resource.go`, `user_role_resource.go` | `Organizations` |
| `permission_resource.go` | `Permissions` |
| `role_resource.go` | `Roles` |
| `user_resource.go` | `Users` |

Remove each file's now-unused `github.com/nxt-fwd/kinde-go` root import, and the `fmt` import in `api_data_source.go`, which only the deleted error message used. Keep the `api/...` subpackage imports. `go build ./...` lists anything left over.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/... && TF_ACC=1 go test ./internal/provider/ -run 'TestAccProviderRejectsBadCredentials|TestProviderSchema|TestReadRemovesMissingObjects' -v`
Expected: PASS. `TestReadRemovesMissingObjects` passes with no subtests.

- [ ] **Step 7: Commit**

```bash
git add internal/provider
git commit -m "Configure the provider with the kindeapi client" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 5: Permissions adapter and fake

**Files:**
- Create: `internal/kindeapi/pagination.go`
- Create: `internal/kindeapi/permissions.go`
- Modify: `internal/kindefake/fake.go` (`Fake` fields, `New`, and the `newID`, `LimitPageSize`, and `nextTokenPage` helpers)
- Modify: `internal/kindefake/errors.go` (`notFound`)
- Modify: `internal/kindefake/handler.go` (`success`)
- Create: `internal/kindefake/permissions.go`
- Test: `internal/kindeapi/pagination_test.go`
- Test: `internal/kindeapi/permissions_test.go`

**Interfaces:**
- Consumes: `call[T]` (Task 1); `Client.api` (Task 2); `Fake`, `apiError`, `handler`, and `newFakeClient` (Task 3).
- Produces:
  - In `internal/kindeapi/pagination.go`: `const maxPageSize = 100` and `func allPages[T any](ctx context.Context, fetch func(ctx context.Context, nextToken string) ([]T, string, error)) ([]T, error)`.
  - `func (c *Client) CreatePermission(ctx context.Context, req mgmt.CreatePermissionReq) error`. Kinde's response carries no ID; callers find the new permission with `ListPermissions`.
  - `func (c *Client) ListPermissions(ctx context.Context) ([]mgmt.Permissions, error)`, which returns every page.
  - `func (c *Client) UpdatePermission(ctx context.Context, id string, req mgmt.UpdatePermissionsReq) error`.
  - `func (c *Client) DeletePermission(ctx context.Context, id string) error`.
  - In `kindefake`:
    - `func (f *Fake) newID(prefix string) string`, `func notFound(code, message string) error`, and `func success() *mgmt.SuccessResponse`;
    - `func (f *Fake) LimitPageSize(n int)`, a test hook that caps every next_token page at `n` items;
    - `const defaultPageSize = 10` and `func nextTokenPage[T any](f *Fake, items []T, pageSize mgmt.OptNilInt, nextToken mgmt.OptNilString) ([]T, string, error)`, the shared next_token pager;
    - `func (f *Fake) RemovePermission(id string)` and `func (f *Fake) sortedPermissions() []mgmt.Permissions`;
    - handler methods `CreatePermission`, `GetPermissions`, `UpdatePermissions`, and `DeletePermission`.
  - Test helpers in package `kindeapi_test`: `func createPermission(t *testing.T, c *kindeapi.Client, key string) string` and `func findPermission(t *testing.T, c *kindeapi.Client, key string) mgmt.Permissions`.

List methods ask for `page_size=100`. The spec gives no limit for `/api/v1/permissions` or `/api/v1/roles/{role_id}/permissions`. The only limit it documents anywhere is 100, in a `PAGE_SIZE_LIMIT_EXCEEDED` example. Fake tests use `LimitPageSize` to cross page boundaries with a few objects.

- [ ] **Step 1: Write the failing tests**

`internal/kindeapi/pagination_test.go`:

```go
package kindeapi

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestAllPagesFollowsNextToken(t *testing.T) {
	pages := map[string]struct {
		items []int
		next  string
	}{
		"":  {[]int{1, 2}, "a"},
		"a": {[]int{3, 4}, "b"},
		"b": {[]int{5}, ""},
	}
	var tokens []string
	got, err := allPages(t.Context(), func(_ context.Context, nextToken string) ([]int, string, error) {
		tokens = append(tokens, nextToken)
		p := pages[nextToken]
		return p.items, p.next, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if want := []string{"", "a", "b"}; !slices.Equal(tokens, want) {
		t.Fatalf("tokens = %q, want %q", tokens, want)
	}
}

func TestAllPagesStopsWhenATokenRepeats(t *testing.T) {
	calls := 0
	_, err := allPages(t.Context(), func(context.Context, string) ([]int, string, error) {
		calls++
		return []int{calls}, "same", nil
	})
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("expected a repeated-token error, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestAllPagesReturnsFetchErrors(t *testing.T) {
	boom := errors.New("boom")
	_, err := allPages(t.Context(), func(_ context.Context, nextToken string) ([]int, string, error) {
		if nextToken == "" {
			return []int{1}, "a", nil
		}
		return nil, "", boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
}
```

`internal/kindeapi/permissions_test.go`:

```go
package kindeapi_test

import (
	"fmt"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// createPermission creates a permission with the given key and returns its
// ID. Kinde's create response has no ID, so it lists to find it.
func createPermission(t *testing.T, c *kindeapi.Client, key string) string {
	t.Helper()
	err := c.CreatePermission(t.Context(), mgmt.CreatePermissionReq{
		Name: mgmt.NewOptString("Permission " + key),
		Key:  mgmt.NewOptString(key),
	})
	if err != nil {
		t.Fatal(err)
	}
	return findPermission(t, c, key).ID.Value
}

// findPermission returns the permission with the given key.
func findPermission(t *testing.T, c *kindeapi.Client, key string) mgmt.Permissions {
	t.Helper()
	perms, err := c.ListPermissions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range perms {
		if p.Key.Value == key {
			return p
		}
	}
	t.Fatalf("no permission with key %q", key)
	return mgmt.Permissions{}
}

func TestPermissionLifecycle(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()

	err := c.CreatePermission(ctx, mgmt.CreatePermissionReq{
		Name:        mgmt.NewOptString("Read reports"),
		Key:         mgmt.NewOptString("read:reports"),
		Description: mgmt.NewOptString("Lets users read reports"),
	})
	if err != nil {
		t.Fatal(err)
	}
	created := findPermission(t, c, "read:reports")
	id, ok := created.ID.Get()
	if !ok || id == "" {
		t.Fatalf("expected an ID, got %+v", created)
	}

	err = c.UpdatePermission(ctx, id, mgmt.UpdatePermissionsReq{
		Name: mgmt.NewOptString("Read all reports"),
		Key:  mgmt.NewOptString("read:all-reports"),
	})
	if err != nil {
		t.Fatal(err)
	}
	want := mgmt.Permissions{
		ID:          mgmt.NewOptString(id),
		Key:         mgmt.NewOptString("read:all-reports"),
		Name:        mgmt.NewOptString("Read all reports"),
		Description: mgmt.NewOptString("Lets users read reports"),
	}
	if got := findPermission(t, c, "read:all-reports"); got != want {
		t.Fatalf("after update got %+v, want %+v", got, want)
	}

	if err := c.DeletePermission(ctx, id); err != nil {
		t.Fatal(err)
	}
	perms, err := c.ListPermissions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(perms) != 0 {
		t.Fatalf("expected no permissions after delete, got %+v", perms)
	}
}

func TestUpdatePermissionSendsEmptyStringButNotUnset(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	err := c.CreatePermission(ctx, mgmt.CreatePermissionReq{
		Name:        mgmt.NewOptString("Read reports"),
		Key:         mgmt.NewOptString("read:reports"),
		Description: mgmt.NewOptString("Lets users read reports"),
	})
	if err != nil {
		t.Fatal(err)
	}
	id := findPermission(t, c, "read:reports").ID.Value

	// An unset description is not sent, so Kinde keeps the old one.
	if err := c.UpdatePermission(ctx, id, mgmt.UpdatePermissionsReq{Name: mgmt.NewOptString("Reports")}); err != nil {
		t.Fatal(err)
	}
	if got := findPermission(t, c, "read:reports").Description; got != mgmt.NewOptString("Lets users read reports") {
		t.Fatalf("description after unset update = %+v, want it unchanged", got)
	}

	// An empty description is sent and clears it.
	if err := c.UpdatePermission(ctx, id, mgmt.UpdatePermissionsReq{Description: mgmt.NewOptString("")}); err != nil {
		t.Fatal(err)
	}
	if got := findPermission(t, c, "read:reports").Description; got != mgmt.NewOptString("") {
		t.Fatalf("description after empty update = %+v, want set to \"\"", got)
	}
}

func TestPermissionNotFound(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	err := c.UpdatePermission(ctx, "perm_missing", mgmt.UpdatePermissionsReq{Name: mgmt.NewOptString("Missing")})
	if !kindeapi.IsNotFound(err) {
		t.Fatalf("update: expected not found, got %v", err)
	}
	err = c.DeletePermission(ctx, "perm_missing")
	if !kindeapi.IsNotFound(err) || !kindeapi.HasCode(err, "PERMISSION_NOT_FOUND") {
		t.Fatalf("delete: expected PERMISSION_NOT_FOUND, got %v", err)
	}
}

func TestListPermissionsReturnsEveryPage(t *testing.T) {
	f, c := newFakeClient(t)
	f.LimitPageSize(2)
	want := map[string]bool{}
	for i := range 5 {
		want[createPermission(t, c, fmt.Sprintf("perm:%d", i))] = true
	}

	perms, err := c.ListPermissions(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(perms) != len(want) {
		t.Fatalf("got %d permissions, want %d", len(perms), len(want))
	}
	for _, p := range perms {
		if !want[p.ID.Value] {
			t.Fatalf("unexpected permission %+v", p)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/`
Expected: FAIL to compile with `undefined: allPages`.

- [ ] **Step 3: Implement `allPages`**

`internal/kindeapi/pagination.go`:

```go
package kindeapi

import (
	"context"
	"fmt"
)

// maxPageSize is the page size list methods ask for. It is the largest Kinde
// documents: its PAGE_SIZE_LIMIT_EXCEEDED example reads "Page size cannot be
// greater than 100".
const maxPageSize = 100

// allPages collects every page of a next_token-paginated endpoint. fetch gets
// "" for the first page and returns the page's items and the next token, or
// "" on the last page.
func allPages[T any](ctx context.Context, fetch func(ctx context.Context, nextToken string) ([]T, string, error)) ([]T, error) {
	var all []T
	seen := map[string]bool{}
	nextToken := ""
	for {
		items, next, err := fetch(ctx, nextToken)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("kinde: next_token %q repeated; stopping to avoid an endless loop", next)
		}
		seen[next] = true
		nextToken = next
	}
}
```

- [ ] **Step 4: Add the shared fake helpers**

In `internal/kindefake/fake.go`, add `"fmt"` and `"strconv"` to the imports. Add two fields after `throttle`, and the permissions map under `// Domain state.`:

```go
	mu            sync.Mutex
	tokenRequests int
	throttle      int
	nextID        int
	pageLimit     int

	// Domain state.
	permissions map[string]mgmt.Permissions
}
```

In `New`, under `// Initialize domain state.`, add:

```go
	f.permissions = map[string]mgmt.Permissions{}
```

Append to `internal/kindefake/fake.go`:

```go
// newID returns a unique ID such as "perm_0001". Callers must hold f.mu.
func (f *Fake) newID(prefix string) string {
	f.nextID++
	return fmt.Sprintf("%s_%04d", prefix, f.nextID)
}

// LimitPageSize makes next_token-paginated lists return at most n items per
// page, whatever page_size the client asks for, so tests can cross page
// boundaries with a few objects. Zero removes the limit.
func (f *Fake) LimitPageSize(n int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pageLimit = n
}

// defaultPageSize is the page size Kinde's spec documents for requests that
// do not send page_size.
const defaultPageSize = 10

// nextTokenPage returns the page of items that nextToken points at, and the
// token for the page after it, or "" on the last page. Tokens are offsets
// into items, so callers must pass items in a stable order. Callers must hold
// f.mu.
func nextTokenPage[T any](f *Fake, items []T, pageSize mgmt.OptNilInt, nextToken mgmt.OptNilString) ([]T, string, error) {
	size := pageSize.Or(defaultPageSize)
	if size < 1 {
		size = defaultPageSize
	}
	if f.pageLimit > 0 {
		size = min(size, f.pageLimit)
	}
	start := 0
	if token := nextToken.Or(""); token != "" {
		n, err := strconv.Atoi(token)
		if err != nil || n < 0 || n > len(items) {
			return nil, "", &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: invalid next_token " + strconv.Quote(token)}
		}
		start = n
	}
	end := min(start+size, len(items))
	next := ""
	if end < len(items) {
		next = strconv.Itoa(end)
	}
	return items[start:end], next, nil
}
```

Append to `internal/kindefake/errors.go`:

```go
// notFound returns a 404 with Kinde error code code.
func notFound(code, message string) error {
	return &apiError{status: http.StatusNotFound, code: code, message: message}
}
```

Append to `internal/kindefake/handler.go`:

```go
// success returns the body Kinde sends when an operation has nothing else to
// report.
func success() *mgmt.SuccessResponse {
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Success")}
}
```

- [ ] **Step 5: Implement the fake's permission endpoints**

`internal/kindefake/permissions.go`:

```go
package kindefake

import (
	"context"
	"slices"
	"strings"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreatePermission stores a permission. Like Kinde, it answers with a bare
// success body that carries no ID.
func (h handler) CreatePermission(_ context.Context, req mgmt.OptCreatePermissionReq) (mgmt.CreatePermissionRes, error) {
	body, _ := req.Get()
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	id := h.f.newID("perm")
	h.f.permissions[id] = mgmt.Permissions{
		ID:          mgmt.NewOptString(id),
		Key:         body.Key,
		Name:        body.Name,
		Description: body.Description,
	}
	return success(), nil
}

// GetPermissions lists permissions in ID order. It ignores sort.
func (h handler) GetPermissions(_ context.Context, params mgmt.GetPermissionsParams) (mgmt.GetPermissionsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	page, next, err := nextTokenPage(h.f, h.f.sortedPermissions(), params.PageSize, params.NextToken)
	if err != nil {
		return nil, err
	}
	res := &mgmt.GetPermissionsResponse{
		Code:        mgmt.NewOptString("OK"),
		Message:     mgmt.NewOptString("Success"),
		Permissions: page,
	}
	if next != "" {
		res.NextToken = mgmt.NewOptString(next)
	}
	return res, nil
}

// UpdatePermissions changes the fields the request sets and leaves the rest
// alone.
func (h handler) UpdatePermissions(_ context.Context, req mgmt.OptUpdatePermissionsReq, params mgmt.UpdatePermissionsParams) (mgmt.UpdatePermissionsRes, error) {
	body, _ := req.Get()
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	p, ok := h.f.permissions[params.PermissionID]
	if !ok {
		return nil, permissionNotFound()
	}
	if body.Name.Set {
		p.Name = body.Name
	}
	if body.Key.Set {
		p.Key = body.Key
	}
	if body.Description.Set {
		p.Description = body.Description
	}
	h.f.permissions[params.PermissionID] = p
	return success(), nil
}

// DeletePermission deletes a permission.
func (h handler) DeletePermission(_ context.Context, params mgmt.DeletePermissionParams) (mgmt.DeletePermissionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.permissions[params.PermissionID]; !ok {
		return nil, permissionNotFound()
	}
	delete(h.f.permissions, params.PermissionID)
	return success(), nil
}

// RemovePermission deletes a permission behind the provider's back.
func (f *Fake) RemovePermission(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.permissions, id)
}

// sortedPermissions returns every permission in ID order. Callers must hold
// f.mu.
func (f *Fake) sortedPermissions() []mgmt.Permissions {
	perms := make([]mgmt.Permissions, 0, len(f.permissions))
	for _, p := range f.permissions {
		perms = append(perms, p)
	}
	slices.SortFunc(perms, func(a, b mgmt.Permissions) int {
		return strings.Compare(a.ID.Value, b.ID.Value)
	})
	return perms
}

func permissionNotFound() error {
	return notFound("PERMISSION_NOT_FOUND", "Permission not found")
}
```

- [ ] **Step 6: Implement the adapter methods**

`internal/kindeapi/permissions.go`:

```go
package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreatePermission creates a permission. Kinde's response carries no ID, so
// callers find the new permission with ListPermissions.
func (c *Client) CreatePermission(ctx context.Context, req mgmt.CreatePermissionReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "CreatePermission", func(ctx context.Context) (any, error) {
		return c.api.CreatePermission(ctx, mgmt.NewOptCreatePermissionReq(req))
	})
	return err
}

// ListPermissions returns every permission in the business.
func (c *Client) ListPermissions(ctx context.Context) ([]mgmt.Permissions, error) {
	return allPages(ctx, func(ctx context.Context, nextToken string) ([]mgmt.Permissions, string, error) {
		params := mgmt.GetPermissionsParams{PageSize: mgmt.NewOptNilInt(maxPageSize)}
		if nextToken != "" {
			params.NextToken = mgmt.NewOptNilString(nextToken)
		}
		res, err := call[*mgmt.GetPermissionsResponse](ctx, "GetPermissions", func(ctx context.Context) (any, error) {
			return c.api.GetPermissions(ctx, params)
		})
		if err != nil {
			return nil, "", err
		}
		return res.Permissions, res.NextToken.Or(""), nil
	})
}

// UpdatePermission changes a permission. Fields left unset in req are not
// sent, so Kinde leaves them alone.
func (c *Client) UpdatePermission(ctx context.Context, id string, req mgmt.UpdatePermissionsReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "UpdatePermissions", func(ctx context.Context) (any, error) {
		return c.api.UpdatePermissions(ctx, mgmt.NewOptUpdatePermissionsReq(req), mgmt.UpdatePermissionsParams{PermissionID: id})
	})
	return err
}

// DeletePermission deletes a permission.
func (c *Client) DeletePermission(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeletePermission", func(ctx context.Context) (any, error) {
		return c.api.DeletePermission(ctx, mgmt.DeletePermissionParams{PermissionID: id})
	})
	return err
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/kindeapi/ ./internal/kindefake/ -race -v`
Expected: PASS, including `TestAllPagesStopsWhenATokenRepeats`, `TestUpdatePermissionSendsEmptyStringButNotUnset`, and `TestListPermissionsReturnsEveryPage`.

- [ ] **Step 8: Commit**

```bash
git add internal/kindeapi internal/kindefake
git commit -m "Add permission endpoints to kindeapi and the fake" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 6: Move `kinde_permission` to kindeapi

**Files:**
- Create: `internal/provider/values.go`
- Test: `internal/provider/values_test.go`
- Modify: `internal/provider/permission_resource.go` (rewritten; drops `permissionsPage` and `listAllPermissions`)
- Modify: `internal/provider/permission_schema.go` (rewritten; drops the unused `PermissionDataSourceModel` and its `//nolint:unused` helpers, which used old-library types)
- Modify: `internal/provider/permission_resource_test.go`
- Modify: `internal/provider/not_found_test.go`

**Interfaces:**
- Consumes: `CreatePermission`, `ListPermissions`, `UpdatePermission`, `DeletePermission`, and `Fake.RemovePermission` (Task 5); `kindeapi.IsNotFound` (Task 1); `providerDataFrom`, `testAccFake`, and `TestReadRemovesMissingObjects` (Task 4).
- Produces:
  - `func stringValue(v mgmt.OptString) types.String` (unset becomes null) and `func optString(v types.String) mgmt.OptString` (null or unknown becomes unset; a known `""` is sent as `""`).
  - `func permissionByID(perms []mgmt.Permissions, id string) (mgmt.Permissions, bool)` and `func permissionByNameAndKey(perms []mgmt.Permissions, name, key string) (mgmt.Permissions, bool)`.
  - `func expandPermissionCreateReq(d PermissionResourceModel) mgmt.CreatePermissionReq`, `func expandPermissionUpdateReq(d PermissionResourceModel) mgmt.UpdatePermissionsReq`, and `func flattenPermissionResource(permission mgmt.Permissions) PermissionResourceModel`.

Behavior changes:
- An omitted `description` is no longer sent as `""`. Before, a permission without a description failed on create with "Provider produced inconsistent result after apply".
- `description` becomes optional and computed, with `UseStateForUnknown`. Removing it from the configuration keeps the description Kinde already has instead of failing with an inconsistent result. Kinde keeps descriptions once set; that is also why `kinde_role.description` is required. `TestAccPermissionResource_NoDescription` pins both behaviors.
- `Read` keeps its list scan, with the name-and-key fallback. A permission that is gone is removed from state.
- `Update` finds the permission again by ID instead of by name and key.
- `Delete` treats not-found as success.

- [ ] **Step 1: Write the failing tests**

`internal/provider/values_test.go`:

```go
package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// TestOptString pins the difference between an attribute left out of the
// configuration, which is not sent, and one set to "", which is.
func TestOptString(t *testing.T) {
	tests := []struct {
		name     string
		in       types.String
		want     mgmt.OptString
		wantJSON string
	}{
		{"null", types.StringNull(), mgmt.OptString{}, `{"name":"Reports"}`},
		{"unknown", types.StringUnknown(), mgmt.OptString{}, `{"name":"Reports"}`},
		{"empty", types.StringValue(""), mgmt.NewOptString(""), `{"name":"Reports","description":""}`},
		{"value", types.StringValue("Read reports"), mgmt.NewOptString("Read reports"), `{"name":"Reports","description":"Read reports"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optString(tt.in)
			if got != tt.want {
				t.Fatalf("optString(%v) = %+v, want %+v", tt.in, got, tt.want)
			}
			req := mgmt.UpdatePermissionsReq{Name: mgmt.NewOptString("Reports"), Description: got}
			body, err := req.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tt.wantJSON {
				t.Fatalf("request body = %s, want %s", body, tt.wantJSON)
			}
		})
	}
}

func TestStringValue(t *testing.T) {
	tests := []struct {
		name string
		in   mgmt.OptString
		want types.String
	}{
		{"unset", mgmt.OptString{}, types.StringNull()},
		{"empty", mgmt.NewOptString(""), types.StringValue("")},
		{"value", mgmt.NewOptString("Read reports"), types.StringValue("Read reports")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stringValue(tt.in); !got.Equal(tt.want) {
				t.Fatalf("stringValue(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
```

In `internal/provider/not_found_test.go`, add this row to the `tests` slice in `TestReadRemovesMissingObjects`, below the comment:

```go
		{name: "kinde_permission", resource: NewPermissionResource, attrs: map[string]string{"id": "perm_missing"}},
```

Replace `internal/provider/permission_resource_test.go` with:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPermissionResource(t *testing.T) {
	f := testAccFake(t)
	var permissionID string
	updated := testAccPermissionResourceConfig("updated-permission", "updated_permission", "Updated test permission description")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccPermissionResourceConfig("test-permission", "test_permission", "Test permission description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", "test-permission"),
					resource.TestCheckResourceAttr("kinde_permission.test", "key", "test_permission"),
					resource.TestCheckResourceAttr("kinde_permission.test", "description", "Test permission description"),
					resource.TestCheckResourceAttrWith("kinde_permission.test", "id", func(v string) error {
						permissionID = v
						return nil
					}),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_permission.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing: name and key change in place.
			{
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", "updated-permission"),
					resource.TestCheckResourceAttr("kinde_permission.test", "key", "updated_permission"),
					resource.TestCheckResourceAttr("kinde_permission.test", "description", "Updated test permission description"),
					resource.TestCheckResourceAttrPtr("kinde_permission.test", "id", &permissionID),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemovePermission(permissionID) },
				Config:             updated,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: updated,
				Check:  resource.TestCheckResourceAttrSet("kinde_permission.test", "id"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccPermissionResource_NoDescription(t *testing.T) {
	testAccFake(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "kinde_permission" "test" {
  name = "no-description"
  key  = "no_description"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", "no-description"),
					resource.TestCheckNoResourceAttr("kinde_permission.test", "description"),
				),
			},
			{
				Config: testAccPermissionResourceConfig("no-description", "no_description", "Added later"),
				Check:  resource.TestCheckResourceAttr("kinde_permission.test", "description", "Added later"),
			},
			// Kinde keeps a description once it is set, so removing the
			// attribute leaves the current value instead of planning a change.
			{
				Config: `
resource "kinde_permission" "test" {
  name = "no-description"
  key  = "no_description"
}
`,
				Check: resource.TestCheckResourceAttr("kinde_permission.test", "description", "Added later"),
			},
		},
	})
}

func testAccPermissionResourceConfig(name string, key string, description string) string {
	return fmt.Sprintf(`
resource "kinde_permission" "test" {
  name        = %[1]q
  key         = %[2]q
  description = %[3]q
}
`, name, key, description)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run 'TestOptString|TestStringValue|TestReadRemovesMissingObjects'`
Expected: FAIL to compile with `undefined: optString`.

- [ ] **Step 3: Add the value helpers**

`internal/provider/values.go`:

```go
package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// stringValue maps an optional SDK string to Terraform. Unset becomes null.
func stringValue(v mgmt.OptString) types.String {
	if s, ok := v.Get(); ok {
		return types.StringValue(s)
	}
	return types.StringNull()
}

// optString maps a Terraform string to an optional SDK string. Null and
// unknown become unset, so the field is left out of the request and Kinde
// keeps its value; a known "" is sent as "".
func optString(v types.String) mgmt.OptString {
	if v.IsNull() || v.IsUnknown() {
		return mgmt.OptString{}
	}
	return mgmt.NewOptString(v.ValueString())
}
```

- [ ] **Step 4: Rewrite the permission schema helpers**

Replace `internal/provider/permission_schema.go` with:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

type PermissionResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
}

func expandPermissionCreateReq(d PermissionResourceModel) mgmt.CreatePermissionReq {
	return mgmt.CreatePermissionReq{
		Name:        optString(d.Name),
		Key:         optString(d.Key),
		Description: optString(d.Description),
	}
}

func expandPermissionUpdateReq(d PermissionResourceModel) mgmt.UpdatePermissionsReq {
	return mgmt.UpdatePermissionsReq{
		Name:        optString(d.Name),
		Key:         optString(d.Key),
		Description: optString(d.Description),
	}
}

func flattenPermissionResource(permission mgmt.Permissions) PermissionResourceModel {
	return PermissionResourceModel{
		ID:          stringValue(permission.ID),
		Name:        stringValue(permission.Name),
		Key:         stringValue(permission.Key),
		Description: stringValue(permission.Description),
	}
}

// permissionByID returns the permission with the given ID.
func permissionByID(perms []mgmt.Permissions, id string) (mgmt.Permissions, bool) {
	for _, p := range perms {
		if p.ID.Or("") == id {
			return p, true
		}
	}
	return mgmt.Permissions{}, false
}

// permissionByNameAndKey returns the first permission with the given name and
// key.
func permissionByNameAndKey(perms []mgmt.Permissions, name, key string) (mgmt.Permissions, bool) {
	for _, p := range perms {
		if p.Name.Or("") == name && p.Key.Or("") == key {
			return p, true
		}
	}
	return mgmt.Permissions{}, false
}
```

- [ ] **Step 5: Rewrite the permission resource**

`Metadata` and `Schema` are unchanged. Replace `internal/provider/permission_resource.go` with:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &PermissionResource{}
	_ resource.ResourceWithImportState = &PermissionResource{}
)

func NewPermissionResource() resource.Resource {
	return &PermissionResource{}
}

type PermissionResource struct {
	client *kindeapi.Client
}

func (r *PermissionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permission"
}

func (r *PermissionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Permissions represent individual access rights that can be assigned to roles. See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/permissions) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the permission",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the permission",
				Required:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Key identifier of the permission",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the permission. Kinde keeps a description once it is set, so removing this attribute leaves the current value in place.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *PermissionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *PermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PermissionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.CreatePermission(ctx, expandPermissionCreateReq(plan)); err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Permission",
			fmt.Sprintf("Could not create permission: %s", err),
		)
		return
	}

	// Kinde does not return the new permission's ID, so find it by name and key.
	perms, err := r.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Created Permission",
			fmt.Sprintf("Could not read created permission: %s", err),
		)
		return
	}
	permission, ok := permissionByNameAndKey(perms, plan.Name.ValueString(), plan.Key.ValueString())
	if !ok {
		resp.Diagnostics.AddError(
			"Error Reading Created Permission",
			fmt.Sprintf("Could not find permission with name %q and key %q after creating it", plan.Name.ValueString(), plan.Key.ValueString()),
		)
		return
	}

	state := flattenPermissionResource(permission)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *PermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PermissionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	perms, err := r.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Permission",
			fmt.Sprintf("Could not list permissions: %s", err),
		)
		return
	}

	// Find the permission by ID, falling back to its name and key.
	permission, ok := permissionByID(perms, state.ID.ValueString())
	if !ok && !state.Name.IsNull() && !state.Key.IsNull() {
		permission, ok = permissionByNameAndKey(perms, state.Name.ValueString(), state.Key.ValueString())
	}
	if !ok {
		resp.State.RemoveResource(ctx)
		return
	}

	state = flattenPermissionResource(permission)
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *PermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PermissionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.UpdatePermission(ctx, plan.ID.ValueString(), expandPermissionUpdateReq(plan)); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Permission",
			fmt.Sprintf("Could not update permission ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	perms, err := r.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated Permission",
			fmt.Sprintf("Could not read updated permission: %s", err),
		)
		return
	}
	permission, ok := permissionByID(perms, plan.ID.ValueString())
	if !ok {
		resp.Diagnostics.AddError(
			"Error Reading Updated Permission",
			fmt.Sprintf("Could not find permission ID %s after updating it", plan.ID.ValueString()),
		)
		return
	}

	state := flattenPermissionResource(permission)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *PermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PermissionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeletePermission(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Permission",
			fmt.Sprintf("Could not delete permission ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *PermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	perms, err := r.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Permission",
			fmt.Sprintf("Could not list permissions: %s", err),
		)
		return
	}

	permission, ok := permissionByID(perms, req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Error Reading Permission",
			fmt.Sprintf("Could not find permission with ID %s", req.ID),
		)
		return
	}

	state := flattenPermissionResource(permission)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/...`
Expected: PASS.

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccPermissionResource|TestReadRemovesMissingObjects' -v`
Expected: PASS for `TestAccPermissionResource`, `TestAccPermissionResource_NoDescription`, and `TestReadRemovesMissingObjects/kinde_permission`.

- [ ] **Step 7: Commit**

```bash
git add internal/provider
git commit -m "Move kinde_permission to the official Kinde SDK" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 7: Roles adapter and fake

**Files:**
- Create: `internal/kindeapi/roles.go`
- Modify: `internal/kindefake/fake.go` (`roles` state)
- Create: `internal/kindefake/roles.go`
- Test: `internal/kindeapi/roles_test.go`

**Interfaces:**
- Consumes:
  - `call[T]` (Task 1);
  - `allPages`, `maxPageSize`, `newID`, `notFound`, `success`, `nextTokenPage`, and `sortedPermissions` (Task 5);
  - the `createPermission` test helper (Task 5).
- Produces:
  - `func (c *Client) CreateRole(ctx context.Context, req mgmt.CreateRoleReq) (*mgmt.CreateRolesResponse, error)`.
  - `func (c *Client) GetRole(ctx context.Context, id string) (*mgmt.GetRoleResponse, error)`, which returns the role without its permissions.
  - `func (c *Client) UpdateRole(ctx context.Context, id string, req mgmt.UpdateRolesReq) error`. `UpdateRolesReq.Name` and `.Key` are plain strings; Kinde requires both on every update.
  - `func (c *Client) DeleteRole(ctx context.Context, id string) error`.
  - `func (c *Client) ListRolePermissions(ctx context.Context, roleID string) ([]mgmt.Permissions, error)`, which returns every page.
  - `func (c *Client) UpdateRolePermissions(ctx context.Context, roleID string, req mgmt.UpdateRolePermissionsReq) error`.
  - In `kindefake`: `type role struct { details mgmt.GetRoleResponseRole; permissions map[string]bool }`, `func (f *Fake) RemoveRole(id string)`, and handler methods `CreateRole`, `GetRole`, `UpdateRoles`, `DeleteRole`, `GetRolePermissions`, and `UpdateRolePermissions`.
  - Test helpers in package `kindeapi_test`: `func createRole(t *testing.T, c *kindeapi.Client, key string) string` and `func rolePermissionIDs(t *testing.T, c *kindeapi.Client, roleID string) []string`.

The fake answers a missing role with 404 `ROLE_NOT_FOUND`, a code Kinde's spec documents. It rejects an unknown permission ID in `UpdateRolePermissions` with 400 `PERMISSION_NOT_FOUND` and changes nothing.

- [ ] **Step 1: Write the failing tests**

`internal/kindeapi/roles_test.go`:

```go
package kindeapi_test

import (
	"fmt"
	"slices"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// createRole creates a role with the given key and returns its ID.
func createRole(t *testing.T, c *kindeapi.Client, key string) string {
	t.Helper()
	res, err := c.CreateRole(t.Context(), mgmt.CreateRoleReq{
		Name:        mgmt.NewOptString("Role " + key),
		Key:         mgmt.NewOptString(key),
		Description: mgmt.NewOptString("Test role"),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := res.Role.Value.ID.Get()
	if !ok || id == "" {
		t.Fatalf("expected a role ID, got %+v", res)
	}
	return id
}

// rolePermissionIDs returns the sorted IDs of a role's permissions.
func rolePermissionIDs(t *testing.T, c *kindeapi.Client, roleID string) []string {
	t.Helper()
	perms, err := c.ListRolePermissions(t.Context(), roleID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(perms))
	for _, p := range perms {
		ids = append(ids, p.ID.Value)
	}
	slices.Sort(ids)
	return ids
}

func TestRoleLifecycle(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	id := createRole(t, c, "admin")

	got, err := c.GetRole(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role.Value.Key.Value != "admin" || got.Role.Value.Description.Value != "Test role" {
		t.Fatalf("unexpected role %+v", got.Role.Value)
	}

	err = c.UpdateRole(ctx, id, mgmt.UpdateRolesReq{
		Name:        "Administrators",
		Key:         "administrators",
		Description: mgmt.NewOptString("Updated role"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = c.GetRole(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := mgmt.GetRoleResponseRole{
		ID:            mgmt.NewOptString(id),
		Key:           mgmt.NewOptString("administrators"),
		Name:          mgmt.NewOptString("Administrators"),
		Description:   mgmt.NewOptString("Updated role"),
		IsDefaultRole: mgmt.NewOptBool(false),
	}
	if got.Role.Value != want {
		t.Fatalf("after update got %+v, want %+v", got.Role.Value, want)
	}

	if err := c.DeleteRole(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRole(ctx, id); !kindeapi.IsNotFound(err) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestRoleNotFound(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	if _, err := c.GetRole(ctx, "rol_missing"); !kindeapi.IsNotFound(err) || !kindeapi.HasCode(err, "ROLE_NOT_FOUND") {
		t.Fatalf("get: expected ROLE_NOT_FOUND, got %v", err)
	}
	if err := c.UpdateRole(ctx, "rol_missing", mgmt.UpdateRolesReq{Name: "Missing", Key: "missing"}); !kindeapi.IsNotFound(err) {
		t.Fatalf("update: expected not found, got %v", err)
	}
	if err := c.DeleteRole(ctx, "rol_missing"); !kindeapi.IsNotFound(err) {
		t.Fatalf("delete: expected not found, got %v", err)
	}
	if _, err := c.ListRolePermissions(ctx, "rol_missing"); !kindeapi.IsNotFound(err) {
		t.Fatalf("list permissions: expected not found, got %v", err)
	}
	if err := c.UpdateRolePermissions(ctx, "rol_missing", mgmt.UpdateRolePermissionsReq{}); !kindeapi.IsNotFound(err) {
		t.Fatalf("update permissions: expected not found, got %v", err)
	}
}

func TestUpdateRolePermissionsAddsAndRemoves(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	roleID := createRole(t, c, "editor")
	a := createPermission(t, c, "perm:a")
	b := createPermission(t, c, "perm:b")
	d := createPermission(t, c, "perm:d")

	err := c.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{
		Permissions: []mgmt.UpdateRolePermissionsReqPermissionsItem{
			{ID: mgmt.NewOptString(a)},
			{ID: mgmt.NewOptString(b)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rolePermissionIDs(t, c, roleID), []string{a, b}; !slices.Equal(got, want) {
		t.Fatalf("after adding got %v, want %v", got, want)
	}

	err = c.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{
		Permissions: []mgmt.UpdateRolePermissionsReqPermissionsItem{
			{ID: mgmt.NewOptString(a), Operation: mgmt.NewOptString("delete")},
			{ID: mgmt.NewOptString(d)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rolePermissionIDs(t, c, roleID), []string{b, d}; !slices.Equal(got, want) {
		t.Fatalf("after mixed update got %v, want %v", got, want)
	}

	err = c.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{
		Permissions: []mgmt.UpdateRolePermissionsReqPermissionsItem{
			{ID: mgmt.NewOptString(a)},
			{ID: mgmt.NewOptString("perm_missing")},
		},
	})
	if !kindeapi.HasCode(err, "PERMISSION_NOT_FOUND") || kindeapi.IsNotFound(err) {
		t.Fatalf("expected a 400 PERMISSION_NOT_FOUND, got %v", err)
	}
	if got, want := rolePermissionIDs(t, c, roleID), []string{b, d}; !slices.Equal(got, want) {
		t.Fatalf("a rejected update changed permissions to %v, want %v", got, want)
	}
}

func TestListRolePermissionsReturnsEveryPage(t *testing.T) {
	f, c := newFakeClient(t)
	f.LimitPageSize(2)
	roleID := createRole(t, c, "viewer")
	var want []string
	var items []mgmt.UpdateRolePermissionsReqPermissionsItem
	for i := range 5 {
		id := createPermission(t, c, fmt.Sprintf("perm:%d", i))
		want = append(want, id)
		items = append(items, mgmt.UpdateRolePermissionsReqPermissionsItem{ID: mgmt.NewOptString(id)})
	}
	if err := c.UpdateRolePermissions(t.Context(), roleID, mgmt.UpdateRolePermissionsReq{Permissions: items}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	if got := rolePermissionIDs(t, c, roleID); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/`
Expected: FAIL to compile with `c.CreateRole undefined (type *kindeapi.Client has no field or method CreateRole)`.

- [ ] **Step 3: Implement the fake's role endpoints**

In `internal/kindefake/fake.go`, add under `// Domain state.`:

```go
	roles       map[string]*role
```

and in `New`, under `// Initialize domain state.`:

```go
	f.roles = map[string]*role{}
```

`internal/kindefake/roles.go`:

```go
package kindefake

import (
	"context"
	"net/http"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// role is a role as the fake stores it.
type role struct {
	details mgmt.GetRoleResponseRole
	// permissions holds the IDs of the role's permissions.
	permissions map[string]bool
}

// CreateRole stores a role and answers with its ID.
func (h handler) CreateRole(_ context.Context, req mgmt.OptCreateRoleReq) (mgmt.CreateRoleRes, error) {
	body, _ := req.Get()
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	id := h.f.newID("rol")
	h.f.roles[id] = &role{
		details: mgmt.GetRoleResponseRole{
			ID:            mgmt.NewOptString(id),
			Key:           body.Key,
			Name:          body.Name,
			Description:   body.Description,
			IsDefaultRole: mgmt.NewOptBool(body.IsDefaultRole.Or(false)),
		},
		permissions: map[string]bool{},
	}
	return &mgmt.CreateRolesResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
		Role:    mgmt.NewOptCreateRolesResponseRole(mgmt.CreateRolesResponseRole{ID: mgmt.NewOptString(id)}),
	}, nil
}

// GetRole returns a role without its permissions.
func (h handler) GetRole(_ context.Context, params mgmt.GetRoleParams) (mgmt.GetRoleRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	r, ok := h.f.roles[params.RoleID]
	if !ok {
		return nil, roleNotFound()
	}
	return &mgmt.GetRoleResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
		Role:    mgmt.NewOptGetRoleResponseRole(r.details),
	}, nil
}

// UpdateRoles sets a role's name and key, which Kinde requires, and any
// other fields the request sets.
func (h handler) UpdateRoles(_ context.Context, req mgmt.OptUpdateRolesReq, params mgmt.UpdateRolesParams) (mgmt.UpdateRolesRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	r, ok := h.f.roles[params.RoleID]
	if !ok {
		return nil, roleNotFound()
	}
	body, ok := req.Get()
	if !ok {
		return nil, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: name and key are required"}
	}
	r.details.Name = mgmt.NewOptString(body.Name)
	r.details.Key = mgmt.NewOptString(body.Key)
	if body.Description.Set {
		r.details.Description = body.Description
	}
	if body.IsDefaultRole.Set {
		r.details.IsDefaultRole = body.IsDefaultRole
	}
	return success(), nil
}

// DeleteRole deletes a role.
func (h handler) DeleteRole(_ context.Context, params mgmt.DeleteRoleParams) (mgmt.DeleteRoleRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.roles[params.RoleID]; !ok {
		return nil, roleNotFound()
	}
	delete(h.f.roles, params.RoleID)
	return success(), nil
}

// GetRolePermissions lists a role's permissions in ID order. Permissions
// deleted since they were added drop out, as they do in Kinde. It ignores
// sort.
func (h handler) GetRolePermissions(_ context.Context, params mgmt.GetRolePermissionsParams) (mgmt.GetRolePermissionsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	r, ok := h.f.roles[params.RoleID]
	if !ok {
		return nil, roleNotFound()
	}
	var perms []mgmt.Permissions
	for _, p := range h.f.sortedPermissions() {
		if r.permissions[p.ID.Value] {
			perms = append(perms, p)
		}
	}
	page, next, err := nextTokenPage(h.f, perms, params.PageSize, params.NextToken)
	if err != nil {
		return nil, err
	}
	res := &mgmt.RolePermissionsResponse{
		Code:        mgmt.NewOptString("OK"),
		Message:     mgmt.NewOptString("Success"),
		Permissions: page,
	}
	if next != "" {
		res.NextToken = mgmt.NewOptString(next)
	}
	return res, nil
}

// UpdateRolePermissions adds each listed permission to a role, or removes it
// when its operation is "delete". It changes nothing if any permission does
// not exist.
func (h handler) UpdateRolePermissions(_ context.Context, req *mgmt.UpdateRolePermissionsReq, params mgmt.UpdateRolePermissionsParams) (mgmt.UpdateRolePermissionsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	r, ok := h.f.roles[params.RoleID]
	if !ok {
		return nil, roleNotFound()
	}
	for _, item := range req.Permissions {
		if _, ok := h.f.permissions[item.ID.Value]; !ok {
			return nil, &apiError{status: http.StatusBadRequest, code: "PERMISSION_NOT_FOUND", message: "kindefake: no permission " + item.ID.Value}
		}
	}
	res := &mgmt.UpdateRolePermissionsResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
	}
	for _, item := range req.Permissions {
		id := item.ID.Value
		if item.Operation.Value == "delete" {
			delete(r.permissions, id)
			res.PermissionsRemoved = append(res.PermissionsRemoved, id)
			continue
		}
		r.permissions[id] = true
		res.PermissionsAdded = append(res.PermissionsAdded, id)
	}
	return res, nil
}

// RemoveRole deletes a role behind the provider's back.
func (f *Fake) RemoveRole(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.roles, id)
}

func roleNotFound() error {
	return notFound("ROLE_NOT_FOUND", "Role not found")
}
```

- [ ] **Step 4: Implement the adapter methods**

`internal/kindeapi/roles.go`:

```go
package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateRole creates a role. The response carries the new role's ID.
func (c *Client) CreateRole(ctx context.Context, req mgmt.CreateRoleReq) (*mgmt.CreateRolesResponse, error) {
	return call[*mgmt.CreateRolesResponse](ctx, "CreateRole", func(ctx context.Context) (any, error) {
		return c.api.CreateRole(ctx, mgmt.NewOptCreateRoleReq(req))
	})
}

// GetRole returns a role without its permissions.
func (c *Client) GetRole(ctx context.Context, id string) (*mgmt.GetRoleResponse, error) {
	return call[*mgmt.GetRoleResponse](ctx, "GetRole", func(ctx context.Context) (any, error) {
		return c.api.GetRole(ctx, mgmt.GetRoleParams{RoleID: id})
	})
}

// UpdateRole changes a role. Kinde requires the name and key on every
// update; other fields left unset in req are not sent.
func (c *Client) UpdateRole(ctx context.Context, id string, req mgmt.UpdateRolesReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "UpdateRoles", func(ctx context.Context) (any, error) {
		return c.api.UpdateRoles(ctx, mgmt.NewOptUpdateRolesReq(req), mgmt.UpdateRolesParams{RoleID: id})
	})
	return err
}

// DeleteRole deletes a role.
func (c *Client) DeleteRole(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteRole", func(ctx context.Context) (any, error) {
		return c.api.DeleteRole(ctx, mgmt.DeleteRoleParams{RoleID: id})
	})
	return err
}

// ListRolePermissions returns every permission assigned to a role.
func (c *Client) ListRolePermissions(ctx context.Context, roleID string) ([]mgmt.Permissions, error) {
	return allPages(ctx, func(ctx context.Context, nextToken string) ([]mgmt.Permissions, string, error) {
		params := mgmt.GetRolePermissionsParams{RoleID: roleID, PageSize: mgmt.NewOptNilInt(maxPageSize)}
		if nextToken != "" {
			params.NextToken = mgmt.NewOptNilString(nextToken)
		}
		res, err := call[*mgmt.RolePermissionsResponse](ctx, "GetRolePermissions", func(ctx context.Context) (any, error) {
			return c.api.GetRolePermissions(ctx, params)
		})
		if err != nil {
			return nil, "", err
		}
		return res.Permissions, res.NextToken.Or(""), nil
	})
}

// UpdateRolePermissions adds permissions to a role and, for items whose
// operation is "delete", removes them.
func (c *Client) UpdateRolePermissions(ctx context.Context, roleID string, req mgmt.UpdateRolePermissionsReq) error {
	_, err := call[*mgmt.UpdateRolePermissionsResponse](ctx, "UpdateRolePermissions", func(ctx context.Context) (any, error) {
		return c.api.UpdateRolePermissions(ctx, &req, mgmt.UpdateRolePermissionsParams{RoleID: roleID})
	})
	return err
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/kindeapi/ ./internal/kindefake/ -race -v`
Expected: PASS, including `TestRoleNotFound` and `TestListRolePermissionsReturnsEveryPage`.

- [ ] **Step 6: Commit**

```bash
git add internal/kindeapi internal/kindefake
git commit -m "Add role endpoints to kindeapi and the fake" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 8: Move `kinde_role` to kindeapi

**Files:**
- Modify: `internal/provider/role_resource.go` (rewritten; drops `rolePermissionsPage` and `getRolePermissions`)
- Modify: `internal/provider/role_schema.go` (rewritten; drops the unused `expandRoleResourceModel`, `RoleDataSourceModel`, and their `//nolint:unused` helpers, which used old-library types)
- Delete: `internal/provider/pagination.go` (`getAllPages`, `kindeRequester`, and `kindePage` have no callers once roles move)
- Modify: `internal/provider/role_resource_test.go`
- Modify: `internal/provider/role_resource_internal_test.go` (rewritten against the fake)
- Modify: `internal/provider/not_found_test.go`
- Modify: `docs/resources/role.md`

**Interfaces:**
- Consumes:
  - `CreateRole`, `GetRole`, `UpdateRole`, `DeleteRole`, `ListRolePermissions`, `UpdateRolePermissions`, and `Fake.RemoveRole` (Task 7);
  - `CreatePermission`, `ListPermissions`, and `Fake.LimitPageSize` (Task 5);
  - `stringValue`, `optString`, and the migrated `kinde_permission` (Task 6);
  - `kindeapi.IsNotFound` (Task 1);
  - `providerDataFrom`, `testAccFake`, and `TestReadRemovesMissingObjects` (Task 4).
- Produces:
  - `type kindeRole struct { details mgmt.GetRoleResponseRole; permissionIDs []string }` and `func (r *RoleResource) getRole(ctx context.Context, id string) (*kindeRole, error)`.
  - `func buildRolePermissionOperations(current, desired []string) []mgmt.UpdateRolePermissionsReqPermissionsItem`.
  - `func expandRoleCreateReq(plan RoleResourceModel) mgmt.CreateRoleReq`, `func expandRoleUpdateReq(plan RoleResourceModel) mgmt.UpdateRolesReq`, and `func flattenRoleResource(ctx context.Context, role mgmt.GetRoleResponseRole, permissions []string, nullWhenEmpty bool) (RoleResourceModel, error)`.

Behavior changes:
- Spec section 3: changing `key` updates the role in place. `Update` sends the name and the key; before, the key was never sent, so the plan never converged.
- `Read` removes a missing role from state, and `Delete` treats not-found as success.
- Role permissions are read 100 per page instead of 10.
- `Create` reports a failed permission reconcile with the role ID it already has. Before, the error message read `role.ID` from the nil role that the failed call returned, which panicked.

- [ ] **Step 1: Write the failing tests**

The pure-function tests keep their cases with the SDK's operation type. `TestGetRoleUsesAllPermissionPages` now runs against the fake, with pages capped at 10. Replace `internal/provider/role_resource_internal_test.go` with:

```go
package provider

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

func TestBuildRolePermissionOperations(t *testing.T) {
	got := buildRolePermissionOperations(
		[]string{"perm_c", "perm_a", "perm_b"},
		[]string{"perm_b", "perm_d"},
	)

	want := []mgmt.UpdateRolePermissionsReqPermissionsItem{
		{ID: mgmt.NewOptString("perm_a"), Operation: mgmt.NewOptString("delete")},
		{ID: mgmt.NewOptString("perm_c"), Operation: mgmt.NewOptString("delete")},
		{ID: mgmt.NewOptString("perm_d")},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected operations\nwant: %#v\n got: %#v", want, got)
	}
}

func TestRolePermissionsMatchIgnoresOrdering(t *testing.T) {
	if !rolePermissionsMatch([]string{"perm_b", "perm_a"}, []string{"perm_a", "perm_b"}) {
		t.Fatal("expected matching permission sets with different ordering")
	}

	if rolePermissionsMatch([]string{"perm_a"}, []string{"perm_a", "perm_b"}) {
		t.Fatal("expected different permission sets not to match")
	}
}

func TestFlattenRolePermissionsPreservesNullAndEmpty(t *testing.T) {
	ctx := t.Context()

	nullSet, err := flattenRolePermissions(ctx, nil, true)
	if err != nil {
		t.Fatalf("flatten null permissions: %s", err)
	}
	if !nullSet.IsNull() {
		t.Fatalf("expected null permissions, got %#v", nullSet)
	}

	emptySet, err := flattenRolePermissions(ctx, nil, false)
	if err != nil {
		t.Fatalf("flatten empty permissions: %s", err)
	}
	if emptySet.IsNull() {
		t.Fatal("expected configured empty permissions to remain an empty set, got null")
	}
	if len(emptySet.Elements()) != 0 {
		t.Fatalf("expected empty set, got %d elements", len(emptySet.Elements()))
	}
}

func TestExpandRoleUpdateReqSendsNameAndKey(t *testing.T) {
	got := expandRoleUpdateReq(RoleResourceModel{
		ID:          types.StringValue("rol_0001"),
		Name:        types.StringValue("Admins"),
		Key:         types.StringValue("admins"),
		Description: types.StringValue("Administrators"),
	})
	want := mgmt.UpdateRolesReq{
		Name:        "Admins",
		Key:         "admins",
		Description: mgmt.NewOptString("Administrators"),
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestGetRoleUsesAllPermissionPages(t *testing.T) {
	ctx := t.Context()
	f := kindefake.New(t)
	// With pages of 10, the eleventh permission is on the second page.
	f.LimitPageSize(10)
	client, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: f.ClientSecret})
	if err != nil {
		t.Fatal(err)
	}

	created, err := client.CreateRole(ctx, mgmt.CreateRoleReq{
		Name:        mgmt.NewOptString("Role"),
		Key:         mgmt.NewOptString("role"),
		Description: mgmt.NewOptString("Role description"),
	})
	if err != nil {
		t.Fatalf("create role: %s", err)
	}
	roleID := created.Role.Value.ID.Value

	var items []mgmt.UpdateRolePermissionsReqPermissionsItem
	for i := range 11 {
		key := fmt.Sprintf("perm_%02d", i)
		if err := client.CreatePermission(ctx, mgmt.CreatePermissionReq{Name: mgmt.NewOptString(key), Key: mgmt.NewOptString(key)}); err != nil {
			t.Fatalf("create permission: %s", err)
		}
	}
	perms, err := client.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("list permissions: %s", err)
	}
	for _, p := range perms {
		items = append(items, mgmt.UpdateRolePermissionsReqPermissionsItem{ID: p.ID})
	}
	if err := client.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{Permissions: items}); err != nil {
		t.Fatalf("add role permissions: %s", err)
	}

	r := RoleResource{client: client}
	role, err := r.getRole(ctx, roleID)
	if err != nil {
		t.Fatalf("get role: %s", err)
	}
	if got := len(role.permissionIDs); got != 11 {
		t.Fatalf("expected 11 permissions across pages, got %d: %#v", got, role.permissionIDs)
	}
	if got := role.details.Key.Value; got != "role" {
		t.Fatalf("key = %q, want role", got)
	}
}

func TestGetRoleReturnsNotFound(t *testing.T) {
	f := kindefake.New(t)
	client, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: f.ClientSecret})
	if err != nil {
		t.Fatal(err)
	}
	r := RoleResource{client: client}
	if _, err := r.getRole(t.Context(), "rol_missing"); !kindeapi.IsNotFound(err) {
		t.Fatalf("expected a not-found error, got %v", err)
	}
}

func TestFlattenRoleResourceUsesEmptySetForConfiguredEmpty(t *testing.T) {
	state, err := flattenRoleResource(t.Context(), mgmt.GetRoleResponseRole{
		ID:          mgmt.NewOptString("role_1"),
		Name:        mgmt.NewOptString("Role"),
		Key:         mgmt.NewOptString("role"),
		Description: mgmt.NewOptString("Role description"),
	}, nil, false)
	if err != nil {
		t.Fatalf("flatten role: %s", err)
	}

	if state.Permissions.IsNull() {
		t.Fatal("expected empty configured permissions to stay an empty set")
	}

	emptySet, diags := types.SetValueFrom(t.Context(), types.StringType, []string{})
	if diags.HasError() {
		t.Fatalf("build empty set: %v", diags)
	}
	if !state.Permissions.Equal(emptySet) {
		t.Fatalf("expected empty set, got %#v", state.Permissions)
	}
}
```

In `internal/provider/not_found_test.go`, add this row to the `tests` slice, below the `kinde_permission` row:

```go
		{name: "kinde_role", resource: NewRoleResource, attrs: map[string]string{"id": "rol_missing"}},
```

Every test now runs against the fake. `TestAccRoleResource` gains an in-place key change and an out-of-band delete. The pagination test caps fake pages at 10, so the eleventh permission is still on a second page. Replace `internal/provider/role_resource_test.go` with:

```go
package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccRoleResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-")
	var roleID string
	renamed := testAccRoleResourceConfigNewKey(testID)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccRoleResourceConfig(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Test role"),
					resource.TestCheckResourceAttrWith("kinde_role.test", "id", func(v string) error {
						roleID = v
						return nil
					}),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccRoleResourceConfigUpdate(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID+"-updated"),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Updated test role"),
					resource.TestCheckResourceAttrPtr("kinde_role.test", "id", &roleID),
				),
			},
			// Changing the key updates the role in place.
			{
				Config: renamed,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("kinde_role.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID+"-renamed"),
					resource.TestCheckResourceAttrPtr("kinde_role.test", "id", &roleID),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveRole(roleID) },
				Config:             renamed,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: renamed,
				Check:  resource.TestCheckResourceAttrSet("kinde_role.test", "id"),
			},
		},
	})
}

func TestAccRoleResource_AddOnlyPermissionUpdate(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-role-add")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 2, []int{0}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "1"),
				),
			},
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 2, []int{0, 1}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "2"),
				),
			},
		},
	})
}

func TestAccRoleResource_MixedPermissionUpdate(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-role-mixed")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 4, []int{0, 1, 2}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "3"),
					testAccCheckRolePermissionRefs("kinde_role.test", []int{0, 1, 2}),
				),
			},
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 4, []int{1, 2, 3}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "3"),
					testAccCheckRolePermissionRefs("kinde_role.test", []int{1, 2, 3}),
				),
			},
		},
	})
}

func TestAccRoleResource_PermissionsPaginationBoundary(t *testing.T) {
	f := testAccFake(t)
	// With pages of 10, the eleventh permission is on the second page.
	f.LimitPageSize(10)
	testID := acctest.RandomWithPrefix("tfacc-role-page")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 11, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "10"),
				),
			},
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 11, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "11"),
				),
			},
		},
	})
}

func testAccRoleResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "kinde_role" "test" {
	name        = %[1]q
	key         = %[1]q
	description = "Test role"
}
`, name)
}

func testAccRoleResourceConfigUpdate(name string) string {
	return fmt.Sprintf(`
resource "kinde_role" "test" {
	name        = "%[1]s-updated"
	key         = %[1]q
	description = "Updated test role"
}
`, name)
}

func testAccRoleResourceConfigNewKey(name string) string {
	return fmt.Sprintf(`
resource "kinde_role" "test" {
	name        = "%[1]s-updated"
	key         = "%[1]s-renamed"
	description = "Updated test role"
}
`, name)
}

func testAccRoleResourceConfigWithPermissionRefs(name string, permissionCount int, rolePermissionIndexes []int) string {
	var builder strings.Builder

	for i := 0; i < permissionCount; i++ {
		fmt.Fprintf(&builder, `
resource "kinde_permission" "perm_%02d" {
	name        = "%s-permission-%02d"
	key         = "%s_permission_%02d"
	description = "Test permission %02d"
}
`, i, name, i, name, i, i)
	}

	fmt.Fprintf(&builder, `
resource "kinde_role" "test" {
	name        = "%[1]s-role"
	key         = "%[1]s_role"
	description = "Test role"
	permissions = [
`, name)

	for _, idx := range rolePermissionIndexes {
		fmt.Fprintf(&builder, "\t\tkinde_permission.perm_%02d.id,\n", idx)
	}

	builder.WriteString(`	]
}
`)

	return builder.String()
}

func testAccCheckRolePermissionRefs(roleResourceName string, indexes []int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		role, ok := s.RootModule().Resources[roleResourceName]
		if !ok {
			return fmt.Errorf("role resource %s not found", roleResourceName)
		}

		actualPermissions := map[string]struct{}{}
		for key, value := range role.Primary.Attributes {
			if strings.HasPrefix(key, "permissions.") && key != "permissions.#" {
				actualPermissions[value] = struct{}{}
			}
		}

		for _, idx := range indexes {
			permissionName := fmt.Sprintf("kinde_permission.perm_%02d", idx)
			permission, ok := s.RootModule().Resources[permissionName]
			if !ok {
				return fmt.Errorf("permission resource %s not found", permissionName)
			}

			if _, ok := actualPermissions[permission.Primary.ID]; !ok {
				return fmt.Errorf("role %s is missing permission %s (%s)", roleResourceName, permissionName, permission.Primary.ID)
			}
		}

		if len(actualPermissions) != len(indexes) {
			return fmt.Errorf("role %s has %d permissions, expected %d", roleResourceName, len(actualPermissions), len(indexes))
		}

		return nil
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/`
Expected: FAIL to compile with `undefined: expandRoleUpdateReq` and `cannot use client (variable of type *kindeapi.Client) as *roles.Client value in struct literal`.

- [ ] **Step 3: Rewrite the role schema helpers**

Replace `internal/provider/role_schema.go` with:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

type RoleResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
	Permissions types.Set    `tfsdk:"permissions"`
}

func expandRoleCreateReq(plan RoleResourceModel) mgmt.CreateRoleReq {
	return mgmt.CreateRoleReq{
		Name:        optString(plan.Name),
		Key:         optString(plan.Key),
		Description: optString(plan.Description),
	}
}

// expandRoleUpdateReq builds a role update. Kinde requires the name and key
// on every update, which is also how a changed key is applied in place.
func expandRoleUpdateReq(plan RoleResourceModel) mgmt.UpdateRolesReq {
	return mgmt.UpdateRolesReq{
		Name:        plan.Name.ValueString(),
		Key:         plan.Key.ValueString(),
		Description: optString(plan.Description),
	}
}

func flattenRolePermissions(ctx context.Context, permissions []string, nullWhenEmpty bool) (types.Set, error) {
	if len(permissions) == 0 && nullWhenEmpty {
		return types.SetNull(types.StringType), nil
	}
	if permissions == nil {
		permissions = []string{}
	}

	permissionsSet, diags := types.SetValueFrom(ctx, types.StringType, permissions)
	if diags.HasError() {
		return types.Set{}, fmt.Errorf("failed to flatten permissions: %v", diags)
	}

	return permissionsSet, nil
}

func flattenRoleResource(ctx context.Context, role mgmt.GetRoleResponseRole, permissions []string, nullWhenEmpty bool) (RoleResourceModel, error) {
	permissionsSet, err := flattenRolePermissions(ctx, permissions, nullWhenEmpty)
	if err != nil {
		return RoleResourceModel{}, err
	}

	return RoleResourceModel{
		ID:          stringValue(role.ID),
		Name:        stringValue(role.Name),
		Key:         stringValue(role.Key),
		Description: stringValue(role.Description),
		Permissions: permissionsSet,
	}, nil
}
```

- [ ] **Step 4: Rewrite the role resource**

Apart from the `key` description, `Metadata` and `Schema` are unchanged. `sortPermissions`, `rolePermissionsMatch`, and the polling in `waitForRolePermissions` keep their logic. Replace `internal/provider/role_resource.go` with:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &RoleResource{}
	_ resource.ResourceWithImportState = &RoleResource{}
)

func NewRoleResource() resource.Resource {
	return &RoleResource{}
}

type RoleResource struct {
	client *kindeapi.Client
}

// kindeRole is a role as Kinde reports it, with the IDs of its permissions.
type kindeRole struct {
	details       mgmt.GetRoleResponseRole
	permissionIDs []string
}

func (r *RoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *RoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Roles represent collections of permissions that can be assigned to users. See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/roles) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the role",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the role",
				Required:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Key identifier of the role. Changing it updates the role in place.",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the role. This field is required because the Kinde API does not properly handle unsetting or empty descriptions once they are set. To maintain consistent behavior and prevent state drift, we require a description for all roles.",
				Required:            true,
			},
			"permissions": schema.SetAttribute{
				MarkdownDescription: "List of permission IDs associated with this role",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *RoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *RoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoleResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create role with basic details first
	created, err := r.client.CreateRole(ctx, expandRoleCreateReq(plan))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Role",
			fmt.Sprintf("Could not create role: %s", err),
		)
		return
	}
	roleID := created.Role.Value.ID.Or("")
	if roleID == "" {
		resp.Diagnostics.AddError(
			"Error Creating Role",
			"Kinde created the role but did not return its ID.",
		)
		return
	}

	// Get the complete role data
	role, err := r.getRole(ctx, roleID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Created Role",
			fmt.Sprintf("Could not read created role: %s", err),
		)
		return
	}

	// Update permissions if specified
	var planPerms []string
	if !plan.Permissions.IsNull() {
		diags = plan.Permissions.ElementsAs(ctx, &planPerms, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		role, err = r.reconcileRolePermissions(ctx, roleID, role.permissionIDs, planPerms)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Setting Role Permissions",
				fmt.Sprintf("Could not reconcile permissions for role %s: %s", roleID, err),
			)
			return
		}
	}

	state, err := flattenRoleResource(ctx, role.details, sortPermissions(role.permissionIDs), plan.Permissions.IsNull())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Helper function to sort permissions without modifying original.
func sortPermissions(permissions []string) []string {
	sorted := make([]string, len(permissions))
	copy(sorted, permissions)
	sort.Strings(sorted)
	return sorted
}

func (r *RoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RoleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, err := r.getRole(ctx, state.ID.ValueString())
	if kindeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Role",
			fmt.Sprintf("Could not read role ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	state, err = flattenRoleResource(ctx, role.details, sortPermissions(role.permissionIDs), state.Permissions.IsNull())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state RoleResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state for comparison
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// First update role details. Kinde requires the name and key, so a
	// changed key is applied in place.
	if err := r.client.UpdateRole(ctx, plan.ID.ValueString(), expandRoleUpdateReq(plan)); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Role",
			fmt.Sprintf("Could not update role ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Handle permissions update if the field is set in the plan
	var planPerms []string
	if !plan.Permissions.IsNull() {
		diags = plan.Permissions.ElementsAs(ctx, &planPerms, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Get the updated role to ensure we have all fields and permissions
	role, err := r.getRole(ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated Role",
			fmt.Sprintf("Could not read updated role: %s", err),
		)
		return
	}

	if !plan.Permissions.Equal(state.Permissions) {
		role, err = r.reconcileRolePermissions(ctx, plan.ID.ValueString(), role.permissionIDs, planPerms)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Updating Role Permissions",
				fmt.Sprintf("Could not update permissions for role %s: %s", plan.ID.ValueString(), err),
			)
			return
		}
	}

	state, err = flattenRoleResource(ctx, role.details, sortPermissions(role.permissionIDs), plan.Permissions.IsNull())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RoleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteRole(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Role",
			fmt.Sprintf("Could not delete role ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *RoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	role, err := r.getRole(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Kinde Role",
			"Could not read Kinde role ID "+req.ID+": "+err.Error(),
		)
		return
	}

	// Sort the role's permissions for consistent ordering
	sortedPermissions := sortStringSlice(role.permissionIDs)

	state, err := flattenRoleResource(ctx, role.details, sortedPermissions, true)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			"Could not set role state: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// getRole reads a role and every page of its permissions.
func (r *RoleResource) getRole(ctx context.Context, id string) (*kindeRole, error) {
	res, err := r.client.GetRole(ctx, id)
	if err != nil {
		return nil, err
	}
	details, ok := res.Role.Get()
	if !ok {
		return nil, fmt.Errorf("kinde GetRole: the response for role %s has no role", id)
	}

	permissions, err := r.client.ListRolePermissions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get role permissions: %w", err)
	}
	permissionIDs := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		permissionIDs = append(permissionIDs, permission.ID.Or(""))
	}

	return &kindeRole{details: details, permissionIDs: permissionIDs}, nil
}

func buildRolePermissionOperations(current, desired []string) []mgmt.UpdateRolePermissionsReqPermissionsItem {
	currentSet := make(map[string]struct{}, len(current))
	for _, permissionID := range current {
		currentSet[permissionID] = struct{}{}
	}

	desiredSet := make(map[string]struct{}, len(desired))
	for _, permissionID := range desired {
		desiredSet[permissionID] = struct{}{}
	}

	var operations []mgmt.UpdateRolePermissionsReqPermissionsItem

	for _, permissionID := range sortPermissions(current) {
		if _, keep := desiredSet[permissionID]; !keep {
			operations = append(operations, mgmt.UpdateRolePermissionsReqPermissionsItem{
				ID:        mgmt.NewOptString(permissionID),
				Operation: mgmt.NewOptString("delete"),
			})
		}
	}

	for _, permissionID := range sortPermissions(desired) {
		if _, alreadyPresent := currentSet[permissionID]; !alreadyPresent {
			operations = append(operations, mgmt.UpdateRolePermissionsReqPermissionsItem{
				ID: mgmt.NewOptString(permissionID),
			})
		}
	}

	return operations
}

func rolePermissionsMatch(actual, desired []string) bool {
	actualSorted := sortPermissions(actual)
	desiredSorted := sortPermissions(desired)

	if len(actualSorted) != len(desiredSorted) {
		return false
	}

	for i := range actualSorted {
		if actualSorted[i] != desiredSorted[i] {
			return false
		}
	}

	return true
}

func (r *RoleResource) reconcileRolePermissions(ctx context.Context, roleID string, current, desired []string) (*kindeRole, error) {
	operations := buildRolePermissionOperations(current, desired)
	if len(operations) > 0 {
		err := r.client.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{
			Permissions: operations,
		})
		if err != nil {
			return nil, err
		}
	}

	return r.waitForRolePermissions(ctx, roleID, desired)
}

func (r *RoleResource) waitForRolePermissions(ctx context.Context, roleID string, desired []string) (*kindeRole, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastRole *kindeRole
	var lastErr error

	for {
		role, err := r.getRole(waitCtx, roleID)
		if err == nil {
			lastRole = role
			if rolePermissionsMatch(role.permissionIDs, desired) {
				return role, nil
			}
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			if lastRole != nil {
				return nil, fmt.Errorf(
					"timed out waiting for role permissions to converge for role %s: desired=%v observed=%v",
					roleID,
					sortPermissions(desired),
					sortPermissions(lastRole.permissionIDs),
				)
			}
			if lastErr != nil {
				return nil, fmt.Errorf("timed out waiting to read updated role %s: %w", roleID, lastErr)
			}
			return nil, fmt.Errorf("timed out waiting for role permissions to converge for role %s", roleID)
		case <-ticker.C:
		}
	}
}
```

- [ ] **Step 5: Delete the old pagination helper**

Run: `git rm internal/provider/pagination.go`

Nothing calls `getAllPages` any more. `rolePermissionsPage` went with the rewrite in Step 4, and Task 6 removed `permissionsPage`.

- [ ] **Step 6: Update the registry docs**

`go generate ./...` at the repository root does not regenerate `docs/`, because the `//go:generate` lines live in the separate `tools` module. Edit `docs/resources/role.md` by hand. Replace:

```markdown
- `key` (String) Key identifier of the role
```

with:

```markdown
- `key` (String) Key identifier of the role. Changing it updates the role in place.
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/...`
Expected: PASS.

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccRoleResource|TestAccPermissionResource|TestReadRemovesMissingObjects' -v`
Expected: PASS for all four `TestAccRoleResource*` tests, both permission tests, and `TestReadRemovesMissingObjects/kinde_role`. In `TestAccRoleResource`, the key-change step's plan check confirms an in-place update, and the ID stays the same.

Run: `golangci-lint run ./...`
Expected: `0 issues.`, which confirms nothing in the old pagination helper is left unused.

- [ ] **Step 8: Commit**

The deletion of `pagination.go` is already staged by `git rm`.

```bash
git add internal/provider docs/resources/role.md
git commit -m "Move kinde_role to the official Kinde SDK" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 9: APIs

`kinde_api` and the `kinde_api` data source move to `kindeapi`, and `is_management_api` is now read from Kinde (spec section 3). `AddAPIs` returns only the new API's ID, so `Create` reads the API back with `GetAPI`, as the old code did.

**Files:**
- Create: `internal/kindeapi/apis.go`
- Create: `internal/kindefake/apis.go`
- Modify: `internal/kindefake/fake.go` (`Fake` domain state and `New`)
- Modify: `internal/provider/values.go` (add `boolValue`)
- Modify: `internal/provider/api_schema.go`, `internal/provider/api_resource.go`, `internal/provider/api_data_source.go`
- Test: `internal/kindeapi/apis_test.go`
- Test: `internal/provider/values_test.go`
- Test: `internal/provider/api_resource_test.go`, `internal/provider/api_data_source_test.go`
- Test: `internal/provider/not_found_test.go`

**Interfaces:**
- Consumes: `call[T]` and `Client.api` (Tasks 1-2); `kindefake.Fake`, `handler`, and `newFakeClient` (Task 3); `providerDataFrom`, `testAccFake`, and `TestReadRemovesMissingObjects` (Task 4); `Fake.newID` and `notFound` (Task 5); `stringValue` (Task 6).
- Produces:
  - `func (c *Client) AddAPIs(ctx context.Context, req *mgmt.AddAPIsReq) (*mgmt.CreateApisResponse, error)`.
  - `func (c *Client) GetAPI(ctx context.Context, id string) (*mgmt.GetAPIResponse, error)`.
  - `func (c *Client) DeleteAPI(ctx context.Context, id string) error`.
  - `func (f *Fake) RemoveAPI(id string)`.
  - `func boolValue(v mgmt.OptBool) types.Bool` in `internal/provider/values.go`: unset becomes null. Later tasks reuse it.
  - In `api_schema.go`: `func getAPI(ctx context.Context, client *kindeapi.Client, id string) (mgmt.GetAPIResponseAPI, error)`, `func flattenAPIResource(api mgmt.GetAPIResponseAPI) APIResourceModel`, and `func flattenAPIDataSource(api mgmt.GetAPIResponseAPI) APIDataSourceModel`.

- [ ] **Step 1: Write the failing adapter tests**

`internal/kindeapi/apis_test.go`:

```go
package kindeapi_test

import (
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// addAPI registers an API and returns its ID.
func addAPI(t *testing.T, c *kindeapi.Client, name, audience string) string {
	t.Helper()
	created, err := c.AddAPIs(t.Context(), &mgmt.AddAPIsReq{Name: name, Audience: audience})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := created.API.Value.ID.Get()
	if !ok || id == "" {
		t.Fatalf("AddAPIs returned no ID: %+v", created)
	}
	return id
}

func TestAPIRoundTrip(t *testing.T) {
	_, c := newFakeClient(t)
	id := addAPI(t, c, "Orders", "https://orders.example.com")

	got, err := c.GetAPI(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	api, ok := got.API.Get()
	if !ok {
		t.Fatalf("GetAPI returned no api: %+v", got)
	}
	if api.ID.Value != id || api.Name.Value != "Orders" || api.Audience.Value != "https://orders.example.com" {
		t.Fatalf("GetAPI = %+v, want the registered API", api)
	}
	if isMgmt, ok := api.IsManagementAPI.Get(); !ok || isMgmt {
		t.Fatalf("is_management_api = %v (set %v), want false", isMgmt, ok)
	}

	if err := c.DeleteAPI(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetAPI(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("GetAPI after delete: got %v, want not found", err)
	}
	if err := c.DeleteAPI(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("DeleteAPI after delete: got %v, want not found", err)
	}
}

func TestRemoveAPIMakesGetAPINotFound(t *testing.T) {
	f, c := newFakeClient(t)
	id := addAPI(t, c, "Orders", "https://orders.example.com")
	f.RemoveAPI(id)
	if _, err := c.GetAPI(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("got %v, want not found", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/ -run 'TestAPIRoundTrip|TestRemoveAPI'`
Expected: FAIL to compile with `c.AddAPIs undefined (type *kindeapi.Client has no field or method AddAPIs)`.

- [ ] **Step 3: Implement the adapter methods**

`internal/kindeapi/apis.go`:

```go
package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// AddAPIs registers an API. Kinde returns only the new API's ID.
func (c *Client) AddAPIs(ctx context.Context, req *mgmt.AddAPIsReq) (*mgmt.CreateApisResponse, error) {
	return call[*mgmt.CreateApisResponse](ctx, "AddAPIs", func(ctx context.Context) (any, error) {
		return c.api.AddAPIs(ctx, req)
	})
}

// GetAPI returns an API by ID.
func (c *Client) GetAPI(ctx context.Context, id string) (*mgmt.GetAPIResponse, error) {
	return call[*mgmt.GetAPIResponse](ctx, "GetAPI", func(ctx context.Context) (any, error) {
		return c.api.GetAPI(ctx, mgmt.GetAPIParams{APIID: id})
	})
}

// DeleteAPI deletes an API.
func (c *Client) DeleteAPI(ctx context.Context, id string) error {
	_, err := call[*mgmt.DeleteAPIResponse](ctx, "DeleteAPI", func(ctx context.Context) (any, error) {
		return c.api.DeleteAPI(ctx, mgmt.DeleteAPIParams{APIID: id})
	})
	return err
}
```

- [ ] **Step 4: Implement the fake**

`internal/kindefake/apis.go`:

```go
package kindefake

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// apiResource is an API registered in the fake.
type apiResource struct {
	id       string
	name     string
	audience string
}

func (h handler) AddAPIs(_ context.Context, req *mgmt.AddAPIsReq) (mgmt.AddAPIsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a := &apiResource{id: h.f.newID("api"), name: req.Name, audience: req.Audience}
	h.f.apis[a.id] = a
	return &mgmt.CreateApisResponse{
		Message: mgmt.NewOptString("Success"),
		Code:    mgmt.NewOptString("OK"),
		API:     mgmt.NewOptCreateApisResponseAPI(mgmt.CreateApisResponseAPI{ID: mgmt.NewOptString(a.id)}),
	}, nil
}

func (h handler) GetAPI(_ context.Context, params mgmt.GetAPIParams) (mgmt.GetAPIRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, ok := h.f.apis[params.APIID]
	if !ok {
		return nil, notFound("API_NOT_FOUND", "API not found")
	}
	return &mgmt.GetAPIResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("success_response"),
		API: mgmt.NewOptGetAPIResponseAPI(mgmt.GetAPIResponseAPI{
			ID:              mgmt.NewOptString(a.id),
			Name:            mgmt.NewOptString(a.name),
			Audience:        mgmt.NewOptString(a.audience),
			IsManagementAPI: mgmt.NewOptBool(false),
		}),
	}, nil
}

func (h handler) DeleteAPI(_ context.Context, params mgmt.DeleteAPIParams) (mgmt.DeleteAPIRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.apis[params.APIID]; !ok {
		return nil, notFound("API_NOT_FOUND", "API not found")
	}
	delete(h.f.apis, params.APIID)
	return &mgmt.DeleteAPIResponse{
		Message: mgmt.NewOptString("API successfully deleted"),
		Code:    mgmt.NewOptString("API_DELETED"),
	}, nil
}

// RemoveAPI deletes an API behind the provider's back.
func (f *Fake) RemoveAPI(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.apis, id)
}
```

In `internal/kindefake/fake.go`, add this field to `Fake` under `// Domain state.`:

```go
	apis map[string]*apiResource
```

and this line to `New` under `// Initialize domain state.`:

```go
	f.apis = map[string]*apiResource{}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/kindeapi/ ./internal/kindefake/ -race -v`
Expected: PASS, including `TestAPIRoundTrip` and `TestRemoveAPIMakesGetAPINotFound`.

- [ ] **Step 6: Write the failing `boolValue` test**

Add to `internal/provider/values_test.go` (it already imports `testing`, `types`, and `mgmt`):

```go
func TestBoolValue(t *testing.T) {
	tests := []struct {
		name string
		in   mgmt.OptBool
		want types.Bool
	}{
		{"unset", mgmt.OptBool{}, types.BoolNull()},
		{"false", mgmt.NewOptBool(false), types.BoolValue(false)},
		{"true", mgmt.NewOptBool(true), types.BoolValue(true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boolValue(tt.in); !got.Equal(tt.want) {
				t.Fatalf("boolValue = %v, want %v", got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 7: Run the test to verify it fails**

Run: `go test ./internal/provider/ -run TestBoolValue`
Expected: FAIL to compile with `undefined: boolValue`.

- [ ] **Step 8: Add `boolValue`**

Add to `internal/provider/values.go`:

```go
// boolValue converts an optional SDK bool to a Terraform bool. Unset becomes
// null.
func boolValue(v mgmt.OptBool) types.Bool {
	if b, ok := v.Get(); ok {
		return types.BoolValue(b)
	}
	return types.BoolNull()
}
```

Run: `go test ./internal/provider/ -run TestBoolValue -v`
Expected: PASS.

- [ ] **Step 9: Point the acceptance tests at the fake**

Replace `internal/provider/api_resource_test.go` with:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAPIResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-")
	config := fmt.Sprintf(`
resource "kinde_api" "test" {
	name     = "%[1]s"
	audience = "%[1]s"
}
`, testID)
	var apiID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_api.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_api.test", "audience", testID),
					resource.TestCheckResourceAttr("kinde_api.test", "is_management_api", "false"),
					resource.TestCheckResourceAttrWith("kinde_api.test", "id", func(v string) error { apiID = v; return nil }),
				),
			},
			{
				ResourceName:      "kinde_api.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveAPI(apiID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("kinde_api.test", "id"),
			},
		},
	})
}
```

Replace `internal/provider/api_data_source_test.go` with:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAPIDataSource(t *testing.T) {
	testAccFake(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccAPIDataSourceConfig(),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_api.test", "name", "Terraform Acceptance Test API"),
					resource.TestCheckResourceAttr("data.kinde_api.test", "audience", "https://registry.terraform.io/providers/nxt-fwd/kinde"),
					resource.TestCheckResourceAttrPair("data.kinde_api.test", "id", "kinde_api.test", "id"),
				),
			},
		},
	})
}

func testAccAPIDataSourceConfig() string {
	return `
resource "kinde_api" "test" {
	name     = "Terraform Acceptance Test API"
	audience = "https://registry.terraform.io/providers/nxt-fwd/kinde"
}

data "kinde_api" "test" {
	id = kinde_api.test.id
}
`
}
```

In `internal/provider/not_found_test.go`, add this row to the `tests` slice in `TestReadRemovesMissingObjects`:

```go
		{name: "kinde_api", resource: NewAPIResource, attrs: map[string]string{"id": "api_missing"}},
```

- [ ] **Step 10: Run the tests to verify they fail**

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccAPI|TestReadRemovesMissingObjects' -v`
Expected: FAIL. `TestAccAPIResource` fails step 1 with `kinde_api.test: Attribute 'is_management_api' not found`, and `TestReadRemovesMissingObjects/kinde_api` panics with a nil pointer dereference in `(*APIResource).Configure`, which still reads `pd.legacy`.

- [ ] **Step 11: Move the API resource and data source to `kindeapi`**

Replace `internal/provider/api_schema.go` with the following. The `expand*` helpers go: `Create` builds the request from the plan directly, and nothing else used them.

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

type APIResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Audience        types.String `tfsdk:"audience"`
	IsManagementAPI types.Bool   `tfsdk:"is_management_api"`
}

func flattenAPIResource(api mgmt.GetAPIResponseAPI) APIResourceModel {
	return APIResourceModel{
		ID:              stringValue(api.ID),
		Name:            stringValue(api.Name),
		Audience:        stringValue(api.Audience),
		IsManagementAPI: boolValue(api.IsManagementAPI),
	}
}

type APIDataSourceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Audience types.String `tfsdk:"audience"`
}

func flattenAPIDataSource(api mgmt.GetAPIResponseAPI) APIDataSourceModel {
	return APIDataSourceModel{
		ID:       stringValue(api.ID),
		Name:     stringValue(api.Name),
		Audience: stringValue(api.Audience),
	}
}

// getAPI fetches an API's details. Errors from the client, including
// not-found, are returned unchanged.
func getAPI(ctx context.Context, client *kindeapi.Client, id string) (mgmt.GetAPIResponseAPI, error) {
	resp, err := client.GetAPI(ctx, id)
	if err != nil {
		return mgmt.GetAPIResponseAPI{}, err
	}
	api, ok := resp.API.Get()
	if !ok {
		return mgmt.GetAPIResponseAPI{}, fmt.Errorf("kinde GetAPI: the response for %s has no api", id)
	}
	return api, nil
}
```

Replace `internal/provider/api_resource.go` with the following. `Configure`, `Create`, `Read`, and `Delete` change, along with the client field, the imports, and the `is_management_api` description; `Update` and `ImportState` do not.

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &APIResource{}
	_ resource.ResourceWithImportState = &APIResource{}
)

func NewAPIResource() resource.Resource {
	return &APIResource{}
}

type APIResource struct {
	client *kindeapi.Client
}

func (r *APIResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api"
}

func (r *APIResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "APIs represent the resource server to authorise against. See [documentation](https://docs.kinde.com/developer-tools/your-apis/register-manage-apis/) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the API",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the API. Currently, there is no way to change this via the management API.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"audience": schema.StringAttribute{
				MarkdownDescription: "Audience of the API",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"is_management_api": schema.BoolAttribute{
				MarkdownDescription: "Whether this API is the Kinde management API",
				Computed:            true,
			},
		},
	}
}

func (r *APIResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *APIResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan APIResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.AddAPIs(ctx, &mgmt.AddAPIsReq{
		Name:     plan.Name.ValueString(),
		Audience: plan.Audience.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating API",
			fmt.Sprintf("Could not create API: %s", err),
		)
		return
	}
	id, ok := created.API.Value.ID.Get()
	if !ok || id == "" {
		resp.Diagnostics.AddError("Error Creating API", "Kinde did not return the new API's ID.")
		return
	}

	// Kinde returns only the ID, so read the API back for the other fields.
	api, err := getAPI(ctx, r.client, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading API",
			fmt.Sprintf("Could not read API ID %s: %s", id, err),
		)
		return
	}

	state := flattenAPIResource(api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *APIResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state APIResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	api, err := getAPI(ctx, r.client, state.ID.ValueString())
	if err != nil {
		if kindeapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading API",
			fmt.Sprintf("Could not read API ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	state = flattenAPIResource(api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *APIResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"API Update Not Supported",
		"The Kinde API does not support updating APIs. To change the configuration, you must create a new API.",
	)
}

func (r *APIResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state APIResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteAPI(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting API",
			fmt.Sprintf("Could not delete API ID %s: %s", state.ID.ValueString(), err),
		)
	}
}

func (r *APIResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
```

Replace `internal/provider/api_data_source.go` with the following. `Configure` and `Read` change, along with the client field and the imports.

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var _ datasource.DataSource = (*APIDataSource)(nil)

func NewAPIDataSource() datasource.DataSource {
	return &APIDataSource{}
}

type APIDataSource struct {
	client *kindeapi.Client
}

func (d *APIDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api"
}

func (d *APIDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "APIs represent the resource server to authorise against. See [documentation](https://docs.kinde.com/developer-tools/your-apis/register-manage-apis/) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the API",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the API. Currently, there is no way to change this via the management API.",
				Computed:            true,
			},
			"audience": schema.StringAttribute{
				MarkdownDescription: "Audience of the API",
				Computed:            true,
			},
		},
	}
}

func (d *APIDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	d.client = pd.api
}

func (d *APIDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config APIDataSourceModel
	if resp.Diagnostics.Append(req.Config.Get(ctx, &config)...); resp.Diagnostics.HasError() {
		return
	}

	id := config.ID.ValueString()
	tflog.Debug(ctx, "Reading API", map[string]any{"id": id})

	api, err := getAPI(ctx, d.client, id)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get API", err.Error())
		return
	}

	state := flattenAPIDataSource(api)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
```

- [ ] **Step 12: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/... && TF_ACC=1 go test ./internal/provider/ -run 'TestAccAPI|TestReadRemovesMissingObjects|TestAccProviderRejectsBadCredentials' -v`
Expected: PASS, including the out-of-band-delete steps of `TestAccAPIResource` and `TestReadRemovesMissingObjects/kinde_api`.

- [ ] **Step 13: Commit**

```bash
git add internal/kindeapi/apis.go internal/kindeapi/apis_test.go internal/kindefake/apis.go internal/kindefake/fake.go internal/provider/values.go internal/provider/values_test.go internal/provider/api_schema.go internal/provider/api_resource.go internal/provider/api_data_source.go internal/provider/api_resource_test.go internal/provider/api_data_source_test.go internal/provider/not_found_test.go
git commit -m "Move kinde_api to the official SDK" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 10: Connections adapter and fake

Two parts of the official SDK do not work for connections, so this task works around both:

- **Listing.** `GetConnections` decodes every item as empty, because the spec wraps each list item in `{"code", "message", "connection": {...}}` and the live API sends plain objects (kinde-oss/kinde-go issue #53; the fix is the still-open PR #63). `ListConnections` calls the API through `getJSON` and decodes the live shape into the local `kindeapi.Connection` type. The live shape, from PR #63's tests, is:

  ```json
  {"code": "OK", "message": "Success", "has_more": false,
   "connections": [{"id": "conn_123", "name": "saml", "display_name": "SAML Connection", "strategy": "saml"}]}
  ```

  Pagination uses `page_size` (Kinde defaults to 10) and `starting_after` with `has_more`.
- **The fake.** The generated server cannot serve three connection routes:
  - `GET /api/v1/connections` would answer in the spec's wrapped shape;
  - `POST /api/v1/connections` and `PATCH /api/v1/connections/{connection_id}` cannot decode social-connection options. The spec's `options` is a `oneOf` with no field unique to the social variant, so ogen's decoder fails with `unable to detect sum type variant` (the SDK's `fix_oneof.go` makes it fail closed).

  The fake serves these three routes itself through `registerRawRoutes`. `GetConnection` and `DeleteConnection` stay on the generated server.

The fake starts with Kinde's five built-in connections (`email:password`, `email:otp`, `phone:otp`, `username:password`, `username:otp`), because Kinde lists them too. `kinde_connections` filters on them, and the application tests look them up by strategy. They have fixed IDs, so they do not use up `newID` numbers.

**Files:**
- Modify: `internal/kindeapi/pagination.go` (add `allCursorPages`)
- Create: `internal/kindeapi/connections.go`
- Create: `internal/kindefake/connections.go`
- Modify: `internal/kindefake/fake.go` (`Fake` domain state, `New`, and the raw-route helpers)
- Test: `internal/kindeapi/cursor_pagination_test.go`
- Test: `internal/kindeapi/connections_test.go`
- Test: `internal/kindefake/fake_test.go`

**Interfaces:**
- Consumes: `call[T]`, `Client.api`, and `getJSON` (Tasks 1-2); `Fake`, `handler`, `apiError`, `writeAPIError`, and `newFakeClient` (Task 3); `Fake.newID` and `notFound` (Task 5).
- Produces:
  - `func allCursorPages[T any](ctx context.Context, id func(T) string, fetch func(ctx context.Context, startingAfter string) (items []T, hasMore bool, err error)) ([]T, error)` in `pagination.go`. It fails if a page has `has_more` but no items, or if a cursor repeats.
  - `func (c *Client) CreateConnection(ctx context.Context, req *mgmt.CreateConnectionReq) (*mgmt.CreateConnectionResponse, error)`.
  - `func (c *Client) GetConnection(ctx context.Context, id string) (*mgmt.Connection, error)`.
  - `func (c *Client) UpdateConnection(ctx context.Context, id string, req *mgmt.UpdateConnectionReq) error`. It sends only the PATCH; unlike the old library, it does not read the connection back, because the provider never used that result.
  - `func (c *Client) DeleteConnection(ctx context.Context, id string) error`.
  - `func (c *Client) ListConnections(ctx context.Context) ([]Connection, error)` and `type Connection struct { ID, Name, DisplayName, Strategy string }`, which has JSON tags for the live shape. Task 12's `ListApplicationConnections` reads the same shape, so it can reuse `Connection` and `connectionList`.
  - In `kindefake/fake.go`:
    - `func (f *Fake) registerRawRoutes(mux *http.ServeMux)`, called in `New` right before `mux.Handle("/", api)`. Its local `handle(pattern, h)` answers 401 when the token is wrong; Task 12 adds its route with another `handle(...)` line.
    - `func (f *Fake) authorized(r *http.Request) bool`.
    - `func writeJSON(w http.ResponseWriter, status int, v any)`.
  - In `kindefake/connections.go`: the `type connection struct` state type, `type connectionItem` (the live list-item shape, with `func (c *connection) item() connectionItem`), and `func builtinConnections() map[string]*connection`. Task 12 reuses `f.connections` and `connectionItem`.
  - `func (f *Fake) RemoveConnection(id string)` and `func (f *Fake) ConnectionOptions(id string) map[string]any`. The second returns a copy of the options last sent, because Kinde never returns them.

- [ ] **Step 1: Write the failing pagination tests**

`internal/kindeapi/cursor_pagination_test.go`:

```go
package kindeapi

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestAllCursorPagesFollowsLastID(t *testing.T) {
	pages := map[string][]int{"": {1, 2}, "2": {3, 4}, "4": {5}}
	var afters []string
	got, err := allCursorPages(t.Context(), strconv.Itoa, func(_ context.Context, after string) ([]int, bool, error) {
		afters = append(afters, after)
		return pages[after], after != "4", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if want := []string{"", "2", "4"}; !slices.Equal(afters, want) {
		t.Fatalf("starting_after values = %q, want %q", afters, want)
	}
}

func TestAllCursorPagesRejectsEmptyPageWithMore(t *testing.T) {
	_, err := allCursorPages(t.Context(), strconv.Itoa, func(context.Context, string) ([]int, bool, error) {
		return nil, true, nil
	})
	if err == nil || !strings.Contains(err.Error(), "returned none") {
		t.Fatalf("got %v, want an error about an empty page", err)
	}
}

func TestAllCursorPagesRejectsRepeatedCursor(t *testing.T) {
	calls := 0
	_, err := allCursorPages(t.Context(), strconv.Itoa, func(context.Context, string) ([]int, bool, error) {
		calls++
		return []int{1, 2}, true, nil // ignores starting_after
	})
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("got %v, want a repeated-cursor error", err)
	}
	if calls != 2 {
		t.Fatalf("fetch calls = %d, want 2", calls)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/ -run CursorPages`
Expected: FAIL to compile with `undefined: allCursorPages`.

- [ ] **Step 3: Implement `allCursorPages`**

Add to `internal/kindeapi/pagination.go`. It uses `context` and `fmt`, which `allPages` already imports:

```go
// allCursorPages collects every page of a starting_after-paginated endpoint.
// fetch gets "" for the first page; the next page starts after the last
// item's ID.
func allCursorPages[T any](ctx context.Context, id func(T) string, fetch func(ctx context.Context, startingAfter string) (items []T, hasMore bool, err error)) ([]T, error) {
	var all []T
	seen := map[string]bool{}
	after := ""
	for {
		items, hasMore, err := fetch(ctx, after)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if !hasMore {
			return all, nil
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("kinde: a page after %q reported more results but returned none", after)
		}
		after = id(items[len(items)-1])
		if seen[after] {
			return nil, fmt.Errorf("kinde: starting_after %q repeated", after)
		}
		seen[after] = true
	}
}
```

Run: `go test ./internal/kindeapi/ -run CursorPages -v`
Expected: PASS.

- [ ] **Step 4: Write the failing adapter and fake tests**

`internal/kindeapi/connections_test.go`:

```go
package kindeapi_test

import (
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// createConnection creates a Google connection with client credentials and
// returns its ID.
func createConnection(t *testing.T, c *kindeapi.Client, name string) string {
	t.Helper()
	created, err := c.CreateConnection(t.Context(), &mgmt.CreateConnectionReq{
		Name:        mgmt.NewOptString(name),
		DisplayName: mgmt.NewOptString("Display " + name),
		Strategy:    mgmt.NewOptCreateConnectionReqStrategy(mgmt.CreateConnectionReqStrategyOAuth2Google),
		Options: mgmt.NewOptCreateConnectionReqOptions(mgmt.NewCreateConnectionReqOptions0CreateConnectionReqOptions(
			mgmt.CreateConnectionReqOptions0{ClientID: mgmt.NewOptString("cid"), ClientSecret: mgmt.NewOptString("secret")},
		)),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := created.Connection.Value.ID.Get()
	if !ok || id == "" {
		t.Fatalf("CreateConnection returned no ID: %+v", created)
	}
	return id
}

func getConnection(t *testing.T, c *kindeapi.Client, id string) mgmt.ConnectionConnection {
	t.Helper()
	got, err := c.GetConnection(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	conn, ok := got.Connection.Get()
	if !ok {
		t.Fatalf("GetConnection returned no connection: %+v", got)
	}
	return conn
}

func TestConnectionRoundTrip(t *testing.T) {
	f, c := newFakeClient(t)
	id := createConnection(t, c, "google")

	conn := getConnection(t, c, id)
	if conn.ID.Value != id || conn.Name.Value != "google" || conn.DisplayName.Value != "Display google" || conn.Strategy.Value != "oauth2:google" {
		t.Fatalf("GetConnection = %+v, want the created connection", conn)
	}
	if got, want := f.ConnectionOptions(id), map[string]any{"client_id": "cid", "client_secret": "secret"}; !maps.Equal(got, want) {
		t.Fatalf("options sent = %v, want %v", got, want)
	}

	err := c.UpdateConnection(t.Context(), id, &mgmt.UpdateConnectionReq{
		DisplayName: mgmt.NewOptString("Google"),
		Options: mgmt.NewOptUpdateConnectionReqOptions(mgmt.NewUpdateConnectionReqOptions0UpdateConnectionReqOptions(
			mgmt.UpdateConnectionReqOptions0{ClientID: mgmt.NewOptString(""), ClientSecret: mgmt.NewOptString("")},
		)),
	})
	if err != nil {
		t.Fatal(err)
	}
	conn = getConnection(t, c, id)
	if conn.Name.Value != "google" || conn.DisplayName.Value != "Google" {
		t.Fatalf("after update: name %q, display name %q; want google, Google", conn.Name.Value, conn.DisplayName.Value)
	}
	if got, want := f.ConnectionOptions(id), map[string]any{"client_id": "", "client_secret": ""}; !maps.Equal(got, want) {
		t.Fatalf("options sent = %v, want %v", got, want)
	}

	if err := c.DeleteConnection(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetConnection(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("GetConnection after delete: got %v, want not found", err)
	}
	if err := c.UpdateConnection(t.Context(), id, &mgmt.UpdateConnectionReq{Name: mgmt.NewOptString("x")}); !kindeapi.IsNotFound(err) {
		t.Fatalf("UpdateConnection after delete: got %v, want not found", err)
	}
	if err := c.DeleteConnection(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("DeleteConnection after delete: got %v, want not found", err)
	}
}

func TestRemoveConnectionMakesGetConnectionNotFound(t *testing.T) {
	f, c := newFakeClient(t)
	id := createConnection(t, c, "google")
	f.RemoveConnection(id)
	if _, err := c.GetConnection(t.Context(), id); !kindeapi.IsNotFound(err) {
		t.Fatalf("got %v, want not found", err)
	}
}

func TestCreateConnectionRejectsUnknownStrategy(t *testing.T) {
	_, c := newFakeClient(t)
	_, err := c.CreateConnection(t.Context(), &mgmt.CreateConnectionReq{
		Name:     mgmt.NewOptString("bogus"),
		Strategy: mgmt.NewOptCreateConnectionReqStrategy("oauth2:bogus"),
	})
	if !kindeapi.HasCode(err, "INVALID_STRATEGY") {
		t.Fatalf("got %v, want INVALID_STRATEGY", err)
	}
}

func TestListConnectionsReturnsEveryPage(t *testing.T) {
	_, c := newFakeClient(t)
	var created []string
	for i := range 100 {
		created = append(created, createConnection(t, c, fmt.Sprintf("conn-%03d", i)))
	}

	conns, err := c.ListConnections(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	// 100 created plus the 5 built-in connections: more than one page.
	if len(conns) != 105 {
		t.Fatalf("got %d connections, want 105", len(conns))
	}
	var ids []string
	for _, conn := range conns {
		if conn.Name == "" || conn.DisplayName == "" || conn.Strategy == "" {
			t.Fatalf("connection decoded with empty fields: %+v", conn)
		}
		ids = append(ids, conn.ID)
	}
	for _, id := range created {
		if !slices.Contains(ids, id) {
			t.Fatalf("connection %s missing from the list", id)
		}
	}
	if !slices.ContainsFunc(conns, func(conn kindeapi.Connection) bool { return conn.Strategy == "username:password" }) {
		t.Fatal("built-in username:password connection missing from the list")
	}
	slices.Sort(ids)
	if len(slices.Compact(ids)) != len(conns) {
		t.Fatal("the list repeats connections")
	}
}

// TestListConnectionsDecodesLiveShape pins the response shape the live API
// sends, taken from kinde-oss/kinde-go#63: plain connection objects, not the
// {"connection": {...}} envelope the spec describes.
func TestListConnectionsDecodesLiveShape(t *testing.T) {
	var mu sync.Mutex
	var queries []string
	mux := http.NewServeMux()
	mux.HandleFunc("POST /oauth2/token", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"access_token":"tok","token_type":"bearer","expires_in":3600}`)
	})
	mux.HandleFunc("GET /api/v1/connections", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.RawQuery)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("starting_after") == "" {
			_, _ = io.WriteString(w, `{"code":"OK","message":"Success","connections":[
				{"id":"conn_123","name":"saml","display_name":"SAML Connection","strategy":"saml"},
				{"id":"conn_456","name":"oauth","display_name":"OAuth Connection","strategy":"oauth"}
			],"has_more":true}`)
			return
		}
		_, _ = io.WriteString(w, `{"code":"OK","message":"Success","connections":[
			{"id":"conn_789","name":"google","strategy":"oauth2:google"}
		],"has_more":false}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c, err := kindeapi.New(kindeapi.Config{Domain: srv.URL, Audience: srv.URL + "/api", ClientID: "id", ClientSecret: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	got, err := c.ListConnections(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := []kindeapi.Connection{
		{ID: "conn_123", Name: "saml", DisplayName: "SAML Connection", Strategy: "saml"},
		{ID: "conn_456", Name: "oauth", DisplayName: "OAuth Connection", Strategy: "oauth"},
		{ID: "conn_789", Name: "google", Strategy: "oauth2:google"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	mu.Lock()
	defer mu.Unlock()
	if wantQueries := []string{"page_size=100", "page_size=100&starting_after=conn_456"}; !slices.Equal(queries, wantQueries) {
		t.Fatalf("queries = %q, want %q", queries, wantQueries)
	}
}
```

Add to `internal/kindefake/fake_test.go`, which already imports `encoding/json`, `net/http`, and `testing`:

```go
func TestRawRoutesRejectWrongToken(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/connections", "not-the-token")
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, resp) != "UNAUTHORIZED" {
		t.Fatalf("status = %d, want 401 UNAUTHORIZED", resp.StatusCode)
	}
}

func TestConnectionsAreListedInLiveShape(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/connections?page_size=2", fetchToken(t, f))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body struct {
		Connections []map[string]any `json:"connections"`
		HasMore     bool             `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	// The fake starts with 5 built-in connections.
	if len(body.Connections) != 2 || !body.HasMore {
		t.Fatalf("got %d connections, has_more %v; want 2 and true", len(body.Connections), body.HasMore)
	}
	for _, c := range body.Connections {
		if _, wrapped := c["connection"]; wrapped || c["id"] == nil || c["strategy"] == nil {
			t.Fatalf("item %v is not a plain connection object", c)
		}
	}
}
```

- [ ] **Step 5: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/ ./internal/kindefake/ -run 'Connection|RawRoutes'`
Expected:
- `kindeapi` fails to compile with `c.CreateConnection undefined (type *kindeapi.Client has no field or method CreateConnection)`.
- In `kindefake`, `TestConnectionsAreListedInLiveShape` fails with `status = 501, want 200`.
- `TestRawRoutesRejectWrongToken` already passes, because the generated server's security handler also answers 401. It stays to guard the raw routes that replace it.

- [ ] **Step 6: Implement the adapter methods**

`internal/kindeapi/connections.go`:

```go
package kindeapi

import (
	"context"
	"net/url"
	"strconv"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateConnection creates a connection. Kinde returns only its ID.
func (c *Client) CreateConnection(ctx context.Context, req *mgmt.CreateConnectionReq) (*mgmt.CreateConnectionResponse, error) {
	return call[*mgmt.CreateConnectionResponse](ctx, "CreateConnection", func(ctx context.Context) (any, error) {
		return c.api.CreateConnection(ctx, req)
	})
}

// GetConnection returns a connection by ID. Kinde never returns its options.
func (c *Client) GetConnection(ctx context.Context, id string) (*mgmt.Connection, error) {
	return call[*mgmt.Connection](ctx, "GetConnection", func(ctx context.Context) (any, error) {
		return c.api.GetConnection(ctx, mgmt.GetConnectionParams{ConnectionID: id})
	})
}

// UpdateConnection changes the fields set in req and leaves the rest as they
// are.
func (c *Client) UpdateConnection(ctx context.Context, id string, req *mgmt.UpdateConnectionReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "UpdateConnection", func(ctx context.Context) (any, error) {
		return c.api.UpdateConnection(ctx, req, mgmt.UpdateConnectionParams{ConnectionID: id})
	})
	return err
}

// DeleteConnection deletes a connection.
func (c *Client) DeleteConnection(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteConnection", func(ctx context.Context) (any, error) {
		return c.api.DeleteConnection(ctx, mgmt.DeleteConnectionParams{ConnectionID: id})
	})
	return err
}

// Connection is one item of a connection list, in the shape the live API
// returns it.
type Connection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Strategy    string `json:"strategy"`
}

// connectionList is one page of a connection list.
type connectionList struct {
	Connections []Connection `json:"connections"`
	HasMore     bool         `json:"has_more"`
}

// connectionPageSize is how many connections each list request asks for.
// Kinde defaults to 10.
const connectionPageSize = 100

// ListConnections returns every connection, including the built-in ones.
//
// It calls the API directly because the SDK cannot decode the response: the
// spec wraps each list item in {"code", "message", "connection"}, which the
// API does not send, so GetConnections returns empty items. Remove this
// fallback once a kinde-go release includes
// https://github.com/kinde-oss/kinde-go/pull/63.
func (c *Client) ListConnections(ctx context.Context) ([]Connection, error) {
	return allCursorPages(ctx, func(conn Connection) string { return conn.ID },
		func(ctx context.Context, startingAfter string) ([]Connection, bool, error) {
			query := url.Values{"page_size": {strconv.Itoa(connectionPageSize)}}
			if startingAfter != "" {
				query.Set("starting_after", startingAfter)
			}
			var page connectionList
			if err := c.getJSON(ctx, "ListConnections", "/api/v1/connections", query, &page); err != nil {
				return nil, false, err
			}
			return page.Connections, page.HasMore, nil
		})
}
```

- [ ] **Step 7: Implement the fake**

`internal/kindefake/connections.go`:

```go
package kindefake

import (
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"slices"
	"strconv"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// The fake serves three connection routes itself, from registerRawRoutes,
// instead of through the generated server:
//   - GET /api/v1/connections, because the live API lists plain connection
//     objects while the spec wraps each one in {"code", "message",
//     "connection"} (kinde-oss/kinde-go#53).
//   - POST /api/v1/connections and PATCH /api/v1/connections/{id}, because
//     the generated decoder rejects social-connection options with "unable
//     to detect sum type variant": no field is unique to the social variant
//     of the spec's options oneOf.

// connection is a connection held by the fake.
type connection struct {
	id          string
	name        string
	displayName string
	strategy    string
	// options are the options last sent, as decoded JSON. Kinde never
	// returns them.
	options map[string]any
}

// connectionItem is a connection in the shape the live API lists it.
type connectionItem struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Strategy    string `json:"strategy"`
}

func (c *connection) item() connectionItem {
	return connectionItem{ID: c.id, Name: c.name, DisplayName: c.displayName, Strategy: c.strategy}
}

// builtinConnections returns the sign-in methods every Kinde business starts
// with. Kinde lists them with the connections a business creates.
func builtinConnections() map[string]*connection {
	conns := map[string]*connection{}
	for _, c := range []connection{
		{id: "conn_email_password", name: "email-password", displayName: "Email + password", strategy: "email:password"},
		{id: "conn_email_otp", name: "email-otp", displayName: "Email + code", strategy: "email:otp"},
		{id: "conn_phone_otp", name: "phone-otp", displayName: "Phone + code", strategy: "phone:otp"},
		{id: "conn_username_password", name: "username-password", displayName: "Username + password", strategy: "username:password"},
		{id: "conn_username_otp", name: "username-otp", displayName: "Username + code", strategy: "username:otp"},
	} {
		conns[c.id] = &c
	}
	return conns
}

// serveListConnections lists connections in ID order, paginated with
// page_size (default 10) and starting_after.
func (f *Fake) serveListConnections(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	pageSize := 10
	if s := q.Get("page_size"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_PAGE_SIZE", message: "page_size must be a positive integer"})
			return
		}
		pageSize = n
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	ids := slices.Sorted(maps.Keys(f.connections))
	start := 0
	if after := q.Get("starting_after"); after != "" {
		i, found := slices.BinarySearch(ids, after)
		if found {
			i++
		}
		start = i
	}
	end := min(start+pageSize, len(ids))
	items := make([]connectionItem, 0, end-start)
	for _, id := range ids[start:end] {
		items = append(items, f.connections[id].item())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"code":        "OK",
		"message":     "Success",
		"connections": items,
		"has_more":    end < len(ids),
	})
}

func (f *Fake) serveCreateConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string         `json:"name"`
		DisplayName string         `json:"display_name"`
		Strategy    string         `json:"strategy"`
		Options     map[string]any `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: err.Error()})
		return
	}
	if err := mgmt.CreateConnectionReqStrategy(body.Strategy).Validate(); err != nil {
		writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_STRATEGY", message: err.Error()})
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	c := &connection{
		id:          f.newID("conn"),
		name:        body.Name,
		displayName: body.DisplayName,
		strategy:    body.Strategy,
		options:     body.Options,
	}
	f.connections[c.id] = c
	writeJSON(w, http.StatusCreated, map[string]any{
		"message":    "Connection successfully created",
		"code":       "CONNECTION_CREATED",
		"connection": map[string]string{"id": c.id},
	})
}

func (f *Fake) serveUpdateConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        *string        `json:"name"`
		DisplayName *string        `json:"display_name"`
		Options     map[string]any `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: err.Error()})
		return
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.connections[r.PathValue("connection_id")]
	if !ok {
		writeAPIError(w, &apiError{status: http.StatusNotFound, code: "CONNECTION_NOT_FOUND", message: "Connection not found"})
		return
	}
	if body.Name != nil {
		c.name = *body.Name
	}
	if body.DisplayName != nil {
		c.displayName = *body.DisplayName
	}
	if body.Options != nil {
		c.options = body.Options
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "Connection successfully updated", "code": "CONNECTION_UPDATED"})
}

func (h handler) GetConnection(_ context.Context, params mgmt.GetConnectionParams) (mgmt.GetConnectionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	c, ok := h.f.connections[params.ConnectionID]
	if !ok {
		return nil, notFound("CONNECTION_NOT_FOUND", "Connection not found")
	}
	return &mgmt.Connection{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
		Connection: mgmt.NewOptConnectionConnection(mgmt.ConnectionConnection{
			ID:          mgmt.NewOptString(c.id),
			Name:        mgmt.NewOptString(c.name),
			DisplayName: mgmt.NewOptString(c.displayName),
			Strategy:    mgmt.NewOptString(c.strategy),
		}),
	}, nil
}

func (h handler) DeleteConnection(_ context.Context, params mgmt.DeleteConnectionParams) (mgmt.DeleteConnectionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.connections[params.ConnectionID]; !ok {
		return nil, notFound("CONNECTION_NOT_FOUND", "Connection not found")
	}
	delete(h.f.connections, params.ConnectionID)
	return &mgmt.SuccessResponse{
		Message: mgmt.NewOptString("Connection successfully deleted"),
		Code:    mgmt.NewOptString("CONNECTION_DELETED"),
	}, nil
}

// RemoveConnection deletes a connection behind the provider's back.
func (f *Fake) RemoveConnection(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.connections, id)
}

// ConnectionOptions returns a copy of the options last sent for a
// connection, or nil if none were sent.
func (f *Fake) ConnectionOptions(id string) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.connections[id]; ok {
		return maps.Clone(c.options)
	}
	return nil
}
```

In `internal/kindefake/fake.go`:

1. Add this field to `Fake` under `// Domain state.` (gofmt aligns the block):

```go
	connections map[string]*connection
```

2. Add this line to `New` under `// Initialize domain state.`:

```go
	f.connections = builtinConnections()
```

3. In `New`, register the raw routes between the token route and the generated server:

```go
	mux.HandleFunc("POST /oauth2/token", f.serveToken)
	f.registerRawRoutes(mux)
	mux.Handle("/", api)
```

4. Add these functions at the end of the file. `encoding/json` and `net/http` are already imported:

```go
// registerRawRoutes adds the routes the fake serves itself instead of through
// the generated server, for endpoints where the live API and the spec
// disagree or the generated server cannot decode the request. They take
// precedence over the generated server, need the access token, and are
// throttled like any other API route.
func (f *Fake) registerRawRoutes(mux *http.ServeMux) {
	handle := func(pattern string, h http.HandlerFunc) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if !f.authorized(r) {
				writeAPIError(w, &apiError{status: http.StatusUnauthorized, code: "UNAUTHORIZED", message: "kindefake: invalid access token"})
				return
			}
			h(w, r)
		})
	}
	handle("GET /api/v1/connections", f.serveListConnections)
	handle("POST /api/v1/connections", f.serveCreateConnection)
	handle("PATCH /api/v1/connections/{connection_id}", f.serveUpdateConnection)
}

// authorized reports whether r carries the access token the fake issued.
func (f *Fake) authorized(r *http.Request) bool {
	return r.Header.Get("Authorization") == "Bearer "+f.token
}

// writeJSON writes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
```

- [ ] **Step 8: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/kindeapi/ ./internal/kindefake/ -race -v`
Expected: PASS. `TestListConnectionsReturnsEveryPage` lists 105 connections (100 created plus 5 built-in) across two pages of 100. `TestListConnectionsDecodesLiveShape` sends `page_size=100`, then `page_size=100&starting_after=conn_456`.

- [ ] **Step 9: Commit**

```bash
git add internal/kindeapi/pagination.go internal/kindeapi/cursor_pagination_test.go internal/kindeapi/connections.go internal/kindeapi/connections_test.go internal/kindefake/connections.go internal/kindefake/fake.go internal/kindefake/fake_test.go
git commit -m "Add connection endpoints to kindeapi and the fake" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 11: Connections in the provider

`kinde_connection` and `kinde_connections` move to `kindeapi`. `kinde_connections` now returns every connection; it used to stop at Kinde's default page of 10 (spec section 3).

The handling of the sensitive options does not change:
- `options = {...}` sends `client_id` and `client_secret`. Either one, if unset, is sent as `""`, so `options = {}` still clears both in Kinde.
- Leaving `options` out sends no options.
- State keeps the configured values, because Kinde never returns them.

The old library also always sent `is_use_custom_domain: false`. The provider has no such attribute, so it no longer sends it, and Kinde keeps its default or the value set in the Kinde console. `TestAccConnectionResource_OAuth2` pins the exact options sent.

The built-in strategy names that `kinde_connections` filters on become local constants. The OAuth2 strategy list for options uses the SDK's `CreateConnectionReqStrategy` constants.

**Files:**
- Modify: `internal/provider/connection_resource.go`
- Modify: `internal/provider/connections_data_source.go`
- Test: `internal/provider/connection_resource_test.go`
- Create: `internal/provider/connections_data_source_test.go`
- Test: `internal/provider/not_found_test.go`

**Interfaces:**
- Consumes:
  - `CreateConnection`, `GetConnection`, `UpdateConnection`, `DeleteConnection`, `ListConnections`, `kindeapi.Connection`, `Fake.RemoveConnection`, `Fake.ConnectionOptions`, and the fake's built-in connections (Task 10);
  - `providerDataFrom`, `testAccFake`, and `TestReadRemovesMissingObjects` (Task 4);
  - `stringValue` and `optString` (Task 6).
- Produces:
  - In `connection_resource.go`: `func (m *ConnectionOptionsModel) createOptions() mgmt.OptCreateConnectionReqOptions`, `func (m *ConnectionOptionsModel) updateOptions() mgmt.OptUpdateConnectionReqOptions`, and `func isSocialStrategy(strategy string) bool`. They replace `ToAPIOptions` and `convertOptionsToMap`.
  - In `connections_data_source.go`: the constants `strategyEmailPassword`, `strategyEmailOTP`, `strategyPhoneOTP`, `strategyUsernamePassword`, and `strategyUsernameOTP`.

- [ ] **Step 1: Point the resource tests at the fake**

In `internal/provider/connection_resource_test.go`, replace everything from the `import (` line through the end of `TestAccConnectionResource_EmptyToPopulatedOptions` with the code below. The `testAccConnectionResourceConfig_*` helpers after it stay as they are.

The changes are:
- every test starts the fake, and `PreCheck` goes;
- the old-library strategy constant becomes `"oauth2:google"`;
- `TestAccConnectionResource_OAuth2` checks the options Kinde receives and gains the out-of-band-delete steps.

```go
import (
	"fmt"
	"maps"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

// testCheckConnectionOptions checks the options Kinde last received for the
// connection whose ID is in *id. Kinde never returns options, so only the
// fake can show them.
func testCheckConnectionOptions(f *kindefake.Fake, id *string, want map[string]any) resource.TestCheckFunc {
	return func(*terraform.State) error {
		if got := f.ConnectionOptions(*id); !maps.Equal(got, want) {
			return fmt.Errorf("options sent to Kinde = %v, want %v", got, want)
		}
		return nil
	}
}

func TestAccConnectionResource_OAuth2(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")
	var connectionID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccConnectionResourceConfig_OAuth2(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "display_name", "Test OAuth2 Connection"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "strategy", "oauth2:google"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_id", "test-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_secret", "test-client-secret"),
					resource.TestCheckResourceAttrWith("kinde_connection.oauth2", "id", func(v string) error { connectionID = v; return nil }),
					testCheckConnectionOptions(f, &connectionID, map[string]any{"client_id": "test-client-id", "client_secret": "test-client-secret"}),
				),
			},
			// ImportState testing - we now expect empty options but not null
			{
				ResourceName:      "kinde_connection.oauth2",
				ImportState:       true,
				ImportStateVerify: true,
				// Only verify basic fields, since imported resource won't have sensitive values from API
				ImportStateVerifyIgnore: []string{
					"options",
					"options.client_id",
					"options.client_secret",
				},
			},
			// Update with empty options - should reset sensitive fields
			{
				Config: testAccConnectionResourceConfig_OAuth2EmptyOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "display_name", "Test OAuth2 Connection"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "strategy", "oauth2:google"),
					resource.TestCheckNoResourceAttr("kinde_connection.oauth2", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.oauth2", "options.client_secret"),
					testCheckConnectionOptions(f, &connectionID, map[string]any{"client_id": "", "client_secret": ""}),
				),
			},
			// Update with new values
			{
				Config: testAccConnectionResourceConfig_OAuth2Updated(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "display_name", "Updated OAuth2 Connection"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "strategy", "oauth2:google"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_id", "updated-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_secret", "updated-client-secret"),
					testCheckConnectionOptions(f, &connectionID, map[string]any{"client_id": "updated-client-id", "client_secret": "updated-client-secret"}),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveConnection(connectionID) },
				Config:             testAccConnectionResourceConfig_OAuth2Updated(testID),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: testAccConnectionResourceConfig_OAuth2Updated(testID),
				Check:  resource.TestCheckResourceAttrSet("kinde_connection.oauth2", "id"),
			},
		},
	})
}

func TestAccConnectionResource_NoOptions(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccConnectionResourceConfig_NoOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.no_options", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.no_options", "display_name", "Test Connection With Minimal Options"),
					resource.TestCheckResourceAttr("kinde_connection.no_options", "strategy", "oauth2:google"),
					resource.TestCheckResourceAttr("kinde_connection.no_options", "options.client_id", "minimal-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.no_options", "options.client_secret", "minimal-client-secret"),
				),
			},
			// ImportState testing - we now expect empty options but not null
			{
				ResourceName:      "kinde_connection.no_options",
				ImportState:       true,
				ImportStateVerify: true,
				// Only verify basic fields, since imported resource won't have sensitive values from API
				ImportStateVerifyIgnore: []string{
					"options",
					"options.client_id",
					"options.client_secret",
				},
			},
		},
	})
}

func TestAccConnectionResource_EmptyOptionsNoDiff(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with options
			{
				Config: testAccConnectionResourceConfig_OAuth2(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_id", "test-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_secret", "test-client-secret"),
				),
			},
			// Update with empty options - should reset sensitive values
			{
				Config: testAccConnectionResourceConfig_OAuth2EmptyOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckNoResourceAttr("kinde_connection.oauth2", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.oauth2", "options.client_secret"),
				),
			},
		},
	})
}

func TestAccConnectionResource_SensitiveFieldHandling(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with no sensitive fields - should create with null values
			{
				Config: testAccConnectionResourceConfig_NoSensitiveFields(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "name", testID),
					resource.TestCheckNoResourceAttr("kinde_connection.sensitive", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.sensitive", "options.client_secret"),
				),
			},
			// Update to set sensitive fields - should update with new values
			{
				Config: testAccConnectionResourceConfig_WithSensitiveFields(testID, "new-id", "new-secret"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "options.client_id", "new-id"),
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "options.client_secret", "new-secret"),
				),
			},
			// Update with empty options - should reset sensitive fields to null
			{
				Config: testAccConnectionResourceConfig_EmptySensitiveFields(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("kinde_connection.sensitive", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.sensitive", "options.client_secret"),
				),
			},
			// Update with new values again - should set both fields
			{
				Config: testAccConnectionResourceConfig_WithSensitiveFields(testID, "final-id", "final-secret"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "options.client_id", "final-id"),
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "options.client_secret", "final-secret"),
				),
			},
		},
	})
}

func TestAccConnectionResource_ImportEmptyOptions(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with empty options
			{
				Config: testAccConnectionResourceConfig_NoSensitiveFields(testID),
			},
			// Import and verify no changes
			{
				ResourceName:            "kinde_connection.sensitive",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"options"},
				// Verify that re-applying the same config doesn't cause changes
				Config:   testAccConnectionResourceConfig_NoSensitiveFields(testID),
				PlanOnly: true,
			},
		},
	})
}

func TestAccConnectionResource_EmptyToPopulatedOptions(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with populated options - options should now be in state
			{
				Config: testAccConnectionResourceConfig_PopulatedOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "display_name", "Test Empty Options"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "strategy", "oauth2:google"),
					// Sensitive fields should now be stored in state
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "options.client_id", "populated-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "options.client_secret", "populated-client-secret"),
				),
			},
			// Update to empty options - should reset options in state
			{
				Config: testAccConnectionResourceConfig_EmptyOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "display_name", "Test Empty Options"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "strategy", "oauth2:google"),
					// Options should still exist but be empty
					resource.TestCheckResourceAttrSet("kinde_connection.empty_to_populated", "options.%"),
					resource.TestCheckNoResourceAttr("kinde_connection.empty_to_populated", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.empty_to_populated", "options.client_secret"),
				),
			},
			// Import state verification - options will be empty but present
			{
				ResourceName:      "kinde_connection.empty_to_populated",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"options",
					"options.client_id",
					"options.client_secret",
				},
			},
			// Add options back to verify we can still set them after removing
			{
				Config: testAccConnectionResourceConfig_PopulatedOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "display_name", "Test Empty Options"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "strategy", "oauth2:google"),
					// Sensitive fields should be stored in state again
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "options.client_id", "populated-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "options.client_secret", "populated-client-secret"),
				),
			},
		},
	})
}
```

- [ ] **Step 2: Add a data source test**

`internal/provider/connections_data_source_test.go`:

```go
package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccConnectionsDataSource lists more connections than one page of
// Kinde's default page size (10): the fake's 5 built-in connections plus 11
// created ones.
func TestAccConnectionsDataSource(t *testing.T) {
	testAccFake(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccConnectionsDataSourceConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_connections.all", "connections.#", "16"),
					resource.TestCheckResourceAttr("data.kinde_connections.builtin", "connections.#", "5"),
					resource.TestCheckTypeSetElemNestedAttrs("data.kinde_connections.builtin", "connections.*", map[string]string{
						"strategy": "username:password",
					}),
					resource.TestCheckResourceAttr("data.kinde_connections.custom", "connections.#", "11"),
					resource.TestCheckTypeSetElemNestedAttrs("data.kinde_connections.custom", "connections.*", map[string]string{
						"name":         "tfacc-connection-10",
						"display_name": "Connection 10",
						"strategy":     "oauth2:google",
					}),
				),
			},
		},
	})
}

const testAccConnectionsDataSourceConfig = `
resource "kinde_connection" "test" {
	count        = 11
	name         = "tfacc-connection-${count.index}"
	display_name = "Connection ${count.index}"
	strategy     = "oauth2:google"
}

data "kinde_connections" "all" {
	depends_on = [kinde_connection.test]
}

data "kinde_connections" "builtin" {
	filter     = "builtin"
	depends_on = [kinde_connection.test]
}

data "kinde_connections" "custom" {
	filter     = "custom"
	depends_on = [kinde_connection.test]
}
`
```

- [ ] **Step 3: Add the not-found row**

In `internal/provider/not_found_test.go`, add this row to the `tests` slice in `TestReadRemovesMissingObjects`:

```go
		{name: "kinde_connection", resource: NewConnectionResource, attrs: map[string]string{"id": "conn_missing"}},
```

- [ ] **Step 4: Run the tests to verify they fail**

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccConnection|TestReadRemovesMissingObjects' -v`
Expected: FAIL.
- `TestAccConnectionResource_OAuth2` fails step 1 with `options sent to Kinde = map[client_id:test-client-id client_secret:test-client-secret is_use_custom_domain:false], want map[client_id:test-client-id client_secret:test-client-secret]`.
- `TestAccConnectionsDataSource` fails with `data.kinde_connections.all: Attribute 'connections.#' expected "16", got "10"`.
- `TestReadRemovesMissingObjects/kinde_connection` panics with a nil pointer dereference in `(*ConnectionResource).Configure`.

- [ ] **Step 5: Move `kinde_connection` to `kindeapi`**

Replace `internal/provider/connection_resource.go` with the following. The changes are:
- the imports and the client field;
- `ToAPIOptions` is replaced by `createOptions`, `updateOptions`, and `isSocialStrategy`;
- `Configure`, `Create`, `Read`, `Update`, and `Delete` change;
- `convertOptionsToMap` is removed;
- `ValidateConfig` no longer converts to the old library's `Strategy` type.

`Read` keeps the ID from state and treats not-found as removal. `Delete` treats not-found as success. `Update` no longer reads the prior state, which it never used.

```go
package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &ConnectionResource{}
	_ resource.ResourceWithImportState = &ConnectionResource{}
)

func NewConnectionResource() resource.Resource {
	return &ConnectionResource{}
}

type ConnectionResource struct {
	client *kindeapi.Client
}

// ConnectionOptionsModel represents OAuth2 connection options.
type ConnectionOptionsModel struct {
	ClientID     types.String `tfsdk:"client_id" json:"client_id,omitempty"`
	ClientSecret types.String `tfsdk:"client_secret" json:"client_secret,omitempty"`
}

// IsEmpty returns true if both fields are null or empty.
func (m *ConnectionOptionsModel) IsEmpty() bool {
	if m == nil {
		return true
	}
	// Consider both null and empty string as empty
	isClientIDEmpty := m.ClientID.IsNull() || m.ClientID.ValueString() == ""
	isClientSecretEmpty := m.ClientSecret.IsNull() || m.ClientSecret.ValueString() == ""
	return isClientIDEmpty && isClientSecretEmpty
}

// Validate ensures both fields are either both set or both null.
func (m *ConnectionOptionsModel) Validate() error {
	if m == nil {
		return nil
	}

	// If either field is set, both must be set
	if (!m.ClientID.IsNull() || !m.ClientSecret.IsNull()) &&
		(m.ClientID.IsNull() || m.ClientSecret.IsNull()) {
		return fmt.Errorf("both client_id and client_secret must be set if either is provided")
	}

	return nil
}

// createOptions converts the model to social-connection options for a
// create request. Unset fields are sent as "", so removing them from the
// configuration clears them in Kinde.
func (m *ConnectionOptionsModel) createOptions() mgmt.OptCreateConnectionReqOptions {
	return mgmt.NewOptCreateConnectionReqOptions(mgmt.NewCreateConnectionReqOptions0CreateConnectionReqOptions(
		mgmt.CreateConnectionReqOptions0{
			ClientID:     mgmt.NewOptString(m.ClientID.ValueString()),
			ClientSecret: mgmt.NewOptString(m.ClientSecret.ValueString()),
		},
	))
}

// updateOptions is createOptions for an update request.
func (m *ConnectionOptionsModel) updateOptions() mgmt.OptUpdateConnectionReqOptions {
	return mgmt.NewOptUpdateConnectionReqOptions(mgmt.NewUpdateConnectionReqOptions0UpdateConnectionReqOptions(
		mgmt.UpdateConnectionReqOptions0{
			ClientID:     mgmt.NewOptString(m.ClientID.ValueString()),
			ClientSecret: mgmt.NewOptString(m.ClientSecret.ValueString()),
		},
	))
}

// isSocialStrategy reports whether strategy is an OAuth2 social connection,
// the only kind whose options this resource manages.
func isSocialStrategy(strategy string) bool {
	switch mgmt.CreateConnectionReqStrategy(strategy) {
	case mgmt.CreateConnectionReqStrategyOAuth2Apple,
		mgmt.CreateConnectionReqStrategyOAuth2AzureAd,
		mgmt.CreateConnectionReqStrategyOAuth2Bitbucket,
		mgmt.CreateConnectionReqStrategyOAuth2Discord,
		mgmt.CreateConnectionReqStrategyOAuth2Facebook,
		mgmt.CreateConnectionReqStrategyOAuth2Github,
		mgmt.CreateConnectionReqStrategyOAuth2Gitlab,
		mgmt.CreateConnectionReqStrategyOAuth2Google,
		mgmt.CreateConnectionReqStrategyOAuth2Linkedin,
		mgmt.CreateConnectionReqStrategyOAuth2Microsoft,
		mgmt.CreateConnectionReqStrategyOAuth2Patreon,
		mgmt.CreateConnectionReqStrategyOAuth2Slack,
		mgmt.CreateConnectionReqStrategyOAuth2Stripe,
		mgmt.CreateConnectionReqStrategyOAuth2Twitch,
		mgmt.CreateConnectionReqStrategyOAuth2Twitter,
		mgmt.CreateConnectionReqStrategyOAuth2Xero:
		return true
	default:
		return false
	}
}

// ConnectionResourceModel represents the resource model.
type ConnectionResourceModel struct {
	ID          types.String            `tfsdk:"id"`
	Name        types.String            `tfsdk:"name"`
	DisplayName types.String            `tfsdk:"display_name"`
	Strategy    types.String            `tfsdk:"strategy"`
	Options     *ConnectionOptionsModel `tfsdk:"options"`
}

// Equal compares two ConnectionResourceModel instances.
func (m *ConnectionResourceModel) Equal(other *ConnectionResourceModel) bool {
	if m == nil && other == nil {
		return true
	}
	if m == nil || other == nil {
		return false
	}

	if !m.ID.Equal(other.ID) ||
		!m.Name.Equal(other.Name) ||
		!m.DisplayName.Equal(other.DisplayName) ||
		!m.Strategy.Equal(other.Strategy) {
		return false
	}

	// Handle options comparison
	if m.Options == nil && other.Options == nil {
		return true
	}
	if m.Options == nil || other.Options == nil {
		return false
	}
	if m.Options.IsEmpty() && other.Options.IsEmpty() {
		return true
	}

	// For sensitive fields, we need special handling
	// If both values are set (not null), consider them equal
	// This prevents unnecessary updates when the actual values aren't changing
	clientIDEqual := m.Options.ClientID.IsNull() && other.Options.ClientID.IsNull() ||
		(!m.Options.ClientID.IsNull() && !other.Options.ClientID.IsNull())

	clientSecretEqual := m.Options.ClientSecret.IsNull() && other.Options.ClientSecret.IsNull() ||
		(!m.Options.ClientSecret.IsNull() && !other.Options.ClientSecret.IsNull())

	return clientIDEqual && clientSecretEqual
}

// Plan modifier for options.
type optionsEmptyPreserveModifier struct{}

func (m optionsEmptyPreserveModifier) Description(ctx context.Context) string {
	return "Handles options removal and preserves plan values since API never returns sensitive values."
}

func (m optionsEmptyPreserveModifier) MarkdownDescription(ctx context.Context) string {
	return "Handles options removal and preserves plan values since API never returns sensitive values."
}

func (m optionsEmptyPreserveModifier) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	// If config has a value, use it
	if !req.ConfigValue.IsNull() {
		resp.PlanValue = req.ConfigValue
		return
	}

	// If state has a value, preserve it
	if !req.StateValue.IsNull() {
		resp.PlanValue = req.StateValue
		return
	}

	// Otherwise, use null
	resp.PlanValue = req.ConfigValue
}

func (r *ConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (r *ConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a connection in Kinde.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the connection",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the connection",
				Required:            true,
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "Display name of the connection",
				Required:            true,
			},
			"strategy": schema.StringAttribute{
				MarkdownDescription: "Strategy of the connection",
				Required:            true,
			},
			"options": schema.SingleNestedAttribute{
				MarkdownDescription: "Options for the connection. Required for OAuth2 connections. Sensitive values are stored in state and rely on state encryption for security.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Object{&optionsEmptyPreserveModifier{}},
				Attributes: map[string]schema.Attribute{
					"client_id": schema.StringAttribute{
						Optional:  true,
						Sensitive: true,
					},
					"client_secret": schema.StringAttribute{
						Optional:  true,
						Sensitive: true,
					},
				},
			},
		},
	}
}

func (r *ConnectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *ConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ConnectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &mgmt.CreateConnectionReq{
		Name:        optString(plan.Name),
		DisplayName: optString(plan.DisplayName),
		Strategy:    mgmt.NewOptCreateConnectionReqStrategy(mgmt.CreateConnectionReqStrategy(plan.Strategy.ValueString())),
	}
	if plan.Options != nil {
		if !isSocialStrategy(plan.Strategy.ValueString()) {
			resp.Diagnostics.AddError(
				"Error Converting Options",
				fmt.Sprintf("Could not convert options: unsupported strategy: %s", plan.Strategy.ValueString()),
			)
			return
		}
		createReq.Options = plan.Options.createOptions()
	}

	created, err := r.client.CreateConnection(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Connection",
			fmt.Sprintf("Could not create connection: %s", err),
		)
		return
	}
	id, ok := created.Connection.Value.ID.Get()
	if !ok || id == "" {
		resp.Diagnostics.AddError("Error Creating Connection", "Kinde did not return the new connection's ID.")
		return
	}

	// Set ID from response, keep other fields from plan including options
	plan.ID = types.StringValue(id)

	// Store plan in state, including options with sensitive values
	// We'll rely on state encryption for security
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ConnectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetConnection(ctx, state.ID.ValueString())
	if err != nil {
		if kindeapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Connection",
			fmt.Sprintf("Could not read connection ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
	conn, ok := got.Connection.Get()
	if !ok {
		resp.Diagnostics.AddError(
			"Error Reading Connection",
			fmt.Sprintf("Kinde returned no connection for ID %s.", state.ID.ValueString()),
		)
		return
	}

	// Set basic fields from API response
	state.Name = stringValue(conn.Name)
	state.DisplayName = stringValue(conn.DisplayName)
	state.Strategy = stringValue(conn.Strategy)

	// API doesn't return sensitive options, so preserve them from state
	// We're relying on state encryption for security

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ConnectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &mgmt.UpdateConnectionReq{
		Name:        optString(plan.Name),
		DisplayName: optString(plan.DisplayName),
	}
	if plan.Options != nil {
		if !isSocialStrategy(plan.Strategy.ValueString()) {
			resp.Diagnostics.AddError(
				"Error Converting Options",
				fmt.Sprintf("Could not convert options: unsupported strategy: %s", plan.Strategy.ValueString()),
			)
			return
		}
		updateReq.Options = plan.Options.updateOptions()
	}

	if err := r.client.UpdateConnection(ctx, plan.ID.ValueString(), updateReq); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Connection",
			fmt.Sprintf("Could not update connection ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Store plan in state, including options with sensitive values
	// We'll rely on state encryption for security
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ConnectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteConnection(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Connection",
			fmt.Sprintf("Could not delete connection ID %s: %s", state.ID.ValueString(), err),
		)
	}
}

func (r *ConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import just the ID, the Read method will handle the rest
	// Note that sensitive options won't be imported and will need to be set in configuration
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)

	// Add a warning about sensitive values
	resp.Diagnostics.AddWarning(
		"Sensitive Values Not Imported",
		"Sensitive connection options like client_id and client_secret cannot be imported and must be set in your configuration. "+
			"After import, you'll need to set these values in your configuration before making any changes that would trigger an update.",
	)
}

func (r *ConnectionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ConnectionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate strategy
	if !data.Strategy.IsNull() {
		if strings.HasPrefix(data.Strategy.ValueString(), "oauth2:") {
			// Validate options if present
			if data.Options != nil {
				if err := data.Options.Validate(); err != nil {
					resp.Diagnostics.AddError(
						"Invalid Options Configuration",
						err.Error(),
					)
				}
			}
		}
	}
}
```

- [ ] **Step 6: Move `kinde_connections` to `kindeapi`**

Replace `internal/provider/connections_data_source.go` with the following. The changes are:
- the imports and the client field;
- the schema description;
- `Configure`;
- the built-in strategy constants and `isBuiltinStrategy`;
- `Read`, which now calls `ListConnections` and so returns every page.

```go
package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var _ datasource.DataSource = &ConnectionsDataSource{}

func NewConnectionsDataSource() datasource.DataSource {
	return &ConnectionsDataSource{}
}

type ConnectionsDataSource struct {
	client *kindeapi.Client
}

type ConnectionsDataSourceModel struct {
	Filter      types.String      `tfsdk:"filter"`
	Connections []ConnectionModel `tfsdk:"connections"`
}

type ConnectionModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	DisplayName types.String `tfsdk:"display_name"`
	Strategy    types.String `tfsdk:"strategy"`
}

func (d *ConnectionsDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connections"
}

func (d *ConnectionsDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Use this data source to list every connection in the business, including the built-in ones.",

		Attributes: map[string]schema.Attribute{
			"filter": schema.StringAttribute{
				MarkdownDescription: "Filter connections by type. Valid values are: `builtin`, `custom`, `all`. Defaults to `all`.",
				Optional:            true,
			},
			"connections": schema.ListNestedAttribute{
				Computed: true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.StringAttribute{
							Computed: true,
						},
						"name": schema.StringAttribute{
							Computed: true,
						},
						"display_name": schema.StringAttribute{
							Computed: true,
						},
						"strategy": schema.StringAttribute{
							Computed: true,
						},
					},
				},
			},
		},
	}
}

func (d *ConnectionsDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	d.client = pd.api
}

// Strategies of the built-in connections every Kinde business has.
const (
	strategyEmailPassword    = "email:password"
	strategyEmailOTP         = "email:otp"
	strategyPhoneOTP         = "phone:otp"
	strategyUsernamePassword = "username:password"
	strategyUsernameOTP      = "username:otp"
)

func isBuiltinStrategy(strategy string) bool {
	switch strategy {
	case strategyEmailPassword,
		strategyEmailOTP,
		strategyPhoneOTP,
		strategyUsernamePassword,
		strategyUsernameOTP:
		return true
	default:
		return false
	}
}

func (d *ConnectionsDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var data ConnectionsDataSourceModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get all connections
	conns, err := d.client.ListConnections(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Client Error", fmt.Sprintf("Unable to read connections, got error: %s", err))
		return
	}

	// Filter connections based on the filter parameter
	filter := "all"
	if !data.Filter.IsNull() {
		filter = data.Filter.ValueString()
	}

	var filteredConns []kindeapi.Connection
	switch filter {
	case "builtin":
		for _, conn := range conns {
			if isBuiltinStrategy(conn.Strategy) {
				filteredConns = append(filteredConns, conn)
			}
		}
	case "custom":
		for _, conn := range conns {
			if !isBuiltinStrategy(conn.Strategy) {
				filteredConns = append(filteredConns, conn)
			}
		}
	default:
		filteredConns = conns
	}

	// Convert to model
	data.Connections = make([]ConnectionModel, len(filteredConns))
	for i, conn := range filteredConns {
		data.Connections[i] = ConnectionModel{
			ID:          types.StringValue(conn.ID),
			Name:        types.StringValue(conn.Name),
			DisplayName: types.StringValue(conn.DisplayName),
			Strategy:    types.StringValue(conn.Strategy),
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/... && TF_ACC=1 go test ./internal/provider/ -run 'TestAccConnection|TestReadRemovesMissingObjects' -v`
Expected: PASS, including `TestAccConnectionsDataSource` (16 connections: 5 built-in and 11 custom) and `TestReadRemovesMissingObjects/kinde_connection`.

- [ ] **Step 8: Commit**

```bash
git add internal/provider/connection_resource.go internal/provider/connection_resource_test.go internal/provider/connections_data_source.go internal/provider/connections_data_source_test.go internal/provider/not_found_test.go
git commit -m "Move kinde_connection and kinde_connections to the official SDK" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 12: Applications adapter and fake

**Files:**
- Create: `internal/kindeapi/applications.go`
- Create: `internal/kindefake/applications.go`
- Modify: `internal/kindefake/fake.go` (`Fake` fields, `New`, and `registerRawRoutes`)
- Test: `internal/kindeapi/applications_test.go`
- Test: `internal/kindefake/applications_test.go`

**Interfaces:**
- Consumes:
  - `call[T]`, `(*Client).getJSON`, and `Client.api` from Tasks 1-2; `newFakeClient` from Task 3.
  - `handler`, `apiError`, `writeAPIError`, and the `get`, `fetchToken`, and `errorCode` helpers in `internal/kindefake/fake_test.go` from Task 3.
  - `(*Fake).newID` and `notFound` from Task 5.
  - `allCursorPages` and `(*Fake).registerRawRoutes` with its local `handle` wrapper, which checks the access token, from Task 10.
- Produces:
  - `func (c *Client) CreateApplication(ctx context.Context, req *mgmt.CreateApplicationReq) (*mgmt.CreateApplicationResponse, error)`
  - `func (c *Client) GetApplication(ctx context.Context, id string) (*mgmt.GetApplicationResponse, error)`
  - `func (c *Client) UpdateApplication(ctx context.Context, id string, req mgmt.UpdateApplicationReq) error`
  - `func (c *Client) DeleteApplication(ctx context.Context, id string) error`
  - `func (c *Client) GetLogoutURLs(ctx context.Context, applicationID string) (*mgmt.LogoutRedirectUrls, error)`
  - `func (c *Client) GetCallbackURLs(ctx context.Context, applicationID string) (*mgmt.RedirectCallbackUrls, error)`
  - `func (c *Client) EnableConnection(ctx context.Context, applicationID, connectionID string) error`
  - `func (c *Client) RemoveConnection(ctx context.Context, applicationID, connectionID string) error`
  - `func (c *Client) ListApplicationConnections(ctx context.Context, applicationID string) ([]mgmt.ConnectionConnection, error)`
  - Fake handlers for the eight SDK operations above, plus a raw `GET /api/v1/applications/{application_id}/connections` route.
  - Test hooks: `func (f *Fake) RemoveApplication(id string)`, `func (f *Fake) SetApplicationURIs(id string, logoutURIs, redirectURIs []string)`, and `func (f *Fake) RemoveApplicationConnection(applicationID, connectionID string)`.

`GetApplication` does not return logout or redirect URIs, so the adapter reads them with `GetLogoutURLs` and `GetCallbackURLs`. It writes them with `UpdateApplication`: that one PATCH already carries the name, login URI, and homepage URI, and it replaces each URI list it is sent (an empty list clears it), whereas the dedicated add, replace, and delete URL endpoints would need extra calls and a client-side diff.

The SDK's `UpdateApplicationReq` omits a nil `LogoutUris` or `RedirectUris` and sends a non-nil empty slice as `[]`. That is the difference between "leave alone" and "clear", and the round-trip test pins it.

`GetApplicationConnections` decodes every item as empty, the same spec envelope bug as `GetConnections` (kinde-oss/kinde-go issue #53, fix in the still-open PR #63). `ListApplicationConnections` therefore calls the endpoint with `getJSON` and decodes each item as `mgmt.ConnectionConnection`, the item type PR #63 switches the SDK to. The endpoint documents no paging parameters but returns `has_more`, so the adapter pages with `starting_after` through `allCursorPages`, as for `GET /api/v1/connections`.

The generated server validates the application `type` against `reg`, `spa`, `m2m`, and `device` when it decodes a create request, and the client validates it again when it decodes `GetApplicationResponse`. The fake therefore only ever holds valid types.

- [ ] **Step 1: Write the failing adapter and fake tests**

`internal/kindeapi/applications_test.go`:

```go
package kindeapi_test

import (
	"context"
	"fmt"
	"slices"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// requireURIs checks an application's logout and redirect URIs, in order.
func requireURIs(t *testing.T, c *kindeapi.Client, id string, wantLogout, wantRedirect []string) {
	t.Helper()
	logout, err := c.GetLogoutURLs(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(logout.LogoutUrls, wantLogout) {
		t.Fatalf("logout URIs = %q, want %q", logout.LogoutUrls, wantLogout)
	}
	redirect, err := c.GetCallbackURLs(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(redirect.RedirectUrls, wantRedirect) {
		t.Fatalf("redirect URIs = %q, want %q", redirect.RedirectUrls, wantRedirect)
	}
}

func TestApplicationRoundTrip(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()

	created, err := c.CreateApplication(ctx, &mgmt.CreateApplicationReq{Name: "Web", Type: mgmt.CreateApplicationReqTypeReg})
	if err != nil {
		t.Fatal(err)
	}
	app := created.Application.Value
	if app.ID.Value == "" || app.ClientID.Value == "" || app.ClientSecret.Value == "" {
		t.Fatalf("created application = %+v, want an ID, client ID, and client secret", app)
	}
	id := app.ID.Value
	requireURIs(t, c, id, nil, nil)

	logout := []string{"https://app.example/logout"}
	redirect := []string{"https://app.example/callback", "https://app.example/callback2"}
	if err := c.UpdateApplication(ctx, id, mgmt.UpdateApplicationReq{
		Name:         mgmt.NewOptString("Web 2"),
		LoginURI:     mgmt.NewOptString("https://app.example/login"),
		HomepageURI:  mgmt.NewOptString("https://app.example"),
		LogoutUris:   logout,
		RedirectUris: redirect,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := c.GetApplication(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	a := got.Application.Value
	if a.Name.Value != "Web 2" || a.Type.Value != mgmt.GetApplicationResponseApplicationTypeReg ||
		a.ClientID.Value != app.ClientID.Value || a.ClientSecret.Value != app.ClientSecret.Value ||
		a.LoginURI.Value != "https://app.example/login" || a.HomepageURI.Value != "https://app.example" {
		t.Fatalf("application = %+v", a)
	}
	requireURIs(t, c, id, logout, redirect)

	// Lists left nil are not sent, so the URIs stay.
	if err := c.UpdateApplication(ctx, id, mgmt.UpdateApplicationReq{Name: mgmt.NewOptString("Web 3")}); err != nil {
		t.Fatal(err)
	}
	requireURIs(t, c, id, logout, redirect)

	// Empty lists are sent and clear the URIs.
	if err := c.UpdateApplication(ctx, id, mgmt.UpdateApplicationReq{LogoutUris: []string{}, RedirectUris: []string{}}); err != nil {
		t.Fatal(err)
	}
	requireURIs(t, c, id, nil, nil)

	if err := c.DeleteApplication(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetApplication(ctx, id); !kindeapi.IsNotFound(err) {
		t.Fatalf("GetApplication after delete: got %v, want not found", err)
	}
}

func TestApplicationOperationsReportMissingApplication(t *testing.T) {
	_, c := newFakeClient(t)
	const id = "app_missing"
	tests := map[string]func(context.Context) error{
		"GetApplication": func(ctx context.Context) error {
			_, err := c.GetApplication(ctx, id)
			return err
		},
		"UpdateApplication": func(ctx context.Context) error {
			return c.UpdateApplication(ctx, id, mgmt.UpdateApplicationReq{Name: mgmt.NewOptString("x")})
		},
		"DeleteApplication": func(ctx context.Context) error {
			return c.DeleteApplication(ctx, id)
		},
		"GetLogoutURLs": func(ctx context.Context) error {
			_, err := c.GetLogoutURLs(ctx, id)
			return err
		},
		"GetCallbackURLs": func(ctx context.Context) error {
			_, err := c.GetCallbackURLs(ctx, id)
			return err
		},
		"EnableConnection": func(ctx context.Context) error {
			return c.EnableConnection(ctx, id, "conn_0001")
		},
		"RemoveConnection": func(ctx context.Context) error {
			return c.RemoveConnection(ctx, id, "conn_0001")
		},
		"ListApplicationConnections": func(ctx context.Context) error {
			_, err := c.ListApplicationConnections(ctx, id)
			return err
		},
	}
	for name, fn := range tests {
		t.Run(name, func(t *testing.T) {
			if err := fn(t.Context()); !kindeapi.IsNotFound(err) || !kindeapi.HasCode(err, "APPLICATION_NOT_FOUND") {
				t.Fatalf("got %v, want a 404 APPLICATION_NOT_FOUND", err)
			}
		})
	}
}

// TestListApplicationConnectionsReturnsAllPages also checks the raw-JSON
// fallback: the SDK's own decoder would return every ID as empty.
func TestListApplicationConnectionsReturnsAllPages(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	created, err := c.CreateApplication(ctx, &mgmt.CreateApplicationReq{Name: "Web", Type: mgmt.CreateApplicationReqTypeReg})
	if err != nil {
		t.Fatal(err)
	}
	id := created.Application.Value.ID.Value

	// The fake pages 10 connections at a time, so 12 take two pages.
	var want []string
	for i := range 12 {
		connID := fmt.Sprintf("conn_%02d", i)
		if err := c.EnableConnection(ctx, id, connID); err != nil {
			t.Fatal(err)
		}
		want = append(want, connID)
	}

	listIDs := func() []string {
		t.Helper()
		conns, err := c.ListApplicationConnections(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		ids := make([]string, 0, len(conns))
		for _, conn := range conns {
			ids = append(ids, conn.ID.Value)
		}
		return ids
	}
	if got := listIDs(); !slices.Equal(got, want) {
		t.Fatalf("connections = %q, want %q", got, want)
	}

	if err := c.RemoveConnection(ctx, id, "conn_03"); err != nil {
		t.Fatal(err)
	}
	want = slices.DeleteFunc(want, func(s string) bool { return s == "conn_03" })
	if got := listIDs(); !slices.Equal(got, want) {
		t.Fatalf("connections after removal = %q, want %q", got, want)
	}

	if err := c.RemoveConnection(ctx, id, "conn_03"); !kindeapi.IsNotFound(err) {
		t.Fatalf("removing a disabled connection: got %v, want not found", err)
	}
}
```

`internal/kindefake/applications_test.go`:

```go
package kindefake_test

import (
	"net/http"
	"testing"

	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

// The application connections list is served outside the generated router,
// so it checks the token itself.
func TestApplicationConnectionsRouteChecksToken(t *testing.T) {
	f := kindefake.New(t)
	const path = "/api/v1/applications/app_missing/connections"

	resp := get(t, f, path, "")
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, resp) != "UNAUTHORIZED" {
		t.Fatalf("without a token: status = %d, want 401 UNAUTHORIZED", resp.StatusCode)
	}
	resp = get(t, f, path, fetchToken(t, f))
	if resp.StatusCode != http.StatusNotFound || errorCode(t, resp) != "APPLICATION_NOT_FOUND" {
		t.Fatalf("with a token: status = %d, want 404 APPLICATION_NOT_FOUND", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/ ./internal/kindefake/ -run 'Application'`
Expected:
- `internal/kindeapi` fails to compile with `c.GetLogoutURLs undefined (type *kindeapi.Client has no field or method GetLogoutURLs)`.
- `internal/kindefake` fails `TestApplicationConnectionsRouteChecksToken` with `with a token: status = 501, want 404 APPLICATION_NOT_FOUND`, because the generated router still answers `GetApplicationConnections` with `UnimplementedHandler`.

- [ ] **Step 3: Implement the fake**

`internal/kindefake/applications.go`:

```go
package kindefake

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// application is an application as the fake stores it.
type application struct {
	id           string
	name         string
	appType      string
	clientSecret string
	loginURI     mgmt.OptString
	homepageURI  mgmt.OptString
	logoutURIs   []string
	redirectURIs []string
	// connections holds the IDs of enabled connections in the order they
	// were enabled. The fake does not check that the connections exist.
	connections []string
}

// lookupApplication returns the application with the given ID or a 404.
// Callers must hold f.mu.
func (f *Fake) lookupApplication(id string) (*application, error) {
	a, ok := f.applications[id]
	if !ok {
		return nil, notFound("APPLICATION_NOT_FOUND", "Application not found")
	}
	return a, nil
}

// CreateApplication stores an application. As in Kinde, its client ID is
// its ID.
func (h handler) CreateApplication(_ context.Context, req *mgmt.CreateApplicationReq) (mgmt.CreateApplicationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	id := h.f.newID("app")
	a := &application{id: id, name: req.Name, appType: string(req.Type), clientSecret: id + "_secret"}
	h.f.applications[id] = a
	return &mgmt.CreateApplicationResponse{
		Code:    mgmt.NewOptString("APPLICATION_CREATED"),
		Message: mgmt.NewOptString("Application successfully created"),
		Application: mgmt.NewOptCreateApplicationResponseApplication(mgmt.CreateApplicationResponseApplication{
			ID:           mgmt.NewOptString(a.id),
			ClientID:     mgmt.NewOptString(a.id),
			ClientSecret: mgmt.NewOptString(a.clientSecret),
		}),
	}, nil
}

// GetApplication returns an application. Kinde serves its logout and
// redirect URIs from separate endpoints.
func (h handler) GetApplication(_ context.Context, params mgmt.GetApplicationParams) (mgmt.GetApplicationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.ApplicationID)
	if err != nil {
		return nil, err
	}
	return &mgmt.GetApplicationResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("success_response"),
		Application: mgmt.NewOptGetApplicationResponseApplication(mgmt.GetApplicationResponseApplication{
			ID:           mgmt.NewOptString(a.id),
			Name:         mgmt.NewOptString(a.name),
			Type:         mgmt.NewOptGetApplicationResponseApplicationType(mgmt.GetApplicationResponseApplicationType(a.appType)),
			ClientID:     mgmt.NewOptString(a.id),
			ClientSecret: mgmt.NewOptString(a.clientSecret),
			LoginURI:     a.loginURI,
			HomepageURI:  a.homepageURI,
		}),
	}, nil
}

// UpdateApplication changes the fields the request sets and leaves the rest
// alone.
func (h handler) UpdateApplication(_ context.Context, req mgmt.OptUpdateApplicationReq, params mgmt.UpdateApplicationParams) (mgmt.UpdateApplicationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.ApplicationID)
	if err != nil {
		return nil, err
	}
	u, _ := req.Get()
	if name, ok := u.Name.Get(); ok {
		a.name = name
	}
	if u.LoginURI.Set {
		a.loginURI = u.LoginURI
	}
	if u.HomepageURI.Set {
		a.homepageURI = u.HomepageURI
	}
	// A list that is sent replaces the stored one; an empty list clears it.
	if u.LogoutUris != nil {
		a.logoutURIs = slices.Clone(u.LogoutUris)
	}
	if u.RedirectUris != nil {
		a.redirectURIs = slices.Clone(u.RedirectUris)
	}
	return &mgmt.UpdateApplicationOK{}, nil
}

// DeleteApplication deletes an application.
func (h handler) DeleteApplication(_ context.Context, params mgmt.DeleteApplicationParams) (mgmt.DeleteApplicationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, err := h.f.lookupApplication(params.ApplicationID); err != nil {
		return nil, err
	}
	delete(h.f.applications, params.ApplicationID)
	return &mgmt.SuccessResponse{
		Code:    mgmt.NewOptString("APPLICATION_DELETED"),
		Message: mgmt.NewOptString("Application successfully deleted"),
	}, nil
}

// GetLogoutURLs returns an application's logout URIs.
func (h handler) GetLogoutURLs(_ context.Context, params mgmt.GetLogoutURLsParams) (mgmt.GetLogoutURLsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.AppID)
	if err != nil {
		return nil, err
	}
	return &mgmt.LogoutRedirectUrls{
		LogoutUrls: append([]string{}, a.logoutURIs...),
		Code:       mgmt.NewOptString("OK"),
		Message:    mgmt.NewOptString("Success"),
	}, nil
}

// GetCallbackURLs returns an application's redirect URIs.
func (h handler) GetCallbackURLs(_ context.Context, params mgmt.GetCallbackURLsParams) (mgmt.GetCallbackURLsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.AppID)
	if err != nil {
		return nil, err
	}
	return &mgmt.RedirectCallbackUrls{RedirectUrls: append([]string{}, a.redirectURIs...)}, nil
}

// EnableConnection records a connection as enabled for an application. It
// accepts any connection ID.
func (h handler) EnableConnection(_ context.Context, params mgmt.EnableConnectionParams) (mgmt.EnableConnectionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.ApplicationID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(a.connections, params.ConnectionID) {
		a.connections = append(a.connections, params.ConnectionID)
	}
	return &mgmt.EnableConnectionOK{}, nil
}

// RemoveConnection disables a connection for an application. It answers
// 404 if the connection is not enabled.
func (h handler) RemoveConnection(_ context.Context, params mgmt.RemoveConnectionParams) (mgmt.RemoveConnectionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	a, err := h.f.lookupApplication(params.ApplicationID)
	if err != nil {
		return nil, err
	}
	i := slices.Index(a.connections, params.ConnectionID)
	if i < 0 {
		return nil, notFound("CONNECTION_NOT_FOUND", "Connection is not enabled for this application")
	}
	a.connections = slices.Delete(a.connections, i, i+1)
	return &mgmt.SuccessResponse{
		Code:    mgmt.NewOptString("CONNECTION_REMOVED"),
		Message: mgmt.NewOptString("Connection successfully removed"),
	}, nil
}

// serveApplicationConnections serves
// GET /api/v1/applications/{application_id}/connections in the shape the
// API really sends: plain connection objects, not the {"connection": {...}}
// envelope that the spec declares and the SDK decodes as empty. Items carry
// only the connection ID, because the fake records enabled IDs without
// looking the connections up. It pages with page_size (default 10) and
// starting_after, like GET /api/v1/connections.
func (f *Fake) serveApplicationConnections(w http.ResponseWriter, r *http.Request) {
	pageSize := 10
	if v := r.URL.Query().Get("page_size"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: invalid page_size " + v})
			return
		}
		pageSize = n
	}

	f.mu.Lock()
	a, ok := f.applications[r.PathValue("application_id")]
	var ids []string
	if ok {
		ids = slices.Clone(a.connections)
	}
	f.mu.Unlock()
	if !ok {
		writeAPIError(w, &apiError{status: http.StatusNotFound, code: "APPLICATION_NOT_FOUND", message: "Application not found"})
		return
	}

	start := 0
	if after := r.URL.Query().Get("starting_after"); after != "" {
		i := slices.Index(ids, after)
		if i < 0 {
			writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: unknown starting_after " + after})
			return
		}
		start = i + 1
	}
	end := min(start+pageSize, len(ids))

	type item struct {
		ID string `json:"id"`
	}
	items := make([]item, 0, end-start)
	for _, id := range ids[start:end] {
		items = append(items, item{ID: id})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":        "OK",
		"message":     "Success",
		"connections": items,
		"has_more":    end < len(ids),
	})
}

// RemoveApplication deletes an application behind the provider's back.
func (f *Fake) RemoveApplication(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.applications, id)
}

// SetApplicationURIs replaces an application's logout and redirect URIs
// behind the provider's back. It does nothing if the application does not
// exist.
func (f *Fake) SetApplicationURIs(id string, logoutURIs, redirectURIs []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.applications[id]; ok {
		a.logoutURIs = slices.Clone(logoutURIs)
		a.redirectURIs = slices.Clone(redirectURIs)
	}
}

// RemoveApplicationConnection disables a connection for an application
// behind the provider's back.
func (f *Fake) RemoveApplicationConnection(applicationID, connectionID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if a, ok := f.applications[applicationID]; ok {
		a.connections = slices.DeleteFunc(a.connections, func(id string) bool { return id == connectionID })
	}
}
```

In `internal/kindefake/fake.go`, add the state field under `// Domain state.`:

```go
	applications map[string]*application
```

Initialize it in `New` under `// Initialize domain state.`:

```go
	f.applications = map[string]*application{}
```

Add the raw route inside `registerRawRoutes` (created in Task 10), after the connections routes. Task 10's local `handle` checks the access token, so the handler doesn't:

```go
	handle("GET /api/v1/applications/{application_id}/connections", f.serveApplicationConnections)
```

The pattern is more specific than the `/` pattern that serves the generated router, so this route wins for `GET` requests. `POST` and `DELETE` on `/api/v1/applications/{application_id}/connections/{connection_id}` still reach the generated `EnableConnection` and `RemoveConnection` handlers.

- [ ] **Step 4: Implement the adapter**

`internal/kindeapi/applications.go`:

```go
package kindeapi

import (
	"context"
	"net/url"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateApplication creates an application. The response carries its ID,
// client ID, and client secret.
func (c *Client) CreateApplication(ctx context.Context, req *mgmt.CreateApplicationReq) (*mgmt.CreateApplicationResponse, error) {
	return call[*mgmt.CreateApplicationResponse](ctx, "CreateApplication", func(ctx context.Context) (any, error) {
		return c.api.CreateApplication(ctx, req)
	})
}

// GetApplication gets an application. Its logout and redirect URIs come
// from GetLogoutURLs and GetCallbackURLs.
func (c *Client) GetApplication(ctx context.Context, id string) (*mgmt.GetApplicationResponse, error) {
	return call[*mgmt.GetApplicationResponse](ctx, "GetApplication", func(ctx context.Context) (any, error) {
		return c.api.GetApplication(ctx, mgmt.GetApplicationParams{ApplicationID: id})
	})
}

// UpdateApplication changes the fields set in req. A nil URI list leaves the
// application's URIs alone; a non-nil list, even an empty one, replaces them.
func (c *Client) UpdateApplication(ctx context.Context, id string, req mgmt.UpdateApplicationReq) error {
	_, err := call[*mgmt.UpdateApplicationOK](ctx, "UpdateApplication", func(ctx context.Context) (any, error) {
		return c.api.UpdateApplication(ctx, mgmt.NewOptUpdateApplicationReq(req), mgmt.UpdateApplicationParams{ApplicationID: id})
	})
	return err
}

// DeleteApplication deletes an application.
func (c *Client) DeleteApplication(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteApplication", func(ctx context.Context) (any, error) {
		return c.api.DeleteApplication(ctx, mgmt.DeleteApplicationParams{ApplicationID: id})
	})
	return err
}

// GetLogoutURLs returns an application's logout redirect URIs.
func (c *Client) GetLogoutURLs(ctx context.Context, applicationID string) (*mgmt.LogoutRedirectUrls, error) {
	return call[*mgmt.LogoutRedirectUrls](ctx, "GetLogoutURLs", func(ctx context.Context) (any, error) {
		return c.api.GetLogoutURLs(ctx, mgmt.GetLogoutURLsParams{AppID: applicationID})
	})
}

// GetCallbackURLs returns an application's redirect (callback) URIs.
func (c *Client) GetCallbackURLs(ctx context.Context, applicationID string) (*mgmt.RedirectCallbackUrls, error) {
	return call[*mgmt.RedirectCallbackUrls](ctx, "GetCallbackURLs", func(ctx context.Context) (any, error) {
		return c.api.GetCallbackURLs(ctx, mgmt.GetCallbackURLsParams{AppID: applicationID})
	})
}

// EnableConnection enables a connection for an application.
func (c *Client) EnableConnection(ctx context.Context, applicationID, connectionID string) error {
	_, err := call[*mgmt.EnableConnectionOK](ctx, "EnableConnection", func(ctx context.Context) (any, error) {
		return c.api.EnableConnection(ctx, mgmt.EnableConnectionParams{ApplicationID: applicationID, ConnectionID: connectionID})
	})
	return err
}

// RemoveConnection disables a connection for an application.
func (c *Client) RemoveConnection(ctx context.Context, applicationID, connectionID string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "RemoveConnection", func(ctx context.Context) (any, error) {
		return c.api.RemoveConnection(ctx, mgmt.RemoveConnectionParams{ApplicationID: applicationID, ConnectionID: connectionID})
	})
	return err
}

// applicationConnectionsPage is one page of
// GET /api/v1/applications/{application_id}/connections as the API sends it.
type applicationConnectionsPage struct {
	Connections []mgmt.ConnectionConnection `json:"connections"`
	HasMore     bool                        `json:"has_more"`
}

// ListApplicationConnections returns every connection enabled for an
// application.
//
// The SDK's GetApplicationConnections decodes every item as empty, because
// the spec wraps each item in a {"connection": {...}} envelope that the API
// does not send (https://github.com/kinde-oss/kinde-go/issues/53). This
// calls the endpoint directly and decodes each item as
// mgmt.ConnectionConnection, the item type that
// https://github.com/kinde-oss/kinde-go/pull/63 gives the SDK. Remove this
// fallback once a release includes that fix.
//
// The endpoint documents no paging parameters but returns has_more, so this
// pages with starting_after like GET /api/v1/connections.
func (c *Client) ListApplicationConnections(ctx context.Context, applicationID string) ([]mgmt.ConnectionConnection, error) {
	path := "/api/v1/applications/" + url.PathEscape(applicationID) + "/connections"
	return allCursorPages(ctx,
		func(conn mgmt.ConnectionConnection) string { return conn.ID.Value },
		func(ctx context.Context, startingAfter string) ([]mgmt.ConnectionConnection, bool, error) {
			var query url.Values
			if startingAfter != "" {
				query = url.Values{"starting_after": {startingAfter}}
			}
			var page applicationConnectionsPage
			if err := c.getJSON(ctx, "GetApplicationConnections", path, query, &page); err != nil {
				return nil, false, err
			}
			return page.Connections, page.HasMore, nil
		})
}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/kindeapi/ ./internal/kindefake/ -race -run 'Application' -v`
Expected: PASS. `TestListApplicationConnectionsReturnsAllPages` lists 12 connections through two fake pages of 10, and `TestApplicationOperationsReportMissingApplication` passes all eight subtests.

Run: `go build ./... && go vet ./... && go test ./internal/...`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/kindeapi/applications.go internal/kindeapi/applications_test.go internal/kindefake/applications.go internal/kindefake/applications_test.go internal/kindefake/fake.go
git commit -m "Add the applications adapter and fake" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 13: Migrate `kinde_application` and `kinde_application_connection`

**Files:**
- Modify: `internal/provider/application_resource.go`
- Modify: `internal/provider/application_schema.go` (rewritten)
- Modify: `internal/provider/application_data_source.go`
- Modify: `internal/provider/application_connection_resource.go`
- Test: `internal/provider/application_resource_test.go`
- Test: `internal/provider/application_data_source_test.go`
- Test: `internal/provider/application_connection_resource_test.go`
- Test: `internal/provider/not_found_test.go`

**Interfaces:**
- Consumes:
  - The Task 12 adapter methods: `CreateApplication`, `GetApplication`, `UpdateApplication`, `DeleteApplication`, `GetLogoutURLs`, `GetCallbackURLs`, `EnableConnection`, `RemoveConnection`, and `ListApplicationConnections`.
  - The Task 12 fake hooks `RemoveApplication`, `SetApplicationURIs`, and `RemoveApplicationConnection`.
  - `stringValue` and `optString` from Task 6.
  - `providerDataFrom`, `testAccFake`, and the `TestReadRemovesMissingObjects` table from Task 4.
  - In acceptance-test HCL: the `kinde_connection` resource and the `kinde_connections` data source, migrated in Task 11.
- Produces, in `internal/provider/application_schema.go`:
  - `func applicationTypeValue(v mgmt.OptGetApplicationResponseApplicationType) types.String`
  - `func uriValue(v mgmt.OptString) types.String`
  - `func uriSetValue(ctx context.Context, uris []string, prior types.Set) (types.Set, diag.Diagnostics)`
  - `func uriList(ctx context.Context, s types.Set) ([]string, diag.Diagnostics)`
  - `func expandApplicationUpdate(ctx context.Context, m applicationResourceModel) (mgmt.UpdateApplicationReq, diag.Diagnostics)`
- Schema: `kinde_application.logout_uris` and `redirect_uris` change from `schema.ListAttribute` to `schema.SetAttribute` and are read from Kinde.

How the URI sets behave:
- Read sets them from `GetLogoutURLs` and `GetCallbackURLs`. A change made outside Terraform shows as drift.
- Update always sends both lists. A set that is null, because the attribute was removed, or empty, because it is `[]`, is sent as `[]` and clears the URIs.
- Kinde cannot tell "no URIs" from an empty list. Read therefore keeps an empty set when the prior state held one, and returns null otherwise, so both `logout_uris = []` and a missing attribute converge.

`login_uri` and `homepage_uri` keep their current behavior: Read maps empty to null, and a null plan value is not sent.

- [ ] **Step 1: Write the failing tests**

Replace `internal/provider/application_resource_test.go` with:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccApplicationResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-")
	uri := fmt.Sprintf("http://localhost:%d", acctest.RandIntRange(3000, 4000))
	var appID string

	initial := testAccApplicationResourceConfig(testID, uri, fmt.Sprintf(`
	logout_uris   = ["%[1]s/oauth/logout"]
	redirect_uris = ["%[1]s/oauth/redirect"]`, uri))
	updated := testAccApplicationResourceConfig(testID, uri, fmt.Sprintf(`
	logout_uris   = ["%[1]s/oauth/logout", "%[1]s/signed-out"]
	redirect_uris = ["%[1]s/oauth/callback"]`, uri))
	// An empty set and a missing attribute both clear the URIs.
	cleared := testAccApplicationResourceConfig(testID, uri, `
	logout_uris = []`)

	updatedURIs := testAccApplicationURIs(
		knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(uri + "/oauth/logout"), knownvalue.StringExact(uri + "/signed-out")}),
		knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(uri + "/oauth/callback")}),
	)
	updatesInPlace := []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("kinde_application.test", plancheck.ResourceActionUpdate),
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: initial,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_application.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttr("kinde_application.test", "login_uri", uri+"/oauth/login"),
					resource.TestCheckResourceAttr("kinde_application.test", "homepage_uri", uri),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_secret"),
					resource.TestCheckResourceAttrWith("kinde_application.test", "id", func(v string) error {
						appID = v
						return nil
					}),
				),
				ConfigStateChecks: testAccApplicationURIs(
					knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(uri + "/oauth/logout")}),
					knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(uri + "/oauth/redirect")}),
				),
			},
			// Every attribute, URIs included, is read from Kinde.
			{
				ResourceName:      "kinde_application.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Changing the URIs updates the application in place.
			{
				Config:            updated,
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: updatesInPlace},
				ConfigStateChecks: updatedURIs,
			},
			// URIs changed outside Terraform show up as drift.
			{
				PreConfig: func() {
					f.SetApplicationURIs(appID, []string{uri + "/elsewhere"}, []string{uri + "/oauth/callback"})
				},
				Config:             updated,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks:   resource.ConfigPlanChecks{PostApplyPostRefresh: updatesInPlace},
			},
			// Applying puts the configured URIs back.
			{
				Config:            updated,
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: updatesInPlace},
				ConfigStateChecks: updatedURIs,
			},
			// Clearing the URIs updates the application in place.
			{
				Config:           cleared,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: updatesInPlace},
				ConfigStateChecks: testAccApplicationURIs(
					knownvalue.SetExact([]knownvalue.Check{}),
					knownvalue.Null(),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveApplication(appID) },
				Config:             cleared,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: cleared,
				Check:  resource.TestCheckResourceAttrSet("kinde_application.test", "id"),
			},
		},
	})
}

// testAccApplicationResourceConfig returns a kinde_application with the
// given name, login and homepage URIs under uri, and extra attributes.
func testAccApplicationResourceConfig(name, uri, extra string) string {
	return fmt.Sprintf(`
resource "kinde_application" "test" {
	name         = %[1]q
	type         = "reg"
	login_uri    = "%[2]s/oauth/login"
	homepage_uri = %[2]q
	%[3]s
}
`, name, uri, extra)
}

// testAccApplicationURIs checks kinde_application.test's logout and
// redirect URIs.
func testAccApplicationURIs(logout, redirect knownvalue.Check) []statecheck.StateCheck {
	return []statecheck.StateCheck{
		statecheck.ExpectKnownValue("kinde_application.test", tfjsonpath.New("logout_uris"), logout),
		statecheck.ExpectKnownValue("kinde_application.test", tfjsonpath.New("redirect_uris"), redirect),
	}
}

func TestAccApplicationResource_Connections(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccApplicationResourceConfig_WithConnections(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_application.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_secret"),
					// Check that both connections exist and are linked
					resource.TestCheckResourceAttrSet("kinde_application_connection.password", "id"),
					resource.TestCheckResourceAttrSet("kinde_application_connection.otp", "id"),
					// Verify the application IDs match
					resource.TestCheckResourceAttrPair(
						"kinde_application_connection.password", "application_id",
						"kinde_application.test", "id",
					),
					resource.TestCheckResourceAttrPair(
						"kinde_application_connection.otp", "application_id",
						"kinde_application.test", "id",
					),
				),
			},
		},
	})
}

func testAccApplicationResourceConfig_WithConnections(name string) string {
	return fmt.Sprintf(`
data "kinde_connections" "builtin" {
	filter = "builtin"
}

resource "kinde_application" "test" {
	name = %[1]q
	type = "reg"
}

resource "kinde_application_connection" "password" {
	application_id = kinde_application.test.id
	connection_id  = data.kinde_connections.builtin.connections[index(data.kinde_connections.builtin.connections[*].strategy, "username:password")].id
}

resource "kinde_application_connection" "otp" {
	application_id = kinde_application.test.id
	connection_id  = data.kinde_connections.builtin.connections[index(data.kinde_connections.builtin.connections[*].strategy, "username:otp")].id
}
`, name)
}
```

`TestAccApplicationResource_Connections` assumes that Task 10's fake seeds the built-in connections (`username:password` and `username:otp`), as a new Kinde business has them.

In `internal/provider/application_data_source_test.go`, replace `TestAccApplicationDataSource` with:

```go
func TestAccApplicationDataSource(t *testing.T) {
	testAccFake(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
				resource "kinde_application" "test" {
					name = "Terraform Acceptance Example Application"
					type = "reg"
				}

				data "kinde_application" "test" {
					id = kinde_application.test.id
				}
				`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrSet("data.kinde_application.test", "id"),
					resource.TestCheckResourceAttr("data.kinde_application.test", "name", "Terraform Acceptance Example Application"),
					resource.TestCheckResourceAttr("data.kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttrPair("data.kinde_application.test", "client_id", "kinde_application.test", "client_id"),
				),
			},
		},
	})
}
```

Replace `TestAccApplicationConnectionResource` in `internal/provider/application_connection_resource_test.go` with the version below. `testAccApplicationConnectionResourceConfig` does not change.

```go
func TestAccApplicationConnectionResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")
	config := testAccApplicationConnectionResourceConfig(testID)
	var appID, connID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_application_connection.test", "application_id", "kinde_application.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_application_connection.test", "connection_id", "kinde_connection.test", "id"),
					resource.TestCheckResourceAttrWith("kinde_application_connection.test", "application_id", func(v string) error {
						appID = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("kinde_application_connection.test", "connection_id", func(v string) error {
						connID = v
						return nil
					}),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_application_connection.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Disabled outside Terraform: refresh drops it and the plan enables it again.
			{
				PreConfig:          func() { f.RemoveApplicationConnection(appID, connID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying enables it again.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("kinde_application_connection.test", "id"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}
```

In `internal/provider/not_found_test.go`, add these rows to the `tests` slice in `TestReadRemovesMissingObjects`:

```go
		{name: "kinde_application", resource: NewApplicationResource, attrs: map[string]string{"id": "app_missing"}},
		{name: "kinde_application_connection", resource: NewApplicationConnectionResource, attrs: map[string]string{
			"id": "app_missing:conn_missing", "application_id": "app_missing", "connection_id": "conn_missing",
		}},
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run 'TestReadRemovesMissingObjects' -v`
Expected: FAIL. `TestReadRemovesMissingObjects/kinde_application` panics with `runtime error: invalid memory address or nil pointer dereference`, because `Configure` still reads `pd.legacy`, which the table test leaves nil.

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccApplicationResource$' -v`
Expected: FAIL at `Step 2/8 error running import: ImportStateVerify attributes not equivalent`, with `logout_uris.#`, `logout_uris.0`, `redirect_uris.#`, and `redirect_uris.0` missing after import. The legacy Read copies the URIs from state instead of reading them from Kinde.

`TestAccApplicationDataSource` and `TestAccApplicationConnectionResource` already pass at this point, because the legacy client works against the Task 12 fake. They guard the migration.

- [ ] **Step 3: Rewrite the schema helpers**

Replace `internal/provider/application_schema.go` with the version below. It drops the unused `ApplicationResourceModel` and the `//nolint:unused` expand and flatten functions, which used the old library and `internal/serde`. `role_schema.go` still uses `internal/serde`.

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

type ApplicationDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
}

// applicationTypeValue converts an application type read from Kinde to a
// Terraform string. Unset becomes null.
func applicationTypeValue(v mgmt.OptGetApplicationResponseApplicationType) types.String {
	if !v.Set {
		return types.StringNull()
	}
	return types.StringValue(string(v.Value))
}

// uriValue converts a login or homepage URI read from Kinde to a Terraform
// string. Empty and unset both become null.
func uriValue(v mgmt.OptString) types.String {
	if v.Value == "" {
		return types.StringNull()
	}
	return types.StringValue(v.Value)
}

// uriSetValue converts logout or redirect URIs read from Kinde to a set.
// Kinde does not tell "no URIs" from an empty list, so no URIs stay an
// empty set when prior is one and are null otherwise.
func uriSetValue(ctx context.Context, uris []string, prior types.Set) (types.Set, diag.Diagnostics) {
	if len(uris) > 0 {
		return types.SetValueFrom(ctx, types.StringType, uris)
	}
	if prior.IsNull() || prior.IsUnknown() {
		return types.SetNull(types.StringType), nil
	}
	return types.SetValueMust(types.StringType, []attr.Value{}), nil
}

// uriList returns the URIs in s for an update request. A null set gives an
// empty list, which is still sent and clears the URIs in Kinde.
func uriList(ctx context.Context, s types.Set) ([]string, diag.Diagnostics) {
	uris := []string{}
	if s.IsNull() {
		return uris, nil
	}
	diags := s.ElementsAs(ctx, &uris, false)
	return uris, diags
}

// expandApplicationUpdate builds the update request for m. It always sends
// both URI lists, so Kinde ends up with exactly the configured URIs.
func expandApplicationUpdate(ctx context.Context, m applicationResourceModel) (mgmt.UpdateApplicationReq, diag.Diagnostics) {
	var diags diag.Diagnostics
	logoutURIs, d := uriList(ctx, m.LogoutURIs)
	diags.Append(d...)
	redirectURIs, d := uriList(ctx, m.RedirectURIs)
	diags.Append(d...)
	return mgmt.UpdateApplicationReq{
		Name:         optString(m.Name),
		LoginURI:     optString(m.LoginURI),
		HomepageURI:  optString(m.HomepageURI),
		LogoutUris:   logoutURIs,
		RedirectUris: redirectURIs,
	}, diags
}
```

- [ ] **Step 4: Migrate the `kinde_application` resource**

Replace `internal/provider/application_resource.go` with the version below. The model's URI fields become `types.Set`; the schema's URI attributes become `schema.SetAttribute`; and `Configure`, `Create`, `Read`, `Update`, and `Delete` move to `kindeapi`. `Metadata`, the other attributes, and `ImportState` are unchanged.

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &ApplicationResource{}
	_ resource.ResourceWithImportState = &ApplicationResource{}
)

func NewApplicationResource() resource.Resource {
	return &ApplicationResource{}
}

type ApplicationResource struct {
	client *kindeapi.Client
}

type applicationResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	LoginURI     types.String `tfsdk:"login_uri"`
	HomepageURI  types.String `tfsdk:"homepage_uri"`
	LogoutURIs   types.Set    `tfsdk:"logout_uris"`
	RedirectURIs types.Set    `tfsdk:"redirect_uris"`
}

func (r *ApplicationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (r *ApplicationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Applications facilitates the interface for users to authenticate against. See [documentation](https://docs.kinde.com/build/applications/about-applications/) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the application",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the application. Currently, there is no way to change this via the management application.",
				Required:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Type of the application",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "Client id of the application",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "Client secret of the application",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"login_uri": schema.StringAttribute{
				Description: "The login URI of the application.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"homepage_uri": schema.StringAttribute{
				Description: "The homepage URI of the application.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"logout_uris": schema.SetAttribute{
				MarkdownDescription: "The logout URIs of the application. They are read from Kinde, so changes made outside Terraform show as drift. Set to `[]` or remove the attribute to clear them.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"redirect_uris": schema.SetAttribute{
				MarkdownDescription: "The redirect (callback) URIs of the application. They are read from Kinde, so changes made outside Terraform show as drift. Set to `[]` or remove the attribute to clear them.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *ApplicationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
	tflog.Debug(ctx, "Application resource configured")
}

func (r *ApplicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan applicationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateApplication(ctx, &mgmt.CreateApplicationReq{
		Name: plan.Name.ValueString(),
		Type: mgmt.CreateApplicationReqType(plan.Type.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Application",
			fmt.Sprintf("Could not create application: %s", err),
		)
		return
	}
	app, ok := created.Application.Get()
	if !ok || app.ID.Value == "" {
		resp.Diagnostics.AddError("Error Creating Application", "Kinde did not return the new application's ID.")
		return
	}
	plan.ID = stringValue(app.ID)
	plan.ClientID = stringValue(app.ClientID)
	plan.ClientSecret = stringValue(app.ClientSecret)

	// Kinde creates an application from its name and type only; the URIs
	// need a second call.
	if !plan.LoginURI.IsNull() || !plan.HomepageURI.IsNull() || !plan.LogoutURIs.IsNull() || !plan.RedirectURIs.IsNull() {
		update, diags := expandApplicationUpdate(ctx, plan)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if err := r.client.UpdateApplication(ctx, app.ID.Value, update); err != nil {
			resp.Diagnostics.AddError(
				"Error Updating Application",
				fmt.Sprintf("Could not update application ID %s: %s", app.ID.Value, err),
			)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ApplicationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state applicationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	app, err := r.client.GetApplication(ctx, id)
	var logout *mgmt.LogoutRedirectUrls
	if err == nil {
		logout, err = r.client.GetLogoutURLs(ctx, id)
	}
	var redirect *mgmt.RedirectCallbackUrls
	if err == nil {
		redirect, err = r.client.GetCallbackURLs(ctx, id)
	}
	if kindeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Application",
			fmt.Sprintf("Could not read application ID %s: %s", id, err),
		)
		return
	}

	a := app.Application.Value
	state.Name = stringValue(a.Name)
	state.Type = applicationTypeValue(a.Type)
	state.ClientID = stringValue(a.ClientID)
	state.ClientSecret = stringValue(a.ClientSecret)
	state.LoginURI = uriValue(a.LoginURI)
	state.HomepageURI = uriValue(a.HomepageURI)

	var diags diag.Diagnostics
	state.LogoutURIs, diags = uriSetValue(ctx, logout.LogoutUrls, state.LogoutURIs)
	resp.Diagnostics.Append(diags...)
	state.RedirectURIs, diags = uriSetValue(ctx, redirect.RedirectUrls, state.RedirectURIs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ApplicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan applicationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	update, diags := expandApplicationUpdate(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := plan.ID.ValueString()
	if err := r.client.UpdateApplication(ctx, id, update); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Application",
			fmt.Sprintf("Could not update application ID %s: %s", id, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ApplicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state applicationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteApplication(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Application",
			fmt.Sprintf("Could not delete application ID %s: %s", state.ID.ValueString(), err),
		)
	}
}

func (r *ApplicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
```

- [ ] **Step 5: Migrate the `kinde_application` data source**

In `internal/provider/application_data_source.go`, replace the import block and the `ApplicationDataSource` struct:

```go
import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)
```

```go
type ApplicationDataSource struct {
	client *kindeapi.Client
}
```

In `Schema`, change the `type` attribute's description:

```go
			"type": schema.StringAttribute{
				Description: "The type of the application (reg, spa, m2m, or device).",
				Computed:    true,
			},
```

Replace `Configure` and `Read`:

```go
func (d *ApplicationDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	d.client = pd.api
}

func (d *ApplicationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ApplicationDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	app, err := d.client.GetApplication(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Application",
			fmt.Sprintf("Could not read application ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	a := app.Application.Value
	state.Name = stringValue(a.Name)
	state.Type = applicationTypeValue(a.Type)
	state.ClientID = stringValue(a.ClientID)
	state.ClientSecret = stringValue(a.ClientSecret)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
```

- [ ] **Step 6: Migrate the `kinde_application_connection` resource**

In `internal/provider/application_connection_resource.go`, replace the import block and the `ApplicationConnectionResource` struct:

```go
import (
	"context"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)
```

```go
type ApplicationConnectionResource struct {
	client *kindeapi.Client
}
```

Replace `Configure`, `Read`, and `Delete`. `Create` keeps its code, because `kindeapi`'s `EnableConnection` has the legacy method's signature. `Update` and `ImportState` do not change.

```go
func (r *ApplicationConnectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *ApplicationConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state applicationConnectionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	connections, err := r.client.ListApplicationConnections(ctx, state.ApplicationID.ValueString())
	if kindeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Application Connections",
			fmt.Sprintf("Could not read connections for application ID %s: %s", state.ApplicationID.ValueString(), err),
		)
		return
	}

	// The connection may have been disabled outside Terraform.
	enabled := slices.ContainsFunc(connections, func(c mgmt.ConnectionConnection) bool {
		return c.ID.Value == state.ConnectionID.ValueString()
	})
	if !enabled {
		resp.State.RemoveResource(ctx)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state applicationConnectionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveConnection(ctx, state.ApplicationID.ValueString(), state.ConnectionID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Disabling Connection",
			fmt.Sprintf("Could not disable connection ID %s for application ID %s: %s", state.ConnectionID.ValueString(), state.ApplicationID.ValueString(), err),
		)
		return
	}
}
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go build ./... && go vet ./... && go test ./internal/...`
Expected: PASS. `TestReadRemovesMissingObjects` passes its `kinde_application` and `kinde_application_connection` subtests.

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccApplication|TestReadRemovesMissingObjects' -v`
Expected: PASS for `TestAccApplicationResource` (8 steps), `TestAccApplicationResource_Connections`, `TestAccApplicationDataSource`, and `TestAccApplicationConnectionResource` (4 steps).

Run: `grep -rn 'kinde-go/api/applications' internal/provider`
Expected: no output.

- [ ] **Step 8: Commit**

```bash
git add internal/provider/application_resource.go internal/provider/application_schema.go internal/provider/application_data_source.go internal/provider/application_connection_resource.go internal/provider/application_resource_test.go internal/provider/application_data_source_test.go internal/provider/application_connection_resource_test.go internal/provider/not_found_test.go
git commit -m "Move applications to the official Kinde SDK" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 14: Organizations adapter and fake

**Files:**
- Modify: `internal/kindefake/fake.go`
- Create: `internal/kindefake/organizations.go`
- Create: `internal/kindeapi/organizations.go`
- Test: `internal/kindeapi/organizations_test.go`

**Interfaces:**
- Consumes:
  - `call[T]` and `APIError`, `IsNotFound`, `HasCode` (Task 1); `Client.api` (Task 2).
  - `Fake`, `handler`, `apiError`, and, in tests, `newFakeClient` (Task 3).
  - `func (f *Fake) newID(prefix string) string` and `func notFound(code, message string) error` (Task 5).
- Produces:
  - In `internal/kindefake/fake.go`: `const createdOn = "2026-01-01T00:00:00Z"`, shared by every later fake domain.
  - `func (c *Client) CreateOrganization(ctx context.Context, req *mgmt.CreateOrganizationReq) (*mgmt.CreateOrganizationResponse, error)`.
  - `func (c *Client) GetOrganization(ctx context.Context, code string) (*mgmt.GetOrganizationResponse, error)`.
  - `func (c *Client) UpdateOrganization(ctx context.Context, code string, req *mgmt.UpdateOrganizationReq) error`.
  - `func (c *Client) DeleteOrganization(ctx context.Context, code string) error`.
  - Fake state: `Fake.organizations map[string]*organization`, keyed by organization code, with `type organization struct { details mgmt.GetOrganizationResponse }`. Task 18 adds a `users` field.
  - `func (f *Fake) findOrganization(code string) (*organization, error)`: the organization, or a 404 `ORGANIZATION_NOT_FOUND`. Callers hold `f.mu`.
  - Test hook: `func (f *Fake) RemoveOrganization(code string)`.
  - In package `kindeapi_test`: `func createOrganization(t *testing.T, c *kindeapi.Client, req *mgmt.CreateOrganizationReq) string`, which returns the new code.

Notes on the SDK, all checked against v0.3.0:
- `CreateOrganization` returns only the new code, at `.Organization.Value.Code`.
- `GetOrganization` returns a flat `GetOrganizationResponse`. It has no declared 404, so a missing code is an undeclared status; the fake answers 404 `ORGANIZATION_NOT_FOUND`, the code Kinde's spec uses for a missing organization.
- `UpdateOrganization` answers with a `SuccessResponse`, so the adapter returns only an error and callers read the organization back.
- `DeleteOrganization` declares its 404 as a typed `NotFoundResponse` with an object, not a list, under `errors`. The fake returns that typed result, so the adapter test covers a typed 404.
- `created_on` is a string. The four brand colors are `OptNil...Color` objects with `Raw`, `Hex`, and `Hsl`. `theme_code` and `color_scheme` are enums that the client validates when decoding.

- [ ] **Step 1: Write the failing adapter tests**

`internal/kindeapi/organizations_test.go`:

```go
package kindeapi_test

import (
	"errors"
	"net/http"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// createOrganization creates an organization and returns its code.
func createOrganization(t *testing.T, c *kindeapi.Client, req *mgmt.CreateOrganizationReq) string {
	t.Helper()
	created, err := c.CreateOrganization(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	code, ok := created.Organization.Value.Code.Get()
	if !ok || code == "" {
		t.Fatalf("CreateOrganization returned no code: %+v", created)
	}
	return code
}

func TestOrganizationRoundTrip(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{
		Name:            "Acme",
		Handle:          mgmt.NewOptString("acme"),
		BackgroundColor: mgmt.NewOptString("#ffffff"),
		ThemeCode:       mgmt.NewOptString("dark"),
	})

	org, err := c.GetOrganization(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if got := org.Code.Value; got != code {
		t.Errorf("code = %q, want %q", got, code)
	}
	if got := org.Name.Value; got != "Acme" {
		t.Errorf("name = %q, want Acme", got)
	}
	if got, _ := org.Handle.Get(); got != "acme" {
		t.Errorf("handle = %q, want acme", got)
	}
	if !org.ExternalID.IsNull() {
		t.Errorf("external_id = %+v, want null", org.ExternalID)
	}
	if bg, _ := org.BackgroundColor.Get(); bg.Hex.Value != "#ffffff" {
		t.Errorf("background_color = %+v, want hex #ffffff", org.BackgroundColor)
	}
	if !org.LinkColor.IsNull() {
		t.Errorf("link_color = %+v, want null", org.LinkColor)
	}
	if got := org.ThemeCode.Value; got != mgmt.GetOrganizationResponseThemeCodeDark {
		t.Errorf("theme_code = %q, want dark", got)
	}
	if got := org.CreatedOn.Value; got != "2026-01-01T00:00:00Z" {
		t.Errorf("created_on = %q, want 2026-01-01T00:00:00Z", got)
	}

	err = c.UpdateOrganization(ctx, code, &mgmt.UpdateOrganizationReq{
		Name:       mgmt.NewOptString("Acme Corp"),
		ExternalID: mgmt.NewOptString("ext-1"),
		LinkColor:  mgmt.NewOptString("#0056f1"),
		ThemeCode:  mgmt.NewOptUpdateOrganizationReqThemeCode(mgmt.UpdateOrganizationReqThemeCodeUserPreference),
	})
	if err != nil {
		t.Fatal(err)
	}
	org, err = c.GetOrganization(ctx, code)
	if err != nil {
		t.Fatal(err)
	}
	if got := org.Name.Value; got != "Acme Corp" {
		t.Errorf("name = %q, want Acme Corp", got)
	}
	if got, _ := org.Handle.Get(); got != "acme" {
		t.Errorf("handle = %q, want it unchanged", got)
	}
	if got, _ := org.ExternalID.Get(); got != "ext-1" {
		t.Errorf("external_id = %q, want ext-1", got)
	}
	if link, _ := org.LinkColor.Get(); link.Hex.Value != "#0056f1" {
		t.Errorf("link_color = %+v, want hex #0056f1", org.LinkColor)
	}
	if bg, _ := org.BackgroundColor.Get(); bg.Hex.Value != "#ffffff" {
		t.Errorf("background_color = %+v, want it unchanged", org.BackgroundColor)
	}
	// The color scheme differs from the theme code for user_preference.
	if got := org.ThemeCode.Value; got != mgmt.GetOrganizationResponseThemeCodeUserPreference {
		t.Errorf("theme_code = %q, want user_preference", got)
	}
	if got := org.ColorScheme.Value; got != mgmt.GetOrganizationResponseColorSchemeLightDark {
		t.Errorf("color_scheme = %q, want light dark", got)
	}

	if err := c.DeleteOrganization(ctx, code); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetOrganization(ctx, code); !kindeapi.IsNotFound(err) {
		t.Fatalf("after delete: got %v, want not found", err)
	}
}

func TestCreateOrganizationDefaults(t *testing.T) {
	_, c := newFakeClient(t)
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{Name: "Acme"})
	org, err := c.GetOrganization(t.Context(), code)
	if err != nil {
		t.Fatal(err)
	}
	if !org.Handle.IsNull() || !org.BackgroundColor.IsNull() {
		t.Errorf("handle = %+v, background_color = %+v; want both null", org.Handle, org.BackgroundColor)
	}
	if got := org.ThemeCode.Value; got != mgmt.GetOrganizationResponseThemeCodeLight {
		t.Errorf("theme_code = %q, want light", got)
	}
}

func TestOrganizationNotFound(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	_, err := c.GetOrganization(ctx, "org_missing")
	if !kindeapi.IsNotFound(err) || !kindeapi.HasCode(err, "ORGANIZATION_NOT_FOUND") {
		t.Errorf("GetOrganization: got %v, want ORGANIZATION_NOT_FOUND 404", err)
	}
	err = c.UpdateOrganization(ctx, "org_missing", &mgmt.UpdateOrganizationReq{Name: mgmt.NewOptString("Acme")})
	if !kindeapi.IsNotFound(err) {
		t.Errorf("UpdateOrganization: got %v, want not found", err)
	}
	// DeleteOrganization declares its 404, so this exercises a typed result.
	err = c.DeleteOrganization(ctx, "org_missing")
	if !kindeapi.IsNotFound(err) || !kindeapi.HasCode(err, "ORGANIZATION_NOT_FOUND") {
		t.Errorf("DeleteOrganization: got %v, want ORGANIZATION_NOT_FOUND 404", err)
	}
}

func TestCreateOrganizationRejectsUnknownThemeCode(t *testing.T) {
	_, c := newFakeClient(t)
	_, err := c.CreateOrganization(t.Context(), &mgmt.CreateOrganizationReq{Name: "Acme", ThemeCode: mgmt.NewOptString("light dark")})
	var apiErr *kindeapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("got %v, want a 400 *APIError", err)
	}
}

func TestRemoveOrganization(t *testing.T) {
	f, c := newFakeClient(t)
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{Name: "Acme"})
	f.RemoveOrganization(code)
	if _, err := c.GetOrganization(t.Context(), code); !kindeapi.IsNotFound(err) {
		t.Fatalf("got %v, want not found", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/ -run Organization`
Expected: FAIL to compile with `c.CreateOrganization undefined (type *kindeapi.Client has no field or method CreateOrganization)`.

- [ ] **Step 3: Add the organization state to the fake**

In `internal/kindefake/fake.go`, add the shared timestamp below the import block:

```go
// createdOn is the timestamp the fake reports for every object it creates.
const createdOn = "2026-01-01T00:00:00Z"
```

Add this field under `// Domain state.` in `Fake`:

```go
	organizations map[string]*organization
```

Add this line under `// Initialize domain state.` in `New`:

```go
	f.organizations = map[string]*organization{}
```

Run `gofmt -w internal/kindefake` so the new field lines up with the other domain fields.

- [ ] **Step 4: Implement the fake's organization endpoints**

New organizations get theme `light` when the request sets none. An unset handle, external ID, or color comes back as JSON `null`, which the spec allows for those fields. Colors are stored with the value sent as both `raw` and `hex`.

`internal/kindefake/organizations.go`:

```go
package kindefake

import (
	"context"
	"net/http"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// organization is the fake's record of one organization.
type organization struct {
	// details is what GetOrganization returns.
	details mgmt.GetOrganizationResponse
}

// colorSchemes maps each theme code to the color scheme Kinde reports with
// it. Its keys are the only theme codes the fake accepts.
var colorSchemes = map[mgmt.GetOrganizationResponseThemeCode]mgmt.GetOrganizationResponseColorScheme{
	mgmt.GetOrganizationResponseThemeCodeLight:          mgmt.GetOrganizationResponseColorSchemeLight,
	mgmt.GetOrganizationResponseThemeCodeDark:           mgmt.GetOrganizationResponseColorSchemeDark,
	mgmt.GetOrganizationResponseThemeCodeUserPreference: mgmt.GetOrganizationResponseColorSchemeLightDark,
}

// findOrganization returns the organization with the given code, or Kinde's
// not-found error. Callers must hold f.mu.
func (f *Fake) findOrganization(code string) (*organization, error) {
	org, ok := f.organizations[code]
	if !ok {
		return nil, notFound("ORGANIZATION_NOT_FOUND", "Organization not found")
	}
	return org, nil
}

// setTheme sets the theme code and the color scheme Kinde derives from it.
func setTheme(d *mgmt.GetOrganizationResponse, theme mgmt.GetOrganizationResponseThemeCode) error {
	scheme, ok := colorSchemes[theme]
	if !ok {
		return &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "theme_code must be light, dark, or user_preference"}
	}
	d.ThemeCode = mgmt.NewOptGetOrganizationResponseThemeCode(theme)
	d.ColorScheme = mgmt.NewOptGetOrganizationResponseColorScheme(scheme)
	return nil
}

// brandColor is how Kinde reports a brand color: raw, hex, and HSL forms.
// The fake fills the raw and hex forms with the value it was sent.
func brandColor(v string) mgmt.GetOrganizationResponseLinkColor {
	return mgmt.GetOrganizationResponseLinkColor{Raw: mgmt.NewOptString(v), Hex: mgmt.NewOptString(v)}
}

// setColors applies the brand colors a create or update request sets and
// leaves the others alone. Kinde's color types share one shape, so
// brandColor's result converts to each of them.
func setColors(d *mgmt.GetOrganizationResponse, background, button, buttonText, link mgmt.OptString) {
	if v, ok := background.Get(); ok {
		d.BackgroundColor = mgmt.NewOptNilGetOrganizationResponseBackgroundColor(mgmt.GetOrganizationResponseBackgroundColor(brandColor(v)))
	}
	if v, ok := button.Get(); ok {
		d.ButtonColor = mgmt.NewOptNilGetOrganizationResponseButtonColor(mgmt.GetOrganizationResponseButtonColor(brandColor(v)))
	}
	if v, ok := buttonText.Get(); ok {
		d.ButtonTextColor = mgmt.NewOptNilGetOrganizationResponseButtonTextColor(mgmt.GetOrganizationResponseButtonTextColor(brandColor(v)))
	}
	if v, ok := link.Get(); ok {
		d.LinkColor = mgmt.NewOptNilGetOrganizationResponseLinkColor(brandColor(v))
	}
}

// setNilString copies an optional request field into a nullable response
// field when the request sets it.
func setNilString(dst *mgmt.OptNilString, v mgmt.OptString) {
	if s, ok := v.Get(); ok {
		dst.SetTo(s)
	}
}

func (h handler) CreateOrganization(_ context.Context, req *mgmt.CreateOrganizationReq) (mgmt.CreateOrganizationRes, error) {
	if req.Name == "" {
		return nil, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "name is required"}
	}

	// Kinde reports unset optional fields as null.
	d := mgmt.GetOrganizationResponse{
		Name:      mgmt.NewOptString(req.Name),
		IsDefault: mgmt.NewOptBool(false),
		CreatedOn: mgmt.NewOptString(createdOn),
	}
	d.Handle.SetToNull()
	d.ExternalID.SetToNull()
	d.BackgroundColor.SetToNull()
	d.ButtonColor.SetToNull()
	d.ButtonTextColor.SetToNull()
	d.LinkColor.SetToNull()
	setNilString(&d.Handle, req.Handle)
	setNilString(&d.ExternalID, req.ExternalID)
	setColors(&d, req.BackgroundColor, req.ButtonColor, req.ButtonTextColor, req.LinkColor)
	theme := mgmt.GetOrganizationResponseThemeCodeLight
	if v, ok := req.ThemeCode.Get(); ok {
		theme = mgmt.GetOrganizationResponseThemeCode(v)
	}
	if err := setTheme(&d, theme); err != nil {
		return nil, err
	}

	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	code := h.f.newID("org")
	d.Code = mgmt.NewOptString(code)
	h.f.organizations[code] = &organization{details: d}
	return &mgmt.CreateOrganizationResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Organization successfully created"),
		Organization: mgmt.NewOptCreateOrganizationResponseOrganization(mgmt.CreateOrganizationResponseOrganization{
			Code: mgmt.NewOptString(code),
		}),
	}, nil
}

func (h handler) GetOrganization(_ context.Context, params mgmt.GetOrganizationParams) (mgmt.GetOrganizationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.findOrganization(params.Code)
	if err != nil {
		return nil, err
	}
	d := org.details
	return &d, nil
}

func (h handler) UpdateOrganization(_ context.Context, req mgmt.OptUpdateOrganizationReq, params mgmt.UpdateOrganizationParams) (mgmt.UpdateOrganizationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.findOrganization(params.OrgCode)
	if err != nil {
		return nil, err
	}

	// A request without a body changes nothing. Changes go to a copy so a
	// rejected request leaves the organization as it was.
	u := req.Or(mgmt.UpdateOrganizationReq{})
	d := org.details
	if v, ok := u.ThemeCode.Get(); ok {
		if err := setTheme(&d, mgmt.GetOrganizationResponseThemeCode(v)); err != nil {
			return nil, err
		}
	}
	if v, ok := u.Name.Get(); ok {
		d.Name = mgmt.NewOptString(v)
	}
	setNilString(&d.Handle, u.Handle)
	setNilString(&d.ExternalID, u.ExternalID)
	setColors(&d, u.BackgroundColor, u.ButtonColor, u.ButtonTextColor, u.LinkColor)
	org.details = d
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Organization successfully updated")}, nil
}

func (h handler) DeleteOrganization(_ context.Context, params mgmt.DeleteOrganizationParams) (mgmt.DeleteOrganizationRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.organizations[params.OrgCode]; !ok {
		// This endpoint declares its 404 in the spec, with an object rather
		// than a list under "errors".
		return &mgmt.NotFoundResponse{Errors: mgmt.NewOptNotFoundResponseErrors(mgmt.NotFoundResponseErrors{
			Code:    mgmt.NewOptString("ORGANIZATION_NOT_FOUND"),
			Message: mgmt.NewOptString("Organization not found"),
		})}, nil
	}
	delete(h.f.organizations, params.OrgCode)
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Organization successfully deleted")}, nil
}

// RemoveOrganization deletes an organization behind the provider's back.
func (f *Fake) RemoveOrganization(code string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.organizations, code)
}
```

- [ ] **Step 5: Implement the adapter methods**

`internal/kindeapi/organizations.go`:

```go
package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateOrganization creates an organization. The response carries only the
// new organization's code; call GetOrganization for the rest.
func (c *Client) CreateOrganization(ctx context.Context, req *mgmt.CreateOrganizationReq) (*mgmt.CreateOrganizationResponse, error) {
	return call[*mgmt.CreateOrganizationResponse](ctx, "CreateOrganization", func(ctx context.Context) (any, error) {
		return c.api.CreateOrganization(ctx, req)
	})
}

// GetOrganization returns the organization with the given code. Brand colors
// come back as objects; their Hex field holds the form the provider uses.
func (c *Client) GetOrganization(ctx context.Context, code string) (*mgmt.GetOrganizationResponse, error) {
	return call[*mgmt.GetOrganizationResponse](ctx, "GetOrganization", func(ctx context.Context) (any, error) {
		return c.api.GetOrganization(ctx, mgmt.GetOrganizationParams{Code: code})
	})
}

// UpdateOrganization changes the fields req sets. Kinde does not return the
// organization; call GetOrganization to read the result.
func (c *Client) UpdateOrganization(ctx context.Context, code string, req *mgmt.UpdateOrganizationReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "UpdateOrganization", func(ctx context.Context) (any, error) {
		return c.api.UpdateOrganization(ctx, mgmt.NewOptUpdateOrganizationReq(*req), mgmt.UpdateOrganizationParams{OrgCode: code})
	})
	return err
}

// DeleteOrganization deletes the organization with the given code.
func (c *Client) DeleteOrganization(ctx context.Context, code string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteOrganization", func(ctx context.Context) (any, error) {
		return c.api.DeleteOrganization(ctx, mgmt.DeleteOrganizationParams{OrgCode: code})
	})
	return err
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/kindeapi/ ./internal/kindefake/ -race -v`
Expected: PASS, including `TestOrganizationRoundTrip`, `TestCreateOrganizationDefaults`, `TestOrganizationNotFound`, `TestCreateOrganizationRejectsUnknownThemeCode`, and `TestRemoveOrganization`.

- [ ] **Step 7: Commit**

```bash
git add internal/kindeapi/organizations.go internal/kindeapi/organizations_test.go internal/kindefake/fake.go internal/kindefake/organizations.go
git commit -m "Add organizations to kindeapi and the fake" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 15: Migrate `kinde_organization`

**Files:**
- Modify: `go.mod`, `go.sum`
- Modify: `internal/provider/organization_resource.go` (whole file)
- Modify: `internal/provider/values.go`
- Test: `internal/provider/values_test.go`
- Test: `internal/provider/organization_resource_test.go` (whole file)
- Test: `internal/provider/not_found_test.go`

**Interfaces:**
- Consumes:
  - `CreateOrganization`, `GetOrganization`, `UpdateOrganization`, `DeleteOrganization`, and `Fake.RemoveOrganization` (Task 14).
  - `providerDataFrom`, `testAccFake`, and the `TestReadRemovesMissingObjects` table (Task 4).
  - `stringValue` and `optString` (Task 6).
- Produces:
  - In `internal/provider/values.go`: `func nilStringValue(v mgmt.OptNilString) types.String` (unset and null become null), tested by `TestNilStringValue`.
  - In `organization_resource.go`: `func themeCodes() []string` and `func flattenOrganization(org *mgmt.GetOrganizationResponse, m *OrganizationResourceModel)`.

Schema changes (spec section 3):
- `theme_code` holds Kinde's theme code. It was filled from `color_scheme`, which differs for `user_preference` (scheme `light dark`). A plan-time `stringvalidator.OneOf` restricts it to the SDK's enum (`light`, `dark`, `user_preference`).
- Color attributes stay hex strings: the request fields are hex strings, and `flattenOrganization` reads each color's `Hex`.
- `created_on` is stored exactly as Kinde returns it, without reformatting.

Behavior changes:
- Create now sends `external_id`, the colors, and `theme_code` as well as `name` and `handle`. The old library sent only the last two, so setting the others on create failed with an inconsistent-result error.
- `ImportState` sets `id` and `code`, and `Read` fills in the rest. Importing a code that does not exist fails, because `Read` removes it.

- [ ] **Step 1: Write the failing tests**

Replace `internal/provider/organization_resource_test.go`:

```go
package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccOrganizationResource(t *testing.T) {
	f := testAccFake(t)
	testName := acctest.RandomWithPrefix("tfacc")
	var code string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccOrganizationResourceConfig(testName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization.test", "name", testName),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "code"),
					resource.TestCheckResourceAttrPair("kinde_organization.test", "id", "kinde_organization.test", "code"),
					// created_on is stored exactly as Kinde returns it.
					resource.TestCheckResourceAttr("kinde_organization.test", "created_on", "2026-01-01T00:00:00Z"),
					// Kinde's default theme.
					resource.TestCheckResourceAttr("kinde_organization.test", "theme_code", "light"),
					resource.TestCheckNoResourceAttr("kinde_organization.test", "background_color"),
					resource.TestCheckResourceAttrWith("kinde_organization.test", "code", func(v string) error {
						code = v
						return nil
					}),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_organization.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccOrganizationResourceConfigUpdate(testName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization.test", "name", testName+"-updated"),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "code"),
					resource.TestCheckResourceAttr("kinde_organization.test", "external_id", testName+"-ext"),
					// Colors are read back in hex form.
					resource.TestCheckResourceAttr("kinde_organization.test", "background_color", "#ffffff"),
					resource.TestCheckResourceAttr("kinde_organization.test", "link_color", "#0056f1"),
					// Kinde's color scheme for this theme is "light dark";
					// theme_code must hold the theme code itself.
					resource.TestCheckResourceAttr("kinde_organization.test", "theme_code", "user_preference"),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveOrganization(code) },
				Config:             testAccOrganizationResourceConfigUpdate(testName),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: testAccOrganizationResourceConfigUpdate(testName),
				Check:  resource.TestCheckResourceAttrSet("kinde_organization.test", "id"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccOrganizationResource_InvalidThemeCode(t *testing.T) {
	testAccFake(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// "light dark" is a color scheme, not a theme code.
			{
				Config: `
resource "kinde_organization" "test" {
	name       = "tfacc-invalid-theme"
	theme_code = "light dark"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

func testAccOrganizationResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name = %[1]q
}
`, name)
}

func testAccOrganizationResourceConfigUpdate(name string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name             = "%[1]s-updated"
	external_id      = "%[1]s-ext"
	background_color = "#ffffff"
	link_color       = "#0056f1"
	theme_code       = "user_preference"
}
`, name)
}
```

Add this row to the `tests` slice in `TestReadRemovesMissingObjects` (`internal/provider/not_found_test.go`):

```go
		{name: "kinde_organization", resource: NewOrganizationResource, attrs: map[string]string{"id": "org_missing", "code": "org_missing"}},
```

Append to `internal/provider/values_test.go`. It uses `testing`, `types`, and `mgmt`, which Task 6's tests already import.

```go
func TestNilStringValue(t *testing.T) {
	var null mgmt.OptNilString
	null.SetToNull()
	tests := []struct {
		name string
		in   mgmt.OptNilString
		want types.String
	}{
		{"unset", mgmt.OptNilString{}, types.StringNull()},
		{"null", null, types.StringNull()},
		{"empty", mgmt.NewOptNilString(""), types.StringValue("")},
		{"value", mgmt.NewOptNilString("acme"), types.StringValue("acme")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nilStringValue(tt.in); !got.Equal(tt.want) {
				t.Fatalf("nilStringValue(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run 'TestReadRemovesMissingObjects|TestNilStringValue'`
Expected: FAIL to compile with `undefined: nilStringValue`.

The other new tests fail against the old resource too. `TestReadRemovesMissingObjects/kinde_organization` panics with a nil pointer dereference, because `Configure` reads `pd.legacy`, which the test leaves nil. `TestAccOrganizationResource` fails with `Provider produced inconsistent result after apply`, because `theme_code` reads back as the color scheme. `TestAccOrganizationResource_InvalidThemeCode` fails with `expected an error with pattern, no match`.

- [ ] **Step 3: Add the validators dependency**

Run:

```bash
go get github.com/hashicorp/terraform-plugin-framework-validators@v0.18.0
```

Expected: `go.mod` requires `terraform-plugin-framework-validators v0.18.0`, and `terraform-plugin-framework` moves from v1.14.0 to v1.14.1. Do not take v0.19.0: it raises `terraform-plugin-go` to v0.29.0. `terraform-plugin-testing` v1.11.0 builds `terraform-plugin-sdk/v2` v2.35.0, which does not compile against that version (`missing method GetResourceIdentitySchemas`). Upgrading the test framework is separate work.

- [ ] **Step 4: Add `nilStringValue`**

Append to `internal/provider/values.go`:

```go
// nilStringValue converts an optional, nullable SDK string to a Terraform
// string. Unset and null both become null.
func nilStringValue(v mgmt.OptNilString) types.String {
	if s, ok := v.Get(); ok {
		return types.StringValue(s)
	}
	return types.StringNull()
}
```

- [ ] **Step 5: Migrate the resource**

Replace `internal/provider/organization_resource.go`. The `time` and `github.com/nxt-fwd/kinde-go/api/organizations` imports go away.

```go
package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &OrganizationResource{}
	_ resource.ResourceWithImportState = &OrganizationResource{}
)

func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

type OrganizationResource struct {
	client *kindeapi.Client
}

type OrganizationResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Code            types.String `tfsdk:"code"`
	Name            types.String `tfsdk:"name"`
	ExternalID      types.String `tfsdk:"external_id"`
	BackgroundColor types.String `tfsdk:"background_color"`
	ButtonColor     types.String `tfsdk:"button_color"`
	ButtonTextColor types.String `tfsdk:"button_text_color"`
	LinkColor       types.String `tfsdk:"link_color"`
	ThemeCode       types.String `tfsdk:"theme_code"`
	Handle          types.String `tfsdk:"handle"`
	CreatedOn       types.String `tfsdk:"created_on"`
}

// themeCodes lists the theme codes Kinde accepts, from the SDK's enum.
func themeCodes() []string {
	values := mgmt.UpdateOrganizationReqThemeCodeLight.AllValues()
	codes := make([]string, len(values))
	for i, v := range values {
		codes[i] = string(v)
	}
	return codes
}

func (r *OrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Kinde organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the organization.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"code": schema.StringAttribute{
				Description: "The organization code.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the organization.",
				Required:    true,
			},
			"external_id": schema.StringAttribute{
				Description: "The external ID of the organization.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"background_color": schema.StringAttribute{
				Description: "The background color of the organization's theme, as a hex code such as `#ffffff`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"button_color": schema.StringAttribute{
				Description: "The button color of the organization's theme, as a hex code such as `#0056f1`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"button_text_color": schema.StringAttribute{
				Description: "The button text color of the organization's theme, as a hex code such as `#ffffff`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"link_color": schema.StringAttribute{
				Description: "The link color of the organization's theme, as a hex code such as `#0056f1`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"theme_code": schema.StringAttribute{
				Description: "Whether the organization's pages use light mode, dark mode, or the user's preference: `light`, `dark`, or `user_preference`. Kinde chooses a default when this is not set.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf(themeCodes()...),
				},
			},
			"handle": schema.StringAttribute{
				Description: "The organization handle.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_on": schema.StringAttribute{
				Description: "When the organization was created, in ISO 8601 format as Kinde returns it.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *OrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

// flattenOrganization copies Kinde's view of an organization into m. Brand
// colors are read in hex form, the form they are configured in.
func flattenOrganization(org *mgmt.GetOrganizationResponse, m *OrganizationResourceModel) {
	m.ID = stringValue(org.Code)
	m.Code = stringValue(org.Code)
	m.Name = stringValue(org.Name)
	m.Handle = nilStringValue(org.Handle)
	m.ExternalID = nilStringValue(org.ExternalID)
	m.CreatedOn = stringValue(org.CreatedOn)

	// Get returns a zero color, whose Hex is unset, for a missing or null
	// color, so stringValue turns it into null.
	background, _ := org.BackgroundColor.Get()
	m.BackgroundColor = stringValue(background.Hex)
	button, _ := org.ButtonColor.Get()
	m.ButtonColor = stringValue(button.Hex)
	buttonText, _ := org.ButtonTextColor.Get()
	m.ButtonTextColor = stringValue(buttonText.Hex)
	link, _ := org.LinkColor.Get()
	m.LinkColor = stringValue(link.Hex)

	if theme, ok := org.ThemeCode.Get(); ok {
		m.ThemeCode = types.StringValue(string(theme))
	} else {
		m.ThemeCode = types.StringNull()
	}
}

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateOrganization(ctx, &mgmt.CreateOrganizationReq{
		Name:            plan.Name.ValueString(),
		Handle:          optString(plan.Handle),
		ExternalID:      optString(plan.ExternalID),
		BackgroundColor: optString(plan.BackgroundColor),
		ButtonColor:     optString(plan.ButtonColor),
		ButtonTextColor: optString(plan.ButtonTextColor),
		LinkColor:       optString(plan.LinkColor),
		ThemeCode:       optString(plan.ThemeCode),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Organization",
			fmt.Sprintf("Could not create organization: %s", err),
		)
		return
	}
	code, ok := created.Organization.Value.Code.Get()
	if !ok || code == "" {
		resp.Diagnostics.AddError(
			"Error Creating Organization",
			"Kinde did not return the new organization's code.",
		)
		return
	}

	organization, err := r.client.GetOrganization(ctx, code)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", code, err),
		)
		return
	}
	flattenOrganization(organization, &plan)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization, err := r.client.GetOrganization(ctx, state.Code.ValueString())
	if kindeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", state.Code.ValueString(), err),
		)
		return
	}
	flattenOrganization(organization, &state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	code := plan.Code.ValueString()
	var themeCode mgmt.OptUpdateOrganizationReqThemeCode
	if !plan.ThemeCode.IsNull() && !plan.ThemeCode.IsUnknown() {
		themeCode = mgmt.NewOptUpdateOrganizationReqThemeCode(mgmt.UpdateOrganizationReqThemeCode(plan.ThemeCode.ValueString()))
	}
	err := r.client.UpdateOrganization(ctx, code, &mgmt.UpdateOrganizationReq{
		Name:            mgmt.NewOptString(plan.Name.ValueString()),
		Handle:          optString(plan.Handle),
		ExternalID:      optString(plan.ExternalID),
		BackgroundColor: optString(plan.BackgroundColor),
		ButtonColor:     optString(plan.ButtonColor),
		ButtonTextColor: optString(plan.ButtonTextColor),
		LinkColor:       optString(plan.LinkColor),
		ThemeCode:       themeCode,
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Organization",
			fmt.Sprintf("Could not update organization code %s: %s", code, err),
		)
		return
	}

	organization, err := r.client.GetOrganization(ctx, code)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", code, err),
		)
		return
	}
	flattenOrganization(organization, &plan)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteOrganization(ctx, state.Code.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Organization",
			fmt.Sprintf("Could not delete organization code %s: %s", state.Code.ValueString(), err),
		)
	}
}

// ImportState takes an organization code. Read fills in the rest, and a code
// that does not exist fails the import.
func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("code"), req.ID)...)
}
```

Run `go mod tidy`. It makes the validators module a direct requirement.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/... && TF_ACC=1 go test ./internal/provider/ -run 'TestAccOrganizationResource|TestReadRemovesMissingObjects' -v`
Expected: PASS, including `TestReadRemovesMissingObjects/kinde_organization` and `TestAccOrganizationResource_InvalidThemeCode`.

- [ ] **Step 7: Commit**

```bash
git add go.mod go.sum internal/provider/organization_resource.go internal/provider/organization_resource_test.go internal/provider/values.go internal/provider/values_test.go internal/provider/not_found_test.go
git commit -m "Move kinde_organization to kindeapi" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 16: Users adapter and fake

**Files:**
- Modify: `go.mod`, `go.sum`
- Create: `internal/kindeapi/phone.go`
- Create: `internal/kindeapi/users.go`
- Create: `internal/kindefake/users.go`
- Modify: `internal/kindefake/fake.go` (`Fake` domain state, `New`, `registerRawRoutes`)
- Test: `internal/kindeapi/phone_test.go`
- Test: `internal/kindeapi/users_test.go`
- Test: `internal/kindefake/users_test.go`

**Interfaces:**
- Consumes:
  - `call[T]` (Task 1), `(*Client).getJSON` (Task 2), and `newFakeClient` (Task 3).
  - `handler`, `apiError`, and `writeAPIError` (Task 3); the `fetchToken`, `get`, and `errorCode` helpers in `internal/kindefake/fake_test.go` (Task 3).
  - `(*Fake).newID` and `notFound` (Task 5).
  - `allCursorPages` and `(*Fake).registerRawRoutes` with its local `handle` wrapper, which checks the access token (Task 10).
  - `createdOn` (Task 14).
- Produces:
  - In `kindeapi`:
    - `func (c *Client) CreateUser(ctx context.Context, req mgmt.CreateUserReq) (*mgmt.CreateUserResponse, error)`
    - `func (c *Client) GetUserData(ctx context.Context, id string) (*mgmt.User, error)`
    - `func (c *Client) UpdateUser(ctx context.Context, id string, req mgmt.UpdateUserReq) (*mgmt.UpdateUserResponse, error)`
    - `func (c *Client) DeleteUser(ctx context.Context, id string) error`
    - `func (c *Client) CreateUserIdentity(ctx context.Context, userID string, req mgmt.CreateUserIdentityReq) (*mgmt.CreateIdentityResponse, error)`, which splits phone numbers.
    - `type UserIdentity struct { ID, Type, Name string }` and `func (c *Client) GetUserIdentities(ctx context.Context, userID string) ([]UserIdentity, error)`, which returns every page.
    - `func parsePhone(number string) (national, countryID string, err error)`.
  - In `kindefake`:
    - `func (f *Fake) userExists(id string) bool` (callers hold `f.mu`; Task 18 uses it) and `func userNotFound(id string) error` (404 `USER_NOT_FOUND`).
    - Test hooks `func (f *Fake) RemoveUser(id string)`, `func (f *Fake) AddUserIdentity(userID, identityType, name string)`, and `func (f *Fake) UserOrganizationCode(id string) string`.
    - User IDs look like `kp_0001`; identity IDs look like `identity_0002`.

`GetUserIdentities` is the one user endpoint that does not go through the SDK. Kinde's spec documents `"is_confirmed": null` for identities that record no confirmation, such as usernames. The SDK models `is_confirmed` as a non-nullable `OptBool`, so one username identity fails the whole page with `decode field "is_confirmed": unexpected byte 110 'n'`. The adapter decodes the endpoint with `getJSON` instead. The fake serves it as a raw route that sends the null, so the fallback stays covered.

The old library sent the user's organization under `org_code`, which Kinde ignores; `mgmt.CreateUserReq.OrganizationCode` sends `organization_code`. The fake records the code without checking that the organization exists or adding the user to it, so this task does not depend on the organization fake.

- [ ] **Step 1: Add the phone number dependency**

Run: `go get github.com/nyaruka/phonenumbers@latest`
Expected: `go.mod` requires `github.com/nyaruka/phonenumbers` (the old library already pulled it in as an indirect requirement). Step 6 runs `go mod tidy`, which makes it a direct requirement.

- [ ] **Step 2: Write the failing phone parsing test**

The old library split phone numbers in `internal/phone.ParseNumber` before adding a phone identity. This step ports its table and adds edge cases.

`internal/kindeapi/phone_test.go`:

```go
package kindeapi

import "testing"

func TestParsePhone(t *testing.T) {
	tests := []struct {
		name         string
		number       string
		wantNational string
		wantCountry  string
		wantErr      bool
	}{
		{name: "armenia", number: "+37455251234", wantNational: "55251234", wantCountry: "am"},
		{name: "australia", number: "+61412345678", wantNational: "412345678", wantCountry: "au"},
		{name: "united states", number: "+12025550123", wantNational: "2025550123", wantCountry: "us"},
		{name: "united kingdom", number: "+442079460123", wantNational: "2079460123", wantCountry: "gb"},
		{name: "too short", number: "+1234", wantErr: true},
		{name: "no country code", number: "0412345678", wantErr: true},
		{name: "not a number", number: "phone", wantErr: true},
		{name: "empty", number: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			national, country, err := parsePhone(tt.number)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parsePhone(%q) = %q, %q; want an error", tt.number, national, country)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if national != tt.wantNational || country != tt.wantCountry {
				t.Fatalf("parsePhone(%q) = %q, %q; want %q, %q", tt.number, national, country, tt.wantNational, tt.wantCountry)
			}
		})
	}
}
```

Run: `go test ./internal/kindeapi/ -run TestParsePhone`
Expected: FAIL to compile with `undefined: parsePhone`.

- [ ] **Step 3: Port the phone parsing**

`internal/kindeapi/phone.go`:

```go
package kindeapi

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/nyaruka/phonenumbers"
)

// parsePhone splits a phone number in international format, such as
// "+61412345678", into the national number ("412345678") and the lower-case
// country ID ("au") that Kinde's create-identity endpoint expects. It is
// ported from github.com/nxt-fwd/kinde-go's internal/phone package.
func parsePhone(number string) (national, countryID string, err error) {
	num, err := phonenumbers.Parse(number, "")
	if err != nil {
		return "", "", fmt.Errorf("invalid phone number %q: %w", number, err)
	}
	if !phonenumbers.IsValidNumber(num) {
		return "", "", fmt.Errorf("invalid phone number %q", number)
	}
	national = strconv.FormatUint(num.GetNationalNumber(), 10)
	countryID = strings.ToLower(phonenumbers.GetRegionCodeForNumber(num))
	return national, countryID, nil
}
```

Run: `go test ./internal/kindeapi/ -run TestParsePhone -v`
Expected: PASS.

- [ ] **Step 4: Write the failing adapter and fake tests**

`internal/kindeapi/users_test.go`:

```go
package kindeapi_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

func emailIdentity(email string) mgmt.CreateUserReqIdentitiesItem {
	return mgmt.CreateUserReqIdentitiesItem{
		Type:    mgmt.NewOptCreateUserReqIdentitiesItemType(mgmt.CreateUserReqIdentitiesItemTypeEmail),
		Details: mgmt.NewOptCreateUserReqIdentitiesItemDetails(mgmt.CreateUserReqIdentitiesItemDetails{Email: mgmt.NewOptString(email)}),
	}
}

func createUser(t *testing.T, c *kindeapi.Client, req mgmt.CreateUserReq) string {
	t.Helper()
	created, err := c.CreateUser(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := created.ID.Get()
	if !ok {
		t.Fatal("CreateUser returned no ID")
	}
	return id
}

// identityNames returns "type:name" for each identity, in order.
func identityNames(identities []kindeapi.UserIdentity) []string {
	names := make([]string, len(identities))
	for i, identity := range identities {
		names[i] = identity.Type + ":" + identity.Name
	}
	return names
}

func TestUserRoundTrip(t *testing.T) {
	f, c := newFakeClient(t)
	ctx := t.Context()

	id := createUser(t, c, mgmt.CreateUserReq{
		Profile: mgmt.NewOptCreateUserReqProfile(mgmt.CreateUserReqProfile{
			GivenName:  mgmt.NewOptString("Ada"),
			FamilyName: mgmt.NewOptString("Lovelace"),
		}),
		OrganizationCode: mgmt.NewOptString("org_engines"),
		Identities: []mgmt.CreateUserReqIdentitiesItem{
			emailIdentity("ada@example.com"),
			{
				Type:    mgmt.NewOptCreateUserReqIdentitiesItemType(mgmt.CreateUserReqIdentitiesItemTypeUsername),
				Details: mgmt.NewOptCreateUserReqIdentitiesItemDetails(mgmt.CreateUserReqIdentitiesItemDetails{Username: mgmt.NewOptString("ada")}),
			},
		},
	})
	if got := f.UserOrganizationCode(id); got != "org_engines" {
		t.Fatalf("organization code = %q, want org_engines", got)
	}

	user, err := c.GetUserData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if user.FirstName.Value != "Ada" || user.LastName.Value != "Lovelace" || user.IsSuspended.Value {
		t.Fatalf("got %+v, want Ada Lovelace, not suspended", user)
	}
	if got, _ := user.CreatedOn.Get(); got != "2026-01-01T00:00:00Z" {
		t.Fatalf("created_on = %q, want Kinde's string unchanged", got)
	}

	// The username identity arrives with "is_confirmed": null, which the
	// SDK's own decoder rejects.
	identities, err := c.GetUserIdentities(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := identityNames(identities), []string{"email:ada@example.com", "username:ada"}; !slices.Equal(got, want) {
		t.Fatalf("identities = %q, want %q", got, want)
	}

	if _, err := c.UpdateUser(ctx, id, mgmt.UpdateUserReq{
		GivenName:   mgmt.NewOptString("Augusta"),
		IsSuspended: mgmt.NewOptBool(true),
	}); err != nil {
		t.Fatal(err)
	}
	user, err = c.GetUserData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if user.FirstName.Value != "Augusta" || user.LastName.Value != "Lovelace" || !user.IsSuspended.Value {
		t.Fatalf("after update got %+v, want Augusta Lovelace, suspended", user)
	}

	if err := c.DeleteUser(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetUserData(ctx, id); !kindeapi.IsNotFound(err) {
		t.Fatalf("GetUserData after delete: got %v, want not found", err)
	}
}

func TestCreateUserIdentity(t *testing.T) {
	_, c := newFakeClient(t)
	id := createUser(t, c, mgmt.CreateUserReq{Identities: []mgmt.CreateUserReqIdentitiesItem{emailIdentity("grace@example.com")}})

	for _, req := range []mgmt.CreateUserIdentityReq{
		{Type: mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypeEmail), Value: mgmt.NewOptString("grace@navy.example")},
		// The fake accepts a phone identity only as a national number plus
		// country, so this passes only if the adapter splits the number.
		{Type: mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypePhone), Value: mgmt.NewOptString("+61412345678")},
	} {
		created, err := c.CreateUserIdentity(t.Context(), id, req)
		if err != nil {
			t.Fatal(err)
		}
		if !created.Identity.Value.ID.IsSet() {
			t.Fatal("CreateUserIdentity returned no identity ID")
		}
	}

	identities, err := c.GetUserIdentities(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"email:grace@example.com", "email:grace@navy.example", "phone:+61412345678"}
	if got := identityNames(identities); !slices.Equal(got, want) {
		t.Fatalf("identities = %q, want %q", got, want)
	}
}

func TestCreateUserIdentityRejectsInvalidPhone(t *testing.T) {
	_, c := newFakeClient(t)
	id := createUser(t, c, mgmt.CreateUserReq{Identities: []mgmt.CreateUserReqIdentitiesItem{emailIdentity("alan@example.com")}})

	_, err := c.CreateUserIdentity(t.Context(), id, mgmt.CreateUserIdentityReq{
		Type:  mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypePhone),
		Value: mgmt.NewOptString("+1234"),
	})
	var apiErr *kindeapi.APIError
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("expected a local phone parsing error, got %v", err)
	}
}

func TestGetUserIdentitiesReturnsEveryPage(t *testing.T) {
	_, c := newFakeClient(t)
	id := createUser(t, c, mgmt.CreateUserReq{Identities: []mgmt.CreateUserReqIdentitiesItem{emailIdentity("user0@example.com")}})

	// The fake pages identities 10 at a time, so 25 identities take 3 pages.
	want := []string{"email:user0@example.com"}
	for i := 1; i < 25; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		if _, err := c.CreateUserIdentity(t.Context(), id, mgmt.CreateUserIdentityReq{
			Type:  mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypeEmail),
			Value: mgmt.NewOptString(email),
		}); err != nil {
			t.Fatal(err)
		}
		want = append(want, "email:"+email)
	}

	identities, err := c.GetUserIdentities(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := identityNames(identities); !slices.Equal(got, want) {
		t.Fatalf("identities = %q, want %q", got, want)
	}
}

func TestUserOperationsOnMissingUser(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	const id = "kp_missing"

	_, getErr := c.GetUserData(ctx, id)
	_, updateErr := c.UpdateUser(ctx, id, mgmt.UpdateUserReq{GivenName: mgmt.NewOptString("Nobody")})
	deleteErr := c.DeleteUser(ctx, id)
	_, identitiesErr := c.GetUserIdentities(ctx, id)
	_, addErr := c.CreateUserIdentity(ctx, id, mgmt.CreateUserIdentityReq{
		Type:  mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypeEmail),
		Value: mgmt.NewOptString("nobody@example.com"),
	})
	for name, err := range map[string]error{
		"GetUserData":        getErr,
		"UpdateUser":         updateErr,
		"DeleteUser":         deleteErr,
		"GetUserIdentities":  identitiesErr,
		"CreateUserIdentity": addErr,
	} {
		if !kindeapi.IsNotFound(err) {
			t.Errorf("%s: got %v, want not found", name, err)
		}
	}
}
```

`internal/kindefake/users_test.go`:

```go
package kindefake_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

// createUser posts a CreateUser body and returns the new user's ID.
func createUser(t *testing.T, f *kindefake.Fake, token, body string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.URL+"/api/v1/user", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.ID == "" {
		t.Fatalf("CreateUser: status %d, decode error %v", resp.StatusCode, err)
	}
	return created.ID
}

// TestUserIdentitiesSendNullIsConfirmedForUsernames pins the response shape
// that kindeapi.GetUserIdentities decodes itself: Kinde's spec documents a
// null is_confirmed for username identities, which the SDK cannot decode.
func TestUserIdentitiesSendNullIsConfirmedForUsernames(t *testing.T) {
	f := kindefake.New(t)
	token := fetchToken(t, f)
	id := createUser(t, f, token, `{"identities":[
		{"type":"email","details":{"email":"ada@example.com"}},
		{"type":"username","details":{"username":"ada"}}
	]}`)

	resp := get(t, f, "/api/v1/users/"+id+"/identities", token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var page struct {
		Identities []struct {
			Type        string          `json:"type"`
			IsConfirmed json.RawMessage `json:"is_confirmed"`
		} `json:"identities"`
		HasMore bool `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, identity := range page.Identities {
		got[identity.Type] = string(identity.IsConfirmed)
	}
	if got["email"] != "true" || got["username"] != "null" || page.HasMore {
		t.Fatalf("is_confirmed by type = %v, has_more = %v; want email true, username null, no more pages", got, page.HasMore)
	}
}

func TestUserIdentitiesRequireToken(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/users/kp_0001/identities", "")
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, resp) != "UNAUTHORIZED" {
		t.Fatalf("status = %d, want 401 UNAUTHORIZED", resp.StatusCode)
	}
}

func TestUserIdentitiesOfMissingUser(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/users/kp_missing/identities", fetchToken(t, f))
	if resp.StatusCode != http.StatusNotFound || errorCode(t, resp) != "USER_NOT_FOUND" {
		t.Fatalf("status = %d, want 404 USER_NOT_FOUND", resp.StatusCode)
	}
}
```

Run: `go test ./internal/kindeapi/ ./internal/kindefake/ -run 'User'`
Expected: `internal/kindeapi` fails to compile with `c.CreateUser undefined (type *kindeapi.Client has no field or method CreateUser)`. `internal/kindefake` fails `TestUserIdentitiesSendNullIsConfirmedForUsernames` with `CreateUser: status 501` and `TestUserIdentitiesOfMissingUser` with `status = 501, want 404 USER_NOT_FOUND`.

- [ ] **Step 5: Implement the fake users**

`internal/kindefake/users.go`:

```go
package kindefake

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nyaruka/phonenumbers"
)

// identitiesPageSize is how many identities one page of
// GET /api/v1/users/{user_id}/identities holds. Kinde's spec does not say.
const identitiesPageSize = 10

// user is a Kinde user.
type user struct {
	id          string
	firstName   string
	lastName    string
	isSuspended bool
	// organizationCode is recorded as CreateUser sent it. The fake does not
	// check that the organization exists or add the user to it.
	organizationCode string
	identities       []identity
}

// identity is one of a user's identities. name is its value; phone numbers
// are kept in international format, as Kinde reports them.
type identity struct {
	id   string
	typ  string
	name string
}

func userNotFound(id string) error {
	return notFound("USER_NOT_FOUND", "kindefake: no user "+id)
}

func invalidUserRequest(message string) error {
	return &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: " + message}
}

// userExists reports whether a user exists. Callers must hold f.mu.
func (f *Fake) userExists(id string) bool {
	_, ok := f.users[id]
	return ok
}

// RemoveUser deletes a user behind the provider's back.
func (f *Fake) RemoveUser(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, id)
}

// AddUserIdentity gives a user an identity behind the provider's back, the
// way Kinde adds an "oauth2:google" identity when the user signs in with
// Google. It panics if the user does not exist.
func (f *Fake) AddUserIdentity(userID, identityType, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[userID]
	if !ok {
		panic("kindefake: AddUserIdentity: no user " + userID)
	}
	u.identities = append(u.identities, identity{id: f.newID("identity"), typ: identityType, name: name})
}

// UserOrganizationCode returns the organization_code a user was created
// with, or "" if there was none or the user does not exist.
func (f *Fake) UserOrganizationCode(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		return u.organizationCode
	}
	return ""
}

func (h handler) CreateUser(_ context.Context, req mgmt.OptCreateUserReq) (mgmt.CreateUserRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	r := req.Value
	u := &user{
		firstName:        r.Profile.Value.GivenName.Value,
		lastName:         r.Profile.Value.FamilyName.Value,
		organizationCode: r.OrganizationCode.Value,
	}
	resp := &mgmt.CreateUserResponse{Created: mgmt.NewOptBool(true)}
	for _, item := range r.Identities {
		details := item.Details.Value
		var name string
		switch item.Type.Value {
		case mgmt.CreateUserReqIdentitiesItemTypeEmail:
			name = details.Email.Value
		case mgmt.CreateUserReqIdentitiesItemTypePhone:
			name = details.Phone.Value
		case mgmt.CreateUserReqIdentitiesItemTypeUsername:
			name = details.Username.Value
		}
		if name == "" {
			return nil, invalidUserRequest("identity of type " + string(item.Type.Value) + " has no value in details")
		}
		u.identities = append(u.identities, identity{id: h.f.newID("identity"), typ: string(item.Type.Value), name: name})
		resp.Identities = append(resp.Identities, mgmt.UserIdentity{
			Type:   mgmt.NewOptString(string(item.Type.Value)),
			Result: mgmt.NewOptUserIdentityResult(mgmt.UserIdentityResult{Created: mgmt.NewOptBool(true)}),
		})
	}
	u.id = h.f.newID("kp")
	h.f.users[u.id] = u
	resp.ID = mgmt.NewOptString(u.id)
	return resp, nil
}

func (h handler) GetUserData(_ context.Context, params mgmt.GetUserDataParams) (mgmt.GetUserDataRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	u, ok := h.f.users[params.ID]
	if !ok {
		return nil, userNotFound(params.ID)
	}
	return &mgmt.User{
		ID:          mgmt.NewOptString(u.id),
		FirstName:   mgmt.NewOptString(u.firstName),
		LastName:    mgmt.NewOptString(u.lastName),
		IsSuspended: mgmt.NewOptBool(u.isSuspended),
		CreatedOn:   mgmt.NewOptNilString(createdOn),
	}, nil
}

func (h handler) UpdateUser(_ context.Context, req *mgmt.UpdateUserReq, params mgmt.UpdateUserParams) (mgmt.UpdateUserRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	u, ok := h.f.users[params.ID]
	if !ok {
		return nil, userNotFound(params.ID)
	}
	if v, ok := req.GivenName.Get(); ok {
		u.firstName = v
	}
	if v, ok := req.FamilyName.Get(); ok {
		u.lastName = v
	}
	if v, ok := req.IsSuspended.Get(); ok {
		u.isSuspended = v
	}
	return &mgmt.UpdateUserResponse{
		ID:                       mgmt.NewOptString(u.id),
		GivenName:                mgmt.NewOptString(u.firstName),
		FamilyName:               mgmt.NewOptString(u.lastName),
		IsSuspended:              mgmt.NewOptBool(u.isSuspended),
		IsPasswordResetRequested: mgmt.NewOptBool(false),
	}, nil
}

func (h handler) DeleteUser(_ context.Context, params mgmt.DeleteUserParams) (mgmt.DeleteUserRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	if !h.f.userExists(params.ID) {
		return nil, userNotFound(params.ID)
	}
	delete(h.f.users, params.ID)
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Success")}, nil
}

func (h handler) CreateUserIdentity(_ context.Context, req mgmt.OptCreateUserIdentityReq, params mgmt.CreateUserIdentityParams) (mgmt.CreateUserIdentityRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	u, ok := h.f.users[params.UserID]
	if !ok {
		return nil, userNotFound(params.UserID)
	}
	r := req.Value
	typ, name := string(r.Type.Value), r.Value.Value
	switch r.Type.Value {
	case mgmt.CreateUserIdentityReqTypeEmail, mgmt.CreateUserIdentityReqTypeUsername:
	case mgmt.CreateUserIdentityReqTypePhone:
		// Kinde takes the national number and its country, and reports the
		// identity in international format.
		country, ok := r.PhoneCountryID.Get()
		if !ok || strings.HasPrefix(name, "+") {
			return nil, invalidUserRequest("a phone identity needs a national number and phone_country_id")
		}
		num, err := phonenumbers.Parse(name, strings.ToUpper(country))
		if err != nil || !phonenumbers.IsValidNumber(num) {
			return nil, invalidUserRequest("invalid phone number " + name)
		}
		name = phonenumbers.Format(num, phonenumbers.E164)
	default:
		return nil, invalidUserRequest("identity type " + typ + " is not supported")
	}
	if name == "" {
		return nil, invalidUserRequest("identity value is required")
	}
	if slices.ContainsFunc(u.identities, func(i identity) bool { return i.typ == typ && i.name == name }) {
		return nil, invalidUserRequest("the user already has identity " + typ + ":" + name)
	}

	id := h.f.newID("identity")
	u.identities = append(u.identities, identity{id: id, typ: typ, name: name})
	return &mgmt.CreateIdentityResponse{
		Code:     mgmt.NewOptString("IDENTITY_CREATED"),
		Message:  mgmt.NewOptString("Identity successfully created"),
		Identity: mgmt.NewOptCreateIdentityResponseIdentity(mgmt.CreateIdentityResponseIdentity{ID: mgmt.NewOptString(id)}),
	}, nil
}

// identityJSON is an identity as GET /api/v1/users/{user_id}/identities
// returns it.
type identityJSON struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	IsConfirmed  *bool   `json:"is_confirmed"`
	CreatedOn    string  `json:"created_on"`
	LastLoginOn  *string `json:"last_login_on"`
	TotalLogins  int     `json:"total_logins"`
	Name         string  `json:"name"`
	Email        *string `json:"email"`
	ConnectionID *string `json:"connection_id"`
	IsPrimary    bool    `json:"is_primary"`
}

// serveUserIdentities answers GET /api/v1/users/{user_id}/identities. It
// sits outside the generated server because the SDK cannot encode the null
// is_confirmed that Kinde's spec documents for username identities, and
// kindeapi decodes this endpoint itself for the same reason.
func (f *Fake) serveUserIdentities(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	userID := r.PathValue("user_id")
	u, ok := f.users[userID]
	if !ok {
		writeAPIError(w, &apiError{status: http.StatusNotFound, code: "USER_NOT_FOUND", message: "kindefake: no user " + userID})
		return
	}

	start := 0
	if after := r.URL.Query().Get("starting_after"); after != "" {
		i := slices.IndexFunc(u.identities, func(i identity) bool { return i.id == after })
		if i < 0 {
			writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: unknown starting_after " + after})
			return
		}
		start = i + 1
	}
	end := min(start+identitiesPageSize, len(u.identities))

	page := make([]identityJSON, 0, end-start)
	for i, ident := range u.identities[start:end] {
		out := identityJSON{
			ID:        ident.id,
			Type:      ident.typ,
			CreatedOn: createdOn,
			Name:      ident.name,
			IsPrimary: start+i == 0,
		}
		// The spec says is_confirmed is null for identity types that record no
		// confirmation, such as username, and email is null except for email
		// identities.
		if ident.typ != "username" {
			confirmed := true
			out.IsConfirmed = &confirmed
		}
		if ident.typ == "email" {
			email := ident.name
			out.Email = &email
		}
		page = append(page, out)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       "OK",
		"message":    "Success",
		"identities": page,
		"has_more":   end < len(u.identities),
	})
}
```

In `internal/kindefake/fake.go`, add the state field under `// Domain state.` in `Fake`:

```go
	users map[string]*user
```

Initialize it under `// Initialize domain state.` in `New`:

```go
	f.users = map[string]*user{}
```

Add the identities route at the end of `registerRawRoutes`. The generated server's `GetUserIdentities` stays unimplemented; this more specific pattern takes `GET` requests first, and `POST` to the same path still reaches the generated `CreateUserIdentity`.

```go
	handle("GET /api/v1/users/{user_id}/identities", f.serveUserIdentities)
```

- [ ] **Step 6: Implement the adapter**

`internal/kindeapi/users.go`:

```go
package kindeapi

import (
	"context"
	"fmt"
	"net/url"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateUser creates a user. The response carries the new user's ID; call
// GetUserData for the rest.
func (c *Client) CreateUser(ctx context.Context, req mgmt.CreateUserReq) (*mgmt.CreateUserResponse, error) {
	return call[*mgmt.CreateUserResponse](ctx, "CreateUser", func(ctx context.Context) (any, error) {
		return c.api.CreateUser(ctx, mgmt.NewOptCreateUserReq(req))
	})
}

// GetUserData gets a user.
func (c *Client) GetUserData(ctx context.Context, id string) (*mgmt.User, error) {
	return call[*mgmt.User](ctx, "GetUserData", func(ctx context.Context) (any, error) {
		return c.api.GetUserData(ctx, mgmt.GetUserDataParams{ID: id})
	})
}

// UpdateUser updates a user. Unset fields in req keep their values.
func (c *Client) UpdateUser(ctx context.Context, id string, req mgmt.UpdateUserReq) (*mgmt.UpdateUserResponse, error) {
	return call[*mgmt.UpdateUserResponse](ctx, "UpdateUser", func(ctx context.Context) (any, error) {
		return c.api.UpdateUser(ctx, &req, mgmt.UpdateUserParams{ID: id})
	})
}

// DeleteUser deletes a user.
func (c *Client) DeleteUser(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteUser", func(ctx context.Context) (any, error) {
		return c.api.DeleteUser(ctx, mgmt.DeleteUserParams{ID: id})
	})
	return err
}

// CreateUserIdentity adds an identity to a user. For a phone identity,
// req.Value is a number in international format, such as "+61412345678";
// Kinde wants the national number and its country instead, so this method
// splits it and sets PhoneCountryID.
func (c *Client) CreateUserIdentity(ctx context.Context, userID string, req mgmt.CreateUserIdentityReq) (*mgmt.CreateIdentityResponse, error) {
	if req.Type.Value == mgmt.CreateUserIdentityReqTypePhone {
		national, countryID, err := parsePhone(req.Value.Value)
		if err != nil {
			return nil, fmt.Errorf("kinde CreateUserIdentity: %w", err)
		}
		req.Value = mgmt.NewOptString(national)
		req.PhoneCountryID = mgmt.NewOptString(countryID)
	}
	return call[*mgmt.CreateIdentityResponse](ctx, "CreateUserIdentity", func(ctx context.Context) (any, error) {
		return c.api.CreateUserIdentity(ctx, mgmt.NewOptCreateUserIdentityReq(req), mgmt.CreateUserIdentityParams{UserID: userID})
	})
}

// UserIdentity is one of a user's identities.
type UserIdentity struct {
	ID string `json:"id"`
	// Type is the identity type, such as "email", "phone", "username", or
	// "oauth2:google".
	Type string `json:"type"`
	// Name is the identity's value. Kinde reports phone numbers in
	// international format.
	Name string `json:"name"`
}

// GetUserIdentities returns every identity of a user.
//
// It decodes the response itself instead of calling the SDK. Kinde's spec
// documents "is_confirmed": null for identities that record no confirmation,
// such as usernames, and the SDK models is_confirmed as a non-nullable
// OptBool, so a single username identity would fail the whole page. Switch to
// the SDK's GetUserIdentities once mgmt.Identity.IsConfirmed is nullable.
func (c *Client) GetUserIdentities(ctx context.Context, userID string) ([]UserIdentity, error) {
	path := "/api/v1/users/" + url.PathEscape(userID) + "/identities"
	return allCursorPages(ctx, func(i UserIdentity) string { return i.ID },
		func(ctx context.Context, startingAfter string) ([]UserIdentity, bool, error) {
			query := url.Values{}
			if startingAfter != "" {
				query.Set("starting_after", startingAfter)
			}
			var page struct {
				Identities []UserIdentity `json:"identities"`
				HasMore    bool           `json:"has_more"`
			}
			if err := c.getJSON(ctx, "GetUserIdentities", path, query, &page); err != nil {
				return nil, false, err
			}
			return page.Identities, page.HasMore, nil
		})
}
```

Run `go mod tidy`.

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/kindeapi/ ./internal/kindefake/ -race -v`
Expected: PASS, including `TestUserRoundTrip`, `TestGetUserIdentitiesReturnsEveryPage`, `TestCreateUserIdentity`, and `TestUserIdentitiesSendNullIsConfirmedForUsernames`.

- [ ] **Step 8: Commit**

```bash
git add go.mod go.sum internal/kindeapi/phone.go internal/kindeapi/phone_test.go internal/kindeapi/users.go internal/kindeapi/users_test.go internal/kindefake/users.go internal/kindefake/users_test.go internal/kindefake/fake.go
git commit -m "Add kindeapi user endpoints and fake users" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 17: Migrate `kinde_user`

**Files:**
- Modify: `internal/provider/user_resource.go` (whole file)
- Modify: `internal/provider/user_resource_test.go` (whole file)
- Modify: `internal/provider/not_found_test.go`

**Interfaces:**
- Consumes:
  - From Task 16: `CreateUser`, `GetUserData`, `UpdateUser`, `DeleteUser`, `CreateUserIdentity`, `GetUserIdentities`, `kindeapi.UserIdentity`, and the `RemoveUser`, `AddUserIdentity`, and `UserOrganizationCode` hooks.
  - `kindeapi.IsNotFound` (Task 1), `providerDataFrom` (Task 4), `optString` (Task 6).
  - `testAccFake`, `TestReadRemovesMissingObjects`, and `requireNoErrors` (Task 4).
- Produces: no helpers for other tasks. `user_resource.go` gains private helpers: `userIdentityModel`, `userIdentityObjectType`, `(*UserResourceModel).setFromKinde`, `userIdentitiesValue`, `isOAuth2Identity`, `hasEmailIdentity`, `identityTypes`, and `newCreateUserIdentity`.

Section 3 schema changes:
- `updated_on` is removed; the SDK's `mgmt.User` has no such field.
- `created_on` is stored exactly as Kinde returns it. It used to be reformatted with `time.Time.String()`.
- `organization_code` now reaches Kinde on create. Its description says that later changes have no effect.

Other changes:
- Create, Read, Update, and ImportState each had a copy of the identity-to-state conversion. Create, Read, and Update now share `setFromKinde` and `userIdentitiesValue`. The conversion no longer sorts identities, because a set has no order.
- Update no longer reads the user before updating it. It also no longer lists identities just to find the OAuth2 ones: those are already skipped, and the plan-versus-state comparison never re-adds them. The email-identity check now runs before any API call. Before, it ran after the names were already updated.
- ImportState sets only `id`, `first_name`, and `last_name`. Read runs next and fills in `created_on` and the identities, filtering OAuth2 identities as it always did.
- `TestUserResource_FiltersOAuthIdentities` and `TestUserResource_SortsIdentitiesConsistently` exercised copies of the logic inside the tests. `TestUserIdentitiesValue` replaces both and tests the real helper.
- Test fixtures use fixed emails and phone numbers. Each test gets its own fake, so the random suffixes only made failures harder to reproduce.
- `TestAccUserResource_OAuth2Identity` now adds a Google identity out of band. It checks that the identity causes no drift and survives an update. Against the live tenant it never had one.
- `TestAccUserResource_NameHandling` adds an import step and the out-of-band delete sequence.
- `TestAccUserResource_OrganizationCode` is new.

- [ ] **Step 1: Write the failing tests**

Replace `internal/provider/user_resource_test.go` with:

```go
package provider

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

func TestUserIdentitiesValue(t *testing.T) {
	identities := []kindeapi.UserIdentity{
		{ID: "identity_0001", Type: "email", Name: "test@example.com"},
		{ID: "identity_0002", Type: "username", Name: "testuser"},
		{ID: "identity_0003", Type: "oauth2:google", Name: "test@gmail.com"},
		{ID: "identity_0004", Type: "oauth2:github", Name: "githubuser"},
		{ID: "identity_0005", Type: "phone", Name: "+12025550123"},
	}
	tests := []struct {
		name       string
		knownTypes map[string]string
		want       []userIdentityModel
	}{
		{
			name: "drops OAuth2 identities",
			want: []userIdentityModel{
				{Type: "email", Value: "test@example.com"},
				{Type: "phone", Value: "+12025550123"},
				{Type: "username", Value: "testuser"},
			},
		},
		{
			name:       "keeps known types",
			knownTypes: map[string]string{"testuser": "enterprise"},
			want: []userIdentityModel{
				{Type: "email", Value: "test@example.com"},
				{Type: "enterprise", Value: "testuser"},
				{Type: "phone", Value: "+12025550123"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, diags := userIdentitiesValue(t.Context(), identities, tt.knownTypes)
			requireNoErrors(t, diags)
			var got []userIdentityModel
			requireNoErrors(t, set.ElementsAs(t.Context(), &got, false))
			slices.SortFunc(got, func(a, b userIdentityModel) int {
				return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Value, b.Value))
			})
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccUserResource_ComplexAttributes(t *testing.T) {
	testAccFake(t)
	email := "complex.user@example.com"
	altEmail := "complex.user.alt@example.com"
	username := "complex-user"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a user with email and username identities, is_suspended=false
			{
				Config: testAccUserResourceConfig_ComplexAttributes(email, username, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.complex", "first_name", "Complex"),
					resource.TestCheckResourceAttr("kinde_user.complex", "last_name", "User"),
					resource.TestCheckResourceAttr("kinde_user.complex", "is_suspended", "false"),
					resource.TestCheckResourceAttr("kinde_user.complex", "identities.#", "2"),
					// Check that both identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "username",
						"value": username,
					}),
				),
			},
			// Update is_suspended to true and add another email identity
			{
				Config: testAccUserResourceConfig_ComplexAttributesWithAltEmail(email, altEmail, username, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.complex", "first_name", "Complex"),
					resource.TestCheckResourceAttr("kinde_user.complex", "last_name", "User"),
					resource.TestCheckResourceAttr("kinde_user.complex", "is_suspended", "true"),
					resource.TestCheckResourceAttr("kinde_user.complex", "identities.#", "3"),
					// Check that all identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "username",
						"value": username,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "email",
						"value": altEmail,
					}),
				),
			},
		},
	})
}

func testAccUserResourceConfig_ComplexAttributes(email, username string, isSuspended bool) string {
	return fmt.Sprintf(`
resource "kinde_user" "complex" {
	first_name = "Complex"
	last_name = "User"
	is_suspended = %[3]t

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "username"
			value = %[2]q
		}
	]
}
`, email, username, isSuspended)
}

func testAccUserResourceConfig_ComplexAttributesWithAltEmail(email, altEmail, username string, isSuspended bool) string {
	return fmt.Sprintf(`
resource "kinde_user" "complex" {
	first_name = "Complex"
	last_name = "User"
	is_suspended = %[4]t

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "username"
			value = %[3]q
		},
		{
			type = "email"
			value = %[2]q
		}
	]
}
`, email, altEmail, username, isSuspended)
}

func TestAccUserResource_PhoneIdentity(t *testing.T) {
	testAccFake(t)
	email := "phone.user@example.com"
	phone := "+12025550123"
	phone2 := "+12025550124"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a user with email and phone identities
			{
				Config: testAccUserResourceConfig_WithPhone(email, phone),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.phone", "first_name", "Phone"),
					resource.TestCheckResourceAttr("kinde_user.phone", "last_name", "User"),
					resource.TestCheckResourceAttr("kinde_user.phone", "identities.#", "2"),
					// Check that both identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "phone",
						"value": phone,
					}),
				),
			},
			// Add another phone identity. The adapter splits it into a national
			// number and country, and Kinde reports it back in international
			// format.
			{
				Config: testAccUserResourceConfig_WithMultiplePhones(email, phone, phone2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.phone", "identities.#", "3"),
					// Check that all identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "phone",
						"value": phone,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "phone",
						"value": phone2,
					}),
				),
			},
		},
	})
}

func testAccUserResourceConfig_WithPhone(email, phone string) string {
	return fmt.Sprintf(`
resource "kinde_user" "phone" {
	first_name = "Phone"
	last_name = "User"

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "phone"
			value = %[2]q
		}
	]
}
`, email, phone)
}

func testAccUserResourceConfig_WithMultiplePhones(email, phone1, phone2 string) string {
	return fmt.Sprintf(`
resource "kinde_user" "phone" {
	first_name = "Phone"
	last_name = "User"

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "phone"
			value = %[2]q
		},
		{
			type = "phone"
			value = %[3]q
		}
	]
}
`, email, phone1, phone2)
}

func TestAccUserResource_OAuth2Identity(t *testing.T) {
	f := testAccFake(t)
	email := "oauth2.user@example.com"
	username := "oauth2-user"
	var userID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a user with email and username identities
			{
				Config: testAccUserResourceConfig_OAuth2(email, username),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("kinde_user.oauth2", "id", func(v string) error { userID = v; return nil }),
					resource.TestCheckResourceAttr("kinde_user.oauth2", "first_name", "OAuth2"),
					resource.TestCheckResourceAttr("kinde_user.oauth2", "last_name", "User"),
					// We expect exactly 2 identities in the state (email and username)
					resource.TestCheckResourceAttr("kinde_user.oauth2", "identities.#", "2"),
					// Check that both identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.oauth2", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.oauth2", "identities.*", map[string]string{
						"type":  "username",
						"value": username,
					}),
				),
			},
			// The user signs in with Google, so Kinde adds an OAuth2 identity.
			// Update user details: the OAuth2 identity stays out of state and
			// causes no drift.
			{
				PreConfig: func() { f.AddUserIdentity(userID, "oauth2:google", "oauth2.user@gmail.com") },
				Config:    testAccUserResourceConfig_OAuth2Updated(email, username),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.oauth2", "first_name", "Updated"),
					resource.TestCheckResourceAttr("kinde_user.oauth2", "last_name", "OAuth2"),
					// We still expect exactly 2 identities in the state (OAuth identities excluded)
					resource.TestCheckResourceAttr("kinde_user.oauth2", "identities.#", "2"),
					// Check that both identities still exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.oauth2", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.oauth2", "identities.*", map[string]string{
						"type":  "username",
						"value": username,
					}),
				),
			},
		},
	})
}

func testAccUserResourceConfig_OAuth2(email, username string) string {
	return fmt.Sprintf(`
resource "kinde_user" "oauth2" {
	first_name = "OAuth2"
	last_name = "User"

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "username"
			value = %[2]q
		}
	]
}
`, email, username)
}

func testAccUserResourceConfig_OAuth2Updated(email, username string) string {
	return fmt.Sprintf(`
resource "kinde_user" "oauth2" {
	first_name = "Updated"
	last_name = "OAuth2"

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "username"
			value = %[2]q
		}
	]
}
`, email, username)
}

func TestAccUserResource_NameHandling(t *testing.T) {
	f := testAccFake(t)
	email := "name.test@example.com"
	config := testAccUserResourceConfig_Names(email, "Jane", "Smith")
	var userID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with both names set
			{
				Config: testAccUserResourceConfig_Names(email, "John", "Doe"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("kinde_user.name_test", "id", func(v string) error { userID = v; return nil }),
					resource.TestCheckResourceAttr("kinde_user.name_test", "first_name", "John"),
					resource.TestCheckResourceAttr("kinde_user.name_test", "last_name", "Doe"),
					// created_on is stored exactly as Kinde returns it.
					resource.TestCheckResourceAttr("kinde_user.name_test", "created_on", "2026-01-01T00:00:00Z"),
				),
			},
			// Import by ID
			{
				ResourceName:      "kinde_user.name_test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update with new values
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.name_test", "first_name", "Jane"),
					resource.TestCheckResourceAttr("kinde_user.name_test", "last_name", "Smith"),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveUser(userID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("kinde_user.name_test", "id"),
			},
		},
	})
}

func testAccUserResourceConfig_Names(email, firstName, lastName string) string {
	return fmt.Sprintf(`
resource "kinde_user" "name_test" {
	first_name = %[2]q
	last_name = %[3]q
	identities = [
		{
			type = "email"
			value = %[1]q
		}
	]
}
`, email, firstName, lastName)
}

func TestAccUserResource_OrganizationCode(t *testing.T) {
	f := testAccFake(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "kinde_user" "org" {
  first_name        = "Org"
  last_name         = "Member"
  organization_code = "org_engines"
  identities = [
    {
      type  = "email"
      value = "org.member@example.com"
    }
  ]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.org", "organization_code", "org_engines"),
					// The fake records organization_code without checking that
					// the organization exists.
					resource.TestCheckResourceAttrWith("kinde_user.org", "id", func(id string) error {
						if got := f.UserOrganizationCode(id); got != "org_engines" {
							return fmt.Errorf("Kinde got organization_code %q, want org_engines", got)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestUserResource_ErrorOnCreateWithIsSuspended(t *testing.T) {
	testAccFake(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "kinde_user" "test" {
  first_name   = "Test"
  last_name    = "User"
  is_suspended = true
  identities = [
    {
      type  = "email"
      value = "test@example.com"
    }
  ]
}
`,
				ExpectError: regexp.MustCompile("Setting is_suspended=true when creating a user is not supported"),
			},
		},
	})
}

func TestAccUserResource_IsSuspendedBehavior(t *testing.T) {
	testAccFake(t)
	email := "test-suspended@example.com"
	firstName := "John"
	lastName := "Doe"
	phone := "+358452301234"

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Create user without is_suspended
				Config: testAccUserResourceConfigWithNames(email, firstName, lastName, phone),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.test", "first_name", firstName),
					resource.TestCheckResourceAttr("kinde_user.test", "last_name", lastName),
					resource.TestCheckNoResourceAttr("kinde_user.test", "is_suspended"),
				),
			},
			{
				// Try to set is_suspended during update
				Config: testAccUserResourceConfigWithNamesAndSuspended(email, firstName, lastName, phone, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.test", "first_name", firstName),
					resource.TestCheckResourceAttr("kinde_user.test", "last_name", lastName),
					resource.TestCheckResourceAttr("kinde_user.test", "is_suspended", "true"),
				),
			},
		},
	})
}

func testAccUserResourceConfigWithNames(email, firstName, lastName, phone string) string {
	return fmt.Sprintf(`
resource "kinde_user" "test" {
  first_name = %q
  last_name  = %q
  identities = [
    {
      type  = "email"
      value = %q
    },
    {
      type  = "phone"
      value = %q
    }
  ]
}
`, firstName, lastName, email, phone)
}

func testAccUserResourceConfigWithNamesAndSuspended(email, firstName, lastName, phone string, isSuspended bool) string {
	return fmt.Sprintf(`
resource "kinde_user" "test" {
  first_name = %q
  last_name  = %q
  is_suspended = %t
  identities = [
    {
      type  = "email"
      value = %q
    },
    {
      type  = "phone"
      value = %q
    }
  ]
}
`, firstName, lastName, isSuspended, email, phone)
}
```

In `internal/provider/not_found_test.go`, add this row to the `tests` slice in `TestReadRemovesMissingObjects`:

```go
		{name: "kinde_user", resource: NewUserResource, attrs: map[string]string{"id": "kp_missing"}},
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go vet ./internal/provider/`
Expected: FAIL to compile with `undefined: userIdentityModel`.

- [ ] **Step 3: Move `kinde_user` to `kindeapi`**

Replace `internal/provider/user_resource.go` with the file below. Every method changes. The old `github.com/nxt-fwd/kinde-go/api/users` import is gone, and `Configure` now takes `pd.api`.

```go
package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &UserResource{}
	_ resource.ResourceWithImportState = &UserResource{}
)

func NewUserResource() resource.Resource {
	return &UserResource{}
}

type UserResource struct {
	client *kindeapi.Client
}

type UserResourceModel struct {
	ID               types.String `tfsdk:"id"`
	FirstName        types.String `tfsdk:"first_name"`
	LastName         types.String `tfsdk:"last_name"`
	IsSuspended      types.Bool   `tfsdk:"is_suspended"`
	OrganizationCode types.String `tfsdk:"organization_code"`
	CreatedOn        types.String `tfsdk:"created_on"`
	Identities       types.Set    `tfsdk:"identities"`
}

// userIdentityModel is one element of the identities attribute.
type userIdentityModel struct {
	Type  string `tfsdk:"type"`
	Value string `tfsdk:"value"`
}

// userIdentityObjectType is the element type of the identities attribute.
var userIdentityObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"type":  types.StringType,
	"value": types.StringType,
}}

func (r *UserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a user within a Kinde organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier for the user.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"first_name": schema.StringAttribute{
				Description:         "The first name of the user.",
				Required:            true,
				MarkdownDescription: "The first name of the user.",
			},
			"last_name": schema.StringAttribute{
				Description:         "The last name of the user.",
				Required:            true,
				MarkdownDescription: "The last name of the user.",
			},
			"is_suspended": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether the user is suspended.",
			},
			"organization_code": schema.StringAttribute{
				Optional:            true,
				Description:         "The code of an organization to add the user to when the user is created. Changing it later has no effect; use kinde_organization_user to manage memberships.",
				MarkdownDescription: "The code of an organization to add the user to when the user is created. Changing it later has no effect; use `kinde_organization_user` to manage memberships.",
			},
			"created_on": schema.StringAttribute{
				Description: "When the user was created, as Kinde reports it (ISO 8601).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"identities": schema.SetNestedAttribute{
				Description: "Identities for the user (email, username, phone, etc.).",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Description: "The type of identity (email, username, phone, enterprise, social).",
							Required:    true,
						},
						"value": schema.StringAttribute{
							Description: "The value of the identity. Give phone numbers in international format, such as +61412345678.",
							Required:    true,
						},
					},
				},
			},
		},
	}
}

func (r *UserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	tflog.Debug(ctx, "Starting user creation")

	var plan UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Kinde cannot create a suspended user.
	if plan.IsSuspended.ValueBool() {
		resp.Diagnostics.AddError(
			"Invalid Configuration",
			"Setting is_suspended=true when creating a user is not supported. Create the user first, then update the is_suspended attribute.",
		)
		return
	}

	var identities []userIdentityModel
	resp.Diagnostics.Append(plan.Identities.ElementsAs(ctx, &identities, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !hasEmailIdentity(identities) {
		resp.Diagnostics.AddError(
			"Missing Email Identity",
			"At least one email identity must be provided for the user.",
		)
		return
	}

	createReq := mgmt.CreateUserReq{
		Profile: mgmt.NewOptCreateUserReqProfile(mgmt.CreateUserReqProfile{
			GivenName:  optString(plan.FirstName),
			FamilyName: optString(plan.LastName),
		}),
		OrganizationCode: optString(plan.OrganizationCode),
	}
	for _, identity := range identities {
		createReq.Identities = append(createReq.Identities, newCreateUserIdentity(identity))
	}

	created, err := r.client.CreateUser(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating User",
			fmt.Sprintf("Could not create user: %s", err),
		)
		return
	}
	id, ok := created.ID.Get()
	if !ok {
		resp.Diagnostics.AddError("Error Creating User", "Kinde did not return the new user's ID.")
		return
	}
	plan.ID = types.StringValue(id)

	user, err := r.client.GetUserData(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Created User",
			fmt.Sprintf("Could not read created user ID %s: %s", id, err),
		)
		return
	}
	kindeIdentities, err := r.client.GetUserIdentities(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user %s: %s", id, err),
		)
		return
	}

	resp.Diagnostics.Append(plan.setFromKinde(ctx, user, kindeIdentities, identityTypes(identities))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	user, err := r.client.GetUserData(ctx, id)
	if kindeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User",
			fmt.Sprintf("Could not read user ID %s: %s", id, err),
		)
		return
	}
	kindeIdentities, err := r.client.GetUserIdentities(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user ID %s: %s", id, err),
		)
		return
	}

	// Keep the types state already has; after an import there are none.
	var stateIdentities []userIdentityModel
	if !state.Identities.IsNull() {
		resp.Diagnostics.Append(state.Identities.ElementsAs(ctx, &stateIdentities, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	resp.Diagnostics.Append(state.setFromKinde(ctx, user, kindeIdentities, identityTypes(stateIdentities))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Debug(ctx, "Starting user update")

	var plan, state UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if first_name was previously set and is now being omitted or set to empty
	if !state.FirstName.IsNull() && plan.FirstName.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Cannot Reset First Name",
			"The Kinde API does not allow resetting first_name once it has been set. Please provide the existing first_name value in your configuration.",
		)
	}
	// Check if last_name was previously set and is now being omitted or set to empty
	if !state.LastName.IsNull() && plan.LastName.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Cannot Reset Last Name",
			"The Kinde API does not allow resetting last_name once it has been set. Please provide the existing last_name value in your configuration.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var planned, current []userIdentityModel
	resp.Diagnostics.Append(plan.Identities.ElementsAs(ctx, &planned, false)...)
	resp.Diagnostics.Append(state.Identities.ElementsAs(ctx, &current, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !hasEmailIdentity(planned) {
		resp.Diagnostics.AddError(
			"Missing Email Identity",
			"At least one email identity must be provided for the user.",
		)
		return
	}

	id := plan.ID.ValueString()
	updateReq := mgmt.UpdateUserReq{
		GivenName:  optString(plan.FirstName),
		FamilyName: optString(plan.LastName),
	}
	// Only send is_suspended when it is configured.
	if !plan.IsSuspended.IsNull() {
		updateReq.IsSuspended = mgmt.NewOptBool(plan.IsSuspended.ValueBool())
	}
	if _, err := r.client.UpdateUser(ctx, id, updateReq); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating User",
			fmt.Sprintf("Could not update user ID %s: %s", id, err),
		)
		return
	}

	// Add identities that are new in the plan. OAuth2 identities belong to
	// Kinde, and identities removed from the configuration stay in Kinde.
	for _, identity := range planned {
		if isOAuth2Identity(identity.Type) || slices.Contains(current, identity) {
			continue
		}
		_, err := r.client.CreateUserIdentity(ctx, id, mgmt.CreateUserIdentityReq{
			Type:  mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqType(identity.Type)),
			Value: mgmt.NewOptString(identity.Value),
		})
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Adding User Identity",
				fmt.Sprintf("Could not add identity to user %s: %s", id, err),
			)
			return
		}
	}

	user, err := r.client.GetUserData(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated User",
			fmt.Sprintf("Could not read updated user %s: %s", id, err),
		)
		return
	}
	kindeIdentities, err := r.client.GetUserIdentities(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user %s: %s", id, err),
		)
		return
	}

	resp.Diagnostics.Append(plan.setFromKinde(ctx, user, kindeIdentities, identityTypes(planned))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteUser(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting User",
			fmt.Sprintf("Could not delete user ID %s: %s", state.ID.ValueString(), err),
		)
	}
}

// ImportState sets the ID and names. Read, which Terraform calls next, fills
// in created_on and identities; it keeps names only when they are set.
func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	user, err := r.client.GetUserData(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User",
			fmt.Sprintf("Could not read user ID %s: %s", req.ID, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("first_name"), user.FirstName.Value)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("last_name"), user.LastName.Value)...)
}

// setFromKinde copies Kinde's view of the user into m. first_name, last_name,
// and is_suspended stay null when they are null in m, so settings left out of
// the configuration never show up as drift. knownTypes maps an identity value
// to the type the plan or state gave it.
func (m *UserResourceModel) setFromKinde(ctx context.Context, user *mgmt.User, identities []kindeapi.UserIdentity, knownTypes map[string]string) diag.Diagnostics {
	if !m.FirstName.IsNull() {
		m.FirstName = types.StringValue(user.FirstName.Value)
	}
	if !m.LastName.IsNull() {
		m.LastName = types.StringValue(user.LastName.Value)
	}
	if !m.IsSuspended.IsNull() {
		m.IsSuspended = types.BoolValue(user.IsSuspended.Value)
	}
	if createdOn, ok := user.CreatedOn.Get(); ok {
		m.CreatedOn = types.StringValue(createdOn)
	} else {
		m.CreatedOn = types.StringNull()
	}

	var diags diag.Diagnostics
	m.Identities, diags = userIdentitiesValue(ctx, identities, knownTypes)
	return diags
}

// userIdentitiesValue converts Kinde's identities to the identities
// attribute. It leaves out OAuth2 identities. An identity whose value is in
// knownTypes keeps the type given there instead of Kinde's.
func userIdentitiesValue(ctx context.Context, identities []kindeapi.UserIdentity, knownTypes map[string]string) (types.Set, diag.Diagnostics) {
	elems := make([]userIdentityModel, 0, len(identities))
	for _, identity := range identities {
		if isOAuth2Identity(identity.Type) {
			continue
		}
		typ := identity.Type
		if known, ok := knownTypes[identity.Name]; ok {
			typ = known
		}
		elems = append(elems, userIdentityModel{Type: typ, Value: identity.Name})
	}
	return types.SetValueFrom(ctx, userIdentityObjectType, elems)
}

// isOAuth2Identity reports whether an identity type, such as "oauth2:google",
// is one Kinde adds when the user signs in with a social connection. The
// provider leaves these out of state and never adds them.
func isOAuth2Identity(identityType string) bool {
	return strings.HasPrefix(identityType, "oauth2:")
}

// hasEmailIdentity reports whether identities include an email identity.
func hasEmailIdentity(identities []userIdentityModel) bool {
	return slices.ContainsFunc(identities, func(i userIdentityModel) bool {
		return i.Type == string(mgmt.CreateUserReqIdentitiesItemTypeEmail)
	})
}

// identityTypes maps each identity's value to its type.
func identityTypes(identities []userIdentityModel) map[string]string {
	byValue := make(map[string]string, len(identities))
	for _, identity := range identities {
		byValue[identity.Value] = identity.Type
	}
	return byValue
}

// newCreateUserIdentity converts a configured identity for CreateUser. Kinde
// creates email, phone, and username identities this way; a phone value
// keeps its international format.
func newCreateUserIdentity(identity userIdentityModel) mgmt.CreateUserReqIdentitiesItem {
	var details mgmt.CreateUserReqIdentitiesItemDetails
	switch mgmt.CreateUserReqIdentitiesItemType(identity.Type) {
	case mgmt.CreateUserReqIdentitiesItemTypeEmail:
		details.Email = mgmt.NewOptString(identity.Value)
	case mgmt.CreateUserReqIdentitiesItemTypePhone:
		details.Phone = mgmt.NewOptString(identity.Value)
	case mgmt.CreateUserReqIdentitiesItemTypeUsername:
		details.Username = mgmt.NewOptString(identity.Value)
	}
	return mgmt.CreateUserReqIdentitiesItem{
		Type:    mgmt.NewOptCreateUserReqIdentitiesItemType(mgmt.CreateUserReqIdentitiesItemType(identity.Type)),
		Details: mgmt.NewOptCreateUserReqIdentitiesItemDetails(details),
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/...`
Expected: PASS, including `TestUserIdentitiesValue` and the `kinde_user` subtest of `TestReadRemovesMissingObjects`.

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccUserResource|TestUserResource_ErrorOnCreateWithIsSuspended' -v`
Expected: PASS for `TestAccUserResource_ComplexAttributes`, `_PhoneIdentity`, `_OAuth2Identity`, `_NameHandling`, `_OrganizationCode`, `_IsSuspendedBehavior`, and `TestUserResource_ErrorOnCreateWithIsSuspended`.

- [ ] **Step 5: Commit**

```bash
git add internal/provider/user_resource.go internal/provider/user_resource_test.go internal/provider/not_found_test.go
git commit -m "Move kinde_user to the official Kinde SDK" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 18: Organization users and roles adapter and fake

**Files:**
- Modify: `internal/kindefake/organizations.go` (`organization` and `CreateOrganization`)
- Create: `internal/kindefake/organization_users.go`
- Create: `internal/kindeapi/organization_users.go`
- Test: `internal/kindeapi/organization_users_test.go`

**Interfaces:**
- Consumes:
  - `call[T]`, `APIError`, `IsNotFound`, `HasCode` (Task 1); `apiError`, `handler`, `newFakeClient` (Task 3); `notFound` (Task 5).
  - `organization`, `Fake.organizations`, `findOrganization`, and the test helper `createOrganization` (Task 14).
  - From Task 16: `func (f *Fake) userExists(id string) bool` (callers hold `f.mu`) and `func (c *Client) CreateUser(ctx context.Context, req mgmt.CreateUserReq) (*mgmt.CreateUserResponse, error)`. The tests create a user with a profile and no identities.
- Produces:
  - `func (c *Client) AddOrganizationUsers(ctx context.Context, code string, userIDs []string) error`.
  - `func (c *Client) RemoveOrganizationUser(ctx context.Context, code, userID string) error`. It replaces the provider's raw `DELETE /api/v1/organizations/{code}/users/{uid}`.
  - `func (c *Client) CreateOrganizationUserRole(ctx context.Context, code, userID, roleID string) error`.
  - `func (c *Client) GetOrganizationUserRoles(ctx context.Context, code, userID string) ([]mgmt.OrganizationUserRole, error)`.
  - `func (c *Client) DeleteOrganizationUserRole(ctx context.Context, code, userID, roleID string) error`.
  - Fake state: `organization.users map[string][]string`, which maps each member's user ID to role IDs in the order they were added. Deleting an organization drops its members.
  - `func (f *Fake) member(code, userID string) (*organization, error)`. Callers hold `f.mu`.
  - Test hooks: `func (f *Fake) AddOrganizationUser(code, userID string)`, `func (f *Fake) RemoveOrganizationUser(code, userID string)`, and `func (f *Fake) RemoveOrganizationUserRole(code, userID, roleID string)`.
  - In package `kindeapi_test`: `createUserForMembership(t, c) string`, `requireNotInOrganization(t, err)`, and `requireRoleIDs(t, c, code, userID string, want ...string)`.

Fake behavior:
- A missing organization is a 404 `ORGANIZATION_NOT_FOUND`, and a missing user is a 404 `USER_NOT_FOUND`.
- A user who exists but is not a member gets a 400 `USER_NOT_IN_ORGANIZATION` from listing roles, adding or deleting a role, or removing the user. `kinde_user_role` relies on this code today through the error text. The code is not in Kinde's spec: `GetOrganizationUserRoles` declares no 400 at all, so the adapter gets it from the captured body.
- Deleting a role the member does not hold is a 404 `ROLE_NOT_FOUND`. Adding a role the member already holds changes nothing.
- The fake does not check that a role ID exists, and it lists roles by ID only, without `key` and `name`. This avoids coupling to the roles fake, and the provider reads only IDs.
- `AddOrganizationUsers` answers 501 if a request carries roles or permissions (role and permission keys), which the provider never sends.
- `GetOrganizationUserRoles` takes no paging parameters, so the adapter returns the single response's list.

- [ ] **Step 1: Write the failing adapter tests**

`internal/kindeapi/organization_users_test.go`:

```go
package kindeapi_test

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// createUserForMembership creates a user and returns its ID.
func createUserForMembership(t *testing.T, c *kindeapi.Client) string {
	t.Helper()
	created, err := c.CreateUser(t.Context(), mgmt.CreateUserReq{
		Profile: mgmt.NewOptCreateUserReqProfile(mgmt.CreateUserReqProfile{
			GivenName:  mgmt.NewOptString("Ada"),
			FamilyName: mgmt.NewOptString("Lovelace"),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := created.ID.Get()
	if !ok || id == "" {
		t.Fatalf("CreateUser returned no ID: %+v", created)
	}
	return id
}

// requireNotInOrganization fails unless err is Kinde's 400
// USER_NOT_IN_ORGANIZATION.
func requireNotInOrganization(t *testing.T, err error) {
	t.Helper()
	var apiErr *kindeapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || !kindeapi.HasCode(err, "USER_NOT_IN_ORGANIZATION") {
		t.Fatalf("got %v, want a 400 USER_NOT_IN_ORGANIZATION", err)
	}
}

// requireRoleIDs fails unless the member holds exactly the given roles, in
// order.
func requireRoleIDs(t *testing.T, c *kindeapi.Client, code, userID string, want ...string) {
	t.Helper()
	roles, err := c.GetOrganizationUserRoles(t.Context(), code, userID)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(roles))
	for i, role := range roles {
		got[i] = role.ID.Value
	}
	if !slices.Equal(got, want) {
		t.Fatalf("roles = %q, want %q", got, want)
	}
}

func TestOrganizationUserRoundTrip(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{Name: "Acme"})
	userID := createUserForMembership(t, c)

	_, err := c.GetOrganizationUserRoles(ctx, code, userID)
	requireNotInOrganization(t, err)

	if err := c.AddOrganizationUsers(ctx, code, []string{userID}); err != nil {
		t.Fatal(err)
	}
	requireRoleIDs(t, c, code, userID)

	// Adding a role twice changes nothing.
	for _, roleID := range []string{"rol_admin", "rol_viewer", "rol_admin"} {
		if err := c.CreateOrganizationUserRole(ctx, code, userID, roleID); err != nil {
			t.Fatal(err)
		}
	}
	requireRoleIDs(t, c, code, userID, "rol_admin", "rol_viewer")

	if err := c.DeleteOrganizationUserRole(ctx, code, userID, "rol_admin"); err != nil {
		t.Fatal(err)
	}
	requireRoleIDs(t, c, code, userID, "rol_viewer")
	if err := c.DeleteOrganizationUserRole(ctx, code, userID, "rol_admin"); !kindeapi.IsNotFound(err) {
		t.Fatalf("deleting a role the user lacks: got %v, want not found", err)
	}

	if err := c.RemoveOrganizationUser(ctx, code, userID); err != nil {
		t.Fatal(err)
	}
	_, err = c.GetOrganizationUserRoles(ctx, code, userID)
	requireNotInOrganization(t, err)
	requireNotInOrganization(t, c.RemoveOrganizationUser(ctx, code, userID))
	requireNotInOrganization(t, c.CreateOrganizationUserRole(ctx, code, userID, "rol_admin"))
}

func TestOrganizationUsersNeedExistingOrganizationAndUser(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{Name: "Acme"})
	userID := createUserForMembership(t, c)

	tests := []struct {
		name     string
		call     func() error
		wantCode string
	}{
		{"add to missing organization", func() error {
			return c.AddOrganizationUsers(ctx, "org_missing", []string{userID})
		}, "ORGANIZATION_NOT_FOUND"},
		{"add missing user", func() error {
			return c.AddOrganizationUsers(ctx, code, []string{"kp_missing"})
		}, "USER_NOT_FOUND"},
		{"list roles in missing organization", func() error {
			_, err := c.GetOrganizationUserRoles(ctx, "org_missing", userID)
			return err
		}, "ORGANIZATION_NOT_FOUND"},
		{"list roles of missing user", func() error {
			_, err := c.GetOrganizationUserRoles(ctx, code, "kp_missing")
			return err
		}, "USER_NOT_FOUND"},
		{"add role in missing organization", func() error {
			return c.CreateOrganizationUserRole(ctx, "org_missing", userID, "rol_admin")
		}, "ORGANIZATION_NOT_FOUND"},
		{"delete role in missing organization", func() error {
			return c.DeleteOrganizationUserRole(ctx, "org_missing", userID, "rol_admin")
		}, "ORGANIZATION_NOT_FOUND"},
		{"remove from missing organization", func() error {
			return c.RemoveOrganizationUser(ctx, "org_missing", userID)
		}, "ORGANIZATION_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if !kindeapi.IsNotFound(err) || !kindeapi.HasCode(err, tt.wantCode) {
				t.Fatalf("got %v, want a 404 %s", err, tt.wantCode)
			}
		})
	}
}

func TestOrganizationUserHooks(t *testing.T) {
	f, c := newFakeClient(t)
	ctx := t.Context()
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{Name: "Acme"})
	userID := createUserForMembership(t, c)

	f.AddOrganizationUser(code, userID)
	if err := c.CreateOrganizationUserRole(ctx, code, userID, "rol_admin"); err != nil {
		t.Fatal(err)
	}
	f.RemoveOrganizationUserRole(code, userID, "rol_admin")
	requireRoleIDs(t, c, code, userID)

	f.RemoveOrganizationUser(code, userID)
	_, err := c.GetOrganizationUserRoles(ctx, code, userID)
	requireNotInOrganization(t, err)
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/kindeapi/ -run 'OrganizationUser'`
Expected: FAIL to compile with `c.GetOrganizationUserRoles undefined (type *kindeapi.Client has no field or method GetOrganizationUserRoles)`.

- [ ] **Step 3: Give fake organizations members**

In `internal/kindefake/organizations.go`, replace the `organization` type:

```go
// organization is the fake's record of one organization.
type organization struct {
	// details is what GetOrganization returns.
	details mgmt.GetOrganizationResponse
	// users maps each member's user ID to the IDs of the roles they hold in
	// the organization, in the order they were added.
	users map[string][]string
}
```

In `CreateOrganization`, replace `h.f.organizations[code] = &organization{details: d}` with:

```go
	h.f.organizations[code] = &organization{details: d, users: map[string][]string{}}
```

- [ ] **Step 4: Implement the fake's membership endpoints and hooks**

`internal/kindefake/organization_users.go`:

```go
package kindefake

import (
	"context"
	"net/http"
	"slices"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// member returns the organization with the given code after checking that
// userID belongs to it. A missing organization or user is a 404; a user
// outside the organization is the 400 USER_NOT_IN_ORGANIZATION that Kinde
// returns. Callers must hold f.mu.
func (f *Fake) member(code, userID string) (*organization, error) {
	org, err := f.findOrganization(code)
	if err != nil {
		return nil, err
	}
	if !f.userExists(userID) {
		return nil, notFound("USER_NOT_FOUND", "User not found")
	}
	if _, ok := org.users[userID]; !ok {
		return nil, &apiError{status: http.StatusBadRequest, code: "USER_NOT_IN_ORGANIZATION", message: "User is not a member of this organization"}
	}
	return org, nil
}

func (h handler) AddOrganizationUsers(_ context.Context, req mgmt.OptAddOrganizationUsersReq, params mgmt.AddOrganizationUsersParams) (mgmt.AddOrganizationUsersRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.findOrganization(params.OrgCode)
	if err != nil {
		return nil, err
	}

	// Check every user before adding any, so a rejected request changes
	// nothing.
	var ids []string
	for _, u := range req.Value.Users {
		id, ok := u.ID.Get()
		switch {
		case !ok || id == "":
			return nil, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "every user needs an id"}
		case len(u.Roles) > 0 || len(u.Permissions) > 0:
			return nil, &apiError{status: http.StatusNotImplemented, code: "NOT_IMPLEMENTED", message: "kindefake: roles and permissions in AddOrganizationUsers are not supported"}
		case !h.f.userExists(id):
			return nil, notFound("USER_NOT_FOUND", "User not found")
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if _, ok := org.users[id]; !ok {
			org.users[id] = nil
		}
	}
	return &mgmt.AddOrganizationUsersResponse{
		Code:       mgmt.NewOptString("OK"),
		Message:    mgmt.NewOptString("Users successfully added"),
		UsersAdded: ids,
	}, nil
}

func (h handler) RemoveOrganizationUser(_ context.Context, params mgmt.RemoveOrganizationUserParams) (mgmt.RemoveOrganizationUserRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.member(params.OrgCode, params.UserID)
	if err != nil {
		return nil, err
	}
	delete(org.users, params.UserID)
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("User successfully removed")}, nil
}

// GetOrganizationUserRoles returns role IDs only. The fake does not look up
// roles' keys and names, which the provider does not read.
func (h handler) GetOrganizationUserRoles(_ context.Context, params mgmt.GetOrganizationUserRolesParams) (mgmt.GetOrganizationUserRolesRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.member(params.OrgCode, params.UserID)
	if err != nil {
		return nil, err
	}
	roles := make([]mgmt.OrganizationUserRole, 0, len(org.users[params.UserID]))
	for _, id := range org.users[params.UserID] {
		roles = append(roles, mgmt.OrganizationUserRole{ID: mgmt.NewOptString(id)})
	}
	return &mgmt.GetOrganizationsUserRolesResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
		Roles:   roles,
	}, nil
}

// CreateOrganizationUserRole gives a member a role. Adding a role the member
// already holds changes nothing. The fake does not check that the role
// exists.
func (h handler) CreateOrganizationUserRole(_ context.Context, req *mgmt.CreateOrganizationUserRoleReq, params mgmt.CreateOrganizationUserRoleParams) (mgmt.CreateOrganizationUserRoleRes, error) {
	roleID, ok := req.RoleID.Get()
	if !ok || roleID == "" {
		return nil, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "role_id is required"}
	}
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.member(params.OrgCode, params.UserID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(org.users[params.UserID], roleID) {
		org.users[params.UserID] = append(org.users[params.UserID], roleID)
	}
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Role successfully added")}, nil
}

func (h handler) DeleteOrganizationUserRole(_ context.Context, params mgmt.DeleteOrganizationUserRoleParams) (mgmt.DeleteOrganizationUserRoleRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.member(params.OrgCode, params.UserID)
	if err != nil {
		return nil, err
	}
	roles := org.users[params.UserID]
	i := slices.Index(roles, params.RoleID)
	if i < 0 {
		return nil, notFound("ROLE_NOT_FOUND", "The user does not have this role in the organization")
	}
	org.users[params.UserID] = slices.Delete(roles, i, i+1)
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Role successfully removed")}, nil
}

// AddOrganizationUser makes a user a member of an organization behind the
// provider's back, as joining through Kinde's own sign-up would. It does
// nothing if the organization does not exist.
func (f *Fake) AddOrganizationUser(code, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, ok := f.organizations[code]
	if !ok {
		return
	}
	if _, ok := org.users[userID]; !ok {
		org.users[userID] = nil
	}
}

// RemoveOrganizationUser removes a user from an organization behind the
// provider's back.
func (f *Fake) RemoveOrganizationUser(code, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if org, ok := f.organizations[code]; ok {
		delete(org.users, userID)
	}
}

// RemoveOrganizationUserRole takes a role from an organization member behind
// the provider's back.
func (f *Fake) RemoveOrganizationUserRole(code, userID, roleID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, ok := f.organizations[code]
	if !ok {
		return
	}
	roles, ok := org.users[userID]
	if !ok {
		return
	}
	org.users[userID] = slices.DeleteFunc(roles, func(id string) bool { return id == roleID })
}
```

- [ ] **Step 5: Implement the adapter methods**

`internal/kindeapi/organization_users.go`:

```go
package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// AddOrganizationUsers adds existing users to an organization, without roles
// or permissions.
func (c *Client) AddOrganizationUsers(ctx context.Context, code string, userIDs []string) error {
	users := make([]mgmt.AddOrganizationUsersReqUsersItem, len(userIDs))
	for i, id := range userIDs {
		users[i] = mgmt.AddOrganizationUsersReqUsersItem{ID: mgmt.NewOptString(id)}
	}
	_, err := call[*mgmt.AddOrganizationUsersResponse](ctx, "AddOrganizationUsers", func(ctx context.Context) (any, error) {
		return c.api.AddOrganizationUsers(ctx,
			mgmt.NewOptAddOrganizationUsersReq(mgmt.AddOrganizationUsersReq{Users: users}),
			mgmt.AddOrganizationUsersParams{OrgCode: code})
	})
	return err
}

// RemoveOrganizationUser removes a user from an organization.
func (c *Client) RemoveOrganizationUser(ctx context.Context, code, userID string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "RemoveOrganizationUser", func(ctx context.Context) (any, error) {
		return c.api.RemoveOrganizationUser(ctx, mgmt.RemoveOrganizationUserParams{OrgCode: code, UserID: userID})
	})
	return err
}

// CreateOrganizationUserRole gives an organization member a role.
func (c *Client) CreateOrganizationUserRole(ctx context.Context, code, userID, roleID string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "CreateOrganizationUserRole", func(ctx context.Context) (any, error) {
		return c.api.CreateOrganizationUserRole(ctx,
			&mgmt.CreateOrganizationUserRoleReq{RoleID: mgmt.NewOptString(roleID)},
			mgmt.CreateOrganizationUserRoleParams{OrgCode: code, UserID: userID})
	})
	return err
}

// GetOrganizationUserRoles returns the roles a user holds in an organization.
// For a user outside the organization, Kinde answers 400 with the code
// USER_NOT_IN_ORGANIZATION. The endpoint takes no paging parameters, so one
// response is the whole list.
func (c *Client) GetOrganizationUserRoles(ctx context.Context, code, userID string) ([]mgmt.OrganizationUserRole, error) {
	resp, err := call[*mgmt.GetOrganizationsUserRolesResponse](ctx, "GetOrganizationUserRoles", func(ctx context.Context) (any, error) {
		return c.api.GetOrganizationUserRoles(ctx, mgmt.GetOrganizationUserRolesParams{OrgCode: code, UserID: userID})
	})
	if err != nil {
		return nil, err
	}
	return resp.Roles, nil
}

// DeleteOrganizationUserRole takes a role from an organization member.
func (c *Client) DeleteOrganizationUserRole(ctx context.Context, code, userID, roleID string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteOrganizationUserRole", func(ctx context.Context) (any, error) {
		return c.api.DeleteOrganizationUserRole(ctx, mgmt.DeleteOrganizationUserRoleParams{OrgCode: code, UserID: userID, RoleID: roleID})
	})
	return err
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/kindeapi/ ./internal/kindefake/ -race -v`
Expected: PASS, including `TestOrganizationUserRoundTrip`, every subtest of `TestOrganizationUsersNeedExistingOrganizationAndUser`, and `TestOrganizationUserHooks`.

- [ ] **Step 7: Commit**

```bash
git add internal/kindeapi/organization_users.go internal/kindeapi/organization_users_test.go internal/kindefake/organizations.go internal/kindefake/organization_users.go
git commit -m "Add organization users and roles to kindeapi and the fake" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 19: Migrate `kinde_organization_user` and `kinde_user_role`

**Files:**
- Modify: `internal/provider/organization_user_resource.go`
- Modify: `internal/provider/organization_user_schema.go` (whole file)
- Modify: `internal/provider/user_role_resource.go`
- Create: `internal/provider/organization_user_resource_test.go`
- Create: `internal/provider/user_role_resource_test.go`
- Test: `internal/provider/not_found_test.go`

**Interfaces:**
- Consumes:
  - The Task 18 adapter methods, plus the hooks `AddOrganizationUser`, `RemoveOrganizationUser`, and `RemoveOrganizationUserRole`.
  - In the acceptance-test HCL: `kinde_organization` (Task 15), `kinde_user` (Task 17), and `kinde_role` (Task 8).
  - `providerDataFrom`, `testAccFake`, and the `TestReadRemovesMissingObjects` table (Task 4); `splitID` (existing `utils.go`).
- Produces, in `organization_user_schema.go`:
  - `const userNotInOrganization = "USER_NOT_IN_ORGANIZATION"`;
  - `func membershipGone(err error) bool`, which is true for not-found or `USER_NOT_IN_ORGANIZATION`;
  - `func organizationUserRoleIDs(roles []mgmt.OrganizationUserRole) []string`.

Changes:
- `kinde_user_role` Create replaces its error-text match with `kindeapi.HasCode(err, userNotInOrganization)`.
- `Read` of either resource removes it from state on not-found or `USER_NOT_IN_ORGANIZATION`, since a user who left the organization holds none of its roles. `Delete` treats both as success.
- `kinde_organization_user` Delete calls `RemoveOrganizationUser` instead of building a raw request.
- `organization_user_schema.go` loses `expandOrganizationUserModel` and `expandOrganizationUserParams`. They were unused (`//nolint:unused`) and built old-library types.
- Schemas, IDs, and import formats are unchanged: `organization_code:user_id` and `organization_code:user_id:role_id`.

The `kinde_user_role` test makes the user a member with the fake's `AddOrganizationUser` hook, not with `kinde_organization_user`. A `kinde_organization_user` with no `roles` would read the role that `kinde_user_role` assigns and plan to remove it.

- [ ] **Step 1: Write the failing tests**

`internal/provider/organization_user_resource_test.go`:

```go
package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccOrganizationUserResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-orguser")
	oneRole := testAccOrganizationUserResourceConfig(testID, "[kinde_role.first.id]")
	twoRoles := testAccOrganizationUserResourceConfig(testID, "[kinde_role.first.id, kinde_role.second.id]")
	var orgCode, userID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: oneRole,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "organization_code", "kinde_organization.test", "code"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "user_id", "kinde_user.test", "id"),
					resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "1"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "roles.0", "kinde_role.first", "id"),
					resource.TestCheckResourceAttrWith("kinde_organization.test", "code", func(v string) error {
						orgCode = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("kinde_user.test", "id", func(v string) error {
						userID = v
						return nil
					}),
					// The ID is organization_code:user_id.
					func(s *terraform.State) error {
						return resource.TestCheckResourceAttr("kinde_organization_user.test", "id", orgCode+":"+userID)(s)
					},
				),
			},
			// ImportState testing: the import ID is organization_code:user_id.
			{
				ResourceName: "kinde_organization_user.test",
				ImportState:  true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return orgCode + ":" + userID, nil
				},
				ImportStateVerify: true,
			},
			// An import ID in any other format is rejected.
			{
				ResourceName:  "kinde_organization_user.test",
				ImportState:   true,
				ImportStateId: "org-code-without-user",
				ExpectError:   regexp.MustCompile(`Invalid Import ID`),
			},
			// Adding a role updates the membership in place.
			{
				Config: twoRoles,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("kinde_organization_user.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "2"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "roles.1", "kinde_role.second", "id"),
				),
			},
			// Removed outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveOrganizationUser(orgCode, userID) },
				Config:             twoRoles,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: twoRoles,
				Check:  resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "2"),
			},
		},
	})
}

// testAccOrganizationUserResourceConfig adds a user to an organization with
// roles, an HCL list of role IDs.
func testAccOrganizationUserResourceConfig(testID, roles string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name = %[1]q
}

resource "kinde_user" "test" {
	first_name = "Org"
	last_name  = "Member"

	identities = [
		{
			type  = "email"
			value = "%[1]s@example.com"
		}
	]
}

resource "kinde_role" "first" {
	name        = "%[1]s-first"
	key         = "%[1]s-first"
	description = "First test role"
}

resource "kinde_role" "second" {
	name        = "%[1]s-second"
	key         = "%[1]s-second"
	description = "Second test role"
}

resource "kinde_organization_user" "test" {
	organization_code = kinde_organization.test.code
	user_id           = kinde_user.test.id
	roles             = %[2]s
}
`, testID, roles)
}
```

`internal/provider/user_role_resource_test.go`:

```go
package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccUserRoleResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-userrole")
	base := testAccUserRoleBaseConfig(testID)
	withRole := base + testAccUserRoleResourceConfig
	var orgCode, userID, roleID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// The organization, user, and role exist, but the user has not
			// joined the organization.
			{
				Config: base,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("kinde_organization.test", "code", func(v string) error {
						orgCode = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("kinde_user.test", "id", func(v string) error {
						userID = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("kinde_role.test", "id", func(v string) error {
						roleID = v
						return nil
					}),
				),
			},
			// A user outside the organization cannot get a role in it.
			{
				Config:      withRole,
				ExpectError: regexp.MustCompile(`User Not in Organization`),
			},
			// Once the user has joined, the role is assigned.
			{
				PreConfig: func() { f.AddOrganizationUser(orgCode, userID) },
				Config:    withRole,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "organization_code", "kinde_organization.test", "code"),
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "user_id", "kinde_user.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "role_id", "kinde_role.test", "id"),
					// The ID is organization_code:user_id:role_id.
					func(s *terraform.State) error {
						return resource.TestCheckResourceAttr("kinde_user_role.test", "id", orgCode+":"+userID+":"+roleID)(s)
					},
				),
			},
			// ImportState testing: the import ID is organization_code:user_id:role_id.
			{
				ResourceName: "kinde_user_role.test",
				ImportState:  true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return orgCode + ":" + userID + ":" + roleID, nil
				},
				ImportStateVerify: true,
			},
			// An import ID in any other format is rejected.
			{
				ResourceName:  "kinde_user_role.test",
				ImportState:   true,
				ImportStateId: "org-code:user-id",
				ExpectError:   regexp.MustCompile(`Invalid Import ID`),
			},
			// Removed outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveOrganizationUserRole(orgCode, userID, roleID) },
				Config:             withRole,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: withRole,
				Check:  resource.TestCheckResourceAttrSet("kinde_user_role.test", "id"),
			},
		},
	})
}

// testAccUserRoleBaseConfig declares an organization, a user, and a role.
// It does not make the user a member of the organization.
func testAccUserRoleBaseConfig(testID string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name = %[1]q
}

resource "kinde_user" "test" {
	first_name = "Role"
	last_name  = "Holder"

	identities = [
		{
			type  = "email"
			value = "%[1]s@example.com"
		}
	]
}

resource "kinde_role" "test" {
	name        = %[1]q
	key         = %[1]q
	description = "Test role"
}
`, testID)
}

const testAccUserRoleResourceConfig = `
resource "kinde_user_role" "test" {
	organization_code = kinde_organization.test.code
	user_id           = kinde_user.test.id
	role_id           = kinde_role.test.id
}
`
```

Add these rows to the `tests` slice in `TestReadRemovesMissingObjects` (`internal/provider/not_found_test.go`), after the `kinde_organization` row:

```go
		{name: "kinde_organization_user", resource: NewOrganizationUserResource, attrs: map[string]string{"id": "org_missing:kp_missing", "organization_code": "org_missing", "user_id": "kp_missing"}},
		{name: "kinde_user_role", resource: NewUserRoleResource, attrs: map[string]string{"id": "org_missing:kp_missing:rol_missing", "organization_code": "org_missing", "user_id": "kp_missing", "role_id": "rol_missing"}},
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/provider/ -run TestReadRemovesMissingObjects`
Expected: FAIL. `TestReadRemovesMissingObjects/kinde_organization_user` panics with a nil pointer dereference because `Configure` still reads `pd.legacy`.

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccOrganizationUserResource|TestAccUserRoleResource' -v`
Expected: `TestAccOrganizationUserResource` fails at the out-of-band step with `Error Reading Organization User`: the old Read treats `USER_NOT_IN_ORGANIZATION` as an error. `TestAccUserRoleResource` may already pass, because the old error-text match and list scan behave the same against the fake.

- [ ] **Step 3: Rewrite the membership helpers**

Replace `internal/provider/organization_user_schema.go`:

```go
// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

type OrganizationUserResourceModel struct {
	ID               types.String `tfsdk:"id"`
	OrganizationCode types.String `tfsdk:"organization_code"`
	UserID           types.String `tfsdk:"user_id"`
	Roles            types.List   `tfsdk:"roles"`
	Permissions      types.List   `tfsdk:"permissions"`
}

// userNotInOrganization is the error code Kinde returns when a user is not a
// member of the organization. Kinde's OpenAPI spec does not document it.
const userNotInOrganization = "USER_NOT_IN_ORGANIZATION"

// membershipGone reports whether err means a membership no longer exists:
// the organization or user is gone, or the user has left the organization.
func membershipGone(err error) bool {
	return kindeapi.IsNotFound(err) || kindeapi.HasCode(err, userNotInOrganization)
}

// organizationUserRoleIDs returns the IDs of roles, in Kinde's order.
func organizationUserRoleIDs(roles []mgmt.OrganizationUserRole) []string {
	ids := make([]string, 0, len(roles))
	for _, role := range roles {
		if id, ok := role.ID.Get(); ok {
			ids = append(ids, id)
		}
	}
	return ids
}
```

- [ ] **Step 4: Migrate `kinde_organization_user`**

In `internal/provider/organization_user_resource.go`, replace the import block:

```go
import (
	"context"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)
```

Change the client field:

```go
type OrganizationUserResource struct {
	client *kindeapi.Client
}
```

Replace `Configure`, `Create`, `Read`, `Update`, and `Delete`. `Metadata`, `Schema`, and `ImportState` are unchanged.

```go
func (r *OrganizationUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *OrganizationUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	code, userID := plan.OrganizationCode.ValueString(), plan.UserID.ValueString()

	// First, add the user to the organization without roles.
	if err := r.client.AddOrganizationUsers(ctx, code, []string{userID}); err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Organization User",
			fmt.Sprintf("Could not create organization user: %s", err),
		)
		return
	}

	// Then add the roles one by one.
	var roles []string
	if !plan.Roles.IsNull() {
		resp.Diagnostics.Append(plan.Roles.ElementsAs(ctx, &roles, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	for _, roleID := range roles {
		if err := r.client.CreateOrganizationUserRole(ctx, code, userID, roleID); err != nil {
			resp.Diagnostics.AddError(
				"Error Adding Role",
				fmt.Sprintf("Could not add role %s: %s", roleID, err),
			)
			return
		}
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", code, userID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Listing the user's roles also checks membership.
	roles, err := r.client.GetOrganizationUserRoles(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
	if membershipGone(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization User",
			fmt.Sprintf("Could not read organization user: %s", err),
		)
		return
	}

	roleIDs := organizationUserRoleIDs(roles)
	if len(roleIDs) > 0 {
		rolesList, diags := types.ListValueFrom(ctx, types.StringType, roleIDs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Roles = rolesList
	} else {
		state.Roles = types.ListNull(types.StringType)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state OrganizationUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Roles.Equal(state.Roles) {
		code, userID := state.OrganizationCode.ValueString(), state.UserID.ValueString()

		// Diff against Kinde's current roles, not the prior state.
		current, err := r.client.GetOrganizationUserRoles(ctx, code, userID)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading Current Roles",
				fmt.Sprintf("Could not read current roles: %s", err),
			)
			return
		}
		currentRoles := organizationUserRoleIDs(current)

		var desiredRoles []string
		if !plan.Roles.IsNull() {
			resp.Diagnostics.Append(plan.Roles.ElementsAs(ctx, &desiredRoles, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}

		for _, roleID := range currentRoles {
			if slices.Contains(desiredRoles, roleID) {
				continue
			}
			if err := r.client.DeleteOrganizationUserRole(ctx, code, userID, roleID); err != nil {
				resp.Diagnostics.AddError(
					"Error Removing Role",
					fmt.Sprintf("Could not remove role %s: %s", roleID, err),
				)
				return
			}
		}
		for _, roleID := range desiredRoles {
			if slices.Contains(currentRoles, roleID) {
				continue
			}
			if err := r.client.CreateOrganizationUserRole(ctx, code, userID, roleID); err != nil {
				resp.Diagnostics.AddError(
					"Error Adding Role",
					fmt.Sprintf("Could not add role %s: %s", roleID, err),
				)
				return
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveOrganizationUser(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
	if err != nil && !membershipGone(err) {
		resp.Diagnostics.AddError(
			"Error Removing User from Organization",
			fmt.Sprintf("Could not remove user from organization: %s", err),
		)
	}
}
```

- [ ] **Step 5: Migrate `kinde_user_role`**

In `internal/provider/user_role_resource.go`, replace the import block. `strings` stays for `ImportState`.

```go
import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)
```

Change the client field:

```go
type UserRoleResource struct {
	client *kindeapi.Client
}
```

Replace `Configure`, `Create`, `Read`, and `Delete`. `Metadata`, `Schema`, `Update`, and `ImportState` are unchanged.

```go
func (r *UserRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *UserRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UserRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	code, userID, roleID := plan.OrganizationCode.ValueString(), plan.UserID.ValueString(), plan.RoleID.ValueString()

	// Listing the user's roles in the organization verifies membership.
	if _, err := r.client.GetOrganizationUserRoles(ctx, code, userID); err != nil {
		if kindeapi.HasCode(err, userNotInOrganization) {
			resp.Diagnostics.AddError(
				"User Not in Organization",
				fmt.Sprintf("User %s is not a member of organization %s. Please add the user to the organization before assigning roles.",
					userID,
					code,
				),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Error Checking User Organization Membership",
			fmt.Sprintf("Could not verify if user %s is a member of organization %s: %s",
				userID,
				code,
				err,
			),
		)
		return
	}

	if err := r.client.CreateOrganizationUserRole(ctx, code, userID, roleID); err != nil {
		resp.Diagnostics.AddError(
			"Error Assigning Role to User",
			fmt.Sprintf("Could not assign role %s to user %s in organization %s: %s",
				roleID,
				userID,
				code,
				err,
			),
		)
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s:%s", code, userID, roleID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	roles, err := r.client.GetOrganizationUserRoles(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
	// A user who left the organization holds none of its roles.
	if membershipGone(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Roles",
			fmt.Sprintf("Could not read roles for user %s in organization %s: %s",
				state.UserID.ValueString(),
				state.OrganizationCode.ValueString(),
				err,
			),
		)
		return
	}

	if !slices.Contains(organizationUserRoleIDs(roles), state.RoleID.ValueString()) {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *UserRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteOrganizationUserRole(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString(), state.RoleID.ValueString())
	if err != nil && !membershipGone(err) {
		resp.Diagnostics.AddError(
			"Error Removing Role from User",
			fmt.Sprintf("Could not remove role %s from user %s in organization %s: %s",
				state.RoleID.ValueString(),
				state.UserID.ValueString(),
				state.OrganizationCode.ValueString(),
				err,
			),
		)
	}
}
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go build ./... && go test ./internal/... && TF_ACC=1 go test ./internal/provider/ -run 'TestAccOrganizationUserResource|TestAccUserRoleResource|TestReadRemovesMissingObjects' -v`
Expected: PASS, including the `kinde_organization_user` and `kinde_user_role` rows of `TestReadRemovesMissingObjects`.

- [ ] **Step 7: Commit**

```bash
git add internal/provider/organization_user_resource.go internal/provider/organization_user_schema.go internal/provider/user_role_resource.go internal/provider/organization_user_resource_test.go internal/provider/user_role_resource_test.go internal/provider/not_found_test.go
git commit -m "Move kinde_organization_user and kinde_user_role to kindeapi" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 20: Remove the old library and finish the migration

**Files:**
- Modify: `internal/provider/provider_data.go`, `internal/provider/provider.go`
- Modify: `internal/provider/provider_test.go`, `internal/provider/integration_test.go`
- Modify: `go.mod`, `go.sum`, `renovate.json`
- Modify: `GNUmakefile`, `README.md`, `examples/provider/provider.tf`
- Delete: `.env.example`
- Modify: `docs/` (regenerated)

**Interfaces:**
- Consumes: every resource and data source on `pd.api` (Tasks 6–19), and `testAccFake` (Task 4).
- Produces: `type providerData struct { api *kindeapi.Client }`. No code references `github.com/nxt-fwd/kinde-go`.

- [ ] **Step 1: Confirm nothing but the provider wiring still uses the old library**

Run: `grep -rln "nxt-fwd/kinde-go" --include=*.go .`
Expected: exactly `./internal/provider/provider.go` and `./internal/provider/provider_data.go`. If any resource file is listed, the task that owns it is incomplete. Stop and finish that task first.

- [ ] **Step 2: Move the integration tests to the fake**

In `internal/provider/integration_test.go`, apply this to each of the five test functions: `TestAccIntegrationBasicWorkflow`, `TestAccIntegrationRoleManagement`, `TestAccIntegrationUserOrganizations`, `TestAccIntegrationApplicationWorkflow`, and `TestAccIntegrationM2MApplicationWorkflow`.
- Delete the line `PreCheck:                 func() { testAccPreCheck(t) },`.
- Add `testAccFake(t)` as the first line of the function body.

`logout_uris` and `redirect_uris` are sets since Task 13, so index-based checks no longer apply. In `TestAccIntegrationApplicationWorkflow`, replace the four index checks (`logout_uris.0`, `redirect_uris.0`, `redirect_uris.1`, and their neighbors) with:

```go
resource.TestCheckResourceAttr("kinde_application.test", "logout_uris.#", "1"),
resource.TestCheckTypeSetElemAttr("kinde_application.test", "logout_uris.*", "https://example.com/logout"),
resource.TestCheckResourceAttr("kinde_application.test", "redirect_uris.#", "2"),
resource.TestCheckTypeSetElemAttr("kinde_application.test", "redirect_uris.*", "https://example.com/callback"),
resource.TestCheckTypeSetElemAttr("kinde_application.test", "redirect_uris.*", "https://example.com/callback2"),
```

In `internal/provider/provider_test.go`, delete `testAccPreCheck` and the `os` import.

- [ ] **Step 3: Run the integration tests**

Run: `TF_ACC=1 go test ./internal/provider/ -run 'TestAccIntegration' -v`
Expected: PASS for all five. A failure here means the fake and a resource disagree in a way the per-resource tests missed. Fix it in the owning resource or fake handler, not in the integration test.

- [ ] **Step 4: Remove the legacy client**

`internal/provider/provider_data.go`: delete the `legacy` field, its comment, and the `github.com/nxt-fwd/kinde-go` import. The struct becomes:

```go
// providerData is what Configure hands to every resource and data source.
type providerData struct {
	api *kindeapi.Client
}
```

`internal/provider/provider.go`: delete everything in `Configure` from `opts := kinde.NewClientOptions()` through `legacy := kinde.New(ctx, opts)`. Change the last three lines to:

```go
	pd := &providerData{api: client}
	resp.DataSourceData = pd
	resp.ResourceData = pd
```

Delete the `github.com/nxt-fwd/kinde-go` import.

Then run:

```bash
go mod tidy
grep -rn "nxt-fwd/kinde-go" go.mod go.sum main.go internal || echo "old library gone"
```

Expected: `old library gone`. The module's own path, `github.com/nxt-fwd/terraform-provider-kinde`, stays, and the pattern does not match it.

- [ ] **Step 5: Update Renovate, the Makefile, and the docs sources**

`renovate.json`: replace the `kinde-go` package rule with:

```json
    {
      "description": "kinde-go is regenerated from Kinde's live spec, so 0.x releases can rename types, and the fake shares that spec, so CI can't catch API changes: review its updates by hand",
      "matchPackageNames": ["github.com/kinde-oss/kinde-go"],
      "groupName": "kinde-go",
      "automerge": false
    },
```

`GNUmakefile`: the acceptance tests no longer need credentials. Replace the `testacc` recipe line with:

```make
	TF_ACC=1 go test ./... -v $(TESTARGS) -timeout 120m
```

Delete the closing comment block that starts `# Note: Test resources are automatically cleaned up`. Then delete `.env.example`.

`README.md`, Testing section: replace from the `make test` code block through the `**Note:**` paragraph with:

````markdown
```sh
# Run unit tests
make test

# Run acceptance tests against an in-memory fake Kinde (no credentials needed)
make testacc
```

Acceptance tests need the `terraform` CLI on your `PATH`, or `TF_ACC_TERRAFORM_PATH` pointing at a `terraform` or `tofu` binary. They never contact a real Kinde business.
````

In `README.md`'s usage section, add this line directly above `export KINDE_AUDIENCE=...`:

```sh
# Optional: defaults to <KINDE_DOMAIN>/api
```

`examples/provider/provider.tf`: change the `audience` line's comment to:

```hcl
  audience      = "https://example.kinde.com/api"  # Optional; defaults to <domain>/api. Also configurable via KINDE_AUDIENCE
```

- [ ] **Step 6: Regenerate the registry docs**

The `//go:generate` directives live in the separate `tools` module, so `go generate ./...` at the repository root does nothing. Run only the docs generator. The `tools` module's other directives also add HashiCorp copyright headers, which this change does not want.

Run: `(cd tools && go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-dir .. -provider-name kinde)`
Expected: `docs/` changes for every schema change in the spec, including:
- `client_secret` sensitive;
- the `audience` default;
- `kinde_user` without `updated_on`;
- `kinde_application` URI sets;
- `kinde_organization` `theme_code` values;
- `kinde_permission` `description` now optional and computed.

Review `git diff docs/` for those changes and nothing unrelated.

- [ ] **Step 7: Verify the whole branch**

Run:

```bash
go build ./...
go vet ./...
go test ./... -race
TF_ACC=1 go test ./internal/provider/ -v -timeout 30m
golangci-lint run ./...
```

Expected:
- every command passes;
- `golangci-lint` reports `0 issues.`;
- `TestReadRemovesMissingObjects` lists a subtest for each of the ten resources.

- [ ] **Step 8: Commit**

```bash
git add -A internal go.mod go.sum renovate.json GNUmakefile README.md examples docs
git rm .env.example
git commit -m "Remove the nxt-fwd/kinde-go dependency" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

---

### Task 21: Run the acceptance tests in CI

**Files:**
- Modify: `.github/workflows/test.yml`

**Interfaces:**
- Consumes: `TF_ACC=1 go test ./internal/provider/` passing against the fake (Task 20).
- Produces: a required-check candidate, `Acceptance (terraform)` and `Acceptance (opentofu)`, on every pull request and on pushes to `main`.

- [ ] **Step 1: Replace the disabled acceptance job**

In `.github/workflows/test.yml`, delete the whole `test` job: the comment `# Run acceptance tests in a matrix with Terraform CLI versions` through its final `timeout-minutes: 10`. Put this in its place:

```yaml
  # Acceptance tests run against an in-memory fake Kinde (internal/kindefake), so they need no secrets.
  acceptance:
    name: Acceptance (${{ matrix.cli }})
    needs: build
    runs-on: ubuntu-latest
    timeout-minutes: 15
    strategy:
      fail-fast: false
      matrix:
        cli: [terraform, opentofu]
    env:
      # renovate: datasource=github-releases depName=hashicorp/terraform
      TERRAFORM_VERSION: 1.16.5
      # renovate: datasource=github-releases depName=opentofu/opentofu
      TOFU_VERSION: 1.13.1
    steps:
      - uses: actions/checkout@692973e3d937129bcbf40652eb9f2f61becf3332 # v4.1.7
        with:
          persist-credentials: false
      - uses: actions/setup-go@0a12ed9d6a96ab950c8f026ed9f722fe0da7ef32 # v5.0.2
        with:
          go-version-file: 'go.mod'
          cache: true
      - if: matrix.cli == 'terraform'
        uses: hashicorp/setup-terraform@b9cd54a3c349d3f38e8881555d616ced269862dd # v3.1.2
        with:
          terraform_version: ${{ env.TERRAFORM_VERSION }}
          terraform_wrapper: false
      # No OpenTofu setup action is on the enterprise allowlist, so download the release and check
      # it against the published checksums.
      - name: Install OpenTofu
        if: matrix.cli == 'opentofu'
        run: |
          base="https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}"
          cd "$RUNNER_TEMP"
          curl -fsSLO "${base}/tofu_${TOFU_VERSION}_linux_amd64.zip"
          curl -fsSLO "${base}/tofu_${TOFU_VERSION}_SHA256SUMS"
          sha256sum --check --ignore-missing "tofu_${TOFU_VERSION}_SHA256SUMS"
          unzip -q "tofu_${TOFU_VERSION}_linux_amd64.zip" -d tofu
          {
            echo "TF_ACC_TERRAFORM_PATH=${RUNNER_TEMP}/tofu/tofu"
            # OpenTofu resolves unqualified providers on its own registry, so the test provider must claim that host.
            echo "TF_ACC_PROVIDER_HOST=registry.opentofu.org"
          } >> "$GITHUB_ENV"
      - run: go mod download
      - env:
          TF_ACC: "1"
        run: go test -v -cover ./internal/provider/
        timeout-minutes: 10
```

Before committing, check the two versions against the latest releases. If either differs, use the newer one:

```bash
gh release view --repo hashicorp/terraform --json tagName -q .tagName
gh release view --repo opentofu/opentofu --json tagName -q .tagName
```

(These printed `v1.16.5` and `v1.13.1` on 2026-10-08.)

- [ ] **Step 2: Lint the workflow**

Run: `docker run --rm -v "$PWD:/repo" -w /repo ghcr.io/zizmorcore/zizmor .github/workflows/test.yml` (or `zizmor .github/workflows/test.yml` if installed).
Expected: no findings at the repository's configured severity. The `Install OpenTofu` step uses only `env` values in `run`, never `${{ }}` expressions, so `template-injection` should not fire.

- [ ] **Step 3: Exercise the OpenTofu path locally**

Run:

```bash
TOFU_VERSION=1.13.1
tmp="$(mktemp -d)" && cd "$tmp"
curl -fsSLO "https://github.com/opentofu/opentofu/releases/download/v${TOFU_VERSION}/tofu_${TOFU_VERSION}_linux_amd64.zip"
unzip -q "tofu_${TOFU_VERSION}_linux_amd64.zip" -d tofu && cd -
TF_ACC=1 TF_ACC_TERRAFORM_PATH="$tmp/tofu/tofu" TF_ACC_PROVIDER_HOST=registry.opentofu.org \
  go test ./internal/provider/ -run 'TestAccRoleResource' -v
```

Use `linux_arm64` instead of `linux_amd64` on an arm64 machine.
Expected: PASS. If OpenTofu reports that it cannot find `registry.opentofu.org/hashicorp/kinde`, add `TF_ACC_PROVIDER_NAMESPACE=hashicorp` to both the local command and the workflow's `GITHUB_ENV` block, and rerun.

- [ ] **Step 4: Commit**

```bash
git add .github/workflows/test.yml
git commit -m "Run acceptance tests against the fake in CI" -m "Claude-Session: https://claude.ai/code/session_01U3QvK5SQcuFhf6gB1yUzro"
```

After the branch is pushed and both acceptance jobs pass, add `Acceptance (terraform)` and `Acceptance (opentofu)` to the branch protection's required checks. A repository admin has to do that in GitHub settings.
