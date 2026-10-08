package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateRole creates a role. The response carries the new role's ID.
func (c *Client) CreateRole(ctx context.Context, req mgmt.CreateRoleReq) (*mgmt.CreateRolesResponse, error) {
	return call[*mgmt.CreateRolesResponse](ctx, "CreateRole", func(ctx context.Context) (any, error) {
		return c.api.CreateRole(ctx, mgmt.NewOptCreateRoleReq(req))
	})
}

// GetRole returns a role without its permissions.
func (c *Client) GetRole(ctx context.Context, id string) (*mgmt.GetRoleResponse, error) {
	return call[*mgmt.GetRoleResponse](ctx, "GetRole", func(ctx context.Context) (any, error) {
		return c.api.GetRole(ctx, mgmt.GetRoleParams{RoleID: id})
	})
}

// UpdateRole changes a role. Kinde requires the name and key on every
// update; other fields left unset in req are not sent.
func (c *Client) UpdateRole(ctx context.Context, id string, req mgmt.UpdateRolesReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "UpdateRoles", func(ctx context.Context) (any, error) {
		return c.api.UpdateRoles(ctx, mgmt.NewOptUpdateRolesReq(req), mgmt.UpdateRolesParams{RoleID: id})
	})
	return err
}

// DeleteRole deletes a role.
func (c *Client) DeleteRole(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteRole", func(ctx context.Context) (any, error) {
		return c.api.DeleteRole(ctx, mgmt.DeleteRoleParams{RoleID: id})
	})
	return err
}

// ListRolePermissions returns every permission assigned to a role.
func (c *Client) ListRolePermissions(ctx context.Context, roleID string) ([]mgmt.Permissions, error) {
	return allPages(ctx, func(ctx context.Context, nextToken string) ([]mgmt.Permissions, string, error) {
		params := mgmt.GetRolePermissionsParams{RoleID: roleID, PageSize: mgmt.NewOptNilInt(maxPageSize)}
		if nextToken != "" {
			params.NextToken = mgmt.NewOptNilString(nextToken)
		}
		res, err := call[*mgmt.RolePermissionsResponse](ctx, "GetRolePermissions", func(ctx context.Context) (any, error) {
			return c.api.GetRolePermissions(ctx, params)
		})
		if err != nil {
			return nil, "", err
		}
		return res.Permissions, res.NextToken.Or(""), nil
	})
}

// UpdateRolePermissions adds permissions to a role and, for items whose
// operation is "delete", removes them.
func (c *Client) UpdateRolePermissions(ctx context.Context, roleID string, req mgmt.UpdateRolePermissionsReq) error {
	_, err := call[*mgmt.UpdateRolePermissionsResponse](ctx, "UpdateRolePermissions", func(ctx context.Context) (any, error) {
		return c.api.UpdateRolePermissions(ctx, &req, mgmt.UpdateRolePermissionsParams{RoleID: roleID})
	})
	return err
}
