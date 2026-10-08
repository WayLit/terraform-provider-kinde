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
