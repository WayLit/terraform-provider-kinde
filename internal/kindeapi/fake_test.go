package kindeapi_test

import (
	"testing"

	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

// newFakeClient starts a fake Kinde and returns a client configured for it.
func newFakeClient(t *testing.T) (*kindefake.Fake, *kindeapi.Client) {
	t.Helper()
	f := kindefake.New(t)
	c, err := kindeapi.New(kindeapi.Config{
		Domain:       f.URL,
		Audience:     f.Audience,
		ClientID:     f.ClientID,
		ClientSecret: f.ClientSecret,
	})
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

func TestCheckCredentialsAgainstFake(t *testing.T) {
	f, c := newFakeClient(t)
	if err := c.CheckCredentials(); err != nil {
		t.Fatal(err)
	}
	if got := f.TokenRequests(); got != 1 {
		t.Fatalf("token requests = %d, want 1", got)
	}
}

func TestCheckCredentialsRejectedByFake(t *testing.T) {
	f := kindefake.New(t)
	c, err := kindeapi.New(kindeapi.Config{Domain: f.URL, Audience: f.Audience, ClientID: f.ClientID, ClientSecret: "wrong"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.CheckCredentials(); err == nil {
		t.Fatal("expected the fake to reject a wrong secret")
	}
}
