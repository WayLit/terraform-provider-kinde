package provider

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &UserResource{}
	_ resource.ResourceWithImportState = &UserResource{}
)

func NewUserResource() resource.Resource {
	return &UserResource{}
}

type UserResource struct {
	client *kindeapi.Client
}

type UserResourceModel struct {
	ID               types.String `tfsdk:"id"`
	FirstName        types.String `tfsdk:"first_name"`
	LastName         types.String `tfsdk:"last_name"`
	IsSuspended      types.Bool   `tfsdk:"is_suspended"`
	OrganizationCode types.String `tfsdk:"organization_code"`
	CreatedOn        types.String `tfsdk:"created_on"`
	Identities       types.Set    `tfsdk:"identities"`
}

// userIdentityModel is one element of the identities attribute.
type userIdentityModel struct {
	Type  string `tfsdk:"type"`
	Value string `tfsdk:"value"`
}

// userIdentityObjectType is the element type of the identities attribute.
var userIdentityObjectType = types.ObjectType{AttrTypes: map[string]attr.Type{
	"type":  types.StringType,
	"value": types.StringType,
}}

func (r *UserResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_user"
}

func (r *UserResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a user within a Kinde organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier for the user.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"first_name": schema.StringAttribute{
				Description:         "The first name of the user.",
				Required:            true,
				MarkdownDescription: "The first name of the user.",
			},
			"last_name": schema.StringAttribute{
				Description:         "The last name of the user.",
				Required:            true,
				MarkdownDescription: "The last name of the user.",
			},
			"is_suspended": schema.BoolAttribute{
				Optional:    true,
				Description: "Whether the user is suspended.",
			},
			"organization_code": schema.StringAttribute{
				Optional:            true,
				Description:         "The code of an organization to add the user to when the user is created. Changing it later has no effect; use kinde_organization_user to manage memberships.",
				MarkdownDescription: "The code of an organization to add the user to when the user is created. Changing it later has no effect; use `kinde_organization_user` to manage memberships.",
			},
			"created_on": schema.StringAttribute{
				Description: "When the user was created, as Kinde reports it (ISO 8601).",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"identities": schema.SetNestedAttribute{
				Description: "Identities for the user (email, username, phone, etc.).",
				Required:    true,
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"type": schema.StringAttribute{
							Description: "The type of identity (email, username, phone, enterprise, social).",
							Required:    true,
						},
						"value": schema.StringAttribute{
							Description: "The value of the identity. Give phone numbers in international format, such as +61412345678.",
							Required:    true,
						},
					},
				},
			},
		},
	}
}

func (r *UserResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *UserResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	tflog.Debug(ctx, "Starting user creation")

	var plan UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Kinde cannot create a suspended user.
	if plan.IsSuspended.ValueBool() {
		resp.Diagnostics.AddError(
			"Invalid Configuration",
			"Setting is_suspended=true when creating a user is not supported. Create the user first, then update the is_suspended attribute.",
		)
		return
	}

	var identities []userIdentityModel
	resp.Diagnostics.Append(plan.Identities.ElementsAs(ctx, &identities, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !hasEmailIdentity(identities) {
		resp.Diagnostics.AddError(
			"Missing Email Identity",
			"At least one email identity must be provided for the user.",
		)
		return
	}

	createReq := mgmt.CreateUserReq{
		Profile: mgmt.NewOptCreateUserReqProfile(mgmt.CreateUserReqProfile{
			GivenName:  optString(plan.FirstName),
			FamilyName: optString(plan.LastName),
		}),
		OrganizationCode: optString(plan.OrganizationCode),
	}
	for _, identity := range identities {
		createReq.Identities = append(createReq.Identities, newCreateUserIdentity(identity))
	}

	created, err := r.client.CreateUser(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating User",
			fmt.Sprintf("Could not create user: %s", err),
		)
		return
	}
	id, ok := created.ID.Get()
	if !ok {
		resp.Diagnostics.AddError("Error Creating User", "Kinde did not return the new user's ID.")
		return
	}
	plan.ID = types.StringValue(id)

	user, err := r.client.GetUserData(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Created User",
			fmt.Sprintf("Could not read created user ID %s: %s", id, err),
		)
		return
	}
	kindeIdentities, err := r.client.GetUserIdentities(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user %s: %s", id, err),
		)
		return
	}

	resp.Diagnostics.Append(plan.setFromKinde(ctx, user, kindeIdentities, identityTypes(identities))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *UserResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()

	user, err := r.client.GetUserData(ctx, id)
	if kindeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User",
			fmt.Sprintf("Could not read user ID %s: %s", id, err),
		)
		return
	}
	kindeIdentities, err := r.client.GetUserIdentities(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user ID %s: %s", id, err),
		)
		return
	}

	// Keep the types state already has; after an import there are none.
	var stateIdentities []userIdentityModel
	if !state.Identities.IsNull() {
		resp.Diagnostics.Append(state.Identities.ElementsAs(ctx, &stateIdentities, false)...)
		if resp.Diagnostics.HasError() {
			return
		}
	}

	resp.Diagnostics.Append(state.setFromKinde(ctx, user, kindeIdentities, identityTypes(stateIdentities))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *UserResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	tflog.Debug(ctx, "Starting user update")

	var plan, state UserResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Check if first_name was previously set and is now being omitted or set to empty
	if !state.FirstName.IsNull() && plan.FirstName.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Cannot Reset First Name",
			"The Kinde API does not allow resetting first_name once it has been set. Please provide the existing first_name value in your configuration.",
		)
	}
	// Check if last_name was previously set and is now being omitted or set to empty
	if !state.LastName.IsNull() && plan.LastName.ValueString() == "" {
		resp.Diagnostics.AddError(
			"Cannot Reset Last Name",
			"The Kinde API does not allow resetting last_name once it has been set. Please provide the existing last_name value in your configuration.",
		)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var planned, current []userIdentityModel
	resp.Diagnostics.Append(plan.Identities.ElementsAs(ctx, &planned, false)...)
	resp.Diagnostics.Append(state.Identities.ElementsAs(ctx, &current, false)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if !hasEmailIdentity(planned) {
		resp.Diagnostics.AddError(
			"Missing Email Identity",
			"At least one email identity must be provided for the user.",
		)
		return
	}

	id := plan.ID.ValueString()
	updateReq := mgmt.UpdateUserReq{
		GivenName:  optString(plan.FirstName),
		FamilyName: optString(plan.LastName),
	}
	// Only send is_suspended when it is configured.
	if !plan.IsSuspended.IsNull() {
		updateReq.IsSuspended = mgmt.NewOptBool(plan.IsSuspended.ValueBool())
	}
	if _, err := r.client.UpdateUser(ctx, id, updateReq); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating User",
			fmt.Sprintf("Could not update user ID %s: %s", id, err),
		)
		return
	}

	// Add identities that are new in the plan. OAuth2 identities belong to
	// Kinde, and identities removed from the configuration stay in Kinde.
	for _, identity := range planned {
		if isOAuth2Identity(identity.Type) || slices.Contains(current, identity) {
			continue
		}
		_, err := r.client.CreateUserIdentity(ctx, id, mgmt.CreateUserIdentityReq{
			Type:  mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqType(identity.Type)),
			Value: mgmt.NewOptString(identity.Value),
		})
		if err != nil {
			resp.Diagnostics.AddError(
				"Error Adding User Identity",
				fmt.Sprintf("Could not add identity to user %s: %s", id, err),
			)
			return
		}
	}

	user, err := r.client.GetUserData(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Updated User",
			fmt.Sprintf("Could not read updated user %s: %s", id, err),
		)
		return
	}
	kindeIdentities, err := r.client.GetUserIdentities(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User Identities",
			fmt.Sprintf("Could not read identities for user %s: %s", id, err),
		)
		return
	}

	resp.Diagnostics.Append(plan.setFromKinde(ctx, user, kindeIdentities, identityTypes(planned))...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *UserResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state UserResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteUser(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting User",
			fmt.Sprintf("Could not delete user ID %s: %s", state.ID.ValueString(), err),
		)
	}
}

// ImportState sets the ID and names. Read, which Terraform calls next, fills
// in created_on and identities; it keeps names only when they are set.
func (r *UserResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	user, err := r.client.GetUserData(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading User",
			fmt.Sprintf("Could not read user ID %s: %s", req.ID, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("first_name"), user.FirstName.Value)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("last_name"), user.LastName.Value)...)
}

// setFromKinde copies Kinde's view of the user into m. first_name, last_name,
// and is_suspended stay null when they are null in m, so settings left out of
// the configuration never show up as drift. knownTypes maps an identity value
// to the type the plan or state gave it.
func (m *UserResourceModel) setFromKinde(ctx context.Context, user *mgmt.User, identities []kindeapi.UserIdentity, knownTypes map[string]string) diag.Diagnostics {
	if !m.FirstName.IsNull() {
		m.FirstName = types.StringValue(user.FirstName.Value)
	}
	if !m.LastName.IsNull() {
		m.LastName = types.StringValue(user.LastName.Value)
	}
	if !m.IsSuspended.IsNull() {
		m.IsSuspended = types.BoolValue(user.IsSuspended.Value)
	}
	if createdOn, ok := user.CreatedOn.Get(); ok {
		m.CreatedOn = types.StringValue(createdOn)
	} else {
		m.CreatedOn = types.StringNull()
	}

	var diags diag.Diagnostics
	m.Identities, diags = userIdentitiesValue(ctx, identities, knownTypes)
	return diags
}

// userIdentitiesValue converts Kinde's identities to the identities
// attribute. It leaves out OAuth2 identities. An identity whose value is in
// knownTypes keeps the type given there instead of Kinde's.
func userIdentitiesValue(ctx context.Context, identities []kindeapi.UserIdentity, knownTypes map[string]string) (types.Set, diag.Diagnostics) {
	elems := make([]userIdentityModel, 0, len(identities))
	for _, identity := range identities {
		if isOAuth2Identity(identity.Type) {
			continue
		}
		typ := identity.Type
		if known, ok := knownTypes[identity.Name]; ok {
			typ = known
		}
		elems = append(elems, userIdentityModel{Type: typ, Value: identity.Name})
	}
	return types.SetValueFrom(ctx, userIdentityObjectType, elems)
}

// isOAuth2Identity reports whether an identity type, such as "oauth2:google",
// is one Kinde adds when the user signs in with a social connection. The
// provider leaves these out of state and never adds them.
func isOAuth2Identity(identityType string) bool {
	return strings.HasPrefix(identityType, "oauth2:")
}

// hasEmailIdentity reports whether identities include an email identity.
func hasEmailIdentity(identities []userIdentityModel) bool {
	return slices.ContainsFunc(identities, func(i userIdentityModel) bool {
		return i.Type == string(mgmt.CreateUserReqIdentitiesItemTypeEmail)
	})
}

// identityTypes maps each identity's value to its type.
func identityTypes(identities []userIdentityModel) map[string]string {
	byValue := make(map[string]string, len(identities))
	for _, identity := range identities {
		byValue[identity.Value] = identity.Type
	}
	return byValue
}

// newCreateUserIdentity converts a configured identity for CreateUser. Kinde
// creates email, phone, and username identities this way; a phone value
// keeps its international format.
func newCreateUserIdentity(identity userIdentityModel) mgmt.CreateUserReqIdentitiesItem {
	var details mgmt.CreateUserReqIdentitiesItemDetails
	switch mgmt.CreateUserReqIdentitiesItemType(identity.Type) {
	case mgmt.CreateUserReqIdentitiesItemTypeEmail:
		details.Email = mgmt.NewOptString(identity.Value)
	case mgmt.CreateUserReqIdentitiesItemTypePhone:
		details.Phone = mgmt.NewOptString(identity.Value)
	case mgmt.CreateUserReqIdentitiesItemTypeUsername:
		details.Username = mgmt.NewOptString(identity.Value)
	}
	return mgmt.CreateUserReqIdentitiesItem{
		Type:    mgmt.NewOptCreateUserReqIdentitiesItemType(mgmt.CreateUserReqIdentitiesItemType(identity.Type)),
		Details: mgmt.NewOptCreateUserReqIdentitiesItemDetails(details),
	}
}
