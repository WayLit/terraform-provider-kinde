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
