package kindeapi_test

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindeapi"
)

func emailIdentity(email string) mgmt.CreateUserReqIdentitiesItem {
	return mgmt.CreateUserReqIdentitiesItem{
		Type:    mgmt.NewOptCreateUserReqIdentitiesItemType(mgmt.CreateUserReqIdentitiesItemTypeEmail),
		Details: mgmt.NewOptCreateUserReqIdentitiesItemDetails(mgmt.CreateUserReqIdentitiesItemDetails{Email: mgmt.NewOptString(email)}),
	}
}

func createUser(t *testing.T, c *kindeapi.Client, req mgmt.CreateUserReq) string {
	t.Helper()
	created, err := c.CreateUser(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	id, ok := created.ID.Get()
	if !ok {
		t.Fatal("CreateUser returned no ID")
	}
	return id
}

// identityNames returns "type:name" for each identity, in order.
func identityNames(identities []kindeapi.UserIdentity) []string {
	names := make([]string, len(identities))
	for i, identity := range identities {
		names[i] = identity.Type + ":" + identity.Name
	}
	return names
}

func TestUserRoundTrip(t *testing.T) {
	f, c := newFakeClient(t)
	ctx := t.Context()

	id := createUser(t, c, mgmt.CreateUserReq{
		Profile: mgmt.NewOptCreateUserReqProfile(mgmt.CreateUserReqProfile{
			GivenName:  mgmt.NewOptString("Ada"),
			FamilyName: mgmt.NewOptString("Lovelace"),
		}),
		OrganizationCode: mgmt.NewOptString("org_engines"),
		Identities: []mgmt.CreateUserReqIdentitiesItem{
			emailIdentity("ada@example.com"),
			{
				Type:    mgmt.NewOptCreateUserReqIdentitiesItemType(mgmt.CreateUserReqIdentitiesItemTypeUsername),
				Details: mgmt.NewOptCreateUserReqIdentitiesItemDetails(mgmt.CreateUserReqIdentitiesItemDetails{Username: mgmt.NewOptString("ada")}),
			},
		},
	})
	if got := f.UserOrganizationCode(id); got != "org_engines" {
		t.Fatalf("organization code = %q, want org_engines", got)
	}

	user, err := c.GetUserData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if user.FirstName.Value != "Ada" || user.LastName.Value != "Lovelace" || user.IsSuspended.Value {
		t.Fatalf("got %+v, want Ada Lovelace, not suspended", user)
	}
	if got, _ := user.CreatedOn.Get(); got != "2026-01-01T00:00:00Z" {
		t.Fatalf("created_on = %q, want Kinde's string unchanged", got)
	}

	// The username identity arrives with "is_confirmed": null, which the
	// SDK's own decoder rejects.
	identities, err := c.GetUserIdentities(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := identityNames(identities), []string{"email:ada@example.com", "username:ada"}; !slices.Equal(got, want) {
		t.Fatalf("identities = %q, want %q", got, want)
	}

	if _, err := c.UpdateUser(ctx, id, mgmt.UpdateUserReq{
		GivenName:   mgmt.NewOptString("Augusta"),
		IsSuspended: mgmt.NewOptBool(true),
	}); err != nil {
		t.Fatal(err)
	}
	user, err = c.GetUserData(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if user.FirstName.Value != "Augusta" || user.LastName.Value != "Lovelace" || !user.IsSuspended.Value {
		t.Fatalf("after update got %+v, want Augusta Lovelace, suspended", user)
	}

	if err := c.DeleteUser(ctx, id); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetUserData(ctx, id); !kindeapi.IsNotFound(err) {
		t.Fatalf("GetUserData after delete: got %v, want not found", err)
	}
}

func TestCreateUserIdentity(t *testing.T) {
	_, c := newFakeClient(t)
	id := createUser(t, c, mgmt.CreateUserReq{Identities: []mgmt.CreateUserReqIdentitiesItem{emailIdentity("grace@example.com")}})

	for _, req := range []mgmt.CreateUserIdentityReq{
		{Type: mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypeEmail), Value: mgmt.NewOptString("grace@navy.example")},
		// The fake accepts a phone identity only as a national number plus
		// country, so this passes only if the adapter splits the number.
		{Type: mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypePhone), Value: mgmt.NewOptString("+61412345678")},
	} {
		created, err := c.CreateUserIdentity(t.Context(), id, req)
		if err != nil {
			t.Fatal(err)
		}
		if !created.Identity.Value.ID.IsSet() {
			t.Fatal("CreateUserIdentity returned no identity ID")
		}
	}

	identities, err := c.GetUserIdentities(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"email:grace@example.com", "email:grace@navy.example", "phone:+61412345678"}
	if got := identityNames(identities); !slices.Equal(got, want) {
		t.Fatalf("identities = %q, want %q", got, want)
	}
}

func TestCreateUserIdentityRejectsInvalidPhone(t *testing.T) {
	_, c := newFakeClient(t)
	id := createUser(t, c, mgmt.CreateUserReq{Identities: []mgmt.CreateUserReqIdentitiesItem{emailIdentity("alan@example.com")}})

	_, err := c.CreateUserIdentity(t.Context(), id, mgmt.CreateUserIdentityReq{
		Type:  mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypePhone),
		Value: mgmt.NewOptString("+1234"),
	})
	var apiErr *kindeapi.APIError
	if err == nil || errors.As(err, &apiErr) {
		t.Fatalf("expected a local phone parsing error, got %v", err)
	}
}

func TestGetUserIdentitiesReturnsEveryPage(t *testing.T) {
	_, c := newFakeClient(t)
	id := createUser(t, c, mgmt.CreateUserReq{Identities: []mgmt.CreateUserReqIdentitiesItem{emailIdentity("user0@example.com")}})

	// The fake pages identities 10 at a time, so 25 identities take 3 pages.
	want := []string{"email:user0@example.com"}
	for i := 1; i < 25; i++ {
		email := fmt.Sprintf("user%d@example.com", i)
		if _, err := c.CreateUserIdentity(t.Context(), id, mgmt.CreateUserIdentityReq{
			Type:  mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypeEmail),
			Value: mgmt.NewOptString(email),
		}); err != nil {
			t.Fatal(err)
		}
		want = append(want, "email:"+email)
	}

	identities, err := c.GetUserIdentities(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := identityNames(identities); !slices.Equal(got, want) {
		t.Fatalf("identities = %q, want %q", got, want)
	}
}

func TestUserOperationsOnMissingUser(t *testing.T) {
	_, c := newFakeClient(t)
	ctx := t.Context()
	const id = "kp_missing"

	_, getErr := c.GetUserData(ctx, id)
	_, updateErr := c.UpdateUser(ctx, id, mgmt.UpdateUserReq{GivenName: mgmt.NewOptString("Nobody")})
	deleteErr := c.DeleteUser(ctx, id)
	_, identitiesErr := c.GetUserIdentities(ctx, id)
	_, addErr := c.CreateUserIdentity(ctx, id, mgmt.CreateUserIdentityReq{
		Type:  mgmt.NewOptCreateUserIdentityReqType(mgmt.CreateUserIdentityReqTypeEmail),
		Value: mgmt.NewOptString("nobody@example.com"),
	})
	for name, err := range map[string]error{
		"GetUserData":        getErr,
		"UpdateUser":         updateErr,
		"DeleteUser":         deleteErr,
		"GetUserIdentities":  identitiesErr,
		"CreateUserIdentity": addErr,
	} {
		if !kindeapi.IsNotFound(err) {
			t.Errorf("%s: got %v, want not found", name, err)
		}
	}
}
