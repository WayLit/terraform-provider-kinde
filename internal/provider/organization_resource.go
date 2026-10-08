package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &OrganizationResource{}
	_ resource.ResourceWithImportState = &OrganizationResource{}
)

func NewOrganizationResource() resource.Resource {
	return &OrganizationResource{}
}

type OrganizationResource struct {
	client *kindeapi.Client
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

// themeCodes lists the theme codes Kinde accepts, from the SDK's enum.
func themeCodes() []string {
	values := mgmt.UpdateOrganizationReqThemeCodeLight.AllValues()
	codes := make([]string, len(values))
	for i, v := range values {
		codes[i] = string(v)
	}
	return codes
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
				Description: "The background color of the organization's theme, as a hex code such as `#ffffff`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"button_color": schema.StringAttribute{
				Description: "The button color of the organization's theme, as a hex code such as `#0056f1`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"button_text_color": schema.StringAttribute{
				Description: "The button text color of the organization's theme, as a hex code such as `#ffffff`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"link_color": schema.StringAttribute{
				Description: "The link color of the organization's theme, as a hex code such as `#0056f1`.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"theme_code": schema.StringAttribute{
				Description: "Whether the organization's pages use light mode, dark mode, or the user's preference: `light`, `dark`, or `user_preference`. Kinde chooses a default when this is not set.",
				Optional:    true,
				Computed:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
				Validators: []validator.String{
					stringvalidator.OneOf(themeCodes()...),
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
				Description: "When the organization was created, in ISO 8601 format as Kinde returns it.",
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
	r.client = pd.api
}

// flattenOrganization copies Kinde's view of an organization into m. Brand
// colors are read in hex form, the form they are configured in.
func flattenOrganization(org *mgmt.GetOrganizationResponse, m *OrganizationResourceModel) {
	m.ID = stringValue(org.Code)
	m.Code = stringValue(org.Code)
	m.Name = stringValue(org.Name)
	m.Handle = nilStringValue(org.Handle)
	m.ExternalID = nilStringValue(org.ExternalID)
	m.CreatedOn = stringValue(org.CreatedOn)

	// Get returns a zero color, whose Hex is unset, for a missing or null
	// color, so stringValue turns it into null.
	background, _ := org.BackgroundColor.Get()
	m.BackgroundColor = stringValue(background.Hex)
	button, _ := org.ButtonColor.Get()
	m.ButtonColor = stringValue(button.Hex)
	buttonText, _ := org.ButtonTextColor.Get()
	m.ButtonTextColor = stringValue(buttonText.Hex)
	link, _ := org.LinkColor.Get()
	m.LinkColor = stringValue(link.Hex)

	if theme, ok := org.ThemeCode.Get(); ok {
		m.ThemeCode = types.StringValue(string(theme))
	} else {
		m.ThemeCode = types.StringNull()
	}
}

func (r *OrganizationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan OrganizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateOrganization(ctx, &mgmt.CreateOrganizationReq{
		Name:            plan.Name.ValueString(),
		Handle:          optString(plan.Handle),
		ExternalID:      optString(plan.ExternalID),
		BackgroundColor: optString(plan.BackgroundColor),
		ButtonColor:     optString(plan.ButtonColor),
		ButtonTextColor: optString(plan.ButtonTextColor),
		LinkColor:       optString(plan.LinkColor),
		ThemeCode:       optString(plan.ThemeCode),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Organization",
			fmt.Sprintf("Could not create organization: %s", err),
		)
		return
	}
	code, ok := created.Organization.Value.Code.Get()
	if !ok || code == "" {
		resp.Diagnostics.AddError(
			"Error Creating Organization",
			"Kinde did not return the new organization's code.",
		)
		return
	}

	organization, err := r.client.GetOrganization(ctx, code)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", code, err),
		)
		return
	}
	flattenOrganization(organization, &plan)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *OrganizationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	organization, err := r.client.GetOrganization(ctx, state.Code.ValueString())
	if kindeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", state.Code.ValueString(), err),
		)
		return
	}
	flattenOrganization(organization, &state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *OrganizationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan OrganizationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	code := plan.Code.ValueString()
	var themeCode mgmt.OptUpdateOrganizationReqThemeCode
	if !plan.ThemeCode.IsNull() && !plan.ThemeCode.IsUnknown() {
		themeCode = mgmt.NewOptUpdateOrganizationReqThemeCode(mgmt.UpdateOrganizationReqThemeCode(plan.ThemeCode.ValueString()))
	}
	err := r.client.UpdateOrganization(ctx, code, &mgmt.UpdateOrganizationReq{
		Name:            mgmt.NewOptString(plan.Name.ValueString()),
		Handle:          optString(plan.Handle),
		ExternalID:      optString(plan.ExternalID),
		BackgroundColor: optString(plan.BackgroundColor),
		ButtonColor:     optString(plan.ButtonColor),
		ButtonTextColor: optString(plan.ButtonTextColor),
		LinkColor:       optString(plan.LinkColor),
		ThemeCode:       themeCode,
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Organization",
			fmt.Sprintf("Could not update organization code %s: %s", code, err),
		)
		return
	}

	organization, err := r.client.GetOrganization(ctx, code)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Organization",
			fmt.Sprintf("Could not read organization code %s: %s", code, err),
		)
		return
	}
	flattenOrganization(organization, &plan)

	resp.Diagnostics.Append(resp.State.Set(ctx, plan)...)
}

func (r *OrganizationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state OrganizationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteOrganization(ctx, state.Code.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Organization",
			fmt.Sprintf("Could not delete organization code %s: %s", state.Code.ValueString(), err),
		)
	}
}

// ImportState takes an organization code. Read fills in the rest, and a code
// that does not exist fails the import.
func (r *OrganizationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("code"), req.ID)...)
}
