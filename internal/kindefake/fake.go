// Package kindefake is an in-memory fake of the Kinde management API for
// tests. It runs the server ogen generated from Kinde's OpenAPI spec, so it
// encodes our reading of that spec rather than the live API's behavior.
package kindefake

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
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
	nextID        int
	pageLimit     int

	// Domain state.
	connections map[string]*connection
	apis        map[string]*apiResource
	permissions map[string]mgmt.Permissions
	roles       map[string]*role
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
	f.connections = builtinConnections()
	f.apis = map[string]*apiResource{}
	f.permissions = map[string]mgmt.Permissions{}
	f.roles = map[string]*role{}

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
	f.registerRawRoutes(mux)
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
