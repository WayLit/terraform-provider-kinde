// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &RoleResource{}
	_ resource.ResourceWithImportState = &RoleResource{}
)

func NewRoleResource() resource.Resource {
	return &RoleResource{}
}

type RoleResource struct {
	client *kindeapi.Client
}

// kindeRole is a role as Kinde reports it, with the IDs of its permissions.
type kindeRole struct {
	details       mgmt.GetRoleResponseRole
	permissionIDs []string
}

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
				MarkdownDescription: "Key identifier of the role. Changing it updates the role in place.",
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
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *RoleResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan RoleResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Create role with basic details first
	created, err := r.client.CreateRole(ctx, expandRoleCreateReq(plan))
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Role",
			fmt.Sprintf("Could not create role: %s", err),
		)
		return
	}
	roleID := created.Role.Value.ID.Or("")
	if roleID == "" {
		resp.Diagnostics.AddError(
			"Error Creating Role",
			"Kinde created the role but did not return its ID.",
		)
		return
	}

	// Get the complete role data
	role, err := r.getRole(ctx, roleID)
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

		role, err = r.reconcileRolePermissions(ctx, roleID, role.permissionIDs, planPerms)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Setting Role Permissions",
				fmt.Sprintf("Could not reconcile permissions for role %s: %s", roleID, err),
			)
			return
		}
	}

	state, err := flattenRoleResource(ctx, role.details, sortPermissions(role.permissionIDs), plan.Permissions.IsNull())
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
	if errors.Is(err, errRoleNotFound) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Role",
			fmt.Sprintf("Could not read role ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	state, err = flattenRoleResource(ctx, role.details, sortPermissions(role.permissionIDs), state.Permissions.IsNull())
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

	// First update role details. Kinde requires the name and key, so a
	// changed key is applied in place.
	if err := r.client.UpdateRole(ctx, plan.ID.ValueString(), expandRoleUpdateReq(plan)); err != nil {
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
		role, err = r.reconcileRolePermissions(ctx, plan.ID.ValueString(), role.permissionIDs, planPerms)
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Updating Role Permissions",
				fmt.Sprintf("Could not update permissions for role %s: %s", plan.ID.ValueString(), err),
			)
			return
		}
	}

	state, err = flattenRoleResource(ctx, role.details, sortPermissions(role.permissionIDs), plan.Permissions.IsNull())
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

	err := r.client.DeleteRole(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
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
	sortedPermissions := sortStringSlice(role.permissionIDs)

	state, err := flattenRoleResource(ctx, role.details, sortedPermissions, true)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Setting Role State",
			"Could not set role state: "+err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// getRole reads a role and every page of its permissions.
// errRoleNotFound marks a 404 from GetRole itself: the role is gone. A 404
// from the follow-up permissions call is an ordinary error, because dropping
// state there would make the next apply create a duplicate role.
var errRoleNotFound = errors.New("role not found")

func (r *RoleResource) getRole(ctx context.Context, id string) (*kindeRole, error) {
	res, err := r.client.GetRole(ctx, id)
	if kindeapi.IsNotFound(err) {
		return nil, fmt.Errorf("%w: %w", errRoleNotFound, err)
	}
	if err != nil {
		return nil, err
	}
	details, ok := res.Role.Get()
	if !ok {
		return nil, fmt.Errorf("kinde GetRole: the response for role %s has no role", id)
	}

	permissions, err := r.client.ListRolePermissions(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get role permissions: %w", err)
	}
	permissionIDs := make([]string, 0, len(permissions))
	for _, permission := range permissions {
		permissionIDs = append(permissionIDs, permission.ID.Or(""))
	}

	return &kindeRole{details: details, permissionIDs: permissionIDs}, nil
}

func buildRolePermissionOperations(current, desired []string) []mgmt.UpdateRolePermissionsReqPermissionsItem {
	currentSet := make(map[string]struct{}, len(current))
	for _, permissionID := range current {
		currentSet[permissionID] = struct{}{}
	}

	desiredSet := make(map[string]struct{}, len(desired))
	for _, permissionID := range desired {
		desiredSet[permissionID] = struct{}{}
	}

	var operations []mgmt.UpdateRolePermissionsReqPermissionsItem

	for _, permissionID := range sortPermissions(current) {
		if _, keep := desiredSet[permissionID]; !keep {
			operations = append(operations, mgmt.UpdateRolePermissionsReqPermissionsItem{
				ID:        mgmt.NewOptString(permissionID),
				Operation: mgmt.NewOptString("delete"),
			})
		}
	}

	for _, permissionID := range sortPermissions(desired) {
		if _, alreadyPresent := currentSet[permissionID]; !alreadyPresent {
			operations = append(operations, mgmt.UpdateRolePermissionsReqPermissionsItem{
				ID: mgmt.NewOptString(permissionID),
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

func (r *RoleResource) reconcileRolePermissions(ctx context.Context, roleID string, current, desired []string) (*kindeRole, error) {
	operations := buildRolePermissionOperations(current, desired)
	if len(operations) > 0 {
		err := r.client.UpdateRolePermissions(ctx, roleID, mgmt.UpdateRolePermissionsReq{
			Permissions: operations,
		})
		if err != nil {
			return nil, err
		}
	}

	return r.waitForRolePermissions(ctx, roleID, desired)
}

func (r *RoleResource) waitForRolePermissions(ctx context.Context, roleID string, desired []string) (*kindeRole, error) {
	waitCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	var lastRole *kindeRole
	var lastErr error

	for {
		role, err := r.getRole(waitCtx, roleID)
		if err == nil {
			lastRole = role
			if rolePermissionsMatch(role.permissionIDs, desired) {
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
					sortPermissions(lastRole.permissionIDs),
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
