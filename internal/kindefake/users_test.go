package kindefake_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/nxt-fwd/terraform-provider-kinde/internal/kindefake"
)

// createUser posts a CreateUser body and returns the new user's ID.
func createUser(t *testing.T, f *kindefake.Fake, token, body string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, f.URL+"/api/v1/user", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil || created.ID == "" {
		t.Fatalf("CreateUser: status %d, decode error %v", resp.StatusCode, err)
	}
	return created.ID
}

// TestUserIdentitiesSendNullIsConfirmedForUsernames pins the response shape
// that kindeapi.GetUserIdentities decodes itself: Kinde's spec documents a
// null is_confirmed for username identities, which the SDK cannot decode.
func TestUserIdentitiesSendNullIsConfirmedForUsernames(t *testing.T) {
	f := kindefake.New(t)
	token := fetchToken(t, f)
	id := createUser(t, f, token, `{"identities":[
		{"type":"email","details":{"email":"ada@example.com"}},
		{"type":"username","details":{"username":"ada"}}
	]}`)

	resp := get(t, f, "/api/v1/users/"+id+"/identities", token)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var page struct {
		Identities []struct {
			Type        string          `json:"type"`
			IsConfirmed json.RawMessage `json:"is_confirmed"`
		} `json:"identities"`
		HasMore bool `json:"has_more"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, identity := range page.Identities {
		got[identity.Type] = string(identity.IsConfirmed)
	}
	if got["email"] != "true" || got["username"] != "null" || page.HasMore {
		t.Fatalf("is_confirmed by type = %v, has_more = %v; want email true, username null, no more pages", got, page.HasMore)
	}
}

func TestUserIdentitiesRequireToken(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/users/kp_0001/identities", "")
	if resp.StatusCode != http.StatusUnauthorized || errorCode(t, resp) != "UNAUTHORIZED" {
		t.Fatalf("status = %d, want 401 UNAUTHORIZED", resp.StatusCode)
	}
}

func TestUserIdentitiesOfMissingUser(t *testing.T) {
	f := kindefake.New(t)
	resp := get(t, f, "/api/v1/users/kp_missing/identities", fetchToken(t, f))
	if resp.StatusCode != http.StatusNotFound || errorCode(t, resp) != "USER_NOT_FOUND" {
		t.Fatalf("status = %d, want 404 USER_NOT_FOUND", resp.StatusCode)
	}
}
