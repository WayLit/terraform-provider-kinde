package provider

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

func TestBuildRolePermissionOperations(t *testing.T) {
	got := buildRolePermissionOperations(
		[]string{"perm_c", "perm_a", "perm_b"},
		[]string{"perm_b", "perm_d"},
	)

	want := []mgmt.UpdateRolePermissionsReqPermissionsItem{
		{ID: mgmt.NewOptString("perm_a"), Operation: mgmt.NewOptString("delete")},
		{ID: mgmt.NewOptString("perm_c"), Operation: mgmt.NewOptString("delete")},
		{ID: mgmt.NewOptString("perm_d")},
	}

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected operations\nwant: %#v\n got: %#v", want, got)
	}
}

func TestRolePermissionsMatchIgnoresOrdering(t *testing.T) {
	if !rolePermissionsMatch([]string{"perm_b", "perm_a"}, []string{"perm_a", "perm_b"}) {
		t.Fatal("expected matching permission sets with different ordering")
	}

	if rolePermissionsMatch([]string{"perm_a"}, []string{"perm_a", "perm_b"}) {
		t.Fatal("expected different permission sets not to match")
	}
}

func TestFlattenRolePermissionsPreservesNullAndEmpty(t *testing.T) {
	ctx := t.Context()

	nullSet, err := flattenRolePermissions(ctx, nil, true)
	if err != nil {
		t.Fatalf("flatten null permissions: %s", err)
	}
	if !nullSet.IsNull() {
		t.Fatalf("expected null permissions, got %#v", nullSet)
	}

	emptySet, err := flattenRolePermissions(ctx, nil, false)
	if err != nil {
		t.Fatalf("flatten empty permissions: %s", err)
	}
	if emptySet.IsNull() {
		t.Fatal("expected configured empty permissions to remain an empty set, got null")
	}
	if len(emptySet.Elements()) != 0 {
		t.Fatalf("expected empty set, got %d elements", len(emptySet.Elements()))
	}
}

func TestExpandRoleUpdateReqSendsNameAndKey(t *testing.T) {
	got := expandRoleUpdateReq(RoleResourceModel{
		ID:          types.StringValue("rol_0001"),
		Name:        types.StringValue("Admins"),
		Key:         types.StringValue("admins"),
		Description: types.StringValue("Administrators"),
	})
	want := mgmt.UpdateRolesReq{
		Name:        "Admins",
		Key:         "admins",
		Description: mgmt.NewOptString("Administrators"),
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestGetRoleUsesAllPermissionPages(t *testing.T) {
	ctx := t.Context()
	f := kindefake.New(t)
	// With pages of 10, the eleventh permission is on the second page.
	f.LimitPageSize(10)
	client, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: f.ClientSecret})
	if err != nil {
		t.Fatal(err)
	}

	created, err := client.CreateRole(ctx, mgmt.CreateRoleReq{
		Name:        mgmt.NewOptString("Role"),
		Key:         mgmt.NewOptString("role"),
		Description: mgmt.NewOptString("Role description"),
	})
	if err != nil {
		t.Fatalf("create role: %s", err)
	}
	roleID := created.Role.Value.ID.Value

	var items []mgmt.UpdateRolePermissionsReqPermissionsItem
	for i := range 11 {
		key := fmt.Sprintf("perm_%02d", i)
		if err := client.CreatePermission(ctx, mgmt.CreatePermissionReq{Name: mgmt.NewOptString(key), Key: mgmt.NewOptString(key)}); err != nil {
			t.Fatalf("create permission: %s", err)
		}
	}
	perms, err := client.ListPermissions(ctx)
	if err != nil {
		t.Fatalf("list permissions: %s", err)
	}
	for _, p := range perms {
		items = append(items, mgmt.UpdateRolePermissionsReqPermissionsItem{ID: p.ID})
	}
	if err := client.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{Permissions: items}); err != nil {
		t.Fatalf("add role permissions: %s", err)
	}

	r := RoleResource{client: client}
	role, err := r.getRole(ctx, roleID)
	if err != nil {
		t.Fatalf("get role: %s", err)
	}
	if got := len(role.permissionIDs); got != 11 {
		t.Fatalf("expected 11 permissions across pages, got %d: %#v", got, role.permissionIDs)
	}
	if got := role.details.Key.Value; got != "role" {
		t.Fatalf("key = %q, want role", got)
	}
}

func TestGetRoleReturnsNotFound(t *testing.T) {
	f := kindefake.New(t)
	client, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: f.ClientSecret})
	if err != nil {
		t.Fatal(err)
	}
	r := RoleResource{client: client}
	if _, err := r.getRole(t.Context(), "rol_missing"); !kindeapi.IsNotFound(err) {
		t.Fatalf("expected a not-found error, got %v", err)
	}
}

func TestFlattenRoleResourceUsesEmptySetForConfiguredEmpty(t *testing.T) {
	state, err := flattenRoleResource(t.Context(), mgmt.GetRoleResponseRole{
		ID:          mgmt.NewOptString("role_1"),
		Name:        mgmt.NewOptString("Role"),
		Key:         mgmt.NewOptString("role"),
		Description: mgmt.NewOptString("Role description"),
	}, nil, false)
	if err != nil {
		t.Fatalf("flatten role: %s", err)
	}

	if state.Permissions.IsNull() {
		t.Fatal("expected empty configured permissions to stay an empty set")
	}

	emptySet, diags := types.SetValueFrom(t.Context(), types.StringType, []string{})
	if diags.HasError() {
		t.Fatalf("build empty set: %v", diags)
	}
	if !state.Permissions.Equal(emptySet) {
		t.Fatalf("expected empty set, got %#v", state.Permissions)
	}
}
