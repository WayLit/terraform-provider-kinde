provider "kinde" {
  domain        = "https://example.kinde.com"     # Also configurable via KINDE_DOMAIN
  audience      = "https://example.kinde.com/api" # Also configurable via KINDE_AUDIENCE
  client_id     = "your_client_id"                # Also configurable via KINDE_CLIENT_ID
  client_secret = "your_client_secret"            # Also configurable via KINDE_CLIENT_SECRET
}
