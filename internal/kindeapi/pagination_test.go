package kindeapi

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestAllPagesFollowsNextToken(t *testing.T) {
	pages := map[string]struct {
		items []int
		next  string
	}{
		"":  {[]int{1, 2}, "a"},
		"a": {[]int{3, 4}, "b"},
		"b": {[]int{5}, ""},
	}
	var tokens []string
	got, err := allPages(t.Context(), func(_ context.Context, nextToken string) ([]int, string, error) {
		tokens = append(tokens, nextToken)
		p := pages[nextToken]
		return p.items, p.next, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(got, want) {
		t.Fatalf("items = %v, want %v", got, want)
	}
	if want := []string{"", "a", "b"}; !slices.Equal(tokens, want) {
		t.Fatalf("tokens = %q, want %q", tokens, want)
	}
}

func TestAllPagesStopsWhenATokenRepeats(t *testing.T) {
	calls := 0
	_, err := allPages(t.Context(), func(context.Context, string) ([]int, string, error) {
		calls++
		return []int{calls}, "same", nil
	})
	if err == nil || !strings.Contains(err.Error(), "repeated") {
		t.Fatalf("expected a repeated-token error, got %v", err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestAllPagesReturnsFetchErrors(t *testing.T) {
	boom := errors.New("boom")
	_, err := allPages(t.Context(), func(_ context.Context, nextToken string) ([]int, string, error) {
		if nextToken == "" {
			return []int{1}, "a", nil
		}
		return nil, "", boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("got %v, want %v", err, boom)
	}
}
