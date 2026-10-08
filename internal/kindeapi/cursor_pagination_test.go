package kindeapi

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestAllCursorPagesFollowsLastID(t *testing.T) {
	pages := map[string][]int{"": {1, 2}, "2": {3, 4}, "4": {5}}
	var afters []string
	got, err := allCursorPages(t.Context(), strconv.Itoa, func(_ context.Context, after string) ([]int, bool, error) {
		afters = append(afters, after)
		return pages[after], after != "4", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if want := []string{"", "2", "4"}; !slices.Equal(afters, want) {
		t.Fatalf("starting_after values = %q, want %q", afters, want)
	}
}

func TestAllCursorPagesRejectsEmptyPageWithMore(t *testing.T) {
	_, err := allCursorPages(t.Context(), strconv.Itoa, func(context.Context, string) ([]int, bool, error) {
		return nil, true, nil
	})
	if err == nil || !strings.Contains(err.Error(), "returned none") {
		t.Fatalf("got %v, want an error about an empty page", err)
	}
}

func TestAllCursorPagesRejectsRepeatedCursor(t *testing.T) {
	calls := 0
	_, err := allCursorPages(t.Context(), strconv.Itoa, func(context.Context, string) ([]int, bool, error) {
		calls++
		return []int{1, 2}, true, nil // ignores starting_after
	})
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("got %v, want a repeated-cursor error", err)
	}
	if calls != 2 {
		t.Fatalf("fetch calls = %d, want 2", calls)
	}
}
