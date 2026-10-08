package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// AddOrganizationUsers adds existing users to an organization, without roles
// or permissions.
func (c *Client) AddOrganizationUsers(ctx context.Context, code string, userIDs []string) error {
	users := make([]mgmt.AddOrganizationUsersReqUsersItem, len(userIDs))
	for i, id := range userIDs {
		users[i] = mgmt.AddOrganizationUsersReqUsersItem{ID: mgmt.NewOptString(id)}
	}
	_, err := call[*mgmt.AddOrganizationUsersResponse](ctx, "AddOrganizationUsers", func(ctx context.Context) (any, error) {
		return c.api.AddOrganizationUsers(ctx,
			mgmt.NewOptAddOrganizationUsersReq(mgmt.AddOrganizationUsersReq{Users: users}),
			mgmt.AddOrganizationUsersParams{OrgCode: code})
	})
	return err
}

// RemoveOrganizationUser removes a user from an organization.
func (c *Client) RemoveOrganizationUser(ctx context.Context, code, userID string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "RemoveOrganizationUser", func(ctx context.Context) (any, error) {
		return c.api.RemoveOrganizationUser(ctx, mgmt.RemoveOrganizationUserParams{OrgCode: code, UserID: userID})
	})
	return err
}

// CreateOrganizationUserRole gives an organization member a role.
func (c *Client) CreateOrganizationUserRole(ctx context.Context, code, userID, roleID string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "CreateOrganizationUserRole", func(ctx context.Context) (any, error) {
		return c.api.CreateOrganizationUserRole(ctx,
			&mgmt.CreateOrganizationUserRoleReq{RoleID: mgmt.NewOptString(roleID)},
			mgmt.CreateOrganizationUserRoleParams{OrgCode: code, UserID: userID})
	})
	return err
}

// GetOrganizationUserRoles returns the roles a user holds in an organization.
// For a user outside the organization, Kinde answers 400 with the code
// USER_NOT_IN_ORGANIZATION. The endpoint takes no paging parameters, so one
// response is the whole list.
func (c *Client) GetOrganizationUserRoles(ctx context.Context, code, userID string) ([]mgmt.OrganizationUserRole, error) {
	resp, err := call[*mgmt.GetOrganizationsUserRolesResponse](ctx, "GetOrganizationUserRoles", func(ctx context.Context) (any, error) {
		return c.api.GetOrganizationUserRoles(ctx, mgmt.GetOrganizationUserRolesParams{OrgCode: code, UserID: userID})
	})
	if err != nil {
		return nil, err
	}
	return resp.Roles, nil
}

// DeleteOrganizationUserRole takes a role from an organization member.
func (c *Client) DeleteOrganizationUserRole(ctx context.Context, code, userID, roleID string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteOrganizationUserRole", func(ctx context.Context) (any, error) {
		return c.api.DeleteOrganizationUserRole(ctx, mgmt.DeleteOrganizationUserRoleParams{OrgCode: code, UserID: userID, RoleID: roleID})
	})
	return err
}
