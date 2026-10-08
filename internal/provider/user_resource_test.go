package provider

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

func TestUserIdentitiesValue(t *testing.T) {
	identities := []kindeapi.UserIdentity{
		{ID: "identity_0001", Type: "email", Name: "test@example.com"},
		{ID: "identity_0002", Type: "username", Name: "testuser"},
		{ID: "identity_0003", Type: "oauth2:google", Name: "test@gmail.com"},
		{ID: "identity_0004", Type: "oauth2:github", Name: "githubuser"},
		{ID: "identity_0005", Type: "phone", Name: "+12025550123"},
	}
	tests := []struct {
		name       string
		knownTypes map[string]string
		want       []userIdentityModel
	}{
		{
			name: "drops OAuth2 identities",
			want: []userIdentityModel{
				{Type: "email", Value: "test@example.com"},
				{Type: "phone", Value: "+12025550123"},
				{Type: "username", Value: "testuser"},
			},
		},
		{
			name:       "keeps known types",
			knownTypes: map[string]string{"testuser": "enterprise"},
			want: []userIdentityModel{
				{Type: "email", Value: "test@example.com"},
				{Type: "enterprise", Value: "testuser"},
				{Type: "phone", Value: "+12025550123"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			set, diags := userIdentitiesValue(t.Context(), identities, tt.knownTypes)
			requireNoErrors(t, diags)
			var got []userIdentityModel
			requireNoErrors(t, set.ElementsAs(t.Context(), &got, false))
			slices.SortFunc(got, func(a, b userIdentityModel) int {
				return cmp.Or(cmp.Compare(a.Type, b.Type), cmp.Compare(a.Value, b.Value))
			})
			if !slices.Equal(got, tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
		})
	}
}

func TestAccUserResource_ComplexAttributes(t *testing.T) {
	testAccFake(t)
	email := "complex.user@example.com"
	altEmail := "complex.user.alt@example.com"
	username := "complex-user"

	resource.Test(t, resource.TestCase{
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
	testAccFake(t)
	email := "phone.user@example.com"
	phone := "+12025550123"
	phone2 := "+12025550124"

	resource.Test(t, resource.TestCase{
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
			// Add another phone identity. The adapter splits it into a national
			// number and country, and Kinde reports it back in international
			// format.
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
	f := testAccFake(t)
	email := "oauth2.user@example.com"
	username := "oauth2-user"
	var userID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create a user with email and username identities
			{
				Config: testAccUserResourceConfig_OAuth2(email, username),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("kinde_user.oauth2", "id", func(v string) error { userID = v; return nil }),
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
			// The user signs in with Google, so Kinde adds an OAuth2 identity.
			// Update user details: the OAuth2 identity stays out of state and
			// causes no drift.
			{
				PreConfig: func() { f.AddUserIdentity(userID, "oauth2:google", "oauth2.user@gmail.com") },
				Config:    testAccUserResourceConfig_OAuth2Updated(email, username),
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
	f := testAccFake(t)
	email := "name.test@example.com"
	config := testAccUserResourceConfig_Names(email, "Jane", "Smith")
	var userID string

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			// Create with both names set
			{
				Config: testAccUserResourceConfig_Names(email, "John", "Doe"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("kinde_user.name_test", "id", func(v string) error { userID = v; return nil }),
					resource.TestCheckResourceAttr("kinde_user.name_test", "first_name", "John"),
					resource.TestCheckResourceAttr("kinde_user.name_test", "last_name", "Doe"),
					// created_on is stored exactly as Kinde returns it.
					resource.TestCheckResourceAttr("kinde_user.name_test", "created_on", "2026-01-01T00:00:00Z"),
				),
			},
			// Import by ID
			{
				ResourceName:      "kinde_user.name_test",
				ImportState:       true,
				ImportStateVerify: true,
			},
			// Update with new values
			{
				Config: config,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.name_test", "first_name", "Jane"),
					resource.TestCheckResourceAttr("kinde_user.name_test", "last_name", "Smith"),
				),
			},
			// Deleted outside Terraform: refresh drops it and the plan recreates it.
			{
				PreConfig:          func() { f.RemoveUser(userID) },
				Config:             config,
				PlanOnly:           true,
				ExpectNonEmptyPlan: true,
			},
			// Applying recreates it.
			{
				Config: config,
				Check:  resource.TestCheckResourceAttrSet("kinde_user.name_test", "id"),
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

func TestAccUserResource_OrganizationCode(t *testing.T) {
	f := testAccFake(t)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
resource "kinde_user" "org" {
  first_name        = "Org"
  last_name         = "Member"
  organization_code = "org_engines"
  identities = [
    {
      type  = "email"
      value = "org.member@example.com"
    }
  ]
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("kinde_user.org", "organization_code", "org_engines"),
					// The fake records organization_code without checking that
					// the organization exists.
					resource.TestCheckResourceAttrWith("kinde_user.org", "id", func(id string) error {
						if got := f.UserOrganizationCode(id); got != "org_engines" {
							return fmt.Errorf("Kinde got organization_code %q, want org_engines", got)
						}
						return nil
					}),
				),
			},
		},
	})
}

func TestUserResource_ErrorOnCreateWithIsSuspended(t *testing.T) {
	testAccFake(t)
	resource.Test(t, resource.TestCase{
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
	testAccFake(t)
	email := "test-suspended@example.com"
	firstName := "John"
	lastName := "Doe"
	phone := "+358452301234"

	resource.Test(t, resource.TestCase{
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
