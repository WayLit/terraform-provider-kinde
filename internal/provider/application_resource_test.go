// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccApplicationResource(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-")
	uri := fmt.Sprintf("http://localhost:%d", acctest.RandIntRange(3000, 4000))

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: fmt.Sprintf(`
				resource "kinde_application" "test" {
					name          = "%[1]s"
					type          = "reg"
					login_uri     = "%[2]s/oauth/login"
					homepage_uri  = "%[2]s"
					logout_uris   = ["%[2]s/oauth/logout"]
					redirect_uris = ["%[2]s/oauth/redirect"]
				}
				`, testID, uri),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_application.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttr("kinde_application.test", "login_uri", uri+"/oauth/login"),
					resource.TestCheckResourceAttr("kinde_application.test", "homepage_uri", uri),
					resource.TestCheckResourceAttr("kinde_application.test", "logout_uris.0", uri+"/oauth/logout"),
					resource.TestCheckResourceAttr("kinde_application.test", "redirect_uris.0", uri+"/oauth/redirect"),
				),
			},
			{
				ResourceName:      "kinde_application.test",
				ImportState:       true,
				ImportStateVerify: true,
				// the api does not return these values
				ImportStateVerifyIgnore: []string{
					"homepage_uri",
					"login_uri",
					"logout_uris",
					"redirect_uris",
				},
			},
		},
	})
}

func TestAccApplicationResource_Connections(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccApplicationResourceConfig_WithConnections(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_application.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_secret"),
					// Check that both connections exist and are linked
					resource.TestCheckResourceAttrSet("kinde_application_connection.password", "id"),
					resource.TestCheckResourceAttrSet("kinde_application_connection.otp", "id"),
					// Verify the application IDs match
					resource.TestCheckResourceAttrPair(
						"kinde_application_connection.password", "application_id",
						"kinde_application.test", "id",
					),
					resource.TestCheckResourceAttrPair(
						"kinde_application_connection.otp", "application_id",
						"kinde_application.test", "id",
					),
				),
			},
		},
	})
}

func testAccApplicationResourceConfig_WithConnections(name string) string {
	return fmt.Sprintf(`
data "kinde_connections" "builtin" {
	filter = "builtin"
}

resource "kinde_application" "test" {
	name = %[1]q
	type = "reg"
}

resource "kinde_application_connection" "password" {
	application_id = kinde_application.test.id
	connection_id  = data.kinde_connections.builtin.connections[index(data.kinde_connections.builtin.connections[*].strategy, "username:password")].id
}

resource "kinde_application_connection" "otp" {
	application_id = kinde_application.test.id
	connection_id  = data.kinde_connections.builtin.connections[index(data.kinde_connections.builtin.connections[*].strategy, "username:otp")].id
}
`, name)
}
