// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

type PermissionResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
}

func expandPermissionCreateReq(d PermissionResourceModel) mgmt.CreatePermissionReq {
	return mgmt.CreatePermissionReq{
		Name:        optString(d.Name),
		Key:         optString(d.Key),
		Description: optString(d.Description),
	}
}

func expandPermissionUpdateReq(d PermissionResourceModel) mgmt.UpdatePermissionsReq {
	return mgmt.UpdatePermissionsReq{
		Name:        optString(d.Name),
		Key:         optString(d.Key),
		Description: optString(d.Description),
	}
}

func flattenPermissionResource(permission mgmt.Permissions) PermissionResourceModel {
	return PermissionResourceModel{
		ID:          stringValue(permission.ID),
		Name:        stringValue(permission.Name),
		Key:         stringValue(permission.Key),
		Description: stringValue(permission.Description),
	}
}

// permissionByID returns the permission with the given ID.
func permissionByID(perms []mgmt.Permissions, id string) (mgmt.Permissions, bool) {
	for _, p := range perms {
		if p.ID.Or("") == id {
			return p, true
		}
	}
	return mgmt.Permissions{}, false
}

// permissionByNameAndKey returns the first permission with the given name and
// key.
func permissionByNameAndKey(perms []mgmt.Permissions, name, key string) (mgmt.Permissions, bool) {
	for _, p := range perms {
		if p.Name.Or("") == name && p.Key.Or("") == key {
			return p, true
		}
	}
	return mgmt.Permissions{}, false
}
