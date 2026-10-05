// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go"
	"github.com/nxt-fwd/kinde-go/api/roles"
)

var (
	_ resource.Resource                = &RoleResource{}
	_ resource.ResourceWithImportState = &RoleResource{}
)

func NewRoleResource() resource.Resource {
	return &RoleResource{}
}

type RoleResource struct {
	client *roles.Client
}

type rolePermissionsPage struct {
	Code        string             `json:"code"`
	Message     string             `json:"message"`
	NextToken   string             `json:"next_token"`
	Permissions []roles.Permission `json:"permissions"`
}

func (p rolePermissionsPage) getData() []roles.Permission { return p.Permissions }

func (p rolePermissionsPage) getNextToken() string { return p.NextToken }

func (r *RoleResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_role"
}

func (r *RoleResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Roles represent collections of permissions that can be assigned to users. See [documentation](https://docs.kinde.com/kinde-apis/management/#tag/roles) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the role",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the role",
				Required:            true,
			},
			"key": schema.StringAttribute{
				MarkdownDescription: "Key identifier of the role",
				Required:            true,
			},
			"description": schema.StringAttribute{
				MarkdownDescription: "Description of the role. This field is required because the Kinde API does not properly handle unsetting or empty descriptions once they are set. To maintain consistent behavior and prevent state drift, we require a description for all roles.",
				Required:            true,
			},
			"permissions": schema.SetAttribute{
				MarkdownDescription: "List of permission IDs associated with this role",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *RoleResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	if req.ProviderData == nil {
		return
	}

	client, ok := req.ProviderData.(*kinde.Client)
	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *kinde.Client, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)
		return
	}

	r.client = client.Roles
}

func (r *RoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoleResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create role with basic details first
	createParams := expandRoleCreateParams(plan)
	role, err := r.client.Create(ctx, createParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Role",
			fmt.Sprintf("Could not create role: %s", err),
		)
		return
	}

	// Get the complete role data
	role, err = r.getRole(ctx, role.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Created Role",
			fmt.Sprintf("Could not read created role: %s", err),
		)
		return
	}

	// Update permissions if specified
	var planPerms []string
	if !plan.Permissions.IsNull() {
		diags = plan.Permissions.ElementsAs(ctx, &planPerms, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}

		role, err = r.reconcileRolePermissions(ctx, role.ID, role.Permissions, planPerms)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Setting Role Permissions",
				fmt.Sprintf("Could not reconcile permissions for role %s: %s", role.ID, err),
			)
			return
		}
	}

	state, err := flattenRoleResource(ctx, role, sortPermissions(role.Permissions), plan.Permissions.IsNull())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

// Helper function to sort permissions without modifying original.
func sortPermissions(permissions []string) []string {
	sorted := make([]string, len(permissions))
	copy(sorted, permissions)
	sort.Strings(sorted)
	return sorted
}

func (r *RoleResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state RoleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	role, err := r.getRole(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Role",
			fmt.Sprintf("Could not read role ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	state, err = flattenRoleResource(ctx, role, sortPermissions(role.Permissions), state.Permissions.IsNull())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state RoleResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get current state for comparison
	diags = req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// First update role details
	updateParams := expandRoleUpdateParams(plan)
	_, err := r.client.Update(ctx, plan.ID.ValueString(), updateParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Role",
			fmt.Sprintf("Could not update role ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Handle permissions update if the field is set in the plan
	var planPerms []string
	if !plan.Permissions.IsNull() {
		diags = plan.Permissions.ElementsAs(ctx, &planPerms, false)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	// Get the updated role to ensure we have all fields and permissions
	role, err := r.getRole(ctx, plan.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated Role",
			fmt.Sprintf("Could not read updated role: %s", err),
		)
		return
	}

	if !plan.Permissions.Equal(state.Permissions) {
		role, err = r.reconcileRolePermissions(ctx, plan.ID.ValueString(), role.Permissions, planPerms)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Updating Role Permissions",
				fmt.Sprintf("Could not update permissions for role %s: %s", plan.ID.ValueString(), err),
			)
			return
		}
	}

	state, err = flattenRoleResource(ctx, role, sortPermissions(role.Permissions), plan.Permissions.IsNull())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			fmt.Sprintf("Could not set role state: %s", err),
		)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *RoleResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state RoleResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.client.Delete(ctx, state.ID.ValueString()); err != nil {
		resp.Diagnostics.AddError(
			"Error Deleting Role",
			fmt.Sprintf("Could not delete role ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *RoleResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	role, err := r.getRole(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Kinde Role",
			"Could not read Kinde role ID "+req.ID+": "+err.Error(),
		)
		return
	}

	// Sort the role's permissions for consistent ordering
	sortedPermissions := sortStringSlice(role.Permissions)

	state, err := flattenRoleResource(ctx, role, sortedPermissions, true)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			"Could not set role state: "+err.Error(),
		)
		return
	}

	resp.State.Set(ctx, &state)
}

func (r *RoleResource) getRole(ctx context.Context, id string) (*roles.Role, error) {
	endpoint := fmt.Sprintf("/api/v1/roles/%s", url.PathEscape(id))
	req, err := r.client.NewRequest(ctx, http.MethodGet, endpoint, nil, nil)
	if err != nil {
		return nil, err
	}

	var response struct {
		Code    string     `json:"code"`
		Message string     `json:"message"`
		Role    roles.Role `json:"role"`
	}
	if err := r.client.DoRequest(req, &response); err != nil {
		return nil, err
	}

	permissions, err := r.getRolePermissions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get role permissions: %w", err)
	}
	response.Role.Permissions = permissions

	return &response.Role, nil
}

func (r *RoleResource) getRolePermissions(ctx context.Context, roleID string) ([]string, error) {
	endpoint := fmt.Sprintf("/api/v1/roles/%s/permissions", url.PathEscape(roleID))
	permissions, err := getAllPages[roles.Permission, rolePermissionsPage](ctx, r.client, endpoint, url.Values{
		"page_size": []string{"10"},
	})
	if err != nil {
		return nil, err
	}

	permissionIDs := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		permissionIDs = append(permissionIDs, permission.ID)
	}

	return permissionIDs, nil
}

func buildRolePermissionOperations(current, desired []string) []roles.UpdatePermissionItem {
	currentSet := make(map[string]struct{}, len(current))
	for _, permissionID := range current {
		currentSet[permissionID] = struct{}{}
	}

	desiredSet := make(map[string]struct{}, len(desired))
	for _, permissionID := range desired {
		desiredSet[permissionID] = struct{}{}
	}

	var operations []roles.UpdatePermissionItem

	for _, permissionID := range sortPermissions(current) {
		if _, keep := desiredSet[permissionID]; !keep {
			operations = append(operations, roles.UpdatePermissionItem{
				ID:        permissionID,
				Operation: "delete",
			})
		}
	}

	for _, permissionID := range sortPermissions(desired) {
		if _, alreadyPresent := currentSet[permissionID]; !alreadyPresent {
			operations = append(operations, roles.UpdatePermissionItem{
				ID: permissionID,
			})
		}
	}

	return operations
}

func rolePermissionsMatch(actual, desired []string) bool {
	actualSorted := sortPermissions(actual)
	desiredSorted := sortPermissions(desired)

	if len(actualSorted) != len(desiredSorted) {
		return false
	}

	for i := range actualSorted {
		if actualSorted[i] != desiredSorted[i] {
			return false
		}
	}

	return true
}

func (r *RoleResource) reconcileRolePermissions(ctx context.Context, roleID string, current, desired []string) (*roles.Role, error) {
	operations := buildRolePermissionOperations(current, desired)
	if len(operations) > 0 {
		_, err := r.client.UpdatePermissions(ctx, roleID, roles.UpdatePermissionsParams{
			Permissions: operations,
		})
		if err != nil {
			return nil, err
		}
	}

	return r.waitForRolePermissions(ctx, roleID, desired)
}

func (r *RoleResource) waitForRolePermissions(ctx context.Context, roleID string, desired []string) (*roles.Role, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastRole *roles.Role
	var lastErr error

	for {
		role, err := r.getRole(waitCtx, roleID)
		if err == nil {
			lastRole = role
			if rolePermissionsMatch(role.Permissions, desired) {
				return role, nil
			}
		} else {
			lastErr = err
		}

		select {
		case <-waitCtx.Done():
			if lastRole != nil {
				return nil, fmt.Errorf(
					"timed out waiting for role permissions to converge for role %s: desired=%v observed=%v",
					roleID,
					sortPermissions(desired),
					sortPermissions(lastRole.Permissions),
				)
			}
			if lastErr != nil {
				return nil, fmt.Errorf("timed out waiting to read updated role %s: %w", roleID, lastErr)
			}
			return nil, fmt.Errorf("timed out waiting for role permissions to converge for role %s", roleID)
		case <-ticker.C:
		}
	}
}
