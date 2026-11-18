package kindeapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// APIError is an error response from the Kinde management API.
type APIError struct {
	// Op names the operation, such as "GetRole".
	Op         string
	StatusCode int
	// Code is Kinde's error code, such as "USER_NOT_IN_ORGANIZATION". It is
	// empty when the response body carried none.
	Code    string
	Message string
}

func (e *APIError) Error() string {
	msg := fmt.Sprintf("kinde %s: HTTP %d", e.Op, e.StatusCode)
	if e.Code != "" {
		msg += " " + e.Code
	}
	if e.Message != "" {
		msg += ": " + e.Message
	}
	return msg
}

// IsNotFound reports whether err is an *APIError for an object that does not
// exist. Kinde's spec documents no 400 error codes for missing IDs on the
// endpoints this provider calls, so only HTTP 404 counts.
func IsNotFound(err error) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound
}

// HasCode reports whether err is an *APIError with the given Kinde error
// code, ignoring case.
func HasCode(err error, code string) bool {
	var apiErr *APIError
	return errors.As(err, &apiErr) && strings.EqualFold(apiErr.Code, code)
}

// maxMessage caps how much of a non-JSON error body goes into Message.
const maxMessage = 500

type errorItem struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// newAPIError builds an *APIError from an error response body. Kinde uses
// three shapes: {"errors":[{...}]}, {"errors":{...}}, and a bare {...}.
func newAPIError(op string, status int, body []byte) *APIError {
	apiErr := &APIError{Op: op, StatusCode: status}

	var parsed struct {
		Errors  json.RawMessage `json:"errors"`
		Code    string          `json:"code"`
		Message string          `json:"message"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		msg := strings.TrimSpace(string(body))
		if len(msg) > maxMessage {
			msg = msg[:maxMessage]
		}
		apiErr.Message = msg
		return apiErr
	}

	item := errorItem{Code: parsed.Code, Message: parsed.Message}
	var list []errorItem
	var single errorItem
	if json.Unmarshal(parsed.Errors, &list) == nil && len(list) > 0 {
		item = list[0]
	} else if json.Unmarshal(parsed.Errors, &single) == nil && single != (errorItem{}) {
		item = single
	}
	apiErr.Code, apiErr.Message = item.Code, item.Message
	return apiErr
}
