package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

// notFoundCase reads a resource whose Kinde object does not exist.
type notFoundCase struct {
	name     string
	resource func() resource.Resource
	// attrs are the state attributes Read needs, such as "id". Every other
	// attribute is null.
	attrs map[string]string
}

// TestReadRemovesMissingObjects checks that Read removes a resource from
// state, without an error, when its object no longer exists in Kinde.
func TestReadRemovesMissingObjects(t *testing.T) {
	tests := []notFoundCase{
		// Each domain task adds its resources here as they move to kindeapi.
		{name: "kinde_permission", resource: NewPermissionResource, attrs: map[string]string{"id": "perm_missing"}},
		{name: "kinde_role", resource: NewRoleResource, attrs: map[string]string{"id": "rol_missing"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := kindefake.New(t)
			client, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: f.ClientSecret})
			if err != nil {
				t.Fatal(err)
			}

			r := tt.resource()
			if rc, ok := r.(resource.ResourceWithConfigure); ok {
				var cresp resource.ConfigureResponse
				rc.Configure(t.Context(), resource.ConfigureRequest{ProviderData: &providerData{api: client}}, &cresp)
				requireNoErrors(t, cresp.Diagnostics)
			}
			var sresp resource.SchemaResponse
			r.Schema(t.Context(), resource.SchemaRequest{}, &sresp)

			state := stateWithAttrs(t, sresp.Schema, tt.attrs)
			resp := resource.ReadResponse{State: tfsdk.State{Schema: sresp.Schema, Raw: state.Raw.Copy()}}
			r.Read(t.Context(), resource.ReadRequest{State: state}, &resp)
			requireNoErrors(t, resp.Diagnostics)
			if !resp.State.Raw.IsNull() {
				t.Fatalf("expected %s to be removed from state", tt.name)
			}
		})
	}
}

// stateWithAttrs builds state for s with the given string attributes set and
// every other attribute null.
func stateWithAttrs(t *testing.T, s schema.Schema, attrs map[string]string) tfsdk.State {
	t.Helper()
	objType, ok := s.Type().TerraformType(t.Context()).(tftypes.Object)
	if !ok {
		t.Fatalf("schema type is not an object")
	}
	for name := range attrs {
		if _, ok := objType.AttributeTypes[name]; !ok {
			t.Fatalf("schema has no attribute %q", name)
		}
	}
	values := make(map[string]tftypes.Value, len(objType.AttributeTypes))
	for name, typ := range objType.AttributeTypes {
		if v, ok := attrs[name]; ok {
			values[name] = tftypes.NewValue(typ, v)
		} else {
			values[name] = tftypes.NewValue(typ, nil)
		}
	}
	return tfsdk.State{Schema: s, Raw: tftypes.NewValue(objType, values)}
}

func requireNoErrors(t *testing.T, diags diag.Diagnostics) {
	t.Helper()
	if diags.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diags)
	}
}
