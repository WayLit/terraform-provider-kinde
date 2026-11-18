package provider

import (
	"slices"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

func TestOrganizationUserPermissionsExcludeRoleOnlyGrants(t *testing.T) {
	permissions := []mgmt.OrganizationUserPermission{
		{ID: mgmt.NewOptString("perm_direct")},
		{ID: mgmt.NewOptString("perm_inherited"), Roles: []mgmt.OrganizationUserPermissionRolesItem{{ID: mgmt.NewOptString("role_admin")}}},
		{ID: mgmt.NewOptString("perm_other")},
	}
	for _, tc := range []struct {
		name  string
		prior []string
		want  []string
	}{
		{"import", nil, []string{"perm_direct", "perm_other"}},
		{"preserve configured order", []string{"perm_other", "perm_direct"}, []string{"perm_other", "perm_direct"}},
		{"tracked overlapping grant", []string{"perm_inherited"}, []string{"perm_inherited", "perm_direct", "perm_other"}},
		{"revoked grant", []string{"perm_missing"}, []string{"perm_direct", "perm_other"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := organizationUserPermissionIDs(permissions, tc.prior); !slices.Equal(got, tc.want) {
				t.Fatalf("permission IDs = %v, want %v", got, tc.want)
			}
		})
	}
}
