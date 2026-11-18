package kindeapi

import (
	"errors"
	"fmt"
	"testing"
)

func TestNewAPIErrorParsesBodyShapes(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantCode    string
		wantMessage string
	}{
		{"list", `{"errors":[{"code":"ROLE_NOT_FOUND","message":"Role not found"}]}`, "ROLE_NOT_FOUND", "Role not found"},
		{"object", `{"errors":{"code":"ROUTE_NOT_FOUND","message":"Not found"}}`, "ROUTE_NOT_FOUND", "Not found"},
		{"bare", `{"code":"INVALID_CREDENTIALS","message":"Bad token"}`, "INVALID_CREDENTIALS", "Bad token"},
		{"html", "<html><body>502 Bad Gateway</body></html>\n", "", "<html><body>502 Bad Gateway</body></html>"},
		{"empty", "", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := newAPIError("GetRole", 502, []byte(tt.body))
			want := APIError{Op: "GetRole", StatusCode: 502, Code: tt.wantCode, Message: tt.wantMessage}
			if *got != want {
				t.Fatalf("got %+v, want %+v", *got, want)
			}
		})
	}
}

func TestNewAPIErrorTruncatesLongBodies(t *testing.T) {
	body := make([]byte, 2*maxMessage)
	for i := range body {
		body[i] = 'x'
	}
	if got := newAPIError("GetRole", 500, body); len(got.Message) != maxMessage {
		t.Fatalf("message length = %d, want %d", len(got.Message), maxMessage)
	}
}

func TestAPIErrorMessage(t *testing.T) {
	err := &APIError{Op: "GetRole", StatusCode: 404, Code: "ROLE_NOT_FOUND", Message: "Role not found"}
	if got, want := err.Error(), "kinde GetRole: HTTP 404 ROLE_NOT_FOUND: Role not found"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestIsNotFound(t *testing.T) {
	notFound := &APIError{Op: "GetRole", StatusCode: 404}
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"404", notFound, true},
		{"wrapped 404", fmt.Errorf("reading role: %w", notFound), true},
		{"400", &APIError{Op: "GetRole", StatusCode: 400}, false},
		{"plain error", errors.New("dial tcp: connection refused"), false},
		{"nil", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Fatalf("IsNotFound = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHasCodeIgnoresCase(t *testing.T) {
	err := fmt.Errorf("listing roles: %w", &APIError{StatusCode: 400, Code: "USER_NOT_IN_ORGANIZATION"})
	if !HasCode(err, "user_not_in_organization") {
		t.Fatal("expected HasCode to match regardless of case")
	}
	if HasCode(err, "ROLE_NOT_FOUND") {
		t.Fatal("expected HasCode not to match a different code")
	}
}
