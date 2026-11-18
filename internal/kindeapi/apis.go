package kindeapi

import (
	"context"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// AddAPIs registers an API. Kinde returns only the new API's ID.
func (c *Client) AddAPIs(ctx context.Context, req *mgmt.AddAPIsReq) (*mgmt.CreateApisResponse, error) {
	return call[*mgmt.CreateApisResponse](ctx, "AddAPIs", func(ctx context.Context) (any, error) {
		return c.api.AddAPIs(ctx, req)
	})
}

// GetAPI returns an API by ID.
func (c *Client) GetAPI(ctx context.Context, id string) (*mgmt.GetAPIResponse, error) {
	return call[*mgmt.GetAPIResponse](ctx, "GetAPI", func(ctx context.Context) (any, error) {
		return c.api.GetAPI(ctx, mgmt.GetAPIParams{APIID: id})
	})
}

// DeleteAPI deletes an API.
func (c *Client) DeleteAPI(ctx context.Context, id string) error {
	_, err := call[*mgmt.DeleteAPIResponse](ctx, "DeleteAPI", func(ctx context.Context) (any, error) {
		return c.api.DeleteAPI(ctx, mgmt.DeleteAPIParams{APIID: id})
	})
	return err
}
