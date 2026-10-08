package kindefake

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strings"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nyaruka/phonenumbers"
)

// identitiesPageSize is how many identities one page of
// GET /api/v1/users/{user_id}/identities holds. Kinde's spec does not say.
const identitiesPageSize = 10

// user is a Kinde user.
type user struct {
	id          string
	firstName   string
	lastName    string
	isSuspended bool
	// organizationCode is recorded as CreateUser sent it. The fake does not
	// check that the organization exists or add the user to it.
	organizationCode string
	identities       []identity
}

// identity is one of a user's identities. name is its value; phone numbers
// are kept in international format, as Kinde reports them.
type identity struct {
	id   string
	typ  string
	name string
}

func userNotFound(id string) error {
	return notFound("USER_NOT_FOUND", "kindefake: no user "+id)
}

func invalidUserRequest(message string) error {
	return &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: " + message}
}

// userExists reports whether a user exists. Callers must hold f.mu.
func (f *Fake) userExists(id string) bool {
	_, ok := f.users[id]
	return ok
}

// RemoveUser deletes a user behind the provider's back.
func (f *Fake) RemoveUser(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.users, id)
}

// AddUserIdentity gives a user an identity behind the provider's back, the
// way Kinde adds an "oauth2:google" identity when the user signs in with
// Google. It panics if the user does not exist.
func (f *Fake) AddUserIdentity(userID, identityType, name string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	u, ok := f.users[userID]
	if !ok {
		panic("kindefake: AddUserIdentity: no user " + userID)
	}
	u.identities = append(u.identities, identity{id: f.newID("identity"), typ: identityType, name: name})
}

// UserOrganizationCode returns the organization_code a user was created
// with, or "" if there was none or the user does not exist.
func (f *Fake) UserOrganizationCode(id string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	if u, ok := f.users[id]; ok {
		return u.organizationCode
	}
	return ""
}

func (h handler) CreateUser(_ context.Context, req mgmt.OptCreateUserReq) (mgmt.CreateUserRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	r := req.Value
	u := &user{
		firstName:        r.Profile.Value.GivenName.Value,
		lastName:         r.Profile.Value.FamilyName.Value,
		organizationCode: r.OrganizationCode.Value,
	}
	resp := &mgmt.CreateUserResponse{Created: mgmt.NewOptBool(true)}
	for _, item := range r.Identities {
		details := item.Details.Value
		var name string
		switch item.Type.Value {
		case mgmt.CreateUserReqIdentitiesItemTypeEmail:
			name = details.Email.Value
		case mgmt.CreateUserReqIdentitiesItemTypePhone:
			name = details.Phone.Value
		case mgmt.CreateUserReqIdentitiesItemTypeUsername:
			name = details.Username.Value
		}
		if name == "" {
			return nil, invalidUserRequest("identity of type " + string(item.Type.Value) + " has no value in details")
		}
		u.identities = append(u.identities, identity{id: h.f.newID("identity"), typ: string(item.Type.Value), name: name})
		resp.Identities = append(resp.Identities, mgmt.UserIdentity{
			Type:   mgmt.NewOptString(string(item.Type.Value)),
			Result: mgmt.NewOptUserIdentityResult(mgmt.UserIdentityResult{Created: mgmt.NewOptBool(true)}),
		})
	}
	u.id = h.f.newID("kp")
	h.f.users[u.id] = u
	resp.ID = mgmt.NewOptString(u.id)
	return resp, nil
}

func (h handler) GetUserData(_ context.Context, params mgmt.GetUserDataParams) (mgmt.GetUserDataRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	u, ok := h.f.users[params.ID]
	if !ok {
		return nil, userNotFound(params.ID)
	}
	return &mgmt.User{
		ID:          mgmt.NewOptString(u.id),
		FirstName:   mgmt.NewOptString(u.firstName),
		LastName:    mgmt.NewOptString(u.lastName),
		IsSuspended: mgmt.NewOptBool(u.isSuspended),
		CreatedOn:   mgmt.NewOptNilString(createdOn),
	}, nil
}

func (h handler) UpdateUser(_ context.Context, req *mgmt.UpdateUserReq, params mgmt.UpdateUserParams) (mgmt.UpdateUserRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	u, ok := h.f.users[params.ID]
	if !ok {
		return nil, userNotFound(params.ID)
	}
	if v, ok := req.GivenName.Get(); ok {
		u.firstName = v
	}
	if v, ok := req.FamilyName.Get(); ok {
		u.lastName = v
	}
	if v, ok := req.IsSuspended.Get(); ok {
		u.isSuspended = v
	}
	return &mgmt.UpdateUserResponse{
		ID:                       mgmt.NewOptString(u.id),
		GivenName:                mgmt.NewOptString(u.firstName),
		FamilyName:               mgmt.NewOptString(u.lastName),
		IsSuspended:              mgmt.NewOptBool(u.isSuspended),
		IsPasswordResetRequested: mgmt.NewOptBool(false),
	}, nil
}

func (h handler) DeleteUser(_ context.Context, params mgmt.DeleteUserParams) (mgmt.DeleteUserRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	if !h.f.userExists(params.ID) {
		return nil, userNotFound(params.ID)
	}
	delete(h.f.users, params.ID)
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Success")}, nil
}

func (h handler) CreateUserIdentity(_ context.Context, req mgmt.OptCreateUserIdentityReq, params mgmt.CreateUserIdentityParams) (mgmt.CreateUserIdentityRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()

	u, ok := h.f.users[params.UserID]
	if !ok {
		return nil, userNotFound(params.UserID)
	}
	r := req.Value
	typ, name := string(r.Type.Value), r.Value.Value
	switch r.Type.Value {
	case mgmt.CreateUserIdentityReqTypeEmail, mgmt.CreateUserIdentityReqTypeUsername:
	case mgmt.CreateUserIdentityReqTypePhone:
		// Kinde takes the national number and its country, and reports the
		// identity in international format.
		country, ok := r.PhoneCountryID.Get()
		if !ok || strings.HasPrefix(name, "+") {
			return nil, invalidUserRequest("a phone identity needs a national number and phone_country_id")
		}
		num, err := phonenumbers.Parse(name, strings.ToUpper(country))
		if err != nil || !phonenumbers.IsValidNumber(num) {
			return nil, invalidUserRequest("invalid phone number " + name)
		}
		name = phonenumbers.Format(num, phonenumbers.E164)
	default:
		return nil, invalidUserRequest("identity type " + typ + " is not supported")
	}
	if name == "" {
		return nil, invalidUserRequest("identity value is required")
	}
	if slices.ContainsFunc(u.identities, func(i identity) bool { return i.typ == typ && i.name == name }) {
		return nil, invalidUserRequest("the user already has identity " + typ + ":" + name)
	}

	id := h.f.newID("identity")
	u.identities = append(u.identities, identity{id: id, typ: typ, name: name})
	return &mgmt.CreateIdentityResponse{
		Code:     mgmt.NewOptString("IDENTITY_CREATED"),
		Message:  mgmt.NewOptString("Identity successfully created"),
		Identity: mgmt.NewOptCreateIdentityResponseIdentity(mgmt.CreateIdentityResponseIdentity{ID: mgmt.NewOptString(id)}),
	}, nil
}

// identityJSON is an identity as GET /api/v1/users/{user_id}/identities
// returns it.
type identityJSON struct {
	ID           string  `json:"id"`
	Type         string  `json:"type"`
	IsConfirmed  *bool   `json:"is_confirmed"`
	CreatedOn    string  `json:"created_on"`
	LastLoginOn  *string `json:"last_login_on"`
	TotalLogins  int     `json:"total_logins"`
	Name         string  `json:"name"`
	Email        *string `json:"email"`
	ConnectionID *string `json:"connection_id"`
	IsPrimary    bool    `json:"is_primary"`
}

// serveUserIdentities answers GET /api/v1/users/{user_id}/identities. It
// sits outside the generated server because the SDK cannot encode the null
// is_confirmed that Kinde's spec documents for username identities, and
// kindeapi decodes this endpoint itself for the same reason.
func (f *Fake) serveUserIdentities(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	userID := r.PathValue("user_id")
	u, ok := f.users[userID]
	if !ok {
		writeAPIError(w, &apiError{status: http.StatusNotFound, code: "USER_NOT_FOUND", message: "kindefake: no user " + userID})
		return
	}

	start := 0
	if after := r.URL.Query().Get("starting_after"); after != "" {
		i := slices.IndexFunc(u.identities, func(i identity) bool { return i.id == after })
		if i < 0 {
			writeAPIError(w, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: unknown starting_after " + after})
			return
		}
		start = i + 1
	}
	end := min(start+identitiesPageSize, len(u.identities))

	page := make([]identityJSON, 0, end-start)
	for i, ident := range u.identities[start:end] {
		out := identityJSON{
			ID:        ident.id,
			Type:      ident.typ,
			CreatedOn: createdOn,
			Name:      ident.name,
			IsPrimary: start+i == 0,
		}
		// The spec says is_confirmed is null for identity types that record no
		// confirmation, such as username, and email is null except for email
		// identities.
		if ident.typ != "username" {
			confirmed := true
			out.IsConfirmed = &confirmed
		}
		if ident.typ == "email" {
			email := ident.name
			out.Email = &email
		}
		page = append(page, out)
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code":       "OK",
		"message":    "Success",
		"identities": page,
		"has_more":   end < len(u.identities),
	})
}
