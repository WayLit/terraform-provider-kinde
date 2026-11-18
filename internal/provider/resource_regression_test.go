package provider

import (
	"fmt"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

func TestAccOrganizationUserPermissions(t *testing.T) {
	f := testAccFake(t)
	client, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: f.ClientSecret})
	if err != nil {
		t.Fatal(err)
	}
	config := testAccOrganizationUserResourceConfig("user-permissions", "[]") + `
		resource "kinde_permission" "first" {
			name = "Read"
			key = "read"
		}
		resource "kinde_permission" "second" {
			name = "Write"
			key = "write"
		}
		`
	// Add the attribute to the existing membership configuration.
	withPermissions := func(ids string) string {
		return strings.Replace(config, "roles             = []", "roles             = []\n permissions = "+ids, 1)
	}
	var code, userID string
	check := func(want int) resource.TestCheckFunc {
		return func(s *terraform.State) error {
			attrs := s.RootModule().Resources["kinde_organization_user.test"].Primary.Attributes
			code, userID = attrs["organization_code"], attrs["user_id"]
			permissions, err := client.GetOrganizationUserPermissions(t.Context(), code, userID)
			if err != nil {
				return err
			}
			if len(permissions) != want {
				return fmt.Errorf("Kinde has %d permissions, want %d", len(permissions), want)
			}
			if want == 1 && permissions[0].ID.Value != s.RootModule().Resources["kinde_permission.second"].Primary.ID {
				return fmt.Errorf("Kinde retained the wrong permission: %s", permissions[0].ID.Value)
			}
			return nil
		}
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: withPermissions("[kinde_permission.first.id, kinde_permission.second.id]"), Check: check(2)},
			{Config: withPermissions("[kinde_permission.second.id, kinde_permission.first.id]"), Check: check(2)},
			{Config: withPermissions("[kinde_permission.second.id]"), Check: check(1)},
			{
				PreConfig: func() {
					permissions, err := client.GetOrganizationUserPermissions(t.Context(), code, userID)
					if err != nil {
						t.Fatal(err)
					}
					if err := client.DeleteOrganizationUserPermission(t.Context(), code, userID, permissions[0].ID.Value); err != nil {
						t.Fatal(err)
					}
				},
				Config: withPermissions("[kinde_permission.second.id]"), Check: check(1),
			},
			{Config: withPermissions("[]"), Check: check(0)},
			{Config: config, Check: check(0)},
		},
	})
}

func TestAccOrganizationUserEmptyRoles(t *testing.T) {
	testAccFake(t)
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{Config: testAccOrganizationUserResourceConfig("empty-roles", "[]")},
			{Config: testAccOrganizationUserResourceConfig("empty-roles", "[kinde_role.first.id]")},
			{Config: testAccOrganizationUserResourceConfig("empty-roles", "[]")},
		},
	})
}

func TestAccApplicationClearOptionalURIs(t *testing.T) {
	for _, value := range []string{"omitted", "empty"} {
		t.Run(value, func(t *testing.T) {
			testAccFake(t)
			base := `resource "kinde_application" "test" {
				name = "clear-uri"
				type = "reg"
				%s
			}`
			cleared := ""
			if value == "empty" {
				cleared = `login_uri = ""
					homepage_uri = ""`
			}
			resource.Test(t, resource.TestCase{
				ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
				Steps: []resource.TestStep{
					{Config: fmt.Sprintf(base, `login_uri = "https://example.com/login"
						homepage_uri = "https://example.com"`)},
					{Config: fmt.Sprintf(base, cleared)},
				},
			})
		})
	}
}

func TestAccUserRemoveAndReplaceIdentities(t *testing.T) {
	f := testAccFake(t)
	client, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: f.ClientSecret})
	if err != nil {
		t.Fatal(err)
	}
	var userID string
	config := func(email, extra string) string {
		return fmt.Sprintf(`resource "kinde_user" "test" {
			first_name = "Identity"
			last_name = "Test"
			identities = [{type = "email", value = %q}%s]
		}`, email, extra)
	}
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: config("before@example.com", `, {type = "username", value = "identity-test"}`),
				Check:  resource.TestCheckResourceAttrWith("kinde_user.test", "id", func(id string) error { userID = id; return nil }),
			},
			{
				PreConfig: func() { f.AddUserIdentity(userID, "oauth2:google", "social@example.com") },
				Config:    config("before@example.com", ""),
				Check:     resource.TestCheckResourceAttr("kinde_user.test", "identities.#", "1"),
			},
			{
				Config: config("after@example.com", ""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckTypeSetElemNestedAttrs("kinde_user.test", "identities.*", map[string]string{"type": "email", "value": "after@example.com"}),
					func(_ *terraform.State) error {
						identities, err := client.GetUserIdentities(t.Context(), userID)
						if err != nil {
							return err
						}
						if len(identities) != 2 {
							return fmt.Errorf("Kinde has %d identities, want email and OAuth2", len(identities))
						}
						for _, identity := range identities {
							if identity.Type == "oauth2:google" && identity.Name == "social@example.com" {
								return nil
							}
						}
						return fmt.Errorf("OAuth2 identity was removed")
					},
				),
			},
		},
	})
}
