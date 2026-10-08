# Basic organization user membership
resource "kinde_organization_user" "basic" {
  organization_code = "org_123" # Replace with your organization code
  user_id           = kinde_user.example.id
}

# Organization user with roles
resource "kinde_organization_user" "with_roles" {
  organization_code = "org_123" # Replace with your organization code
  user_id           = kinde_user.example.id
  roles = [
    kinde_role.admin.id,
    kinde_role.viewer.id
  ]
}

# Organization user with permissions
resource "kinde_organization_user" "with_permissions" {
  organization_code = "org_123" # Replace with your organization code
  user_id           = kinde_user.example.id
  permissions = [
    kinde_permission.read_users.id,
    kinde_permission.write_users.id
  ]
}

# Full example with user creation and organization membership
resource "kinde_user" "example" {
  first_name = "John"
  last_name  = "Doe"
  identities = [
    {
      type  = "email"
      value = "john.doe@example.com"
    }
  ]
}

resource "kinde_organization_user" "full_example" {
  organization_code = "org_123" # Replace with your organization code
  user_id           = kinde_user.example.id
  roles             = [kinde_role.admin.id]
  permissions       = [kinde_permission.read_users.id]
} 