// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccPermissionResource(t *testing.T) {
	f := testAccFake(t)
	var permissionID string
	updated := testAccPermissionResourceConfig("updated-permission", "updated_permission", "Updated test permission description")

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccPermissionResourceConfig("test-permission", "test_permission", "Test permission description"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", "test-permission"),
					resource.TestCheckResourceAttr("kinde_permission.test", "key", "test_permission"),
					resource.TestCheckResourceAttr("kinde_permission.test", "description", "Test permission description"),
					resource.TestCheckResourceAttrWith("kinde_permission.test", "id", func(v string) error {
						permissionID = v
						return nil
					}),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_permission.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing: name and key change in place.
			{
				Config: updated,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", "updated-permission"),
					resource.TestCheckResourceAttr("kinde_permission.test", "key", "updated_permission"),
					resource.TestCheckResourceAttr("kinde_permission.test", "description", "Updated test permission description"),
					resource.TestCheckResourceAttrPtr("kinde_permission.test", "id", &permissionID),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemovePermission(permissionID) },
				Config:             updated,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: updated,
				Check:  resource.TestCheckResourceAttrSet("kinde_permission.test", "id"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccPermissionResource_NoDescription(t *testing.T) {
	testAccFake(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "kinde_permission" "test" {
  name = "no-description"
  key  = "no_description"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_permission.test", "name", "no-description"),
					resource.TestCheckNoResourceAttr("kinde_permission.test", "description"),
				),
			},
			{
				Config: testAccPermissionResourceConfig("no-description", "no_description", "Added later"),
				Check:  resource.TestCheckResourceAttr("kinde_permission.test", "description", "Added later"),
			},
			// Kinde keeps a description once it is set, so removing the
			// attribute leaves the current value instead of planning a change.
			{
				Config: `
resource "kinde_permission" "test" {
  name = "no-description"
  key  = "no_description"
}
`,
				Check: resource.TestCheckResourceAttr("kinde_permission.test", "description", "Added later"),
			},
		},
	})
}

func testAccPermissionResourceConfig(name string, key string, description string) string {
	return fmt.Sprintf(`
resource "kinde_permission" "test" {
  name        = %[1]q
  key         = %[2]q
  description = %[3]q
}
`, name, key, description)
}
