package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// TestOptString pins the difference between an attribute left out of the
// configuration, which is not sent, and one set to "", which is.
func TestOptString(t *testing.T) {
	tests := []struct {
		name     string
		in       types.String
		want     mgmt.OptString
		wantJSON string
	}{
		{"null", types.StringNull(), mgmt.OptString{}, `{"name":"Reports"}`},
		{"unknown", types.StringUnknown(), mgmt.OptString{}, `{"name":"Reports"}`},
		{"empty", types.StringValue(""), mgmt.NewOptString(""), `{"name":"Reports","description":""}`},
		{"value", types.StringValue("Read reports"), mgmt.NewOptString("Read reports"), `{"name":"Reports","description":"Read reports"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := optString(tt.in)
			if got != tt.want {
				t.Fatalf("optString(%v) = %+v, want %+v", tt.in, got, tt.want)
			}
			req := mgmt.UpdatePermissionsReq{Name: mgmt.NewOptString("Reports"), Description: got}
			body, err := req.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(body) != tt.wantJSON {
				t.Fatalf("request body = %s, want %s", body, tt.wantJSON)
			}
		})
	}
}

func TestStringValue(t *testing.T) {
	tests := []struct {
		name string
		in   mgmt.OptString
		want types.String
	}{
		{"unset", mgmt.OptString{}, types.StringNull()},
		{"empty", mgmt.NewOptString(""), types.StringValue("")},
		{"value", mgmt.NewOptString("Read reports"), types.StringValue("Read reports")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stringValue(tt.in); !got.Equal(tt.want) {
				t.Fatalf("stringValue(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
