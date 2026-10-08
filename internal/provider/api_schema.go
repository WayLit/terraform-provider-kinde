// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

type APIResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Name            types.String `tfsdk:"name"`
	Audience        types.String `tfsdk:"audience"`
	IsManagementAPI types.Bool   `tfsdk:"is_management_api"`
}

func flattenAPIResource(api mgmt.GetAPIResponseAPI) APIResourceModel {
	return APIResourceModel{
		ID:              stringValue(api.ID),
		Name:            stringValue(api.Name),
		Audience:        stringValue(api.Audience),
		IsManagementAPI: boolValue(api.IsManagementAPI),
	}
}

type APIDataSourceModel struct {
	ID       types.String `tfsdk:"id"`
	Name     types.String `tfsdk:"name"`
	Audience types.String `tfsdk:"audience"`
}

func flattenAPIDataSource(api mgmt.GetAPIResponseAPI) APIDataSourceModel {
	return APIDataSourceModel{
		ID:       stringValue(api.ID),
		Name:     stringValue(api.Name),
		Audience: stringValue(api.Audience),
	}
}

// getAPI fetches an API's details. Errors from the client, including
// not-found, are returned unchanged.
func getAPI(ctx context.Context, client *kindeapi.Client, id string) (mgmt.GetAPIResponseAPI, error) {
	resp, err := client.GetAPI(ctx, id)
	if err != nil {
		return mgmt.GetAPIResponseAPI{}, err
	}
	api, ok := resp.API.Get()
	if !ok {
		return mgmt.GetAPIResponseAPI{}, fmt.Errorf("kinde GetAPI: the response for %s has no api", id)
	}
	return api, nil
}
