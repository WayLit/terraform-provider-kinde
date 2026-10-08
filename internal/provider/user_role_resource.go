// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go/api/organizations"
)

var (
	_ resource.Resource                = &UserRoleResource{}
	_ resource.ResourceWithImportState = &UserRoleResource{}
)

func NewUserRoleResource() resource.Resource {
	return &UserRoleResource{}
}

type UserRoleResource struct {
	client *organizations.Client
}

type UserRoleResourceModel struct {
	ID               types.String `tfsdk:"id"`
	UserID           types.String `tfsdk:"user_id"`
	RoleID           types.String `tfsdk:"role_id"`
	OrganizationCode types.String `tfsdk:"organization_code"`
}

func (r *UserRoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user_role"
}

func (r *UserRoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Assigns a role to a user within an organization. See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/organizations/post/api/v1/organizations/{org_code}/users/{user_id}/roles) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Computed ID for this role assignment",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"user_id": schema.StringAttribute{
				MarkdownDescription: "ID of the user",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"role_id": schema.StringAttribute{
				MarkdownDescription: "ID of the role to assign",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"organization_code": schema.StringAttribute{
				MarkdownDescription: "Code of the organization",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *UserRoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.legacy.Organizations
}

func (r *UserRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UserRoleResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if user has any roles in the organization (this verifies membership)
	_, err := r.client.GetUserRoles(ctx, plan.OrganizationCode.ValueString(), plan.UserID.ValueString())
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "user_not_in_organization") {
			resp.Diagnostics.AddError(
				"User Not in Organization",
				fmt.Sprintf("User %s is not a member of organization %s. Please add the user to the organization before assigning roles.",
					plan.UserID.ValueString(),
					plan.OrganizationCode.ValueString(),
				),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Error Checking User Organization Membership",
			fmt.Sprintf("Could not verify if user %s is a member of organization %s: %s",
				plan.UserID.ValueString(),
				plan.OrganizationCode.ValueString(),
				err,
			),
		)
		return
	}

	// Assign role to user
	err = r.client.AddUserRole(ctx, plan.OrganizationCode.ValueString(), plan.UserID.ValueString(), plan.RoleID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Assigning Role to User",
			fmt.Sprintf("Could not assign role %s to user %s in organization %s: %s",
				plan.RoleID.ValueString(),
				plan.UserID.ValueString(),
				plan.OrganizationCode.ValueString(),
				err,
			),
		)
		return
	}

	// Generate a composite ID
	plan.ID = types.StringValue(fmt.Sprintf("%s:%s:%s", plan.OrganizationCode.ValueString(), plan.UserID.ValueString(), plan.RoleID.ValueString()))

	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

func (r *UserRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserRoleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get user's roles
	userRoles, err := r.client.GetUserRoles(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Roles",
			fmt.Sprintf("Could not read roles for user %s in organization %s: %s",
				state.UserID.ValueString(),
				state.OrganizationCode.ValueString(),
				err,
			),
		)
		return
	}

	// Check if the role is still assigned
	roleFound := false
	for _, role := range userRoles {
		if role.ID == state.RoleID.ValueString() {
			roleFound = true
			break
		}
	}

	if !roleFound {
		resp.State.RemoveResource(ctx)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *UserRoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// Updates are not supported as all fields require replacement
	resp.Diagnostics.AddError(
		"Update Not Supported",
		"The user_role resource does not support updates. To change the role assignment, delete and recreate the resource.",
	)
}

func (r *UserRoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserRoleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveUserRole(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString(), state.RoleID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Removing Role from User",
			fmt.Sprintf("Could not remove role %s from user %s in organization %s: %s",
				state.RoleID.ValueString(),
				state.UserID.ValueString(),
				state.OrganizationCode.ValueString(),
				err,
			),
		)
		return
	}
}

func (r *UserRoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: organization_code:user_id:role_id
	idParts := strings.Split(req.ID, ":")
	if len(idParts) != 3 {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			"Import ID must be in the format: organization_code:user_id:role_id",
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_code"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("role_id"), idParts[2])...)
}
