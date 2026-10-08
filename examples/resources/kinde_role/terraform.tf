terraform {
  required_providers {
    kinde = {
      source = "nxt-fwd/kinde"
    }
  }
}

# Configure the provider with environment variables
provider "kinde" {
  # Configuration options can be provided here or via environment variables:
  # KINDE_DOMAIN
  # KINDE_AUDIENCE
  # KINDE_CLIENT_ID
  # KINDE_CLIENT_SECRET
}

# Alternatively, configure the provider explicitly
provider "kinde" {
  alias         = "dev"
  domain        = "https://your-org.kinde.com"
  audience      = "https://your-org.kinde.com/api"
  client_id     = "your_client_id"
  client_secret = "your_client_secret"
} 