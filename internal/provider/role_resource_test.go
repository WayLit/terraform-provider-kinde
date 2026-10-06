package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestAccRoleResource(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccRoleResourceConfig(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Test role"),
					resource.TestCheckResourceAttrSet("kinde_role.test", "id"),
				),
			},
			// ImportState testing
			{
				ResourceName:      "kinde_role.test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update and Read testing
			{
				Config: testAccRoleResourceConfigUpdate(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "name", testID+"-updated"),
					resource.TestCheckResourceAttr("kinde_role.test", "key", testID),
					resource.TestCheckResourceAttr("kinde_role.test", "description", "Updated test role"),
				),
			},
		},
	})
}

func TestAccRoleResource_AddOnlyPermissionUpdate(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-role-add")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 2, []int{0}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "1"),
				),
			},
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 2, []int{0, 1}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "2"),
				),
			},
		},
	})
}

func TestAccRoleResource_MixedPermissionUpdate(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-role-mixed")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 4, []int{0, 1, 2}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "3"),
					testAccCheckRolePermissionRefs("kinde_role.test", []int{0, 1, 2}),
				),
			},
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 4, []int{1, 2, 3}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "3"),
					testAccCheckRolePermissionRefs("kinde_role.test", []int{1, 2, 3}),
				),
			},
		},
	})
}

func TestAccRoleResource_PermissionsPaginationBoundary(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc-role-page")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 11, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "10"),
				),
			},
			{
				Config: testAccRoleResourceConfigWithPermissionRefs(testID, 11, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10}),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_role.test", "permissions.#", "11"),
				),
			},
		},
	})
}

func testAccRoleResourceConfig(name string) string {
	return fmt.Sprintf(`
resource "kinde_role" "test" {
	name        = %[1]q
	key         = %[1]q
	description = "Test role"
}
`, name)
}

func testAccRoleResourceConfigUpdate(name string) string {
	return fmt.Sprintf(`
resource "kinde_role" "test" {
	name        = "%[1]s-updated"
	key         = %[1]q
	description = "Updated test role"
}
`, name)
}

func testAccRoleResourceConfigWithPermissionRefs(name string, permissionCount int, rolePermissionIndexes []int) string {
	var builder strings.Builder

	for i := 0; i < permissionCount; i++ {
		fmt.Fprintf(&builder, `
resource "kinde_permission" "perm_%02d" {
	name        = "%s-permission-%02d"
	key         = "%s_permission_%02d"
	description = "Test permission %02d"
}
`, i, name, i, name, i, i)
	}

	fmt.Fprintf(&builder, `
resource "kinde_role" "test" {
	name        = "%[1]s-role"
	key         = "%[1]s_role"
	description = "Test role"
	permissions = [
`, name)

	for _, idx := range rolePermissionIndexes {
		fmt.Fprintf(&builder, "\t\tkinde_permission.perm_%02d.id,\n", idx)
	}

	builder.WriteString(`	]
}
`)

	return builder.String()
}

func testAccCheckRolePermissionRefs(roleResourceName string, indexes []int) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		role, ok := s.RootModule().Resources[roleResourceName]
		if !ok {
			return fmt.Errorf("role resource %s not found", roleResourceName)
		}

		actualPermissions := map[string]struct{}{}
		for key, value := range role.Primary.Attributes {
			if strings.HasPrefix(key, "permissions.") && key != "permissions.#" {
				actualPermissions[value] = struct{}{}
			}
		}

		for _, idx := range indexes {
			permissionName := fmt.Sprintf("kinde_permission.perm_%02d", idx)
			permission, ok := s.RootModule().Resources[permissionName]
			if !ok {
				return fmt.Errorf("permission resource %s not found", permissionName)
			}

			if _, ok := actualPermissions[permission.Primary.ID]; !ok {
				return fmt.Errorf("role %s is missing permission %s (%s)", roleResourceName, permissionName, permission.Primary.ID)
			}
		}

		if len(actualPermissions) != len(indexes) {
			return fmt.Errorf("role %s has %d permissions, expected %d", roleResourceName, len(actualPermissions), len(indexes))
		}

		return nil
	}
}
