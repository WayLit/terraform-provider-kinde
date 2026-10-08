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
