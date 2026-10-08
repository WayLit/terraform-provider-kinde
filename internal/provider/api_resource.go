// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/nxt-fwd/kinde-go/api/apis"
)

var (
	_ resource.Resource                = &APIResource{}
	_ resource.ResourceWithImportState = &APIResource{}
)

func NewAPIResource() resource.Resource {
	return &APIResource{}
}

type APIResource struct {
	client *apis.Client
}

func (r *APIResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api"
}

func (r *APIResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "APIs represent the resource server to authorise against. See [documentation](https://docs.kinde.com/developer-tools/your-apis/register-manage-apis/) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the API",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the API. Currently, there is no way to change this via the management API.",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"audience": schema.StringAttribute{
				MarkdownDescription: "Audience of the API",
				Required:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.RequiresReplace()},
			},
			"is_management_api": schema.BoolAttribute{
				MarkdownDescription: "Whether this API is a management API",
				Computed:            true,
			},
		},
	}
}

func (r *APIResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	r.client = pd.legacy.APIs
}

func (r *APIResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan APIResourceModel
	diags := req.Plan.Get(ctx, &plan)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	api := expandAPIResourceModel(plan)
	createParams := apis.CreateParams{
		Name:     api.Name,
		Audience: api.Audience,
	}

	createdAPI, err := r.client.Create(ctx, createParams)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Creating API",
			fmt.Sprintf("Could not create API: %s", err),
		)
		return
	}

	// Get the created API to populate computed fields
	api, err = r.client.Get(ctx, createdAPI.ID)
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading API",
			fmt.Sprintf("Could not read API ID %s: %s", createdAPI.ID, err),
		)
		return
	}

	state := flattenAPIResource(api)
	diags = resp.State.Set(ctx, state)
	resp.Diagnostics.Append(diags...)
}

func (r *APIResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state APIResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	api, err := r.client.Get(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading API",
			fmt.Sprintf("Could not read API ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	state = flattenAPIResource(api)
	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}

func (r *APIResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.AddError(
		"API Update Not Supported",
		"The Kinde API does not support updating APIs. To change the configuration, you must create a new API.",
	)
}

func (r *APIResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state APIResourceModel
	diags := req.State.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	err := r.client.Delete(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Deleting API",
			fmt.Sprintf("Could not delete API ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}
}

func (r *APIResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
