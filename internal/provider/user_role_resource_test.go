package provider

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccUserRoleResource(t *testing.T) {
	f := testAccFake(t)
	testID := acctest.RandomWithPrefix("tfacc-userrole")
	base := testAccUserRoleBaseConfig(testID)
	withRole := base + testAccUserRoleResourceConfig
	var orgCode, userID, roleID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// The organization, user, and role exist, but the user has not
			// joined the organization.
			{
				Config: base,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("kinde_organization.test", "code", func(v string) error {
						orgCode = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("kinde_user.test", "id", func(v string) error {
						userID = v
						return nil
					}),
					resource.TestCheckResourceAttrWith("kinde_role.test", "id", func(v string) error {
						roleID = v
						return nil
					}),
				),
			},
			// A user outside the organization cannot get a role in it.
			{
				Config:      withRole,
				ExpectError: regexp.MustCompile(`User Not in Organization`),
			},
			// Once the user has joined, the role is assigned.
			{
				PreConfig: func() { f.AddOrganizationUser(orgCode, userID) },
				Config:    withRole,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "organization_code", "kinde_organization.test", "code"),
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "user_id", "kinde_user.test", "id"),
					resource.TestCheckResourceAttrPair("kinde_user_role.test", "role_id", "kinde_role.test", "id"),
					// The ID is organization_code:user_id:role_id.
					func(s *terraform.State) error {
						return resource.TestCheckResourceAttr("kinde_user_role.test", "id", orgCode+":"+userID+":"+roleID)(s)
					},
				),
			},
			// ImportState testing: the import ID is organization_code:user_id:role_id.
			{
				ResourceName: "kinde_user_role.test",
				ImportState:  true,
				ImportStateIdFunc: func(*terraform.State) (string, error) {
					return orgCode + ":" + userID + ":" + roleID, nil
				},
				ImportStateVerify: true,
			},
			// An import ID in any other format is rejected.
			{
				ResourceName:  "kinde_user_role.test",
				ImportState:   true,
				ImportStateId: "org-code:user-id",
				ExpectError:   regexp.MustCompile(`Invalid Import ID`),
			},
			// Removed outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveOrganizationUserRole(orgCode, userID, roleID) },
				Config:             withRole,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: withRole,
				Check:  resource.TestCheckResourceAttrSet("kinde_user_role.test", "id"),
			},
		},
	})
}

// testAccUserRoleBaseConfig declares an organization, a user, and a role.
// It does not make the user a member of the organization.
func testAccUserRoleBaseConfig(testID string) string {
	return fmt.Sprintf(`
resource "kinde_organization" "test" {
	name = %[1]q
}

resource "kinde_user" "test" {
	first_name = "Role"
	last_name  = "Holder"

	identities = [
		{
			type  = "email"
			value = "%[1]s@example.com"
		}
	]
}

resource "kinde_role" "test" {
	name        = %[1]q
	key         = %[1]q
	description = "Test role"
}
`, testID)
}

const testAccUserRoleResourceConfig = `
resource "kinde_user_role" "test" {
	organization_code = kinde_organization.test.code
	user_id           = kinde_user.test.id
	role_id           = kinde_role.test.id
}
`
