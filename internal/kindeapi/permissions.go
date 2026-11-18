package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreatePermission creates a permission. Kinde's response carries no ID, so
// callers find the new permission with ListPermissions.
func (c *Client) CreatePermission(ctx context.Context, req mgmt.CreatePermissionReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "CreatePermission", func(ctx context.Context) (any, error) {
		return c.api.CreatePermission(ctx, mgmt.NewOptCreatePermissionReq(req))
	})
	return err
}

// ListPermissions returns every permission in the business.
func (c *Client) ListPermissions(ctx context.Context) ([]mgmt.Permissions, error) {
	return allPages(ctx, func(ctx context.Context, nextToken string) ([]mgmt.Permissions, string, error) {
		params := mgmt.GetPermissionsParams{PageSize: mgmt.NewOptNilInt(maxPageSize)}
		if nextToken != "" {
			params.NextToken = mgmt.NewOptNilString(nextToken)
		}
		res, err := call[*mgmt.GetPermissionsResponse](ctx, "GetPermissions", func(ctx context.Context) (any, error) {
			return c.api.GetPermissions(ctx, params)
		})
		if err != nil {
			return nil, "", err
		}
		return res.Permissions, res.NextToken.Or(""), nil
	})
}

// UpdatePermission changes a permission. Fields left unset in req are not
// sent, so Kinde leaves them alone.
func (c *Client) UpdatePermission(ctx context.Context, id string, req mgmt.UpdatePermissionsReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "UpdatePermissions", func(ctx context.Context) (any, error) {
		return c.api.UpdatePermissions(ctx, mgmt.NewOptUpdatePermissionsReq(req), mgmt.UpdatePermissionsParams{PermissionID: id})
	})
	return err
}

// DeletePermission deletes a permission.
func (c *Client) DeletePermission(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeletePermission", func(ctx context.Context) (any, error) {
		return c.api.DeletePermission(ctx, mgmt.DeletePermissionParams{PermissionID: id})
	})
	return err
}
