# Kinde Terraform Provider

This provider allows you to manage your [Kinde](https://kinde.com/) resources using Terraform. It supports managing applications, APIs, roles, permissions, users, and organizations.

## Requirements

- [Terraform](https://www.terraform.io/downloads.html) >= 1.0
- [Go](https://golang.org/doc/install) >= 1.20

## Authentication

The provider needs to be configured with the proper credentials before it can be used. You can provide your credentials via environment variables:

```sh
export KINDE_DOMAIN="https://your-domain.kinde.com"
# Optional: defaults to <KINDE_DOMAIN>/api
export KINDE_AUDIENCE="https://your-domain.kinde.com/api"
export KINDE_CLIENT_ID="your-client-id"
export KINDE_CLIENT_SECRET="your-client-secret"
```

## Usage

```hcl
terraform {
  required_providers {
    kinde = {
      source = "nxt-fwd/kinde"
    }
  }
}

# Configure the provider using environment variables
provider "kinde" {}

# Or configure explicitly
provider "kinde" {
  domain        = "https://your-domain.kinde.com"
  audience      = "https://your-domain.kinde.com/api"
  client_id     = "your-client-id"
  client_secret = "your-client-secret"
}
```

See the [examples](./examples) directory for more usage examples.

## Resource Types

The provider includes the following resource types:

- `kinde_application` - Manage applications (regular web apps, SPAs, or M2M)
- `kinde_api` - Manage API resources
- `kinde_permission` - Manage permissions
- `kinde_role` - Manage roles and their associated permissions
- `kinde_user` - Manage users
- `kinde_organization_user` - Manage user organization memberships
- `kinde_user_role` - Manage user role assignments

## Development

### Building

1. Clone the repository
2. Enter the repository directory
3. Build the provider using `make build`

```sh
git clone https://github.com/nxt-fwd/terraform-provider-kinde
cd terraform-provider-kinde
make build
```

### Testing

The provider includes both unit tests and acceptance tests:

```sh
# Run unit tests
make test

# Run acceptance tests against an in-memory fake Kinde (no credentials needed)
make testacc
```

Acceptance tests need the `terraform` CLI on your `PATH`, or `TF_ACC_TERRAFORM_PATH` pointing at a `terraform` or `tofu` binary. They never contact a real Kinde business.

### Contributing

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Run tests
5. Submit a pull request

Please ensure your changes include:
- Appropriate test coverage
- Updated documentation
- Clear commit messages

## License

This provider is licensed under the MPL 2.0 License. See the LICENSE file for details.
