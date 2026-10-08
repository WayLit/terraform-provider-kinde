package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateOrganization creates an organization. The response carries only the
// new organization's code; call GetOrganization for the rest.
func (c *Client) CreateOrganization(ctx context.Context, req *mgmt.CreateOrganizationReq) (*mgmt.CreateOrganizationResponse, error) {
	return call[*mgmt.CreateOrganizationResponse](ctx, "CreateOrganization", func(ctx context.Context) (any, error) {
		return c.api.CreateOrganization(ctx, req)
	})
}

// GetOrganization returns the organization with the given code. Brand colors
// come back as objects; their Hex field holds the form the provider uses.
func (c *Client) GetOrganization(ctx context.Context, code string) (*mgmt.GetOrganizationResponse, error) {
	return call[*mgmt.GetOrganizationResponse](ctx, "GetOrganization", func(ctx context.Context) (any, error) {
		return c.api.GetOrganization(ctx, mgmt.GetOrganizationParams{Code: code})
	})
}

// UpdateOrganization changes the fields req sets. Kinde does not return the
// organization; call GetOrganization to read the result.
func (c *Client) UpdateOrganization(ctx context.Context, code string, req *mgmt.UpdateOrganizationReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "UpdateOrganization", func(ctx context.Context) (any, error) {
		return c.api.UpdateOrganization(ctx, mgmt.NewOptUpdateOrganizationReq(*req), mgmt.UpdateOrganizationParams{OrgCode: code})
	})
	return err
}

// DeleteOrganization deletes the organization with the given code.
func (c *Client) DeleteOrganization(ctx context.Context, code string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteOrganization", func(ctx context.Context) (any, error) {
		return c.api.DeleteOrganization(ctx, mgmt.DeleteOrganizationParams{OrgCode: code})
	})
	return err
}
