// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &OrganizationUserResource{}
	_ resource.ResourceWithImportState = &OrganizationUserResource{}
)

func NewOrganizationUserResource() resource.Resource {
	return &OrganizationUserResource{}
}

type OrganizationUserResource struct {
	client *kindeapi.Client
}

func (r *OrganizationUserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization_user"
}

func (r *OrganizationUserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a user's membership and roles in a Kinde organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The composite ID of the organization user membership.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"organization_code": schema.StringAttribute{
				Required:    true,
				Description: "The code of the organization.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"user_id": schema.StringAttribute{
				Required:    true,
				Description: "The ID of the user.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"roles": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "The list of role IDs to assign to the user.",
			},
			"permissions": schema.ListAttribute{
				ElementType: types.StringType,
				Optional:    true,
				Description: "The list of permission IDs to assign to the user.",
			},
		},
	}
}

func (r *OrganizationUserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *OrganizationUserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	code, userID := plan.OrganizationCode.ValueString(), plan.UserID.ValueString()

	// First, add the user to the organization without roles.
	if err := r.client.AddOrganizationUsers(ctx, code, []string{userID}); err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Organization User",
			fmt.Sprintf("Could not create organization user: %s", err),
		)
		return
	}

	// Then add the roles one by one.
	var roles []string
	if !plan.Roles.IsNull() {
		resp.Diagnostics.Append(plan.Roles.ElementsAs(ctx, &roles, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}
	for _, roleID := range roles {
		if err := r.client.CreateOrganizationUserRole(ctx, code, userID, roleID); err != nil {
			resp.Diagnostics.AddError(
				"Error Adding Role",
				fmt.Sprintf("Could not add role %s: %s", roleID, err),
			)
			return
		}
	}

	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", code, userID))
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationUserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Listing the user's roles also checks membership.
	roles, err := r.client.GetOrganizationUserRoles(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
	if membershipGone(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization User",
			fmt.Sprintf("Could not read organization user: %s", err),
		)
		return
	}

	roleIDs := organizationUserRoleIDs(roles)
	if len(roleIDs) > 0 {
		rolesList, diags := types.ListValueFrom(ctx, types.StringType, roleIDs)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		state.Roles = rolesList
	} else {
		state.Roles = types.ListNull(types.StringType)
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationUserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state OrganizationUserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	if !plan.Roles.Equal(state.Roles) {
		code, userID := state.OrganizationCode.ValueString(), state.UserID.ValueString()

		// Diff against Kinde's current roles, not the prior state.
		current, err := r.client.GetOrganizationUserRoles(ctx, code, userID)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Reading Current Roles",
				fmt.Sprintf("Could not read current roles: %s", err),
			)
			return
		}
		currentRoles := organizationUserRoleIDs(current)

		var desiredRoles []string
		if !plan.Roles.IsNull() {
			resp.Diagnostics.Append(plan.Roles.ElementsAs(ctx, &desiredRoles, false)...)
			if resp.Diagnostics.HasError() {
				return
			}
		}

		for _, roleID := range currentRoles {
			if slices.Contains(desiredRoles, roleID) {
				continue
			}
			if err := r.client.DeleteOrganizationUserRole(ctx, code, userID, roleID); err != nil {
				resp.Diagnostics.AddError(
					"Error Removing Role",
					fmt.Sprintf("Could not remove role %s: %s", roleID, err),
				)
				return
			}
		}
		for _, roleID := range desiredRoles {
			if slices.Contains(currentRoles, roleID) {
				continue
			}
			if err := r.client.CreateOrganizationUserRole(ctx, code, userID, roleID); err != nil {
				resp.Diagnostics.AddError(
					"Error Adding Role",
					fmt.Sprintf("Could not add role %s: %s", roleID, err),
				)
				return
			}
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *OrganizationUserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationUserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.RemoveOrganizationUser(ctx, state.OrganizationCode.ValueString(), state.UserID.ValueString())
	if err != nil && !membershipGone(err) {
		resp.Diagnostics.AddError(
			"Error Removing User from Organization",
			fmt.Sprintf("Could not remove user from organization: %s", err),
		)
	}
}

func (r *OrganizationUserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: organization_code:user_id
	idParts, err := splitID(req.ID, 2, "organization_code:user_id")
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("organization_code"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("user_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
