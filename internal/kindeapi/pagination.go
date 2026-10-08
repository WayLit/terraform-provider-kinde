package kindeapi

import (
	"context"
	"fmt"
)

// maxPageSize is the page size list methods ask for. It is the largest Kinde
// documents: its PAGE_SIZE_LIMIT_EXCEEDED example reads "Page size cannot be
// greater than 100".
const maxPageSize = 100

// allPages collects every page of a next_token-paginated endpoint. fetch gets
// "" for the first page and returns the page's items and the next token, or
// "" on the last page.
func allPages[T any](ctx context.Context, fetch func(ctx context.Context, nextToken string) ([]T, string, error)) ([]T, error) {
	var all []T
	seen := map[string]bool{}
	nextToken := ""
	for {
		items, next, err := fetch(ctx, nextToken)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if next == "" {
			return all, nil
		}
		if seen[next] {
			return nil, fmt.Errorf("kinde: next_token %q repeated; stopping to avoid an endless loop", next)
		}
		seen[next] = true
		nextToken = next
	}
}
