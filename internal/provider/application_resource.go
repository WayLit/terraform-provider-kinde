// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

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
	_ resource.Resource                = &ApplicationResource{}
	_ resource.ResourceWithImportState = &ApplicationResource{}
)

func NewApplicationResource() resource.Resource {
	return &ApplicationResource{}
}

type ApplicationResource struct {
	client *kindeapi.Client
}

type applicationResourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
	LoginURI     types.String `tfsdk:"login_uri"`
	HomepageURI  types.String `tfsdk:"homepage_uri"`
	LogoutURIs   types.Set    `tfsdk:"logout_uris"`
	RedirectURIs types.Set    `tfsdk:"redirect_uris"`
}

func (r *ApplicationResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (r *ApplicationResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Applications facilitates the interface for users to authenticate against. See [documentation](https://docs.kinde.com/build/applications/about-applications/) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the application",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the application. Currently, there is no way to change this via the management application.",
				Required:            true,
			},
			"type": schema.StringAttribute{
				MarkdownDescription: "Type of the application",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "Client id of the application",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "Client secret of the application",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"login_uri": schema.StringAttribute{
				Description: "The login URI of the application.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"homepage_uri": schema.StringAttribute{
				Description: "The homepage URI of the application.",
				Optional:    true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"logout_uris": schema.SetAttribute{
				MarkdownDescription: "The logout URIs of the application. They are read from Kinde, so changes made outside Terraform show as drift. Set to `[]` or remove the attribute to clear them.",
				Optional:            true,
				ElementType:         types.StringType,
			},
			"redirect_uris": schema.SetAttribute{
				MarkdownDescription: "The redirect (callback) URIs of the application. They are read from Kinde, so changes made outside Terraform show as drift. Set to `[]` or remove the attribute to clear them.",
				Optional:            true,
				ElementType:         types.StringType,
			},
		},
	}
}

func (r *ApplicationResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.api
	tflog.Debug(ctx, "Application resource configured")
}

func (r *ApplicationResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan applicationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	created, err := r.client.CreateApplication(ctx, &mgmt.CreateApplicationReq{
		Name: plan.Name.ValueString(),
		Type: mgmt.CreateApplicationReqType(plan.Type.ValueString()),
	})
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating Application",
			fmt.Sprintf("Could not create application: %s", err),
		)
		return
	}
	app, ok := created.Application.Get()
	if !ok || app.ID.Value == "" {
		resp.Diagnostics.AddError("Error Creating Application", "Kinde did not return the new application's ID.")
		return
	}
	plan.ID = stringValue(app.ID)
	plan.ClientID = stringValue(app.ClientID)
	plan.ClientSecret = stringValue(app.ClientSecret)

	// Kinde creates an application from its name and type only; the URIs
	// need a second call.
	if !plan.LoginURI.IsNull() || !plan.HomepageURI.IsNull() || !plan.LogoutURIs.IsNull() || !plan.RedirectURIs.IsNull() {
		update, diags := expandApplicationUpdate(ctx, plan)
		resp.Diagnostics.Append(diags...)
		if resp.Diagnostics.HasError() {
			return
		}
		if err := r.client.UpdateApplication(ctx, app.ID.Value, update); err != nil {
			resp.Diagnostics.AddError(
				"Error Updating Application",
				fmt.Sprintf("Could not update application ID %s: %s", app.ID.Value, err),
			)
			return
		}
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ApplicationResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state applicationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := state.ID.ValueString()
	app, err := r.client.GetApplication(ctx, id)
	var logout *mgmt.LogoutRedirectUrls
	if err == nil {
		logout, err = r.client.GetLogoutURLs(ctx, id)
	}
	var redirect *mgmt.RedirectCallbackUrls
	if err == nil {
		redirect, err = r.client.GetCallbackURLs(ctx, id)
	}
	if kindeapi.IsNotFound(err) {
		resp.State.RemoveResource(ctx)
		return
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Application",
			fmt.Sprintf("Could not read application ID %s: %s", id, err),
		)
		return
	}

	a := app.Application.Value
	state.Name = stringValue(a.Name)
	state.Type = applicationTypeValue(a.Type)
	state.ClientID = stringValue(a.ClientID)
	state.ClientSecret = stringValue(a.ClientSecret)
	state.LoginURI = uriValue(a.LoginURI)
	state.HomepageURI = uriValue(a.HomepageURI)

	var diags diag.Diagnostics
	state.LogoutURIs, diags = uriSetValue(ctx, logout.LogoutUrls, state.LogoutURIs)
	resp.Diagnostics.Append(diags...)
	state.RedirectURIs, diags = uriSetValue(ctx, redirect.RedirectUrls, state.RedirectURIs)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *ApplicationResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan applicationResourceModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}

	update, diags := expandApplicationUpdate(ctx, plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	id := plan.ID.ValueString()
	if err := r.client.UpdateApplication(ctx, id, update); err != nil {
		resp.Diagnostics.AddError(
			"Error Updating Application",
			fmt.Sprintf("Could not update application ID %s: %s", id, err),
		)
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *ApplicationResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state applicationResourceModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.DeleteApplication(ctx, state.ID.ValueString())
	if err != nil && !kindeapi.IsNotFound(err) {
		resp.Diagnostics.AddError(
			"Error Deleting Application",
			fmt.Sprintf("Could not delete application ID %s: %s", state.ID.ValueString(), err),
		)
	}
}

func (r *ApplicationResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
