// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/nxt-fwd/kinde-go/api/apis"
)

var _ datasource.DataSource = (*APIDataSource)(nil)

func NewAPIDataSource() datasource.DataSource {
	return &APIDataSource{}
}

type APIDataSource struct {
	client *apis.Client
}

func (d *APIDataSource) Metadata(ctx context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_api"
}

func (d *APIDataSource) Schema(ctx context.Context, req datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "APIs represent the resource server to authorise against. See [documentation](https://docs.kinde.com/developer-tools/your-apis/register-manage-apis/) for more details.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "ID of the API",
				Required:            true,
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the API. Currently, there is no way to change this via the management API.",
				Computed:            true,
			},
			"audience": schema.StringAttribute{
				MarkdownDescription: "Audience of the API",
				Computed:            true,
			},
		},
	}
}

func (d *APIDataSource) Configure(ctx context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	pd := providerDataFrom(req.ProviderData, &resp.Diagnostics)
	if pd == nil {
		return
	}
	d.client = pd.legacy.APIs
}

func (d *APIDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config APIDataSourceModel
	if resp.Diagnostics.Append(req.Config.Get(ctx, &config)...); resp.Diagnostics.HasError() {
		return
	}

	resource := expandAPIDataSourceModel(config)

	tflog.Debug(ctx, "Reading API", map[string]any{"id": resource.ID})

	resource, err := d.client.Get(ctx, resource.ID)
	if err != nil {
		resp.Diagnostics.AddError("Failed to get API", err.Error())
		return
	}

	tflog.Debug(ctx, "Read API", map[string]any{"resource": resource})

	state := flattenAPIDataSource(resource)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}
