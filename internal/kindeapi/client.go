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
	"time"

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

// tokenTimeout bounds one token request. Token requests cannot use the
// caller's context, so without it a stalled token endpoint would hang the
// Terraform operation.
var tokenTimeout = 30 * time.Second

// Client calls the Kinde management API.
type Client struct {
	api      *mgmt.Client
	http     *http.Client
	domain   string
	audience string
	tokens   oauth2.TokenSource
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
	// must not be bound to that RPC's context. Its HTTP client bounds each
	// token request instead.
	tokenCtx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: tokenTimeout})
	tokens := cc.TokenSource(tokenCtx)

	httpClient := &http.Client{Transport: captureTransport{next: retryTransport{
		next:    http.DefaultTransport,
		retries: maxRetries,
		sleep:   sleepCtx,
	}}}
	api, err := mgmt.NewClient(domain, tokenSecurity{tokens: tokens}, mgmt.WithClient(httpClient))
	if err != nil {
		return nil, fmt.Errorf("kinde: %w", err)
	}
	return &Client{api: api, http: httpClient, domain: domain, audience: audience, tokens: tokens}, nil
}

// Audience returns the audience the client requests tokens for, including
// the <domain>/api default.
func (c *Client) Audience() string {
	return c.audience
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
