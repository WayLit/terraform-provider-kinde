// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &PermissionResource{}
	_ resource.ResourceWithImportState = &PermissionResource{}
)

func NewPermissionResource() resource.Resource {
	return &PermissionResource{}
}

type PermissionResource struct {
	client *kindeapi.Client
}

func (r *PermissionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_permission"
}

func (r *PermissionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Permissions represent individual access rights that can be assigned to roles. See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/permissions) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the permission",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the permission",
				Required:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Key identifier of the permission",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the permission. Kinde keeps a description once it is set, so removing this attribute leaves the current value in place.",
				Optional:            true,
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *PermissionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *PermissionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan PermissionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.CreatePermission(ctx, expandPermissionCreateReq(plan)); err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Permission",
			fmt.Sprintf("Could not create permission: %s", err),
		)
		return
	}

	// Kinde does not return the new permission's ID, so find it by name and key.
	perms, err := r.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Created Permission",
			fmt.Sprintf("Could not read created permission: %s", err),
		)
		return
	}
	permission, ok := permissionByNameAndKey(perms, plan.Name.ValueString(), plan.Key.ValueString())
	if !ok {
		resp.Diagnostics.AddError(
			"Error Reading Created Permission",
			fmt.Sprintf("Could not find permission with name %q and key %q after creating it", plan.Name.ValueString(), plan.Key.ValueString()),
		)
		return
	}

	state := flattenPermissionResource(permission)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *PermissionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state PermissionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	perms, err := r.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Permission",
			fmt.Sprintf("Could not list permissions: %s", err),
		)
		return
	}

	// Find the permission by ID, falling back to its name and key.
	permission, ok := permissionByID(perms, state.ID.ValueString())
	if !ok && !state.Name.IsNull() && !state.Key.IsNull() {
		permission, ok = permissionByNameAndKey(perms, state.Name.ValueString(), state.Key.ValueString())
	}
	if !ok {
		resp.State.RemoveResource(ctx)
		return
	}

	state = flattenPermissionResource(permission)
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *PermissionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan PermissionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.UpdatePermission(ctx, plan.ID.ValueString(), expandPermissionUpdateReq(plan)); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Permission",
			fmt.Sprintf("Could not update permission ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	perms, err := r.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated Permission",
			fmt.Sprintf("Could not read updated permission: %s", err),
		)
		return
	}
	permission, ok := permissionByID(perms, plan.ID.ValueString())
	if !ok {
		resp.Diagnostics.AddError(
			"Error Reading Updated Permission",
			fmt.Sprintf("Could not find permission ID %s after updating it", plan.ID.ValueString()),
		)
		return
	}

	state := flattenPermissionResource(permission)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *PermissionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state PermissionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeletePermission(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Permission",
			fmt.Sprintf("Could not delete permission ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *PermissionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	perms, err := r.client.ListPermissions(ctx)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Permission",
			fmt.Sprintf("Could not list permissions: %s", err),
		)
		return
	}

	permission, ok := permissionByID(perms, req.ID)
	if !ok {
		resp.Diagnostics.AddError(
			"Error Reading Permission",
			fmt.Sprintf("Could not find permission with ID %s", req.ID),
		)
		return
	}

	state := flattenPermissionResource(permission)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
