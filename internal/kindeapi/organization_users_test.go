package kindeapi_test

import (
	"errors"
	"net/http"
	"slices"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// createUserForMembership creates a user and returns its ID.
func createUserForMembership(t *testing.T, c *kindeapi.Client) string {
	t.Helper()
	created, err := c.CreateUser(t.Context(), mgmt.CreateUserReq{
		Profile: mgmt.NewOptCreateUserReqProfile(mgmt.CreateUserReqProfile{
			GivenName:  mgmt.NewOptString("Ada"),
			FamilyName: mgmt.NewOptString("Lovelace"),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}
	id, ok := created.ID.Get()
	if !ok || id == "" {
		t.Fatalf("CreateUser returned no ID: %+v", created)
	}
	return id
}

// requireNotInOrganization fails unless err is Kinde's 400
// USER_NOT_IN_ORGANIZATION.
func requireNotInOrganization(t *testing.T, err error) {
	t.Helper()
	var apiErr *kindeapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest || !kindeapi.HasCode(err, "USER_NOT_IN_ORGANIZATION") {
		t.Fatalf("got %v, want a 400 USER_NOT_IN_ORGANIZATION", err)
	}
}

// requireRoleIDs fails unless the member holds exactly the given roles, in
// order.
func requireRoleIDs(t *testing.T, c *kindeapi.Client, code, userID string, want ...string) {
	t.Helper()
	roles, err := c.GetOrganizationUserRoles(t.Context(), code, userID)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(roles))
	for i, role := range roles {
		got[i] = role.ID.Value
	}
	if !slices.Equal(got, want) {
		t.Fatalf("roles = %q, want %q", got, want)
	}
}

func TestOrganizationUserRoundTrip(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{Name: "Acme"})
	userID := createUserForMembership(t, c)

	_, err := c.GetOrganizationUserRoles(ctx, code, userID)
	requireNotInOrganization(t, err)

	if err := c.AddOrganizationUsers(ctx, code, []string{userID}); err != nil {
		t.Fatal(err)
	}
	requireRoleIDs(t, c, code, userID)

	// Adding a role twice changes nothing.
	for _, roleID := range []string{"rol_admin", "rol_viewer", "rol_admin"} {
		if err := c.CreateOrganizationUserRole(ctx, code, userID, roleID); err != nil {
			t.Fatal(err)
		}
	}
	requireRoleIDs(t, c, code, userID, "rol_admin", "rol_viewer")

	if err := c.DeleteOrganizationUserRole(ctx, code, userID, "rol_admin"); err != nil {
		t.Fatal(err)
	}
	requireRoleIDs(t, c, code, userID, "rol_viewer")
	if err := c.DeleteOrganizationUserRole(ctx, code, userID, "rol_admin"); !kindeapi.IsNotFound(err) {
		t.Fatalf("deleting a role the user lacks: got %v, want not found", err)
	}

	if err := c.RemoveOrganizationUser(ctx, code, userID); err != nil {
		t.Fatal(err)
	}
	_, err = c.GetOrganizationUserRoles(ctx, code, userID)
	requireNotInOrganization(t, err)
	requireNotInOrganization(t, c.RemoveOrganizationUser(ctx, code, userID))
	requireNotInOrganization(t, c.CreateOrganizationUserRole(ctx, code, userID, "rol_admin"))
}

func TestOrganizationUsersNeedExistingOrganizationAndUser(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{Name: "Acme"})
	userID := createUserForMembership(t, c)

	tests := []struct {
		name     string
		call     func() error
		wantCode string
	}{
		{"add to missing organization", func() error {
			return c.AddOrganizationUsers(ctx, "org_missing", []string{userID})
		}, "ORGANIZATION_NOT_FOUND"},
		{"add missing user", func() error {
			return c.AddOrganizationUsers(ctx, code, []string{"kp_missing"})
		}, "USER_NOT_FOUND"},
		{"list roles in missing organization", func() error {
			_, err := c.GetOrganizationUserRoles(ctx, "org_missing", userID)
			return err
		}, "ORGANIZATION_NOT_FOUND"},
		{"list roles of missing user", func() error {
			_, err := c.GetOrganizationUserRoles(ctx, code, "kp_missing")
			return err
		}, "USER_NOT_FOUND"},
		{"add role in missing organization", func() error {
			return c.CreateOrganizationUserRole(ctx, "org_missing", userID, "rol_admin")
		}, "ORGANIZATION_NOT_FOUND"},
		{"delete role in missing organization", func() error {
			return c.DeleteOrganizationUserRole(ctx, "org_missing", userID, "rol_admin")
		}, "ORGANIZATION_NOT_FOUND"},
		{"remove from missing organization", func() error {
			return c.RemoveOrganizationUser(ctx, "org_missing", userID)
		}, "ORGANIZATION_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.call()
			if !kindeapi.IsNotFound(err) || !kindeapi.HasCode(err, tt.wantCode) {
				t.Fatalf("got %v, want a 404 %s", err, tt.wantCode)
			}
		})
	}
}

func TestOrganizationUserHooks(t *testing.T) {
	f, c := newFakeClient(t)
	ctx := t.Context()
	code := createOrganization(t, c, &mgmt.CreateOrganizationReq{Name: "Acme"})
	userID := createUserForMembership(t, c)

	f.AddOrganizationUser(code, userID)
	if err := c.CreateOrganizationUserRole(ctx, code, userID, "rol_admin"); err != nil {
		t.Fatal(err)
	}
	f.RemoveOrganizationUserRole(code, userID, "rol_admin")
	requireRoleIDs(t, c, code, userID)

	f.RemoveOrganizationUser(code, userID)
	_, err := c.GetOrganizationUserRoles(ctx, code, userID)
	requireNotInOrganization(t, err)
}
