// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/nxt-fwd/kinde-go"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// Ensure KindeProvider satisfies various provider interfaces.
var _ provider.Provider = &KindeProvider{}

// KindeProvider defines the provider implementation.
type KindeProvider struct {
	// version is set to the provider version on release, "dev" when the
	// provider is built and ran locally, and "test" when running acceptance
	// testing.
	version string
}

// KindeProviderModel describes the provider data model.
type KindeProviderModel struct {
	Domain       types.String `tfsdk:"domain"`
	Audience     types.String `tfsdk:"audience"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
}

func (p *KindeProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "kinde"
	resp.Version = p.version
}

func (p *KindeProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Attributes: map[string]schema.Attribute{
			"domain": schema.StringAttribute{
				MarkdownDescription: "Kinde organisation domain, also set by KINDE_DOMAIN",
				Optional:            true,
			},
			"audience": schema.StringAttribute{
				MarkdownDescription: "Kinde M2M application audience, also set by KINDE_AUDIENCE. Defaults to `<domain>/api`.",
				Optional:            true,
			},
			"client_id": schema.StringAttribute{
				MarkdownDescription: "Kinde M2M application client id, also set by KINDE_CLIENT_ID",
				Optional:            true,
			},
			"client_secret": schema.StringAttribute{
				MarkdownDescription: "Kinde M2M application client secret, also set by KINDE_CLIENT_SECRET",
				Optional:            true,
				Sensitive:           true,
			},
		},
	}
}

func (p *KindeProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data KindeProviderModel

	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	client, err := kindeapi.New(kindeapi.Config{
		Domain:       data.Domain.ValueString(),
		Audience:     data.Audience.ValueString(),
		ClientID:     data.ClientID.ValueString(),
		ClientSecret: data.ClientSecret.ValueString(),
	})
	if err == nil {
		err = client.CheckCredentials()
	}
	if err != nil {
		resp.Diagnostics.AddError(
			"Unable to Create Kinde Client",
			fmt.Sprintf("Failed to authenticate with Kinde API: %v\n"+
				"Please verify your domain, client_id, client_secret, and audience are correct.", err),
		)
		return
	}

	opts := kinde.NewClientOptions()
	if !data.Domain.IsNull() && !data.Domain.IsUnknown() {
		opts.WithDomain(data.Domain.ValueString())
	}
	// The legacy client has no default audience, so give it the one the
	// kindeapi client resolved.
	opts.WithAudience(client.Audience())
	if !data.ClientID.IsNull() && !data.ClientID.IsUnknown() {
		opts.WithClientID(data.ClientID.ValueString())
	}
	if !data.ClientSecret.IsNull() && !data.ClientSecret.IsUnknown() {
		opts.WithClientSecret(data.ClientSecret.ValueString())
	}
	legacy := kinde.New(ctx, opts)

	pd := &providerData{api: client, legacy: &legacy}
	resp.DataSourceData = pd
	resp.ResourceData = pd
}

func (p *KindeProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewAPIResource,
		NewApplicationResource,
		NewApplicationConnectionResource,
		NewConnectionResource,
		NewOrganizationResource,
		NewOrganizationUserResource,
		NewRoleResource,
		NewUserResource,
		NewPermissionResource,
		NewUserRoleResource,
	}
}

func (p *KindeProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		NewAPIDataSource,
		NewApplicationDataSource,
		NewConnectionsDataSource,
	}
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &KindeProvider{
			version: version,
		}
	}
}
