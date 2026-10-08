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

// allCursorPages collects every page of a starting_after-paginated endpoint.
// fetch gets "" for the first page; the next page starts after the last
// item's ID.
func allCursorPages[T any](ctx context.Context, id func(T) string, fetch func(ctx context.Context, startingAfter string) (items []T, hasMore bool, err error)) ([]T, error) {
	var all []T
	seen := map[string]bool{}
	after := ""
	for {
		items, hasMore, err := fetch(ctx, after)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
		if !hasMore {
			return all, nil
		}
		if len(items) == 0 {
			return nil, fmt.Errorf("kinde: a page after %q reported more results but returned none", after)
		}
		after = id(items[len(items)-1])
		if seen[after] {
			return nil, fmt.Errorf("kinde: starting_after %q repeated", after)
		}
		seen[after] = true
	}
}
