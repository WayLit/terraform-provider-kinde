package kindeapi

import (
	"context"
	"net/url"
	"strconv"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateConnection creates a connection. Kinde returns only its ID.
func (c *Client) CreateConnection(ctx context.Context, req *mgmt.CreateConnectionReq) (*mgmt.CreateConnectionResponse, error) {
	return call[*mgmt.CreateConnectionResponse](ctx, "CreateConnection", func(ctx context.Context) (any, error) {
		return c.api.CreateConnection(ctx, req)
	})
}

// GetConnection returns a connection by ID. Kinde never returns its options.
func (c *Client) GetConnection(ctx context.Context, id string) (*mgmt.Connection, error) {
	return call[*mgmt.Connection](ctx, "GetConnection", func(ctx context.Context) (any, error) {
		return c.api.GetConnection(ctx, mgmt.GetConnectionParams{ConnectionID: id})
	})
}

// UpdateConnection changes the fields set in req and leaves the rest as they
// are.
func (c *Client) UpdateConnection(ctx context.Context, id string, req *mgmt.UpdateConnectionReq) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "UpdateConnection", func(ctx context.Context) (any, error) {
		return c.api.UpdateConnection(ctx, req, mgmt.UpdateConnectionParams{ConnectionID: id})
	})
	return err
}

// DeleteConnection deletes a connection.
func (c *Client) DeleteConnection(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteConnection", func(ctx context.Context) (any, error) {
		return c.api.DeleteConnection(ctx, mgmt.DeleteConnectionParams{ConnectionID: id})
	})
	return err
}

// Connection is one item of a connection list, in the shape the live API
// returns it.
type Connection struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Strategy    string `json:"strategy"`
}

// connectionList is one page of a connection list.
type connectionList struct {
	Connections []Connection `json:"connections"`
	HasMore     bool         `json:"has_more"`
}

// connectionPageSize is how many connections each list request asks for.
// Kinde defaults to 10.
const connectionPageSize = 100

// ListConnections returns every connection, including the built-in ones.
//
// It calls the API directly because the SDK cannot decode the response: the
// spec wraps each list item in {"code", "message", "connection"}, which the
// API does not send, so GetConnections returns empty items. Remove this
// fallback once a kinde-go release includes
// https://github.com/kinde-oss/kinde-go/pull/63.
func (c *Client) ListConnections(ctx context.Context) ([]Connection, error) {
	return allCursorPages(ctx, func(conn Connection) string { return conn.ID },
		func(ctx context.Context, startingAfter string) ([]Connection, bool, error) {
			query := url.Values{"page_size": {strconv.Itoa(connectionPageSize)}}
			if startingAfter != "" {
				query.Set("starting_after", startingAfter)
			}
			var page connectionList
			if err := c.getJSON(ctx, "ListConnections", "/api/v1/connections", query, &page); err != nil {
				return nil, false, err
			}
			return page.Connections, page.HasMore, nil
		})
}
