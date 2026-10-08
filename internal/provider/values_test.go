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

func TestBoolValue(t *testing.T) {
	tests := []struct {
		name string
		in   mgmt.OptBool
		want types.Bool
	}{
		{"unset", mgmt.OptBool{}, types.BoolNull()},
		{"false", mgmt.NewOptBool(false), types.BoolValue(false)},
		{"true", mgmt.NewOptBool(true), types.BoolValue(true)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := boolValue(tt.in); !got.Equal(tt.want) {
				t.Fatalf("boolValue = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestNilStringValue(t *testing.T) {
	var null mgmt.OptNilString
	null.SetToNull()
	tests := []struct {
		name string
		in   mgmt.OptNilString
		want types.String
	}{
		{"unset", mgmt.OptNilString{}, types.StringNull()},
		{"null", null, types.StringNull()},
		{"empty", mgmt.NewOptNilString(""), types.StringValue("")},
		{"value", mgmt.NewOptNilString("acme"), types.StringValue("acme")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nilStringValue(tt.in); !got.Equal(tt.want) {
				t.Fatalf("nilStringValue(%+v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
