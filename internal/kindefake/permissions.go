package kindefake

import (
	"context"
	"slices"
	"strings"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// CreatePermission stores a permission. Like Kinde, it answers with a bare
// success body that carries no ID.
func (h handler) CreatePermission(_ context.Context, req mgmt.OptCreatePermissionReq) (mgmt.CreatePermissionRes, error) {
	body, _ := req.Get()
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	id := h.f.newID("perm")
	h.f.permissions[id] = mgmt.Permissions{
		ID:          mgmt.NewOptString(id),
		Key:         body.Key,
		Name:        body.Name,
		Description: body.Description,
	}
	return success(), nil
}

// GetPermissions lists permissions in ID order. It ignores sort.
func (h handler) GetPermissions(_ context.Context, params mgmt.GetPermissionsParams) (mgmt.GetPermissionsRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	page, next, err := nextTokenPage(h.f, h.f.sortedPermissions(), params.PageSize, params.NextToken)
	if err != nil {
		return nil, err
	}
	res := &mgmt.GetPermissionsResponse{
		Code:        mgmt.NewOptString("OK"),
		Message:     mgmt.NewOptString("Success"),
		Permissions: page,
	}
	if next != "" {
		res.NextToken = mgmt.NewOptString(next)
	}
	return res, nil
}

// UpdatePermissions changes the fields the request sets and leaves the rest
// alone.
func (h handler) UpdatePermissions(_ context.Context, req mgmt.OptUpdatePermissionsReq, params mgmt.UpdatePermissionsParams) (mgmt.UpdatePermissionsRes, error) {
	body, _ := req.Get()
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	p, ok := h.f.permissions[params.PermissionID]
	if !ok {
		return nil, permissionNotFound()
	}
	if body.Name.Set {
		p.Name = body.Name
	}
	if body.Key.Set {
		p.Key = body.Key
	}
	if body.Description.Set {
		p.Description = body.Description
	}
	h.f.permissions[params.PermissionID] = p
	return success(), nil
}

// DeletePermission deletes a permission.
func (h handler) DeletePermission(_ context.Context, params mgmt.DeletePermissionParams) (mgmt.DeletePermissionRes, error) {
	h.f.mu.Lock()
	defer h.f.mu.Unlock()
	if _, ok := h.f.permissions[params.PermissionID]; !ok {
		return nil, permissionNotFound()
	}
	delete(h.f.permissions, params.PermissionID)
	return success(), nil
}

// RemovePermission deletes a permission behind the provider's back.
func (f *Fake) RemovePermission(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.permissions, id)
}

// sortedPermissions returns every permission in ID order. Callers must hold
// f.mu.
func (f *Fake) sortedPermissions() []mgmt.Permissions {
	perms := make([]mgmt.Permissions, 0, len(f.permissions))
	for _, p := range f.permissions {
		perms = append(perms, p)
	}
	slices.SortFunc(perms, func(a, b mgmt.Permissions) int {
		return strings.Compare(a.ID.Value, b.ID.Value)
	})
	return perms
}

func permissionNotFound() error {
	return notFound("PERMISSION_NOT_FOUND", "Permission not found")
}
