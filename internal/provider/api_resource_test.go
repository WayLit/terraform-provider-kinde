// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccAPIResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-")
	config := fmt.Sprintf(`
resource "kinde_api" "test" {
	name     = "%[1]s"
	audience = "%[1]s"
}
`, testID)
	var apiID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_api.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_api.test", "audience", testID),
					resource.TestCheckResourceAttr("kinde_api.test", "is_management_api", "false"),
					resource.TestCheckResourceAttrWith("kinde_api.test", "id", func(v string) error { apiID = v; return nil }),
				),
			},
			{
				ResourceName:      "kinde_api.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveAPI(apiID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("kinde_api.test", "id"),
			},
		},
	})
}
