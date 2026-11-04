package requestid

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
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
		t.Run(fmt.Sprintf("%.20q", tt.id), func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, Valid(tt.id), "Valid(%q)", tt.id)
		})
	}
}

func TestContext(t *testing.T) {
	t.Parallel()

	assert.Empty(t, From(t.Context()), "From(empty ctx)")
	assert.Equal(t, "abc", From(With(t.Context(), "abc")), "From(With(abc))")
}
