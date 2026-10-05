package provider

import (
	"fmt"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/acctest"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/nxt-fwd/kinde-go/api/connections"
)

func TestAccConnectionResource_OAuth2(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccConnectionResourceConfig_OAuth2(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "display_name", "Test OAuth2 Connection"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "strategy", string(connections.StrategyOAuth2Google)),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_id", "test-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_secret", "test-client-secret"),
				),
			},
			// ImportState testing - we now expect empty options but not null
			{
				ResourceName:      "kinde_connection.oauth2",
				ImportState:       true,
				ImportStateVerify: true,
				// Only verify basic fields, since imported resource won't have sensitive values from API
				ImportStateVerifyIgnore: []string{
					"options",
					"options.client_id",
					"options.client_secret",
				},
			},
			// Update with empty options - should reset sensitive fields
			{
				Config: testAccConnectionResourceConfig_OAuth2EmptyOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "display_name", "Test OAuth2 Connection"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "strategy", string(connections.StrategyOAuth2Google)),
					resource.TestCheckNoResourceAttr("kinde_connection.oauth2", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.oauth2", "options.client_secret"),
				),
			},
			// Update with new values
			{
				Config: testAccConnectionResourceConfig_OAuth2Updated(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "display_name", "Updated OAuth2 Connection"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "strategy", string(connections.StrategyOAuth2Google)),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_id", "updated-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_secret", "updated-client-secret"),
				),
			},
		},
	})
}

func TestAccConnectionResource_NoOptions(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create and Read testing
			{
				Config: testAccConnectionResourceConfig_NoOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.no_options", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.no_options", "display_name", "Test Connection With Minimal Options"),
					resource.TestCheckResourceAttr("kinde_connection.no_options", "strategy", "oauth2:google"),
					resource.TestCheckResourceAttr("kinde_connection.no_options", "options.client_id", "minimal-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.no_options", "options.client_secret", "minimal-client-secret"),
				),
			},
			// ImportState testing - we now expect empty options but not null
			{
				ResourceName:      "kinde_connection.no_options",
				ImportState:       true,
				ImportStateVerify: true,
				// Only verify basic fields, since imported resource won't have sensitive values from API
				ImportStateVerifyIgnore: []string{
					"options",
					"options.client_id",
					"options.client_secret",
				},
			},
		},
	})
}

func TestAccConnectionResource_EmptyOptionsNoDiff(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with options
			{
				Config: testAccConnectionResourceConfig_OAuth2(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_id", "test-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "options.client_secret", "test-client-secret"),
				),
			},
			// Update with empty options - should reset sensitive values
			{
				Config: testAccConnectionResourceConfig_OAuth2EmptyOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.oauth2", "name", testID),
					resource.TestCheckNoResourceAttr("kinde_connection.oauth2", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.oauth2", "options.client_secret"),
				),
			},
		},
	})
}

func TestAccConnectionResource_SensitiveFieldHandling(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with no sensitive fields - should create with null values
			{
				Config: testAccConnectionResourceConfig_NoSensitiveFields(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "name", testID),
					resource.TestCheckNoResourceAttr("kinde_connection.sensitive", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.sensitive", "options.client_secret"),
				),
			},
			// Update to set sensitive fields - should update with new values
			{
				Config: testAccConnectionResourceConfig_WithSensitiveFields(testID, "new-id", "new-secret"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "options.client_id", "new-id"),
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "options.client_secret", "new-secret"),
				),
			},
			// Update with empty options - should reset sensitive fields to null
			{
				Config: testAccConnectionResourceConfig_EmptySensitiveFields(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckNoResourceAttr("kinde_connection.sensitive", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.sensitive", "options.client_secret"),
				),
			},
			// Update with new values again - should set both fields
			{
				Config: testAccConnectionResourceConfig_WithSensitiveFields(testID, "final-id", "final-secret"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "options.client_id", "final-id"),
					resource.TestCheckResourceAttr("kinde_connection.sensitive", "options.client_secret", "final-secret"),
				),
			},
		},
	})
}

func TestAccConnectionResource_ImportEmptyOptions(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with empty options
			{
				Config: testAccConnectionResourceConfig_NoSensitiveFields(testID),
			},
			// Import and verify no changes
			{
				ResourceName:            "kinde_connection.sensitive",
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"options"},
				// Verify that re-applying the same config doesn't cause changes
				Config:   testAccConnectionResourceConfig_NoSensitiveFields(testID),
				PlanOnly: true,
			},
		},
	})
}

func TestAccConnectionResource_EmptyToPopulatedOptions(t *testing.T) {
	testID := acctest.RandomWithPrefix("tfacc")

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with populated options - options should now be in state
			{
				Config: testAccConnectionResourceConfig_PopulatedOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "display_name", "Test Empty Options"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "strategy", "oauth2:google"),
					// Sensitive fields should now be stored in state
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "options.client_id", "populated-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "options.client_secret", "populated-client-secret"),
				),
			},
			// Update to empty options - should reset options in state
			{
				Config: testAccConnectionResourceConfig_EmptyOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "display_name", "Test Empty Options"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "strategy", "oauth2:google"),
					// Options should still exist but be empty
					resource.TestCheckResourceAttrSet("kinde_connection.empty_to_populated", "options.%"),
					resource.TestCheckNoResourceAttr("kinde_connection.empty_to_populated", "options.client_id"),
					resource.TestCheckNoResourceAttr("kinde_connection.empty_to_populated", "options.client_secret"),
				),
			},
			// Import state verification - options will be empty but present
			{
				ResourceName:      "kinde_connection.empty_to_populated",
				ImportState:       true,
				ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{
					"options",
					"options.client_id",
					"options.client_secret",
				},
			},
			// Add options back to verify we can still set them after removing
			{
				Config: testAccConnectionResourceConfig_PopulatedOptions(testID),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "name", testID),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "display_name", "Test Empty Options"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "strategy", "oauth2:google"),
					// Sensitive fields should be stored in state again
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "options.client_id", "populated-client-id"),
					resource.TestCheckResourceAttr("kinde_connection.empty_to_populated", "options.client_secret", "populated-client-secret"),
				),
			},
		},
	})
}

func testAccConnectionResourceConfig_OAuth2(name string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "oauth2" {
	name         = %[1]q
	display_name = "Test OAuth2 Connection"
	strategy     = "oauth2:google"
	options = {
		client_id     = "test-client-id"
		client_secret = "test-client-secret"
	}
}
`, name)
}

func testAccConnectionResourceConfig_OAuth2EmptyOptions(name string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "oauth2" {
	name         = %[1]q
	display_name = "Test OAuth2 Connection"
	strategy     = "oauth2:google"
	options      = {}
}
`, name)
}

func testAccConnectionResourceConfig_OAuth2Updated(name string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "oauth2" {
	name         = %[1]q
	display_name = "Updated OAuth2 Connection"
	strategy     = "oauth2:google"
	options = {
		client_id     = "updated-client-id"
		client_secret = "updated-client-secret"
	}
}
`, name)
}

func testAccConnectionResourceConfig_NoOptions(name string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "no_options" {
	name         = %[1]q
	display_name = "Test Connection With Minimal Options"
	strategy     = "oauth2:google"
	options = {
		client_id     = "minimal-client-id"
		client_secret = "minimal-client-secret"
	}
}
`, name)
}

func testAccConnectionResourceConfig_NoSensitiveFields(name string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "sensitive" {
	name         = %[1]q
	display_name = "Test Sensitive Fields"
	strategy     = "oauth2:google"
	options      = {}
}
`, name)
}

func testAccConnectionResourceConfig_WithSensitiveFields(name, clientID, clientSecret string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "sensitive" {
	name         = %[1]q
	display_name = "Test Sensitive Fields"
	strategy     = "oauth2:google"
	options = {
		client_id     = %[2]q
		client_secret = %[3]q
	}
}
`, name, clientID, clientSecret)
}

func testAccConnectionResourceConfig_EmptySensitiveFields(name string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "sensitive" {
	name         = %[1]q
	display_name = "Test Sensitive Fields"
	strategy     = "oauth2:google"
	options      = {}
}
`, name)
}

func testAccConnectionResourceConfig_EmptyOptions(name string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "empty_to_populated" {
	name         = %[1]q
	display_name = "Test Empty Options"
	strategy     = "oauth2:google"
	options      = {}
}
`, name)
}

func testAccConnectionResourceConfig_PopulatedOptions(name string) string {
	return fmt.Sprintf(`
resource "kinde_connection" "empty_to_populated" {
	name         = %[1]q
	display_name = "Test Empty Options"
	strategy     = "oauth2:google"
	options = {
		client_id     = "populated-client-id"
		client_secret = "populated-client-secret"
	}
}
`, name)
}
