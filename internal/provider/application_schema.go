// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

type ApplicationDataSourceModel struct {
	ID           types.String `tfsdk:"id"`
	Name         types.String `tfsdk:"name"`
	Type         types.String `tfsdk:"type"`
	ClientID     types.String `tfsdk:"client_id"`
	ClientSecret types.String `tfsdk:"client_secret"`
}

// applicationTypeValue converts an application type read from Kinde to a
// Terraform string. Unset becomes null.
func applicationTypeValue(v mgmt.OptGetApplicationResponseApplicationType) types.String {
	if !v.Set {
		return types.StringNull()
	}
	return types.StringValue(string(v.Value))
}

// uriValue converts a login or homepage URI read from Kinde to a Terraform
// string. Empty and unset both become null.
func uriValue(v mgmt.OptString) types.String {
	if v.Value == "" {
		return types.StringNull()
	}
	return types.StringValue(v.Value)
}

// uriSetValue converts logout or redirect URIs read from Kinde to a set.
// Kinde does not tell "no URIs" from an empty list, so no URIs stay an
// empty set when prior is one and are null otherwise.
func uriSetValue(ctx context.Context, uris []string, prior types.Set) (types.Set, diag.Diagnostics) {
	if len(uris) > 0 {
		return types.SetValueFrom(ctx, types.StringType, uris)
	}
	if prior.IsNull() || prior.IsUnknown() {
		return types.SetNull(types.StringType), nil
	}
	return types.SetValueMust(types.StringType, []attr.Value{}), nil
}

// uriList returns the URIs in s for an update request. A null set gives an
// empty list, which is still sent and clears the URIs in Kinde.
func uriList(ctx context.Context, s types.Set) ([]string, diag.Diagnostics) {
	uris := []string{}
	if s.IsNull() {
		return uris, nil
	}
	diags := s.ElementsAs(ctx, &uris, false)
	return uris, diags
}

// expandApplicationUpdate builds the update request for m. It always sends
// both URI lists, so Kinde ends up with exactly the configured URIs.
func expandApplicationUpdate(ctx context.Context, m applicationResourceModel) (mgmt.UpdateApplicationReq, diag.Diagnostics) {
	var diags diag.Diagnostics
	logoutURIs, d := uriList(ctx, m.LogoutURIs)
	diags.Append(d...)
	redirectURIs, d := uriList(ctx, m.RedirectURIs)
	diags.Append(d...)
	return mgmt.UpdateApplicationReq{
		Name:         optString(m.Name),
		LoginURI:     optString(m.LoginURI),
		HomepageURI:  optString(m.HomepageURI),
		LogoutUris:   logoutURIs,
		RedirectUris: redirectURIs,
	}, diags
}
