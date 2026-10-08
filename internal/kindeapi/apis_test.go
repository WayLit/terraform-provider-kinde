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
