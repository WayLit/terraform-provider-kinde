// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

type RoleResourceModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Key         types.String `tfsdk:"key"`
	Description types.String `tfsdk:"description"`
	Permissions types.Set    `tfsdk:"permissions"`
}

func expandRoleCreateReq(plan RoleResourceModel) mgmt.CreateRoleReq {
	return mgmt.CreateRoleReq{
		Name:        optString(plan.Name),
		Key:         optString(plan.Key),
		Description: optString(plan.Description),
	}
}

// expandRoleUpdateReq builds a role update. Kinde requires the name and key
// on every update, which is also how a changed key is applied in place.
func expandRoleUpdateReq(plan RoleResourceModel) mgmt.UpdateRolesReq {
	return mgmt.UpdateRolesReq{
		Name:        plan.Name.ValueString(),
		Key:         plan.Key.ValueString(),
		Description: optString(plan.Description),
	}
}

func flattenRolePermissions(ctx context.Context, permissions []string, nullWhenEmpty bool) (types.Set, error) {
	if len(permissions) == 0 && nullWhenEmpty {
		return types.SetNull(types.StringType), nil
	}
	if permissions == nil {
		permissions = []string{}
	}

	permissionsSet, diags := types.SetValueFrom(ctx, types.StringType, permissions)
	if diags.HasError() {
		return types.Set{}, fmt.Errorf("failed to flatten permissions: %v", diags)
	}

	return permissionsSet, nil
}

func flattenRoleResource(ctx context.Context, role mgmt.GetRoleResponseRole, permissions []string, nullWhenEmpty bool) (RoleResourceModel, error) {
	permissionsSet, err := flattenRolePermissions(ctx, permissions, nullWhenEmpty)
	if err != nil {
		return RoleResourceModel{}, err
	}

	return RoleResourceModel{
		ID:          stringValue(role.ID),
		Name:        stringValue(role.Name),
		Key:         stringValue(role.Key),
		Description: stringValue(role.Description),
		Permissions: permissionsSet,
	}, nil
}
