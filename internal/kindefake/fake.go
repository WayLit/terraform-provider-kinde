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
