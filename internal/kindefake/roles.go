package kindefake

import (
	"context"
	"net/http"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// role is a role as the fake stores it.
type role struct {
	details mgmt.GetRoleResponseRole
	// permissions holds the IDs of the role's permissions.
	permissions map[string]bool
}

// CreateRole stores a role and answers with its ID.
func (h handler) CreateRole(_ context.Context, req mgmt.OptCreateRoleReq) (mgmt.CreateRoleRes, error) {
	body, _ := req.Get()
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	id := h.f.newID("rol")
	h.f.roles[id] = &role{
		details: mgmt.GetRoleResponseRole{
			ID:            mgmt.NewOptString(id),
			Key:           body.Key,
			Name:          body.Name,
			Description:   body.Description,
			IsDefaultRole: mgmt.NewOptBool(body.IsDefaultRole.Or(false)),
		},
		permissions: map[string]bool{},
	}
	return &mgmt.CreateRolesResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
		Role:    mgmt.NewOptCreateRolesResponseRole(mgmt.CreateRolesResponseRole{ID: mgmt.NewOptString(id)}),
	}, nil
}

// GetRole returns a role without its permissions.
func (h handler) GetRole(_ context.Context, params mgmt.GetRoleParams) (mgmt.GetRoleRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	r, ok := h.f.roles[params.RoleID]
	if !ok {
		return nil, roleNotFound()
	}
	return &mgmt.GetRoleResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
		Role:    mgmt.NewOptGetRoleResponseRole(r.details),
	}, nil
}

// UpdateRoles sets a role's name and key, which Kinde requires, and any
// other fields the request sets.
func (h handler) UpdateRoles(_ context.Context, req mgmt.OptUpdateRolesReq, params mgmt.UpdateRolesParams) (mgmt.UpdateRolesRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	r, ok := h.f.roles[params.RoleID]
	if !ok {
		return nil, roleNotFound()
	}
	body, ok := req.Get()
	if !ok {
		return nil, &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: "kindefake: name and key are required"}
	}
	r.details.Name = mgmt.NewOptString(body.Name)
	r.details.Key = mgmt.NewOptString(body.Key)
	if body.Description.Set {
		r.details.Description = body.Description
	}
	if body.IsDefaultRole.Set {
		r.details.IsDefaultRole = body.IsDefaultRole
	}
	return success(), nil
}

// DeleteRole deletes a role.
func (h handler) DeleteRole(_ context.Context, params mgmt.DeleteRoleParams) (mgmt.DeleteRoleRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.roles[params.RoleID]; !ok {
		return nil, roleNotFound()
	}
	delete(h.f.roles, params.RoleID)
	return success(), nil
}

// GetRolePermissions lists a role's permissions in ID order. Permissions
// deleted since they were added drop out, as they do in Kinde. It ignores
// sort.
func (h handler) GetRolePermissions(_ context.Context, params mgmt.GetRolePermissionsParams) (mgmt.GetRolePermissionsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	r, ok := h.f.roles[params.RoleID]
	if !ok {
		return nil, roleNotFound()
	}
	var perms []mgmt.Permissions
	for _, p := range h.f.sortedPermissions() {
		if r.permissions[p.ID.Value] {
			perms = append(perms, p)
		}
	}
	page, next, err := nextTokenPage(h.f, perms, params.PageSize, params.NextToken)
	if err != nil {
		return nil, err
	}
	res := &mgmt.RolePermissionsResponse{
		Code:        mgmt.NewOptString("OK"),
		Message:     mgmt.NewOptString("Success"),
		Permissions: page,
	}
	if next != "" {
		res.NextToken = mgmt.NewOptString(next)
	}
	return res, nil
}

// UpdateRolePermissions adds each listed permission to a role, or removes it
// when its operation is "delete". It changes nothing if any permission does
// not exist.
func (h handler) UpdateRolePermissions(_ context.Context, req *mgmt.UpdateRolePermissionsReq, params mgmt.UpdateRolePermissionsParams) (mgmt.UpdateRolePermissionsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	r, ok := h.f.roles[params.RoleID]
	if !ok {
		return nil, roleNotFound()
	}
	for _, item := range req.Permissions {
		if _, ok := h.f.permissions[item.ID.Value]; !ok {
			return nil, &apiError{status: http.StatusBadRequest, code: "PERMISSION_NOT_FOUND", message: "kindefake: no permission " + item.ID.Value}
		}
	}
	res := &mgmt.UpdateRolePermissionsResponse{
		Code:    mgmt.NewOptString("OK"),
		Message: mgmt.NewOptString("Success"),
	}
	for _, item := range req.Permissions {
		id := item.ID.Value
		if item.Operation.Value == "delete" {
			delete(r.permissions, id)
			res.PermissionsRemoved = append(res.PermissionsRemoved, id)
			continue
		}
		r.permissions[id] = true
		res.PermissionsAdded = append(res.PermissionsAdded, id)
	}
	return res, nil
}

// RemoveRole deletes a role behind the provider's back.
func (f *Fake) RemoveRole(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.roles, id)
}

func roleNotFound() error {
	return notFound("ROLE_NOT_FOUND", "Role not found")
}
