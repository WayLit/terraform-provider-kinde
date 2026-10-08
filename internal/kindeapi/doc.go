// Package kindeapi adapts Kinde's official Go SDK for the provider. It builds
// an authenticated client, turns error responses into *APIError, retries rate
// limits, and returns every page of list endpoints.
package kindeapi
