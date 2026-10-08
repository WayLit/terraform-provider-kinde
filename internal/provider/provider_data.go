package provider

import (
	"fmt"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

// providerData is what Configure hands to every resource and data source.
type providerData struct {
	api *kindeapi.Client
}

// providerDataFrom extracts providerData in a resource or data source
// Configure. It returns nil without diagnostics before the provider is
// configured.
func providerDataFrom(data any, diags *diag.Diagnostics) *providerData {
	if data == nil {
		return nil
	}
	pd, ok := data.(*providerData)
	if !ok {
		diags.AddError(
			"Unexpected Configure Type",
			fmt.Sprintf("Expected *providerData, got: %T. Please report this issue to the provider developers.", data),
		)
		return nil
	}
	return pd
}
