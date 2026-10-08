package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go/api/applications"
)

var (
	_ resource.Resource                = &ApplicationConnectionResource{}
	_ resource.ResourceWithImportState = &ApplicationConnectionResource{}
)

func NewApplicationConnectionResource() resource.Resource {
	return &ApplicationConnectionResource{}
}

type ApplicationConnectionResource struct {
	client *applications.Client
}

type applicationConnectionResourceModel struct {
	ID            types.String `tfsdk:"id"`
	ApplicationID types.String `tfsdk:"application_id"`
	ConnectionID  types.String `tfsdk:"connection_id"`
}

func (r *ApplicationConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application_connection"
}

func (r *ApplicationConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a connection for a Kinde application.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Composite ID of the application connection",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"application_id": schema.StringAttribute{
				MarkdownDescription: "ID of the application",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"connection_id": schema.StringAttribute{
				MarkdownDescription: "ID of the connection to enable",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
		},
	}
}

func (r *ApplicationConnectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.legacy.Applications
}

func (r *ApplicationConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan applicationConnectionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Enable the connection
	err := r.client.EnableConnection(ctx, plan.ApplicationID.ValueString(), plan.ConnectionID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Enabling Connection",
			fmt.Sprintf("Could not enable connection ID %s for application ID %s: %s", plan.ConnectionID.ValueString(), plan.ApplicationID.ValueString(), err),
		)
		return
	}

	// Set ID
	plan.ID = types.StringValue(fmt.Sprintf("%s:%s", plan.ApplicationID.ValueString(), plan.ConnectionID.ValueString()))

	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state applicationConnectionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Get application connections
	connections, err := r.client.GetConnections(ctx, state.ApplicationID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Application Connections",
			fmt.Sprintf("Could not read connections for application ID %s: %s", state.ApplicationID.ValueString(), err),
		)
		return
	}

	// Check if our connection is still enabled
	found := false
	for _, conn := range connections {
		if conn.ID == state.ConnectionID.ValueString() {
			found = true
			break
		}
	}

	if !found {
		resp.State.RemoveResource(ctx)
		return
	}

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	// No updates are possible, all fields require replacement
	var plan applicationConnectionResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	diags = resp.State.Set(ctx, &plan)
	resp.Diagnostics.Append(diags...)
}

func (r *ApplicationConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state applicationConnectionResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DisableConnection(ctx, state.ApplicationID.ValueString(), state.ConnectionID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Disabling Connection",
			fmt.Sprintf("Could not disable connection ID %s for application ID %s: %s", state.ConnectionID.ValueString(), state.ApplicationID.ValueString(), err),
		)
		return
	}
}

func (r *ApplicationConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import format: application_id:connection_id
	idParts, err := splitID(req.ID, 2, "application_id:connection_id")
	if err != nil {
		resp.Diagnostics.AddError(
			"Invalid Import ID",
			err.Error(),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("application_id"), idParts[0])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("connection_id"), idParts[1])...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), req.ID)...)
}
