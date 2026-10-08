package kindeapi_test

import (
	"fmt"
	"slices"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// createRole creates a role with the given key and returns its ID.
func createRole(t *testing.T, c *kindeapi.Client, key string) string {
	t.Helper()
	res, err := c.CreateRole(t.Context(), mgmt.CreateRoleReq{
		Name:        mgmt.NewOptString("Role " + key),
		Key:         mgmt.NewOptString(key),
		Description: mgmt.NewOptString("Test role"),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := res.Role.Value.ID.Get()
	if !ok || id == "" {
		t.Fatalf("expected a role ID, got %+v", res)
	}
	return id
}

// rolePermissionIDs returns the sorted IDs of a role's permissions.
func rolePermissionIDs(t *testing.T, c *kindeapi.Client, roleID string) []string {
	t.Helper()
	perms, err := c.ListRolePermissions(t.Context(), roleID)
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, len(perms))
	for _, p := range perms {
		ids = append(ids, p.ID.Value)
	}
	slices.Sort(ids)
	return ids
}

func TestRoleLifecycle(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	id := createRole(t, c, "admin")

	got, err := c.GetRole(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got.Role.Value.Key.Value != "admin" || got.Role.Value.Description.Value != "Test role" {
		t.Fatalf("unexpected role %+v", got.Role.Value)
	}

	err = c.UpdateRole(ctx, id, mgmt.UpdateRolesReq{
		Name:        "Administrators",
		Key:         "administrators",
		Description: mgmt.NewOptString("Updated role"),
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err = c.GetRole(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	want := mgmt.GetRoleResponseRole{
		ID:            mgmt.NewOptString(id),
		Key:           mgmt.NewOptString("administrators"),
		Name:          mgmt.NewOptString("Administrators"),
		Description:   mgmt.NewOptString("Updated role"),
		IsDefaultRole: mgmt.NewOptBool(false),
	}
	if got.Role.Value != want {
		t.Fatalf("after update got %+v, want %+v", got.Role.Value, want)
	}

	if err := c.DeleteRole(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetRole(ctx, id); !kindeapi.IsNotFound(err) {
		t.Fatalf("expected not found after delete, got %v", err)
	}
}

func TestRoleNotFound(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	if _, err := c.GetRole(ctx, "rol_missing"); !kindeapi.IsNotFound(err) || !kindeapi.HasCode(err, "ROLE_NOT_FOUND") {
		t.Fatalf("get: expected ROLE_NOT_FOUND, got %v", err)
	}
	if err := c.UpdateRole(ctx, "rol_missing", mgmt.UpdateRolesReq{Name: "Missing", Key: "missing"}); !kindeapi.IsNotFound(err) {
		t.Fatalf("update: expected not found, got %v", err)
	}
	if err := c.DeleteRole(ctx, "rol_missing"); !kindeapi.IsNotFound(err) {
		t.Fatalf("delete: expected not found, got %v", err)
	}
	if _, err := c.ListRolePermissions(ctx, "rol_missing"); !kindeapi.IsNotFound(err) {
		t.Fatalf("list permissions: expected not found, got %v", err)
	}
	if err := c.UpdateRolePermissions(ctx, "rol_missing", mgmt.UpdateRolePermissionsReq{}); !kindeapi.IsNotFound(err) {
		t.Fatalf("update permissions: expected not found, got %v", err)
	}
}

func TestUpdateRolePermissionsAddsAndRemoves(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	roleID := createRole(t, c, "editor")
	a := createPermission(t, c, "perm:a")
	b := createPermission(t, c, "perm:b")
	d := createPermission(t, c, "perm:d")

	err := c.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{
		Permissions: []mgmt.UpdateRolePermissionsReqPermissionsItem{
			{ID: mgmt.NewOptString(a)},
			{ID: mgmt.NewOptString(b)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rolePermissionIDs(t, c, roleID), []string{a, b}; !slices.Equal(got, want) {
		t.Fatalf("after adding got %v, want %v", got, want)
	}

	err = c.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{
		Permissions: []mgmt.UpdateRolePermissionsReqPermissionsItem{
			{ID: mgmt.NewOptString(a), Operation: mgmt.NewOptString("delete")},
			{ID: mgmt.NewOptString(d)},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rolePermissionIDs(t, c, roleID), []string{b, d}; !slices.Equal(got, want) {
		t.Fatalf("after mixed update got %v, want %v", got, want)
	}

	err = c.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{
		Permissions: []mgmt.UpdateRolePermissionsReqPermissionsItem{
			{ID: mgmt.NewOptString(a)},
			{ID: mgmt.NewOptString("perm_missing")},
		},
	})
	if !kindeapi.HasCode(err, "PERMISSION_NOT_FOUND") || kindeapi.IsNotFound(err) {
		t.Fatalf("expected a 400 PERMISSION_NOT_FOUND, got %v", err)
	}
	if got, want := rolePermissionIDs(t, c, roleID), []string{b, d}; !slices.Equal(got, want) {
		t.Fatalf("a rejected update changed permissions to %v, want %v", got, want)
	}
}

func TestListRolePermissionsReturnsEveryPage(t *testing.T) {
	f, c := newFakeClient(t)
	f.LimitPageSize(2)
	roleID := createRole(t, c, "viewer")
	var want []string
	var items []mgmt.UpdateRolePermissionsReqPermissionsItem
	for i := range 5 {
		id := createPermission(t, c, fmt.Sprintf("perm:%d", i))
		want = append(want, id)
		items = append(items, mgmt.UpdateRolePermissionsReqPermissionsItem{ID: mgmt.NewOptString(id)})
	}
	if err := c.UpdateRolePermissions(t.Context(), roleID, mgmt.UpdateRolePermissionsReq{Permissions: items}); err != nil {
		t.Fatal(err)
	}
	slices.Sort(want)
	if got := rolePermissionIDs(t, c, roleID); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
}
