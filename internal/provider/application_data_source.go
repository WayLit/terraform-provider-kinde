// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go/api/applications"
)

var _ datasource.DataSource = &ApplicationDataSource{}

func NewApplicationDataSource() datasource.DataSource {
	return &ApplicationDataSource{}
}

type ApplicationDataSource struct {
	client *applications.Client
}

func (d *ApplicationDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_application"
}

func (d *ApplicationDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Fetches a Kinde application.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Description: "The unique identifier of the application.",
				Required:    true,
			},
			"name": schema.StringAttribute{
				Description: "The name of the application.",
				Computed:    true,
			},
			"type": schema.StringAttribute{
				Description: "The type of the application (reg, spa, or m2m).",
				Computed:    true,
			},
			"client_id": schema.StringAttribute{
				Description: "The client ID of the application.",
				Computed:    true,
			},
			"client_secret": schema.StringAttribute{
				Description: "The client secret of the application.",
				Computed:    true,
				Sensitive:   true,
			},
		},
	}
}

func (d *ApplicationDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	d.client = pd.legacy.Applications
}

func (d *ApplicationDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var state ApplicationDataSourceModel
	diags := req.Config.Get(ctx, &state)
	resp.Diagnostics.Append(diags...)
	if resp.Diagnostics.HasError() {
		return
	}

	app, err := d.client.Get(ctx, state.ID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(
			"Error Reading Application",
			fmt.Sprintf("Could not read application ID %s: %s", state.ID.ValueString(), err),
		)
		return
	}

	state.Name = types.StringValue(app.Name)
	state.Type = types.StringValue(string(app.Type))
	state.ClientID = types.StringValue(app.ClientID)
	state.ClientSecret = types.StringValue(app.ClientSecret)

	diags = resp.State.Set(ctx, &state)
	resp.Diagnostics.Append(diags...)
}
