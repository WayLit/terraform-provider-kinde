// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccApplicationConnectionResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")
	config := testAccApplicationConnectionResourceConfig(testID)
	var appID, connID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_application_connection.test", "application_id", "kinde_application.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_application_connection.test", "connection_id", "kinde_connection.test", "id"),
					resource.TestCheckResourceAttrWith("kinde_application_connection.test", "application_id", func(v string) error {
						appID = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("kinde_application_connection.test", "connection_id", func(v string) error {
						connID = v
						return nil
					}),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_application_connection.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Disabled outside Terraform: refresh drops it and the plan enables it again.
			{
				PreConfig:          func() { f.RemoveApplicationConnection(appID, connID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying enables it again.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("kinde_application_connection.test", "id"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func testAccApplicationConnectionResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "kinde_application" "test" {
	name = %[1]q
	type = "reg"
}

resource "kinde_connection" "test" {
	name         = %[1]q
	display_name = "Test OAuth2 Connection"
	strategy     = "oauth2:google"
	options = {
		client_id     = "test-client-id"
		client_secret = "test-client-secret"
	}
}

resource "kinde_application_connection" "test" {
	application_id = kinde_application.test.id
	connection_id  = kinde_connection.test.id
}
`, name)
}
