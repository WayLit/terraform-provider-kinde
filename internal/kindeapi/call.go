package kindeapi

import (
	"context"
	"fmt"
)

// call runs one SDK operation and returns its result as the success type T.
// The SDK reports declared error statuses as typed results with a nil error
// and undeclared ones as errors without the body; both become *APIError,
// built from the response captured during the call. Transport and decoding
// failures are wrapped and returned as they are.
func call[T any](ctx context.Context, op string, fn func(context.Context) (any, error)) (T, error) {
	var zero T
	ctx, c := withCapture(ctx)
	res, err := fn(ctx)
	if c.status != 0 {
		return zero, newAPIError(op, c.status, c.body)
	}
	if err != nil {
		return zero, fmt.Errorf("kinde %s: %w", op, err)
	}
	v, ok := res.(T)
	if !ok {
		return zero, fmt.Errorf("kinde %s: unexpected response type %T", op, res)
	}
	return v, nil
}
