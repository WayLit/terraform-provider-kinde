package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// stringValue maps an optional SDK string to Terraform. Unset becomes null.
func stringValue(v mgmt.OptString) types.String {
	if s, ok := v.Get(); ok {
		return types.StringValue(s)
	}
	return types.StringNull()
}

// optString maps a Terraform string to an optional SDK string. Null and
// unknown become unset, so the field is left out of the request and Kinde
// keeps its value; a known "" is sent as "".
func optString(v types.String) mgmt.OptString {
	if v.IsNull() || v.IsUnknown() {
		return mgmt.OptString{}
	}
	return mgmt.NewOptString(v.ValueString())
}

// boolValue converts an optional SDK bool to a Terraform bool. Unset becomes
// null.
func boolValue(v mgmt.OptBool) types.Bool {
	if b, ok := v.Get(); ok {
		return types.BoolValue(b)
	}
	return types.BoolNull()
}

// nilStringValue converts an optional, nullable SDK string to a Terraform
// string. Unset and null both become null.
func nilStringValue(v mgmt.OptNilString) types.String {
	if s, ok := v.Get(); ok {
		return types.StringValue(s)
	}
	return types.StringNull()
}
