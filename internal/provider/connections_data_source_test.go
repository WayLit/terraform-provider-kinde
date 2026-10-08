package provider

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

// TestAccConnectionsDataSource lists more connections than one page of
// Kinde's default page size (10): the fake's 5 built-in connections plus 11
// created ones.
func TestAccConnectionsDataSource(t *testing.T) {
	testAccFake(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: testAccConnectionsDataSourceConfig,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.kinde_connections.all", "connections.#", "16"),
					resource.TestCheckResourceAttr("data.kinde_connections.builtin", "connections.#", "5"),
					resource.TestCheckTypeSetElemNestedAttrs("data.kinde_connections.builtin", "connections.*", map[string]string{
						"strategy": "username:password",
					}),
					resource.TestCheckResourceAttr("data.kinde_connections.custom", "connections.#", "11"),
					resource.TestCheckTypeSetElemNestedAttrs("data.kinde_connections.custom", "connections.*", map[string]string{
						"name":         "tfacc-connection-10",
						"display_name": "Connection 10",
						"strategy":     "oauth2:google",
					}),
				),
			},
		},
	})
}

const testAccConnectionsDataSourceConfig = `
resource "kinde_connection" "test" {
	count        = 11
	name         = "tfacc-connection-${count.index}"
	display_name = "Connection ${count.index}"
	strategy     = "oauth2:google"
}

data "kinde_connections" "all" {
	depends_on = [kinde_connection.test]
}

data "kinde_connections" "builtin" {
	filter     = "builtin"
	depends_on = [kinde_connection.test]
}

data "kinde_connections" "custom" {
	filter     = "custom"
	depends_on = [kinde_connection.test]
}
`
