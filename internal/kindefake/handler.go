package kindefake

import (
	"context"
	"net/http"

	mgmt "github.com/kinde-oss/kinde-go/kinde/management_api"
)

// handler implements the generated server. Operations the provider does not
// call fall through to UnimplementedHandler and answer HTTP 501.
type handler struct {
	mgmt.UnimplementedHandler
	f *Fake
}

// security accepts only the token the fake issued.
type security struct {
	f *Fake
}

func (s security) HandleKindeBearerAuth(ctx context.Context, _ mgmt.OperationName, t mgmt.KindeBearerAuth) (context.Context, error) {
	if t.Token != s.f.token {
		return ctx, &apiError{status: http.StatusUnauthorized, code: "UNAUTHORIZED", message: "kindefake: invalid access token"}
	}
	return ctx, nil
}
