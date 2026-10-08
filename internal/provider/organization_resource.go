package provider

import (
	"context"
	"fmt"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go/api/organizations"
)

var (
	_ resource.Resource                = &OrganizationResource{}
	_ resource.ResourceWithImportState = &OrganizationResource{}
)

func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

type OrganizationResource struct {
	client *organizations.Client
}

type OrganizationResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Code            types.String `tfsdk:"code"`
	Name            types.String `tfsdk:"name"`
	ExternalID      types.String `tfsdk:"external_id"`
	BackgroundColor types.String `tfsdk:"background_color"`
	ButtonColor     types.String `tfsdk:"button_color"`
	ButtonTextColor types.String `tfsdk:"button_text_color"`
	LinkColor       types.String `tfsdk:"link_color"`
	ThemeCode       types.String `tfsdk:"theme_code"`
	Handle          types.String `tfsdk:"handle"`
	CreatedOn       types.String `tfsdk:"created_on"`
}

func (r *OrganizationResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_organization"
}

func (r *OrganizationResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages a Kinde organization.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the organization.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"code": schema.StringAttribute{
				Description: "The organization code.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				Description: "The name of the organization.",
				Required:    true,
			},
			"external_id": schema.StringAttribute{
				Description: "The external ID of the organization.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"background_color": schema.StringAttribute{
				Description: "The background color of the organization's theme.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"button_color": schema.StringAttribute{
				Description: "The button color of the organization's theme.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"button_text_color": schema.StringAttribute{
				Description: "The button text color of the organization's theme.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"link_color": schema.StringAttribute{
				Description: "The link color of the organization's theme.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"theme_code": schema.StringAttribute{
				Description: "The theme code of the organization.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"handle": schema.StringAttribute{
				Description: "The organization handle.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"created_on": schema.StringAttribute{
				Description: "The timestamp when the organization was created.",
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
		},
	}
}

func (r *OrganizationResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.legacy.Organizations
}

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	createParams := organizations.CreateParams{
		Name:   plan.Name.ValueString(),
		Handle: plan.Handle.ValueString(),
	}

	organization, err := r.client.Create(ctx, createParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Organization",
			fmt.Sprintf("Could not create organization: %s", err),
		)
		return
	}

	// Get the created organization to ensure we have all fields
	organization, err = r.client.Get(ctx, organization.Code)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", organization.Code, err),
		)
		return
	}

	// Set values from API response
	if createParams.Code != "" {
		plan.Code = types.StringValue(createParams.Code)
		plan.ID = types.StringValue(createParams.Code)
	} else {
		plan.Code = types.StringValue(organization.Code)
		plan.ID = types.StringValue(organization.Code)
	}
	plan.Name = types.StringValue(organization.Name)
	plan.CreatedOn = types.StringValue(organization.CreatedOn.Format(time.RFC3339))
	plan.ThemeCode = types.StringValue(organization.ColorScheme)

	// Handle optional values
	if organization.Handle != nil {
		plan.Handle = types.StringValue(*organization.Handle)
	} else if createParams.Handle != "" {
		// Fallback to plan value if API doesn't return it
		plan.Handle = types.StringValue(createParams.Handle)
	} else {
		plan.Handle = types.StringNull()
	}

	if organization.ExternalID != nil {
		plan.ExternalID = types.StringValue(*organization.ExternalID)
	} else {
		plan.ExternalID = types.StringNull()
	}

	if organization.BackgroundColor != nil {
		plan.BackgroundColor = types.StringValue(organization.BackgroundColor.Hex)
	} else {
		plan.BackgroundColor = types.StringNull()
	}

	if organization.ButtonColor != nil {
		plan.ButtonColor = types.StringValue(organization.ButtonColor.Hex)
	} else {
		plan.ButtonColor = types.StringNull()
	}

	if organization.ButtonTextColor != nil {
		plan.ButtonTextColor = types.StringValue(organization.ButtonTextColor.Hex)
	} else {
		plan.ButtonTextColor = types.StringNull()
	}

	if organization.LinkColor != nil {
		plan.LinkColor = types.StringValue(organization.LinkColor.Hex)
	} else {
		plan.LinkColor = types.StringNull()
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization, err := r.client.Get(ctx, state.Code.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", state.Code.ValueString(), err),
		)
		return
	}

	// Set known values
	state.Code = types.StringValue(organization.Code)
	state.ID = types.StringValue(organization.Code)
	state.Name = types.StringValue(organization.Name)
	state.CreatedOn = types.StringValue(organization.CreatedOn.Format(time.RFC3339))
	state.ThemeCode = types.StringValue(organization.ColorScheme)

	// Handle optional values
	if organization.Handle != nil {
		state.Handle = types.StringValue(*organization.Handle)
	} else {
		state.Handle = types.StringNull()
	}

	if organization.ExternalID != nil {
		state.ExternalID = types.StringValue(*organization.ExternalID)
	} else {
		state.ExternalID = types.StringNull()
	}

	if organization.BackgroundColor != nil {
		state.BackgroundColor = types.StringValue(organization.BackgroundColor.Hex)
	} else {
		state.BackgroundColor = types.StringNull()
	}

	if organization.ButtonColor != nil {
		state.ButtonColor = types.StringValue(organization.ButtonColor.Hex)
	} else {
		state.ButtonColor = types.StringNull()
	}

	if organization.ButtonTextColor != nil {
		state.ButtonTextColor = types.StringValue(organization.ButtonTextColor.Hex)
	} else {
		state.ButtonTextColor = types.StringNull()
	}

	if organization.LinkColor != nil {
		state.LinkColor = types.StringValue(organization.LinkColor.Hex)
	} else {
		state.LinkColor = types.StringNull()
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateParams := organizations.UpdateParams{
		Name:            plan.Name.ValueString(),
		ExternalID:      plan.ExternalID.ValueString(),
		BackgroundColor: plan.BackgroundColor.ValueString(),
		ButtonColor:     plan.ButtonColor.ValueString(),
		ButtonTextColor: plan.ButtonTextColor.ValueString(),
		LinkColor:       plan.LinkColor.ValueString(),
		ThemeCode:       plan.ThemeCode.ValueString(),
		Handle:          plan.Handle.ValueString(),
	}

	organization, err := r.client.Update(ctx, plan.Code.ValueString(), updateParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Organization",
			fmt.Sprintf("Could not update organization code %s: %s", plan.Code.ValueString(), err),
		)
		return
	}

	// Set known values
	plan.Code = types.StringValue(organization.Code)
	plan.ID = types.StringValue(organization.Code)
	plan.Name = types.StringValue(organization.Name)
	plan.CreatedOn = types.StringValue(organization.CreatedOn.Format(time.RFC3339))
	plan.ThemeCode = types.StringValue(organization.ColorScheme)

	// Handle optional values
	if organization.Handle != nil {
		plan.Handle = types.StringValue(*organization.Handle)
	} else {
		plan.Handle = types.StringNull()
	}

	if organization.ExternalID != nil {
		plan.ExternalID = types.StringValue(*organization.ExternalID)
	} else {
		plan.ExternalID = types.StringNull()
	}

	if organization.BackgroundColor != nil {
		plan.BackgroundColor = types.StringValue(organization.BackgroundColor.Hex)
	} else {
		plan.BackgroundColor = types.StringNull()
	}

	if organization.ButtonColor != nil {
		plan.ButtonColor = types.StringValue(organization.ButtonColor.Hex)
	} else {
		plan.ButtonColor = types.StringNull()
	}

	if organization.ButtonTextColor != nil {
		plan.ButtonTextColor = types.StringValue(organization.ButtonTextColor.Hex)
	} else {
		plan.ButtonTextColor = types.StringNull()
	}

	if organization.LinkColor != nil {
		plan.LinkColor = types.StringValue(organization.LinkColor.Hex)
	} else {
		plan.LinkColor = types.StringNull()
	}

	diags = resp.State.Set(ctx, plan)
	resp.Diagnostics.Append(diags...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, state.Code.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Deleting Organization",
			fmt.Sprintf("Could not delete organization code %s: %s", state.Code.ValueString(), err),
		)
		return
	}
}

func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Get the organization by code
	organization, err := r.client.Get(ctx, req.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", req.ID, err),
		)
		return
	}

	// Create a new state
	var state OrganizationResourceModel

	// Set known values
	state.ID = types.StringValue(organization.Code)
	state.Code = types.StringValue(organization.Code)
	state.Name = types.StringValue(organization.Name)
	state.CreatedOn = types.StringValue(organization.CreatedOn.Format(time.RFC3339))
	state.ThemeCode = types.StringValue(organization.ColorScheme)

	// Handle optional values
	if organization.Handle != nil {
		state.Handle = types.StringValue(*organization.Handle)
	} else {
		state.Handle = types.StringNull()
	}

	if organization.ExternalID != nil {
		state.ExternalID = types.StringValue(*organization.ExternalID)
	} else {
		state.ExternalID = types.StringNull()
	}

	if organization.BackgroundColor != nil {
		state.BackgroundColor = types.StringValue(organization.BackgroundColor.Hex)
	} else {
		state.BackgroundColor = types.StringNull()
	}

	if organization.ButtonColor != nil {
		state.ButtonColor = types.StringValue(organization.ButtonColor.Hex)
	} else {
		state.ButtonColor = types.StringNull()
	}

	if organization.ButtonTextColor != nil {
		state.ButtonTextColor = types.StringValue(organization.ButtonTextColor.Hex)
	} else {
		state.ButtonTextColor = types.StringNull()
	}

	if organization.LinkColor != nil {
		state.LinkColor = types.StringValue(organization.LinkColor.Hex)
	} else {
		state.LinkColor = types.StringNull()
	}

	// Set the state
	diags := resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
