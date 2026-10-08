package kindefake

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	ht "github.com/ogen-go/ogen/http"
	"github.com/ogen-go/ogen/ogenerrors"
)

// apiError makes the fake answer with a Kinde-shaped error response.
// Handlers return it as their error.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("%d %s: %s", e.status, e.code, e.message)
}

// writeError renders handler, security, and request-decoding errors.
func writeError(_ context.Context, w http.ResponseWriter, r *http.Request, err error) {
	var e *apiError
	var secErr *ogenerrors.SecurityError
	switch {
	case errors.As(err, &e):
	case errors.As(err, &secErr):
		e = &apiError{status: http.StatusUnauthorized, code: "UNAUTHORIZED", message: "kindefake: missing access token"}
	case errors.Is(err, ht.ErrNotImplemented):
		e = &apiError{status: http.StatusNotImplemented, code: "NOT_IMPLEMENTED", message: "kindefake: " + r.Method + " " + r.URL.Path + " is not implemented"}
	default:
		e = &apiError{status: http.StatusBadRequest, code: "INVALID_REQUEST", message: err.Error()}
	}
	writeAPIError(w, e)
}

// writeAPIError writes e in Kinde's {"errors":[{"code","message"}]} shape.
func writeAPIError(w http.ResponseWriter, e *apiError) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(e.status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"errors": []map[string]string{{"code": e.code, "message": e.message}},
	})
}

// notFound returns a 404 with Kinde error code code.
func notFound(code, message string) error {
	return &apiError{status: http.StatusNotFound, code: code, message: message}
}
