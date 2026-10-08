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
