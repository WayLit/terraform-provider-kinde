package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

func TestAccOrganizationResource(t *testing.T) {
	f := testAccFake(t)
	testName := acctest.RandomWithPrefix("tfacc")
	var code string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccOrganizationResourceConfig(testName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization.test", "name", testName),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "code"),
					resource.TestCheckResourceAttrPair("kinde_organization.test", "id", "kinde_organization.test", "code"),
					// created_on is stored exactly as Kinde returns it.
					resource.TestCheckResourceAttr("kinde_organization.test", "created_on", "2026-01-01T00:00:00Z"),
					// Kinde's default theme.
					resource.TestCheckResourceAttr("kinde_organization.test", "theme_code", "light"),
					resource.TestCheckNoResourceAttr("kinde_organization.test", "background_color"),
					resource.TestCheckResourceAttrWith("kinde_organization.test", "code", func(v string) error {
						code = v
						return nil
					}),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_organization.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccOrganizationResourceConfigUpdate(testName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization.test", "name", testName+"-updated"),
					resource.TestCheckResourceAttrSet("kinde_organization.test", "code"),
					resource.TestCheckResourceAttr("kinde_organization.test", "external_id", testName+"-ext"),
					// Colors are read back in hex form.
					resource.TestCheckResourceAttr("kinde_organization.test", "background_color", "#ffffff"),
					resource.TestCheckResourceAttr("kinde_organization.test", "link_color", "#0056f1"),
					// Kinde's color scheme for this theme is "light dark";
					// theme_code must hold the theme code itself.
					resource.TestCheckResourceAttr("kinde_organization.test", "theme_code", "user_preference"),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveOrganization(code) },
				Config:             testAccOrganizationResourceConfigUpdate(testName),
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: testAccOrganizationResourceConfigUpdate(testName),
				Check:  resource.TestCheckResourceAttrSet("kinde_organization.test", "id"),
			},
			// Delete testing automatically occurs in TestCase
		},
	})
}

func TestAccOrganizationResource_InvalidThemeCode(t *testing.T) {
	testAccFake(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// "light dark" is a color scheme, not a theme code.
			{
				Config: `
resource "kinde_organization" "test" {
	name       = "tfacc-invalid-theme"
	theme_code = "light dark"
}
`,
				PlanOnly:    true,
				ExpectError: regexp.MustCompile(`value must be one of`),
			},
		},
	})
}

func testAccOrganizationResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name = %[1]q
}
`, name)
}

func testAccOrganizationResourceConfigUpdate(name string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name             = "%[1]s-updated"
	external_id      = "%[1]s-ext"
	background_color = "#ffffff"
	link_color       = "#0056f1"
	theme_code       = "user_preference"
}
`, name)
}
