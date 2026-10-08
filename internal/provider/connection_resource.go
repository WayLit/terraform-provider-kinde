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
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

var (
	_ resource.Resource                = &ConnectionResource{}
	_ resource.ResourceWithImportState = &ConnectionResource{}
)

func NewConnectionResource() resource.Resource {
	return &ConnectionResource{}
}

type ConnectionResource struct {
	client *kindeapi.Client
}

// ConnectionOptionsModel represents OAuth2 connection options.
type ConnectionOptionsModel struct {
	ClientID     types.String `tfsdk:"client_id" json:"client_id,omitempty"`
	ClientSecret types.String `tfsdk:"client_secret" json:"client_secret,omitempty"`
}

// IsEmpty returns true if both fields are null or empty.
func (m *ConnectionOptionsModel) IsEmpty() bool {
	if m == nil {
		return true
	}
	// Consider both null and empty string as empty
	isClientIDEmpty := m.ClientID.IsNull() || m.ClientID.ValueString() == ""
	isClientSecretEmpty := m.ClientSecret.IsNull() || m.ClientSecret.ValueString() == ""
	return isClientIDEmpty && isClientSecretEmpty
}

// Validate ensures both fields are either both set or both null.
func (m *ConnectionOptionsModel) Validate() error {
	if m == nil {
		return nil
	}

	// If either field is set, both must be set
	if (!m.ClientID.IsNull() || !m.ClientSecret.IsNull()) &&
		(m.ClientID.IsNull() || m.ClientSecret.IsNull()) {
		return fmt.Errorf("both client_id and client_secret must be set if either is provided")
	}

	return nil
}

// createOptions converts the model to social-connection options for a
// create request. Unset fields are sent as "", so removing them from the
// configuration clears them in Kinde.
func (m *ConnectionOptionsModel) createOptions() mgmt.OptCreateConnectionReqOptions {
	return mgmt.NewOptCreateConnectionReqOptions(mgmt.NewCreateConnectionReqOptions0CreateConnectionReqOptions(
		mgmt.CreateConnectionReqOptions0{
			ClientID:     mgmt.NewOptString(m.ClientID.ValueString()),
			ClientSecret: mgmt.NewOptString(m.ClientSecret.ValueString()),
		},
	))
}

// updateOptions is createOptions for an update request.
func (m *ConnectionOptionsModel) updateOptions() mgmt.OptUpdateConnectionReqOptions {
	return mgmt.NewOptUpdateConnectionReqOptions(mgmt.NewUpdateConnectionReqOptions0UpdateConnectionReqOptions(
		mgmt.UpdateConnectionReqOptions0{
			ClientID:     mgmt.NewOptString(m.ClientID.ValueString()),
			ClientSecret: mgmt.NewOptString(m.ClientSecret.ValueString()),
		},
	))
}

// isSocialStrategy reports whether strategy is an OAuth2 social connection,
// the only kind whose options this resource manages.
func isSocialStrategy(strategy string) bool {
	switch mgmt.CreateConnectionReqStrategy(strategy) {
	case mgmt.CreateConnectionReqStrategyOAuth2Apple,
		mgmt.CreateConnectionReqStrategyOAuth2AzureAd,
		mgmt.CreateConnectionReqStrategyOAuth2Bitbucket,
		mgmt.CreateConnectionReqStrategyOAuth2Discord,
		mgmt.CreateConnectionReqStrategyOAuth2Facebook,
		mgmt.CreateConnectionReqStrategyOAuth2Github,
		mgmt.CreateConnectionReqStrategyOAuth2Gitlab,
		mgmt.CreateConnectionReqStrategyOAuth2Google,
		mgmt.CreateConnectionReqStrategyOAuth2Linkedin,
		mgmt.CreateConnectionReqStrategyOAuth2Microsoft,
		mgmt.CreateConnectionReqStrategyOAuth2Patreon,
		mgmt.CreateConnectionReqStrategyOAuth2Slack,
		mgmt.CreateConnectionReqStrategyOAuth2Stripe,
		mgmt.CreateConnectionReqStrategyOAuth2Twitch,
		mgmt.CreateConnectionReqStrategyOAuth2Twitter,
		mgmt.CreateConnectionReqStrategyOAuth2Xero:
		return true
	default:
		return false
	}
}

// ConnectionResourceModel represents the resource model.
type ConnectionResourceModel struct {
	ID          types.String            `tfsdk:"id"`
	Name        types.String            `tfsdk:"name"`
	DisplayName types.String            `tfsdk:"display_name"`
	Strategy    types.String            `tfsdk:"strategy"`
	Options     *ConnectionOptionsModel `tfsdk:"options"`
}

// Equal compares two ConnectionResourceModel instances.
func (m *ConnectionResourceModel) Equal(other *ConnectionResourceModel) bool {
	if m == nil && other == nil {
		return true
	}
	if m == nil || other == nil {
		return false
	}

	if !m.ID.Equal(other.ID) ||
		!m.Name.Equal(other.Name) ||
		!m.DisplayName.Equal(other.DisplayName) ||
		!m.Strategy.Equal(other.Strategy) {
		return false
	}

	// Handle options comparison
	if m.Options == nil && other.Options == nil {
		return true
	}
	if m.Options == nil || other.Options == nil {
		return false
	}
	if m.Options.IsEmpty() && other.Options.IsEmpty() {
		return true
	}

	// For sensitive fields, we need special handling
	// If both values are set (not null), consider them equal
	// This prevents unnecessary updates when the actual values aren't changing
	clientIDEqual := m.Options.ClientID.IsNull() && other.Options.ClientID.IsNull() ||
		(!m.Options.ClientID.IsNull() && !other.Options.ClientID.IsNull())

	clientSecretEqual := m.Options.ClientSecret.IsNull() && other.Options.ClientSecret.IsNull() ||
		(!m.Options.ClientSecret.IsNull() && !other.Options.ClientSecret.IsNull())

	return clientIDEqual && clientSecretEqual
}

// Plan modifier for options.
type optionsEmptyPreserveModifier struct{}

func (m optionsEmptyPreserveModifier) Description(ctx context.Context) string {
	return "Handles options removal and preserves plan values since API never returns sensitive values."
}

func (m optionsEmptyPreserveModifier) MarkdownDescription(ctx context.Context) string {
	return "Handles options removal and preserves plan values since API never returns sensitive values."
}

func (m optionsEmptyPreserveModifier) PlanModifyObject(ctx context.Context, req planmodifier.ObjectRequest, resp *planmodifier.ObjectResponse) {
	// If config has a value, use it
	if !req.ConfigValue.IsNull() {
		resp.PlanValue = req.ConfigValue
		return
	}

	// If state has a value, preserve it
	if !req.StateValue.IsNull() {
		resp.PlanValue = req.StateValue
		return
	}

	// Otherwise, use null
	resp.PlanValue = req.ConfigValue
}

func (r *ConnectionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_connection"
}

func (r *ConnectionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a connection in Kinde.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the connection",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the connection",
				Required:            true,
			},
			"display_name": schema.StringAttribute{
				MarkdownDescription: "Display name of the connection",
				Required:            true,
			},
			"strategy": schema.StringAttribute{
				MarkdownDescription: "Strategy of the connection",
				Required:            true,
			},
			"options": schema.SingleNestedAttribute{
				MarkdownDescription: "Options for the connection. Required for OAuth2 connections. Sensitive values are stored in state and rely on state encryption for security.",
				Optional:            true,
				PlanModifiers:       []planmodifier.Object{&optionsEmptyPreserveModifier{}},
				Attributes: map[string]schema.Attribute{
					"client_id": schema.StringAttribute{
						Optional:  true,
						Sensitive: true,
					},
					"client_secret": schema.StringAttribute{
						Optional:  true,
						Sensitive: true,
					},
				},
			},
		},
	}
}

func (r *ConnectionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
}

func (r *ConnectionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan ConnectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	createReq := &mgmt.CreateConnectionReq{
		Name:        optString(plan.Name),
		DisplayName: optString(plan.DisplayName),
		Strategy:    mgmt.NewOptCreateConnectionReqStrategy(mgmt.CreateConnectionReqStrategy(plan.Strategy.ValueString())),
	}
	if plan.Options != nil {
		if !isSocialStrategy(plan.Strategy.ValueString()) {
			resp.Diagnostics.AddError(
				"Error Converting Options",
				fmt.Sprintf("Could not convert options: unsupported strategy: %s", plan.Strategy.ValueString()),
			)
			return
		}
		createReq.Options = plan.Options.createOptions()
	}

	created, err := r.client.CreateConnection(ctx, createReq)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Connection",
			fmt.Sprintf("Could not create connection: %s", err),
		)
		return
	}
	id, ok := created.Connection.Value.ID.Get()
	if !ok || id == "" {
		resp.Diagnostics.AddError("Error Creating Connection", "Kinde did not return the new connection's ID.")
		return
	}

	// Set ID from response, keep other fields from plan including options
	plan.ID = types.StringValue(id)

	// Store plan in state, including options with sensitive values
	// We'll rely on state encryption for security
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConnectionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state ConnectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	got, err := r.client.GetConnection(ctx, state.ID.ValueString())
	if err != nil {
		if kindeapi.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError(
			"Error Reading Connection",
			fmt.Sprintf("Could not read connection ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
	conn, ok := got.Connection.Get()
	if !ok {
		resp.Diagnostics.AddError(
			"Error Reading Connection",
			fmt.Sprintf("Kinde returned no connection for ID %s.", state.ID.ValueString()),
		)
		return
	}

	// Set basic fields from API response
	state.Name = stringValue(conn.Name)
	state.DisplayName = stringValue(conn.DisplayName)
	state.Strategy = stringValue(conn.Strategy)

	// API doesn't return sensitive options, so preserve them from state
	// We're relying on state encryption for security

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ConnectionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan ConnectionResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	updateReq := &mgmt.UpdateConnectionReq{
		Name:        optString(plan.Name),
		DisplayName: optString(plan.DisplayName),
	}
	if plan.Options != nil {
		if !isSocialStrategy(plan.Strategy.ValueString()) {
			resp.Diagnostics.AddError(
				"Error Converting Options",
				fmt.Sprintf("Could not convert options: unsupported strategy: %s", plan.Strategy.ValueString()),
			)
			return
		}
		updateReq.Options = plan.Options.updateOptions()
	}

	if err := r.client.UpdateConnection(ctx, plan.ID.ValueString(), updateReq); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Connection",
			fmt.Sprintf("Could not update connection ID %s: %s", plan.ID.ValueString(), err),
		)
		return
	}

	// Store plan in state, including options with sensitive values
	// We'll rely on state encryption for security
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ConnectionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state ConnectionResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteConnection(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Connection",
			fmt.Sprintf("Could not delete connection ID %s: %s", state.ID.ValueString(), err),
		)
	}
}

func (r *ConnectionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import just the ID, the Read method will handle the rest
	// Note that sensitive options won't be imported and will need to be set in configuration
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)

	// Add a warning about sensitive values
	resp.Diagnostics.AddWarning(
		"Sensitive Values Not Imported",
		"Sensitive connection options like client_id and client_secret cannot be imported and must be set in your configuration. "+
			"After import, you'll need to set these values in your configuration before making any changes that would trigger an update.",
	)
}

func (r *ConnectionResource) ValidateConfig(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var data ConnectionResourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// Validate strategy
	if !data.Strategy.IsNull() {
		if strings.HasPrefix(data.Strategy.ValueString(), "oauth2:") {
			// Validate options if present
			if data.Options != nil {
				if err := data.Options.Validate(); err != nil {
					resp.Diagnostics.AddError(
						"Invalid Options Configuration",
						err.Error(),
					)
				}
			}
		}
	}
}
