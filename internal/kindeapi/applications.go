package kindeapi

import (
	"context"
	"net/url"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateApplication creates an application. The response carries its ID,
// client ID, and client secret.
func (c *Client) CreateApplication(ctx context.Context, req *mgmt.CreateApplicationReq) (*mgmt.CreateApplicationResponse, error) {
	return call[*mgmt.CreateApplicationResponse](ctx, "CreateApplication", func(ctx context.Context) (any, error) {
		return c.api.CreateApplication(ctx, req)
	})
}

// GetApplication gets an application. Its logout and redirect URIs come
// from GetLogoutURLs and GetCallbackURLs.
func (c *Client) GetApplication(ctx context.Context, id string) (*mgmt.GetApplicationResponse, error) {
	return call[*mgmt.GetApplicationResponse](ctx, "GetApplication", func(ctx context.Context) (any, error) {
		return c.api.GetApplication(ctx, mgmt.GetApplicationParams{ApplicationID: id})
	})
}

// UpdateApplication changes the fields set in req. A nil URI list leaves the
// application's URIs alone; a non-nil list, even an empty one, replaces them.
func (c *Client) UpdateApplication(ctx context.Context, id string, req mgmt.UpdateApplicationReq) error {
	_, err := call[*mgmt.UpdateApplicationOK](ctx, "UpdateApplication", func(ctx context.Context) (any, error) {
		return c.api.UpdateApplication(ctx, mgmt.NewOptUpdateApplicationReq(req), mgmt.UpdateApplicationParams{ApplicationID: id})
	})
	return err
}

// DeleteApplication deletes an application.
func (c *Client) DeleteApplication(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteApplication", func(ctx context.Context) (any, error) {
		return c.api.DeleteApplication(ctx, mgmt.DeleteApplicationParams{ApplicationID: id})
	})
	return err
}

// GetLogoutURLs returns an application's logout redirect URIs.
func (c *Client) GetLogoutURLs(ctx context.Context, applicationID string) (*mgmt.LogoutRedirectUrls, error) {
	return call[*mgmt.LogoutRedirectUrls](ctx, "GetLogoutURLs", func(ctx context.Context) (any, error) {
		return c.api.GetLogoutURLs(ctx, mgmt.GetLogoutURLsParams{AppID: applicationID})
	})
}

// GetCallbackURLs returns an application's redirect (callback) URIs.
func (c *Client) GetCallbackURLs(ctx context.Context, applicationID string) (*mgmt.RedirectCallbackUrls, error) {
	return call[*mgmt.RedirectCallbackUrls](ctx, "GetCallbackURLs", func(ctx context.Context) (any, error) {
		return c.api.GetCallbackURLs(ctx, mgmt.GetCallbackURLsParams{AppID: applicationID})
	})
}

// EnableConnection enables a connection for an application.
func (c *Client) EnableConnection(ctx context.Context, applicationID, connectionID string) error {
	_, err := call[*mgmt.EnableConnectionOK](ctx, "EnableConnection", func(ctx context.Context) (any, error) {
		return c.api.EnableConnection(ctx, mgmt.EnableConnectionParams{ApplicationID: applicationID, ConnectionID: connectionID})
	})
	return err
}

// RemoveConnection disables a connection for an application.
func (c *Client) RemoveConnection(ctx context.Context, applicationID, connectionID string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "RemoveConnection", func(ctx context.Context) (any, error) {
		return c.api.RemoveConnection(ctx, mgmt.RemoveConnectionParams{ApplicationID: applicationID, ConnectionID: connectionID})
	})
	return err
}

// applicationConnectionsPage is one page of
// GET /api/v1/applications/{application_id}/connections as the API sends it.
type applicationConnectionsPage struct {
	Connections []mgmt.ConnectionConnection `json:"connections"`
	HasMore     bool                        `json:"has_more"`
}

// ListApplicationConnections returns every connection enabled for an
// application.
//
// The SDK's GetApplicationConnections decodes every item as empty, because
// the spec wraps each item in a {"connection": {...}} envelope that the API
// does not send (https://github.com/kinde-oss/kinde-go/issues/53). This
// calls the endpoint directly and decodes each item as
// mgmt.ConnectionConnection, the item type that
// https://github.com/kinde-oss/kinde-go/pull/63 gives the SDK. Remove this
// fallback once a release includes that fix.
//
// The endpoint documents no paging parameters but returns has_more, so this
// pages with starting_after like GET /api/v1/connections.
func (c *Client) ListApplicationConnections(ctx context.Context, applicationID string) ([]mgmt.ConnectionConnection, error) {
	path := "/api/v1/applications/" + url.PathEscape(applicationID) + "/connections"
	return allCursorPages(ctx,
		func(conn mgmt.ConnectionConnection) string { return conn.ID.Value },
		func(ctx context.Context, startingAfter string) ([]mgmt.ConnectionConnection, bool, error) {
			var query url.Values
			if startingAfter != "" {
				query = url.Values{"starting_after": {startingAfter}}
			}
			var page applicationConnectionsPage
			if err := c.getJSON(ctx, "GetApplicationConnections", path, query, &page); err != nil {
				return nil, false, err
			}
			return page.Connections, page.HasMore, nil
		})
}
