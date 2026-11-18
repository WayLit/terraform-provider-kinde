// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &UserRoleResource{}
	_ resource.ResourceWithImportState = &UserRoleResource{}
)

func NewUserRoleResource() resource.Resource {
	return &UserRoleResource{}
}

type UserRoleResource struct {
	client *kindeapi.Client
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
	r.client = pd.api
}

func (r *UserRoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan UserRoleResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	code, userID, roleID := plan.OrganizationCode.ValueString(), plan.UserID.ValueString(), plan.RoleID.ValueString()

	// Listing the user's roles in the organization verifies membership.
	if _, err := r.client.GetOrganizationUserRoles(ctx, code, userID); err != nil {
		if kindeapi.HasCode(err, userNotInOrganization) {
			resp.Diagnostics.AddError(
				"User Not in Organization",
				fmt.Sprintf("User %s is not a member of organization %s. Please add the user to the organization before assigning roles.",
					userID,
					code,
				),
			)
			return
		}
		resp.Diagnostics.AddError(
			"Error Checking User Organization Membership",
			fmt.Sprintf("Could not verify if user %s is a member of organization %s: %s",
				userID,
				code,
				err,
			),
		)
		return
	}

	if err := r.client.CreateOrganizationUserRole(ctx, code, userID, roleID); err != nil {
		resp.Diagnostics.AddError(
			"Error Assigning Role to User",
			fmt.Sprintf("Could not assign role %s to user %s in organization %s: %s",
				roleID,
				userID,
				code,
				err,
			),
		)
		return
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s:%s", code, userID, roleID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *UserRoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserRoleResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	roles, err := r.client.GetOrganizationUserRoles(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
	// A user who left the organization holds none of its roles.
	if membershipGone(err) {
		resp.State.RemoveResource(ctx)
		return
	}
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

	if !slices.Contains(organizationUserRoleIDs(roles), state.RoleID.ValueString()) {
		resp.State.RemoveResource(ctx)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
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
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteOrganizationUserRole(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString(), state.RoleID.ValueString())
	if err != nil && !membershipGone(err) {
		resp.Diagnostics.AddError(
			"Error Removing Role from User",
			fmt.Sprintf("Could not remove role %s from user %s in organization %s: %s",
				state.RoleID.ValueString(),
				state.UserID.ValueString(),
				state.OrganizationCode.ValueString(),
				err,
			),
		)
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
