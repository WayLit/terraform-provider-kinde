// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
)

// testAccProtoV6ProviderFactories are used to instantiate a provider during
// acceptance testing. The factory function will be invoked for every Terraform
// CLI command executed to create a provider server to which the CLI can
// reattach.
var testAccProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"kinde": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccFake starts a fake Kinde and points the provider at it through the
// KINDE_* environment variables. Tests that call it must not run in parallel.
func testAccFake(t *testing.T) *kindefake.Fake {
	t.Helper()
	f := kindefake.New(t)
	t.Setenv("KINDE_DOMAIN", f.URL)
	t.Setenv("KINDE_AUDIENCE", f.Audience)
	t.Setenv("KINDE_CLIENT_ID", f.ClientID)
	t.Setenv("KINDE_CLIENT_SECRET", f.ClientSecret)
	return f
}

func TestProviderSchemaMarksClientSecretSensitive(t *testing.T) {
	var resp provider.SchemaResponse
	New("test")().Schema(t.Context(), provider.SchemaRequest{}, &resp)
	attr, ok := resp.Schema.Attributes["client_secret"]
	if !ok || !attr.IsSensitive() {
		t.Fatal("client_secret must be marked sensitive")
	}
}

func TestAccProviderRejectsBadCredentials(t *testing.T) {
	testAccFake(t)
	t.Setenv("KINDE_CLIENT_SECRET", "wrong")
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      `data "kinde_api" "test" { id = "api_0001" }`,
			ExpectError: regexp.MustCompile(`Unable to Create Kinde Client`),
		}},
	})
}
