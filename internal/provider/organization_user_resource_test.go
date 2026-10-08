package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccOrganizationUserResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-orguser")
	oneRole := testAccOrganizationUserResourceConfig(testID, "[kinde_role.first.id]")
	twoRoles := testAccOrganizationUserResourceConfig(testID, "[kinde_role.first.id, kinde_role.second.id]")
	var orgCode, userID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: oneRole,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "organization_code", "kinde_organization.test", "code"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "user_id", "kinde_user.test", "id"),
					resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "1"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "roles.0", "kinde_role.first", "id"),
					resource.TestCheckResourceAttrWith("kinde_organization.test", "code", func(v string) error {
						orgCode = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("kinde_user.test", "id", func(v string) error {
						userID = v
						return nil
					}),
					// The ID is organization_code:user_id.
					func(s *terraform.State) error {
						return resource.TestCheckResourceAttr("kinde_organization_user.test", "id", orgCode+":"+userID)(s)
					},
				),
			},
			// ImportState testing: the import ID is organization_code:user_id.
			{
				ResourceName: "kinde_organization_user.test",
				ImportState:  true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return orgCode + ":" + userID, nil
				},
				ImportStateVerify: true,
			},
			// An import ID in any other format is rejected.
			{
				ResourceName:  "kinde_organization_user.test",
				ImportState:   true,
				ImportStateId: "org-code-without-user",
				ExpectError:   regexp.MustCompile(`Invalid Import ID`),
			},
			// Adding a role updates the membership in place.
			{
				Config: twoRoles,
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{
						plancheck.ExpectResourceAction("kinde_organization_user.test", plancheck.ResourceActionUpdate),
					},
				},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "2"),
					resource.TestCheckResourceAttrPair("kinde_organization_user.test", "roles.1", "kinde_role.second", "id"),
				),
			},
			// Removed outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveOrganizationUser(orgCode, userID) },
				Config:             twoRoles,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: twoRoles,
				Check:  resource.TestCheckResourceAttr("kinde_organization_user.test", "roles.#", "2"),
			},
		},
	})
}

// testAccOrganizationUserResourceConfig adds a user to an organization with
// roles, an HCL list of role IDs.
func testAccOrganizationUserResourceConfig(testID, roles string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name = %[1]q
}

resource "kinde_user" "test" {
	first_name = "Org"
	last_name  = "Member"

	identities = [
		{
			type  = "email"
			value = "%[1]s@example.com"
		}
	]
}

resource "kinde_role" "first" {
	name        = "%[1]s-first"
	key         = "%[1]s-first"
	description = "First test role"
}

resource "kinde_role" "second" {
	name        = "%[1]s-second"
	key         = "%[1]s-second"
	description = "Second test role"
}

resource "kinde_organization_user" "test" {
	organization_code = kinde_organization.test.code
	user_id           = kinde_user.test.id
	roles             = %[2]s
}
`, testID, roles)
}
