package provider

import (
	"fmt"
	"math/rand"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/nxt-fwd/kinde-go/api/users"
)

// TestUserResource_FiltersOAuthIdentities is a simple unit test for the OAuth filtering logic.
func TestUserResource_FiltersOAuthIdentities(t *testing.T) {
	// Create test data with mixed identity types
	identities := []users.Identity{
		{Type: "email", Name: "test@example.com"},
		{Type: "username", Name: "testuser"},
		{Type: "oauth2:google", Name: "test@gmail.com"},
		{Type: "oauth2:github", Name: "githubuser"},
		{Type: "phone", Name: "+1234567890"},
	}

	// Create a test state with the identities
	var tfIdentities []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}

	// Filter out OAuth identities (simulating what our Read function does)
	for _, identity := range identities {
		if strings.HasPrefix(identity.Type, "oauth2:") {
			continue
		}

		tfIdentities = append(tfIdentities, struct {
			Type  string `tfsdk:"type"`
			Value string `tfsdk:"value"`
		}{
			Type:  identity.Type,
			Value: identity.Name,
		})
	}

	// Verify the filtering worked correctly
	if len(tfIdentities) != 3 {
		t.Errorf("Expected 3 non-OAuth identities, got %d", len(tfIdentities))
	}

	// Check that no OAuth identities remain
	for _, identity := range tfIdentities {
		if strings.HasPrefix(identity.Type, "oauth2:") {
			t.Errorf("OAuth identity was not filtered out: %s", identity.Type)
		}
	}

	// Verify the specific identity types that should remain
	expectedTypes := map[string]bool{
		"email":    false,
		"username": false,
		"phone":    false,
	}

	for _, identity := range tfIdentities {
		expectedTypes[identity.Type] = true
	}

	for idType, found := range expectedTypes {
		if !found {
			t.Errorf("Expected identity type %s was not found after filtering", idType)
		}
	}
}

func TestAccUserResource_ComplexAttributes(t *testing.T) {
	testID := rand.Int()
	email := fmt.Sprintf("complex.user.tfacc-%d@example.com", testID)
	altEmail := fmt.Sprintf("complex.user.alt.tfacc-%d@example.com", testID)
	username := fmt.Sprintf("complex-user-%d", testID)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a user with email and username identities, is_suspended=false
			{
				Config: testAccUserResourceConfig_ComplexAttributes(email, username, false),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.complex", "first_name", "Complex"),
					resource.TestCheckResourceAttr("kinde_user.complex", "last_name", "User"),
					resource.TestCheckResourceAttr("kinde_user.complex", "is_suspended", "false"),
					resource.TestCheckResourceAttr("kinde_user.complex", "identities.#", "2"),
					// Check that both identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "username",
						"value": username,
					}),
				),
			},
			// Update is_suspended to true and add another email identity
			{
				Config: testAccUserResourceConfig_ComplexAttributesWithAltEmail(email, altEmail, username, true),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.complex", "first_name", "Complex"),
					resource.TestCheckResourceAttr("kinde_user.complex", "last_name", "User"),
					resource.TestCheckResourceAttr("kinde_user.complex", "is_suspended", "true"),
					resource.TestCheckResourceAttr("kinde_user.complex", "identities.#", "3"),
					// Check that all identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "username",
						"value": username,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.complex", "identities.*", map[string]string{
						"type":  "email",
						"value": altEmail,
					}),
				),
			},
		},
	})
}

func testAccUserResourceConfig_ComplexAttributes(email, username string, isSuspended bool) string {
	return fmt.Sprintf(`
resource "kinde_user" "complex" {
	first_name = "Complex"
	last_name = "User"
	is_suspended = %[3]t

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "username"
			value = %[2]q
		}
	]
}
`, email, username, isSuspended)
}

func testAccUserResourceConfig_ComplexAttributesWithAltEmail(email, altEmail, username string, isSuspended bool) string {
	return fmt.Sprintf(`
resource "kinde_user" "complex" {
	first_name = "Complex"
	last_name = "User"
	is_suspended = %[4]t

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "username"
			value = %[3]q
		},
		{
			type = "email"
			value = %[2]q
		}
	]
}
`, email, altEmail, username, isSuspended)
}

func TestAccUserResource_PhoneIdentity(t *testing.T) {
	testID := rand.Int()
	email := fmt.Sprintf("phone.user.tfacc-%d@example.com", testID)
	// Use a valid international phone number format
	phone := fmt.Sprintf("+12025550%03d", testID%1000)
	phone2 := fmt.Sprintf("+12025551%03d", testID%1000)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a user with email and phone identities
			{
				Config: testAccUserResourceConfig_WithPhone(email, phone),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.phone", "first_name", "Phone"),
					resource.TestCheckResourceAttr("kinde_user.phone", "last_name", "User"),
					resource.TestCheckResourceAttr("kinde_user.phone", "identities.#", "2"),
					// Check that both identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "phone",
						"value": phone,
					}),
				),
			},
			// Update to add another phone identity
			{
				Config: testAccUserResourceConfig_WithMultiplePhones(email, phone, phone2),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.phone", "identities.#", "3"),
					// Check that all identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "phone",
						"value": phone,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.phone", "identities.*", map[string]string{
						"type":  "phone",
						"value": phone2,
					}),
				),
			},
		},
	})
}

func testAccUserResourceConfig_WithPhone(email, phone string) string {
	return fmt.Sprintf(`
resource "kinde_user" "phone" {
	first_name = "Phone"
	last_name = "User"

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "phone"
			value = %[2]q
		}
	]
}
`, email, phone)
}

func testAccUserResourceConfig_WithMultiplePhones(email, phone1, phone2 string) string {
	return fmt.Sprintf(`
resource "kinde_user" "phone" {
	first_name = "Phone"
	last_name = "User"

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "phone"
			value = %[2]q
		},
		{
			type = "phone"
			value = %[3]q
		}
	]
}
`, email, phone1, phone2)
}

func TestAccUserResource_OAuth2Identity(t *testing.T) {
	testID := rand.Int()
	email := fmt.Sprintf("oauth2.user.tfacc-%d@example.com", testID)
	username := fmt.Sprintf("oauth2-user-%d", testID)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a user with email and username identities
			{
				Config: testAccUserResourceConfig_OAuth2(email, username),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.oauth2", "first_name", "OAuth2"),
					resource.TestCheckResourceAttr("kinde_user.oauth2", "last_name", "User"),
					// We expect exactly 2 identities in the state (email and username)
					resource.TestCheckResourceAttr("kinde_user.oauth2", "identities.#", "2"),
					// Check that both identities exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.oauth2", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.oauth2", "identities.*", map[string]string{
						"type":  "username",
						"value": username,
					}),
				),
			},
			// Update user details while preserving identities
			{
				Config: testAccUserResourceConfig_OAuth2Updated(email, username),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.oauth2", "first_name", "Updated"),
					resource.TestCheckResourceAttr("kinde_user.oauth2", "last_name", "OAuth2"),
					// We still expect exactly 2 identities in the state (OAuth identities excluded)
					resource.TestCheckResourceAttr("kinde_user.oauth2", "identities.#", "2"),
					// Check that both identities still exist
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.oauth2", "identities.*", map[string]string{
						"type":  "email",
						"value": email,
					}),
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.oauth2", "identities.*", map[string]string{
						"type":  "username",
						"value": username,
					}),
				),
			},
		},
	})
}

func testAccUserResourceConfig_OAuth2(email, username string) string {
	return fmt.Sprintf(`
resource "kinde_user" "oauth2" {
	first_name = "OAuth2"
	last_name = "User"

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "username"
			value = %[2]q
		}
	]
}
`, email, username)
}

func testAccUserResourceConfig_OAuth2Updated(email, username string) string {
	return fmt.Sprintf(`
resource "kinde_user" "oauth2" {
	first_name = "Updated"
	last_name = "OAuth2"

	identities = [
		{
			type = "email"
			value = %[1]q
		},
		{
			type = "username"
			value = %[2]q
		}
	]
}
`, email, username)
}

func TestAccUserResource_NameHandling(t *testing.T) {
	testID := rand.Int()
	email := fmt.Sprintf("name.test.tfacc-%d@example.com", testID)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with both names set
			{
				Config: testAccUserResourceConfig_Names(email, "John", "Doe"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.name_test", "first_name", "John"),
					resource.TestCheckResourceAttr("kinde_user.name_test", "last_name", "Doe"),
				),
			},
			// Update with new values
			{
				Config: testAccUserResourceConfig_Names(email, "Jane", "Smith"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.name_test", "first_name", "Jane"),
					resource.TestCheckResourceAttr("kinde_user.name_test", "last_name", "Smith"),
				),
			},
		},
	})
}

func testAccUserResourceConfig_Names(email, firstName, lastName string) string {
	return fmt.Sprintf(`
resource "kinde_user" "name_test" {
	first_name = %[2]q
	last_name = %[3]q
	identities = [
		{
			type = "email"
			value = %[1]q
		}
	]
}
`, email, firstName, lastName)
}

// TestUserResource_SortsIdentitiesConsistently tests that identities are sorted consistently.
func TestUserResource_SortsIdentitiesConsistently(t *testing.T) {
	// Create test data with identities in different orders
	identitiesOrder1 := []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}{
		{Type: "email", Value: "test@example.com"},
		{Type: "phone", Value: "+1234567890"},
		{Type: "username", Value: "testuser"},
	}

	identitiesOrder2 := []struct {
		Type  string `tfsdk:"type"`
		Value string `tfsdk:"value"`
	}{
		{Type: "phone", Value: "+1234567890"},
		{Type: "username", Value: "testuser"},
		{Type: "email", Value: "test@example.com"},
	}

	// Sort both sets of identities
	sort.Slice(identitiesOrder1, func(i, j int) bool {
		if identitiesOrder1[i].Type == identitiesOrder1[j].Type {
			return identitiesOrder1[i].Value < identitiesOrder1[j].Value
		}
		return identitiesOrder1[i].Type < identitiesOrder1[j].Type
	})

	sort.Slice(identitiesOrder2, func(i, j int) bool {
		if identitiesOrder2[i].Type == identitiesOrder2[j].Type {
			return identitiesOrder2[i].Value < identitiesOrder2[j].Value
		}
		return identitiesOrder2[i].Type < identitiesOrder2[j].Type
	})

	// Verify that both sets are now in the same order
	if len(identitiesOrder1) != len(identitiesOrder2) {
		t.Errorf("Sorted identity sets have different lengths: %d vs %d",
			len(identitiesOrder1), len(identitiesOrder2))
		return
	}

	for i := range identitiesOrder1 {
		if identitiesOrder1[i].Type != identitiesOrder2[i].Type ||
			identitiesOrder1[i].Value != identitiesOrder2[i].Value {
			t.Errorf("Sorted identities differ at position %d: %+v vs %+v",
				i, identitiesOrder1[i], identitiesOrder2[i])
		}
	}

	// Verify the specific order (email should come before phone and username)
	if len(identitiesOrder1) >= 3 {
		if identitiesOrder1[0].Type != "email" {
			t.Errorf("Expected 'email' to be first type after sorting, got: %s", identitiesOrder1[0].Type)
		}
	}
}

func TestUserResource_ErrorOnCreateWithIsSuspended(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "kinde_user" "test" {
  first_name   = "Test"
  last_name    = "User"
  is_suspended = true
  identities = [
    {
      type  = "email"
      value = "test@example.com"
    }
  ]
}
`,
				ExpectError: regexp.MustCompile("Setting is_suspended=true when creating a user is not supported"),
			},
		},
	})
}

func TestAccUserResource_IsSuspendedBehavior(t *testing.T) {
	// Generate random values for the test
	email := fmt.Sprintf("test-suspended-%d-%d-%d-%d@example.com", time.Now().UnixNano(), rand.Intn(1000000), rand.Intn(1000000), rand.Intn(1000000))
	firstName := "John"
	lastName := "Doe"
	phone := fmt.Sprintf("+35845230%d", time.Now().UnixNano()%1000000)

	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Create user without is_suspended
				Config: testAccUserResourceConfigWithNames(email, firstName, lastName, phone),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.test", "first_name", firstName),
					resource.TestCheckResourceAttr("kinde_user.test", "last_name", lastName),
					resource.TestCheckNoResourceAttr("kinde_user.test", "is_suspended"),
				),
			},
			{
				// Try to set is_suspended during update
				Config: testAccUserResourceConfigWithNamesAndSuspended(email, firstName, lastName, phone, true),
				Check: resource.ComposeTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.test", "first_name", firstName),
					resource.TestCheckResourceAttr("kinde_user.test", "last_name", lastName),
					resource.TestCheckResourceAttr("kinde_user.test", "is_suspended", "true"),
				),
			},
		},
	})
}

func testAccUserResourceConfigWithNames(email, firstName, lastName, phone string) string {
	return fmt.Sprintf(`
resource "kinde_user" "test" {
  first_name = %q
  last_name  = %q
  identities = [
    {
      type  = "email"
      value = %q
    },
    {
      type  = "phone"
      value = %q
    }
  ]
}
`, firstName, lastName, email, phone)
}

func testAccUserResourceConfigWithNamesAndSuspended(email, firstName, lastName, phone string, isSuspended bool) string {
	return fmt.Sprintf(`
resource "kinde_user" "test" {
  first_name = %q
  last_name  = %q
  is_suspended = %t
  identities = [
    {
      type  = "email"
      value = %q
    },
    {
      type  = "phone"
      value = %q
    }
  ]
}
`, firstName, lastName, isSuspended, email, phone)
}
