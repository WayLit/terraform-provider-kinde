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
