package kindefake

import (
	"context"
	"net/http"
	"slices"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// member returns the organization with the given code after checking that
// userID belongs to it. A missing organization or user is a 404; a user
// outside the organization is the 400 USER_NOT_IN_ORGANIZATION that Kinde
// returns. Callers must hold f.mu.
func (f *Fake) member(code, userID string) (*organization, error) {
	org, err := f.findOrganization(code)
	if err != nil {
		return nil, err
	}
	if !f.userExists(userID) {
		return nil, notFound("USER_NOT_FOUND", "User not found")
	}
	if _, ok := org.users[userID]; !ok {
		return nil, &apiError{status: http.StatusBadRequest, code: "USER_NOT_IN_ORGANIZATION", message: "User is not a member of this organization"}
	}
	return org, nil
}

func (h handler) AddOrganizationUsers(_ context.Context, req mgmt.OptAddOrganizationUsersReq, params mgmt.AddOrganizationUsersParams) (mgmt.AddOrganizationUsersRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.findOrganization(params.OrgCode)
	if err != nil {
		return nil, err
	}

	// Check every user before adding any, so a rejected request changes
	// nothing.
	var ids []string
	for _, u := range req.Value.Users {
		id, ok := u.ID.Get()
		switch {
		case !ok || id == "":
			return nil, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "every user needs an id"}
		case len(u.Roles) > 0 || len(u.Permissions) > 0:
			return nil, &apiError{status: http.StatusNotImplemented, code: "NOT_IMPLEMENTED", message: "kindefake: roles and permissions in AddOrganizationUsers are not supported"}
		case !h.f.userExists(id):
			return nil, notFound("USER_NOT_FOUND", "User not found")
		}
		ids = append(ids, id)
	}
	for _, id := range ids {
		if _, ok := org.users[id]; !ok {
			org.users[id] = nil
		}
	}
	return &mgmt.AddOrganizationUsersResponse{
		Code:       mgmt.NewOptString("OK"),
		Message:    mgmt.NewOptString("Users successfully added"),
		UsersAdded: ids,
	}, nil
}

func (h handler) RemoveOrganizationUser(_ context.Context, params mgmt.RemoveOrganizationUserParams) (mgmt.RemoveOrganizationUserRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.member(params.OrgCode, params.UserID)
	if err != nil {
		return nil, err
	}
	delete(org.users, params.UserID)
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("User successfully removed")}, nil
}

// GetOrganizationUserRoles returns role IDs only. The fake does not look up
// roles' keys and names, which the provider does not read.
func (h handler) GetOrganizationUserRoles(_ context.Context, params mgmt.GetOrganizationUserRolesParams) (mgmt.GetOrganizationUserRolesRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.member(params.OrgCode, params.UserID)
	if err != nil {
		return nil, err
	}
	roles := make([]mgmt.OrganizationUserRole, 0, len(org.users[params.UserID]))
	for _, id := range org.users[params.UserID] {
		roles = append(roles, mgmt.OrganizationUserRole{ID: mgmt.NewOptString(id)})
	}
	return &mgmt.GetOrganizationsUserRolesResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
		Roles:   roles,
	}, nil
}

// CreateOrganizationUserRole gives a member a role. Adding a role the member
// already holds changes nothing. The fake does not check that the role
// exists.
func (h handler) CreateOrganizationUserRole(_ context.Context, req *mgmt.CreateOrganizationUserRoleReq, params mgmt.CreateOrganizationUserRoleParams) (mgmt.CreateOrganizationUserRoleRes, error) {
	roleID, ok := req.RoleID.Get()
	if !ok || roleID == "" {
		return nil, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "role_id is required"}
	}
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.member(params.OrgCode, params.UserID)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(org.users[params.UserID], roleID) {
		org.users[params.UserID] = append(org.users[params.UserID], roleID)
	}
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Role successfully added")}, nil
}

func (h handler) DeleteOrganizationUserRole(_ context.Context, params mgmt.DeleteOrganizationUserRoleParams) (mgmt.DeleteOrganizationUserRoleRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	org, err := h.f.member(params.OrgCode, params.UserID)
	if err != nil {
		return nil, err
	}
	roles := org.users[params.UserID]
	i := slices.Index(roles, params.RoleID)
	if i < 0 {
		return nil, notFound("ROLE_NOT_FOUND", "The user does not have this role in the organization")
	}
	org.users[params.UserID] = slices.Delete(roles, i, i+1)
	return &mgmt.SuccessResponse{Code: mgmt.NewOptString("OK"), Message: mgmt.NewOptString("Role successfully removed")}, nil
}

// AddOrganizationUser makes a user a member of an organization behind the
// provider's back, as joining through Kinde's own sign-up would. It does
// nothing if the organization does not exist.
func (f *Fake) AddOrganizationUser(code, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, ok := f.organizations[code]
	if !ok {
		return
	}
	if _, ok := org.users[userID]; !ok {
		org.users[userID] = nil
	}
}

// RemoveOrganizationUser removes a user from an organization behind the
// provider's back.
func (f *Fake) RemoveOrganizationUser(code, userID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if org, ok := f.organizations[code]; ok {
		delete(org.users, userID)
	}
}

// RemoveOrganizationUserRole takes a role from an organization member behind
// the provider's back.
func (f *Fake) RemoveOrganizationUserRole(code, userID, roleID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	org, ok := f.organizations[code]
	if !ok {
		return
	}
	roles, ok := org.users[userID]
	if !ok {
		return
	}
	org.users[userID] = slices.DeleteFunc(roles, func(id string) bool { return id == roleID })
}
