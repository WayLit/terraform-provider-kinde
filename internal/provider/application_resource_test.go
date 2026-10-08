// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/knownvalue"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/statecheck"
	"github.com/hashicorp/terraform-plugin-testing/tfjsonpath"
)

func TestAccApplicationResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-")
	uri := fmt.Sprintf("http://localhost:%d", acctest.RandIntRange(3000, 4000))
	var appID string

	initial := testAccApplicationResourceConfig(testID, uri, fmt.Sprintf(`
	logout_uris   = ["%[1]s/oauth/logout"]
	redirect_uris = ["%[1]s/oauth/redirect"]`, uri))
	updated := testAccApplicationResourceConfig(testID, uri, fmt.Sprintf(`
	logout_uris   = ["%[1]s/oauth/logout", "%[1]s/signed-out"]
	redirect_uris = ["%[1]s/oauth/callback"]`, uri))
	// An empty set and a missing attribute both clear the URIs.
	cleared := testAccApplicationResourceConfig(testID, uri, `
	logout_uris = []`)

	updatedURIs := testAccApplicationURIs(
		knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(uri + "/oauth/logout"), knownvalue.StringExact(uri + "/signed-out")}),
		knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(uri + "/oauth/callback")}),
	)
	updatesInPlace := []plancheck.PlanCheck{
		plancheck.ExpectResourceAction("kinde_application.test", plancheck.ResourceActionUpdate),
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: initial,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_application.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_application.test", "type", "reg"),
					resource.TestCheckResourceAttr("kinde_application.test", "login_uri", uri+"/oauth/login"),
					resource.TestCheckResourceAttr("kinde_application.test", "homepage_uri", uri),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_id"),
					resource.TestCheckResourceAttrSet("kinde_application.test", "client_secret"),
					resource.TestCheckResourceAttrWith("kinde_application.test", "id", func(v string) error {
						appID = v
						return nil
					}),
				),
				ConfigStateChecks: testAccApplicationURIs(
					knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(uri + "/oauth/logout")}),
					knownvalue.SetExact([]knownvalue.Check{knownvalue.StringExact(uri + "/oauth/redirect")}),
				),
			},
			// Every attribute, URIs included, is read from Kinde.
			{
				ResourceName:      "kinde_application.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Changing the URIs updates the application in place.
			{
				Config:            updated,
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: updatesInPlace},
				ConfigStateChecks: updatedURIs,
			},
			// URIs changed outside Terraform show up as drift.
			{
				PreConfig: func() {
					f.SetApplicationURIs(appID, []string{uri + "/elsewhere"}, []string{uri + "/oauth/callback"})
				},
				Config:             updated,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
				ConfigPlanChecks:   resource.ConfigPlanChecks{PostApplyPostRefresh: updatesInPlace},
			},
			// Applying puts the configured URIs back.
			{
				Config:            updated,
				ConfigPlanChecks:  resource.ConfigPlanChecks{PreApply: updatesInPlace},
				ConfigStateChecks: updatedURIs,
			},
			// Clearing the URIs updates the application in place.
			{
				Config:           cleared,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: updatesInPlace},
				ConfigStateChecks: testAccApplicationURIs(
					knownvalue.SetExact([]knownvalue.Check{}),
					knownvalue.Null(),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveApplication(appID) },
				Config:             cleared,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: cleared,
				Check:  resource.TestCheckResourceAttrSet("kinde_application.test", "id"),
			},
		},
	})
}

// testAccApplicationResourceConfig returns a kinde_application with the
// given name, login and homepage URIs under uri, and extra attributes.
func testAccApplicationResourceConfig(name, uri, extra string) string {
	return fmt.Sprintf(`
resource "kinde_application" "test" {
	name         = %[1]q
	type         = "reg"
	login_uri    = "%[2]s/oauth/login"
	homepage_uri = %[2]q
	%[3]s
}
`, name, uri, extra)
}

// testAccApplicationURIs checks kinde_application.test's logout and
// redirect URIs.
func testAccApplicationURIs(logout, redirect knownvalue.Check) []statecheck.StateCheck {
	return []statecheck.StateCheck{
		statecheck.ExpectKnownValue("kinde_application.test", tfjsonpath.New("logout_uris"), logout),
		statecheck.ExpectKnownValue("kinde_application.test", tfjsonpath.New("redirect_uris"), redirect),
	}
}

func TestAccApplicationResource_Connections(t *testing.T) {
	testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
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
