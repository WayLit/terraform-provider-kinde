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
