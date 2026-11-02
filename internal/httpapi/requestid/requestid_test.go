package requestid

import (
	"strings"
	"testing"
)

// BR-4.1: принимается ID до 64 символов [A-Za-z0-9-].
func TestValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		id   string
		want bool
	}{
		{"a", true},
		{"AZaz09-", true},
		{"01J9ZQ3X-4f2a-11ee-be56-0242ac120002", true},
		{strings.Repeat("x", 64), true},
		{"", false},
		{strings.Repeat("x", 65), false},
		{"a_b", false},
		{"a.b", false},
		{"a b", false},
		{"a\nb", false},
		{"a\x00b", false},
		{"ид", false},
	}

	for _, tt := range tests {
		if got := Valid(tt.id); got != tt.want {
			t.Errorf("Valid(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestContext(t *testing.T) {
	t.Parallel()

	if got := From(t.Context()); got != "" {
		t.Errorf("From(empty ctx) = %q, want empty", got)
	}
	if got := From(With(t.Context(), "abc")); got != "abc" {
		t.Errorf("From(With(abc)) = %q, want abc", got)
	}
}
