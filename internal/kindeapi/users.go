package kindeapi

import (
	"context"
	"fmt"
	"net/url"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreateUser creates a user. The response carries the new user's ID; call
// GetUserData for the rest.
func (c *Client) CreateUser(ctx context.Context, req mgmt.CreateUserReq) (*mgmt.CreateUserResponse, error) {
	return call[*mgmt.CreateUserResponse](ctx, "CreateUser", func(ctx context.Context) (any, error) {
		return c.api.CreateUser(ctx, mgmt.NewOptCreateUserReq(req))
	})
}

// GetUserData gets a user.
func (c *Client) GetUserData(ctx context.Context, id string) (*mgmt.User, error) {
	return call[*mgmt.User](ctx, "GetUserData", func(ctx context.Context) (any, error) {
		return c.api.GetUserData(ctx, mgmt.GetUserDataParams{ID: id})
	})
}

// UpdateUser updates a user. Unset fields in req keep their values.
func (c *Client) UpdateUser(ctx context.Context, id string, req mgmt.UpdateUserReq) (*mgmt.UpdateUserResponse, error) {
	return call[*mgmt.UpdateUserResponse](ctx, "UpdateUser", func(ctx context.Context) (any, error) {
		return c.api.UpdateUser(ctx, &req, mgmt.UpdateUserParams{ID: id})
	})
}

// DeleteUser deletes a user.
func (c *Client) DeleteUser(ctx context.Context, id string) error {
	_, err := call[*mgmt.SuccessResponse](ctx, "DeleteUser", func(ctx context.Context) (any, error) {
		return c.api.DeleteUser(ctx, mgmt.DeleteUserParams{ID: id})
	})
	return err
}

// CreateUserIdentity adds an identity to a user. For a phone identity,
// req.Value is a number in international format, such as "+61412345678";
// Kinde wants the national number and its country instead, so this method
// splits it and sets PhoneCountryID.
func (c *Client) CreateUserIdentity(ctx context.Context, userID string, req mgmt.CreateUserIdentityReq) (*mgmt.CreateIdentityResponse, error) {
	if req.Type.Value == mgmt.CreateUserIdentityReqTypePhone {
		national, countryID, err := parsePhone(req.Value.Value)
		if err != nil {
			return nil, fmt.Errorf("kinde CreateUserIdentity: %w", err)
		}
		req.Value = mgmt.NewOptString(national)
		req.PhoneCountryID = mgmt.NewOptString(countryID)
	}
	return call[*mgmt.CreateIdentityResponse](ctx, "CreateUserIdentity", func(ctx context.Context) (any, error) {
		return c.api.CreateUserIdentity(ctx, mgmt.NewOptCreateUserIdentityReq(req), mgmt.CreateUserIdentityParams{UserID: userID})
	})
}

// UserIdentity is one of a user's identities.
type UserIdentity struct {
	ID string `json:"id"`
	// Type is the identity type, such as "email", "phone", "username", or
	// "oauth2:google".
	Type string `json:"type"`
	// Name is the identity's value. Kinde reports phone numbers in
	// international format.
	Name string `json:"name"`
}

// GetUserIdentities returns every identity of a user.
//
// It decodes the response itself instead of calling the SDK. Kinde's spec
// documents "is_confirmed": null for identities that record no confirmation,
// such as usernames, and the SDK models is_confirmed as a non-nullable
// OptBool, so a single username identity would fail the whole page. Switch to
// the SDK's GetUserIdentities once mgmt.Identity.IsConfirmed is nullable.
func (c *Client) GetUserIdentities(ctx context.Context, userID string) ([]UserIdentity, error) {
	path := "/api/v1/users/" + url.PathEscape(userID) + "/identities"
	return allCursorPages(ctx, func(i UserIdentity) string { return i.ID },
		func(ctx context.Context, startingAfter string) ([]UserIdentity, bool, error) {
			query := url.Values{}
			if startingAfter != "" {
				query.Set("starting_after", startingAfter)
			}
			var page struct {
				Identities []UserIdentity `json:"identities"`
				HasMore    bool           `json:"has_more"`
			}
			if err := c.getJSON(ctx, "GetUserIdentities", path, query, &page); err != nil {
				return nil, false, err
			}
			return page.Identities, page.HasMore, nil
		})
}
